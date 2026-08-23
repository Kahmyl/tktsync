package event

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/outbox"
)

func (s *Service) ConfigureTransactionPolicy(
	ctx context.Context,
	actorID uuid.UUID,
	eventID uuid.UUID,
	input TransactionPolicyInput,
) error {
	if err := validatePolicy(input); err != nil {
		return err
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

		if state == "COMPLETED" || state == "CANCELLED" {
			return validation("terminal Event policy cannot be modified")
		}

		_, err := tx.Exec(
			ctx,
			`
				INSERT INTO event_transaction_policies (
					event_id,
					hold_duration_seconds,
					checkout_protection_seconds,
					payment_retry_seconds,
					reconciliation_seconds,
					max_reservation_lifetime_seconds,
					max_hold_quantity,
					max_active_reservations_per_partner,
					max_active_reservations_per_buyer_session,
					allow_voided_inventory_rerelease,
					created_at,
					updated_at
				)
				VALUES (
					$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,
					clock_timestamp(),
					clock_timestamp()
				)
				ON CONFLICT (event_id)
				DO UPDATE SET
					hold_duration_seconds = EXCLUDED.hold_duration_seconds,
					checkout_protection_seconds = EXCLUDED.checkout_protection_seconds,
					payment_retry_seconds = EXCLUDED.payment_retry_seconds,
					reconciliation_seconds = EXCLUDED.reconciliation_seconds,
					max_reservation_lifetime_seconds = EXCLUDED.max_reservation_lifetime_seconds,
					max_hold_quantity = EXCLUDED.max_hold_quantity,
					max_active_reservations_per_partner = EXCLUDED.max_active_reservations_per_partner,
					max_active_reservations_per_buyer_session = EXCLUDED.max_active_reservations_per_buyer_session,
					allow_voided_inventory_rerelease = EXCLUDED.allow_voided_inventory_rerelease,
					updated_at = clock_timestamp()
			`,
			eventID,
			input.HoldDurationSeconds,
			input.CheckoutProtectionSeconds,
			input.PaymentRetrySeconds,
			input.ReconciliationSeconds,
			input.MaxReservationLifetimeSeconds,
			input.MaxHoldQuantity,
			input.MaxActiveReservationsPerPartner,
			input.MaxActiveReservationsPerBuyerSession,
			input.AllowVoidedInventoryRerelease,
		)
		if err != nil {
			return err
		}

		if _, err := s.audit.Append(
			ctx,
			tx,
			audit.Event{
				EventID:     &eventID,
				ActorKind:   audit.ActorUser,
				ActorUserID: &actorID,
				Operation:   "EVENT_TRANSACTION_POLICY_CONFIGURED",
				EntityType:  "EVENT",
				EntityID:    &eventID,
			},
		); err != nil {
			return err
		}

		_, err = s.outbox.Append(
			ctx,
			tx,
			outbox.Fact{
				EventID:       &eventID,
				FactType:      "event.transaction_policy_configured",
				AggregateType: "EVENT",
				AggregateID:   &eventID,
			},
		)

		return err
	})
}

func (s *Service) CreatePriceTier(
	ctx context.Context,
	actorID uuid.UUID,
	eventID uuid.UUID,
	input PriceTierInput,
) (uuid.UUID, error) {
	input.Code = strings.TrimSpace(input.Code)
	input.Name = strings.TrimSpace(input.Name)
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))

	if input.Code == "" || input.Name == "" {
		return uuid.Nil, validation("price tier code and name are required")
	}

	if input.AmountMinor < 0 {
		return uuid.Nil, validation("price cannot be negative")
	}

	if !currencyPattern.MatchString(input.Currency) {
		return uuid.Nil, validation("currency must be a three-letter uppercase code")
	}

	id := uuid.New()

	err := s.transactions.Run(ctx, func(tx pgx.Tx) error {
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

		if state != "DRAFT" && state != "ON_SALE" && state != "PAUSED" {
			return validation("Event state does not permit pricing mutation")
		}

		_, err := tx.Exec(
			ctx,
			`
				INSERT INTO event_price_tiers (
					id,
					event_id,
					code,
					name,
					amount_minor,
					currency,
					state,
					created_at,
					updated_at
				)
				VALUES (
					$1,$2,$3,$4,$5,$6,'ACTIVE',
					clock_timestamp(),
					clock_timestamp()
				)
			`,
			id,
			eventID,
			input.Code,
			input.Name,
			input.AmountMinor,
			input.Currency,
		)
		if err != nil {
			return err
		}

		if _, err := s.audit.Append(
			ctx,
			tx,
			audit.Event{
				EventID:     &eventID,
				ActorKind:   audit.ActorUser,
				ActorUserID: &actorID,
				Operation:   "EVENT_PRICE_TIER_CREATED",
				EntityType:  "EVENT_PRICE_TIER",
				EntityID:    &id,
				NewState: map[string]any{
					"amount_minor": input.AmountMinor,
					"currency":     input.Currency,
				},
			},
		); err != nil {
			return err
		}

		_, err = s.outbox.Append(
			ctx,
			tx,
			outbox.Fact{
				EventID:       &eventID,
				FactType:      "event.pricing_changed",
				AggregateType: "EVENT",
				AggregateID:   &eventID,
			},
		)

		return err
	})

	if err != nil {
		return uuid.Nil, err
	}

	return id, nil
}

func (s *Service) AssignPricing(
	ctx context.Context,
	actorID uuid.UUID,
	eventID uuid.UUID,
	input PricingAssignmentInput,
) error {
	if input.PriceTierID == uuid.Nil {
		return validation("price tier is required")
	}

	sections := uniqueStrings(input.SectionObjectKeys)
	reserved := uniqueStrings(input.ReservedObjectKeys)
	gaPools := uniqueStrings(input.GAPoolObjectKeys)

	if len(sections)+len(reserved)+len(gaPools) == 0 {
		return validation("at least one pricing target is required")
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

		if state != "DRAFT" && state != "ON_SALE" && state != "PAUSED" {
			return validation("Event state does not permit pricing mutation")
		}

		var tierActive bool
		if err := tx.QueryRow(
			ctx,
			`
				SELECT EXISTS (
					SELECT 1
					FROM event_price_tiers
					WHERE id = $1
					  AND event_id = $2
					  AND state = 'ACTIVE'
				)
			`,
			input.PriceTierID,
			eventID,
		).Scan(&tierActive); err != nil {
			return err
		}

		if !tierActive {
			return validation("price tier is not active for this Event")
		}

		if len(sections) != 0 {
			result, err := tx.Exec(
				ctx,
				`
					UPDATE event_sections
					SET default_price_tier_id = $1
					WHERE event_id = $2
					  AND snapshot_object_key = ANY($3::text[])
				`,
				input.PriceTierID,
				eventID,
				sections,
			)
			if err != nil {
				return err
			}

			if result.RowsAffected() != int64(len(sections)) {
				return validation("one or more section pricing targets do not exist")
			}
		}

		if len(reserved) != 0 {
			result, err := tx.Exec(
				ctx,
				`
					UPDATE reserved_inventory_units
					SET price_tier_override_id = $1
					WHERE event_id = $2
					  AND snapshot_object_key = ANY($3::text[])
				`,
				input.PriceTierID,
				eventID,
				reserved,
			)
			if err != nil {
				return err
			}

			if result.RowsAffected() != int64(len(reserved)) {
				return validation("one or more reserved pricing targets do not exist")
			}
		}

		if len(gaPools) != 0 {
			result, err := tx.Exec(
				ctx,
				`
					UPDATE ga_inventory_pools
					SET price_tier_id = $1
					WHERE event_id = $2
					  AND snapshot_object_key = ANY($3::text[])
				`,
				input.PriceTierID,
				eventID,
				gaPools,
			)
			if err != nil {
				return err
			}

			if result.RowsAffected() != int64(len(gaPools)) {
				return validation("one or more GA pricing targets do not exist")
			}
		}

		if _, err := s.audit.Append(
			ctx,
			tx,
			audit.Event{
				EventID:     &eventID,
				ActorKind:   audit.ActorUser,
				ActorUserID: &actorID,
				Operation:   "EVENT_PRICING_ASSIGNED",
				EntityType:  "EVENT",
				EntityID:    &eventID,
				Metadata: map[string]any{
					"price_tier_id": input.PriceTierID.String(),
					"sections":      sections,
					"reserved":      reserved,
					"ga_pools":      gaPools,
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
				FactType:      "event.pricing_changed",
				AggregateType: "EVENT",
				AggregateID:   &eventID,
			},
		)

		return err
	})
}
