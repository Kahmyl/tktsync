package event

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/outbox"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

func (s *Service) OpenSales(
	ctx context.Context,
	actorID uuid.UUID,
	eventID uuid.UUID,
) error {
	if actorID == uuid.Nil || eventID == uuid.Nil {
		return validation("actor and Event are required")
	}

	return s.transactions.Run(ctx, func(tx pgx.Tx) error {
		var state string

		if err := tx.QueryRow(
			ctx,
			`SELECT state FROM events WHERE id = $1 FOR UPDATE`,
			eventID,
		).Scan(&state); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notFound("event")
			}
			return err
		}

		if state != "DRAFT" {
			return validation("OpenSales requires Event state DRAFT")
		}

		var snapshotReady bool
		if err := tx.QueryRow(
			ctx,
			`
				SELECT EXISTS (
					SELECT 1
					FROM event_layout_snapshots
					WHERE event_id = $1
					  AND finalized_at IS NOT NULL
				)
			`,
			eventID,
		).Scan(&snapshotReady); err != nil {
			return err
		}

		if !snapshotReady {
			return validation("OpenSales requires a finalized Event layout snapshot")
		}

		var inventoryCount int
		if err := tx.QueryRow(
			ctx,
			`
				SELECT
					(SELECT COUNT(*) FROM reserved_inventory_units WHERE event_id = $1)
					+
					(SELECT COUNT(*) FROM ga_inventory_pools WHERE event_id = $1)
			`,
			eventID,
		).Scan(&inventoryCount); err != nil {
			return err
		}

		if inventoryCount == 0 {
			return validation("OpenSales requires materialized inventory")
		}

		var policyReady bool
		if err := tx.QueryRow(
			ctx,
			`
				SELECT EXISTS (
					SELECT 1
					FROM event_transaction_policies
					WHERE event_id = $1
				)
			`,
			eventID,
		).Scan(&policyReady); err != nil {
			return err
		}

		if !policyReady {
			return validation("OpenSales requires transaction policy configuration")
		}

		var unpricedReserved int
		if err := tx.QueryRow(
			ctx,
			`
				SELECT COUNT(*)
				FROM reserved_inventory_units riu
				JOIN event_sections es
				  ON es.id = riu.event_section_id
				LEFT JOIN event_price_tiers pt
				  ON pt.id = COALESCE(
				      riu.price_tier_override_id,
				      es.default_price_tier_id
				  )
				 AND pt.event_id = riu.event_id
				WHERE riu.event_id = $1
				  AND (
				      pt.id IS NULL
				      OR pt.state <> 'ACTIVE'
				  )
			`,
			eventID,
		).Scan(&unpricedReserved); err != nil {
			return err
		}

		if unpricedReserved != 0 {
			return validation("all reserved inventory requires active pricing")
		}

		var unpricedGA int
		if err := tx.QueryRow(
			ctx,
			`
				SELECT COUNT(*)
				FROM ga_inventory_pools gp
				LEFT JOIN event_price_tiers pt
				  ON pt.id = gp.price_tier_id
				 AND pt.event_id = gp.event_id
				WHERE gp.event_id = $1
				  AND (
				      pt.id IS NULL
				      OR pt.state <> 'ACTIVE'
				  )
			`,
			eventID,
		).Scan(&unpricedGA); err != nil {
			return err
		}

		if unpricedGA != 0 {
			return validation("all GA inventory requires active pricing")
		}

		if _, err := tx.Exec(
			ctx,
			`
				UPDATE events
				SET
					state = 'ON_SALE',
					updated_at = clock_timestamp()
				WHERE id = $1
			`,
			eventID,
		); err != nil {
			return err
		}

		if _, err := s.audit.Append(
			ctx,
			tx,
			audit.Event{
				EventID:     &eventID,
				ActorKind:   audit.ActorUser,
				ActorUserID: &actorID,
				Operation:   "EVENT_SALES_OPENED",
				EntityType:  "EVENT",
				EntityID:    &eventID,
				PreviousState: map[string]any{
					"state": "DRAFT",
				},
				NewState: map[string]any{
					"state": "ON_SALE",
				},
			},
		); err != nil {
			return err
		}

		_, err := s.outbox.Append(
			ctx,
			tx,
			outbox.Fact{
				EventID:       &eventID,
				FactType:      "event.opened_for_sale",
				AggregateType: "EVENT",
				AggregateID:   &eventID,
				Payload: map[string]any{
					"event_id": eventID.String(),
				},
			},
		)

		return err
	})
}

func (s *Service) PauseSales(ctx context.Context, actorID, eventID uuid.UUID) error {
	return s.transitionLifecycle(ctx, actorID, eventID, []string{"ON_SALE"}, "PAUSED", "EVENT_SALES_PAUSED", "event.sales_paused", "")
}

func (s *Service) ResumeSales(ctx context.Context, actorID, eventID uuid.UUID) error {
	return s.transitionLifecycle(ctx, actorID, eventID, []string{"PAUSED"}, "ON_SALE", "EVENT_SALES_RESUMED", "event.sales_resumed", "")
}

func (s *Service) CloseSales(ctx context.Context, actorID, eventID uuid.UUID) error {
	return s.transitionLifecycle(ctx, actorID, eventID, []string{"ON_SALE", "PAUSED"}, "SALES_CLOSED", "EVENT_SALES_CLOSED", "event.sales_closed", "")
}

func (s *Service) CompleteEvent(ctx context.Context, actorID, eventID uuid.UUID) error {
	return s.transitionLifecycle(ctx, actorID, eventID, []string{"SALES_CLOSED"}, "COMPLETED", "EVENT_COMPLETED", "event.completed", "")
}

func (s *Service) CancelEvent(ctx context.Context, actorID, eventID uuid.UUID, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return validation("CancelEvent requires a reason")
	}
	return s.transitionLifecycle(ctx, actorID, eventID, []string{"DRAFT", "ON_SALE", "PAUSED", "SALES_CLOSED"}, "CANCELLED", "EVENT_CANCELLED", "event.cancelled", reason)
}

func (s *Service) transitionLifecycle(
	ctx context.Context,
	actorID uuid.UUID,
	eventID uuid.UUID,
	allowedStates []string,
	nextState string,
	operation string,
	factType string,
	reason string,
) error {
	if actorID == uuid.Nil || eventID == uuid.Nil {
		return validation("actor and Event are required")
	}

	return s.transactions.Run(ctx, func(tx pgx.Tx) error {
		var previousState string
		if err := tx.QueryRow(ctx, `SELECT state FROM events WHERE id = $1 FOR UPDATE`, eventID).Scan(&previousState); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notFound("event")
			}
			return err
		}

		allowed := false
		for _, state := range allowedStates {
			if previousState == state {
				allowed = true
				break
			}
		}
		if !allowed {
			return validation(fmt.Sprintf("cannot transition Event from %s to %s", previousState, nextState))
		}

		if _, err := tx.Exec(ctx, `
			UPDATE events
			SET state = $2,
				cancelled_at = CASE WHEN $2 = 'CANCELLED' THEN clock_timestamp() ELSE cancelled_at END,
				completed_at = CASE WHEN $2 = 'COMPLETED' THEN clock_timestamp() ELSE completed_at END,
				updated_at = clock_timestamp()
			WHERE id = $1
		`, eventID, nextState); err != nil {
			return err
		}

		metadata := map[string]any{}
		if reason != "" {
			metadata["reason"] = reason
		}
		if _, err := s.audit.Append(ctx, tx, audit.Event{
			EventID:       &eventID,
			ActorKind:     audit.ActorUser,
			ActorUserID:   &actorID,
			Operation:     operation,
			EntityType:    "EVENT",
			EntityID:      &eventID,
			PreviousState: map[string]any{"state": previousState},
			NewState:      map[string]any{"state": nextState},
			Reason:        reason,
			Metadata:      metadata,
		}); err != nil {
			return err
		}

		payload := map[string]any{"event_id": eventID.String(), "state": nextState}
		if reason != "" {
			payload["reason"] = reason
		}
		_, err := s.outbox.Append(ctx, tx, outbox.Fact{
			EventID:       &eventID,
			FactType:      factType,
			AggregateType: "EVENT",
			AggregateID:   &eventID,
			Payload:       payload,
		})
		return err
	})
}

func validatePolicy(input TransactionPolicyInput) error {
	if input.HoldDurationSeconds <= 0 {
		return validation("hold duration must be positive")
	}

	if input.CheckoutProtectionSeconds <= 0 {
		return validation("checkout protection duration must be positive")
	}

	if input.PaymentRetrySeconds < 0 {
		return validation("payment retry duration cannot be negative")
	}

	if input.ReconciliationSeconds <= 0 {
		return validation("reconciliation duration must be positive")
	}

	if input.MaxReservationLifetimeSeconds < input.HoldDurationSeconds {
		return validation("maximum Reservation lifetime cannot be shorter than hold duration")
	}

	if input.MaxHoldQuantity <= 0 ||
		input.MaxActiveReservationsPerPartner <= 0 ||
		input.MaxActiveReservationsPerBuyerSession <= 0 {
		return validation("Reservation limits must be positive")
	}

	return nil
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))

	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}

		if _, exists := seen[value]; exists {
			continue
		}

		seen[value] = struct{}{}
		out = append(out, value)
	}

	return out
}

func validation(message string) error {
	return apierror.New(apierror.CodeValidation, message)
}

func notFound(resource string) error {
	return apierror.New(
		apierror.CodeResourceNotFound,
		fmt.Sprintf("%s not found", resource),
	)
}
