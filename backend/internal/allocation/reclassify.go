package allocation

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/outbox"
)

func (s *Service) ReclassifyAllocation(
	ctx context.Context,
	actorID uuid.UUID,
	allocationID uuid.UUID,
	mode string,
	partnerID *uuid.UUID,
) error {
	if actorID == uuid.Nil ||
		allocationID == uuid.Nil {
		return validation(
			"actor and Allocation are required",
		)
	}

	mode = strings.ToUpper(
		strings.TrimSpace(mode),
	)

	switch mode {
	case "CHANNEL":
		if partnerID == nil ||
			*partnerID == uuid.Nil {
			return validation(
				"CHANNEL Allocation requires a Partner",
			)
		}

	case "NON_PUBLIC":
		if partnerID != nil {
			return validation(
				"NON_PUBLIC Allocation cannot identify a Partner",
			)
		}

	default:
		return validation(
			"Allocation mode must be CHANNEL or NON_PUBLIC",
		)
	}

	return s.transactions.Run(
		ctx,
		func(tx pgx.Tx) error {
			var eventID uuid.UUID

			if err := tx.QueryRow(
				ctx,
				`
					SELECT event_id
					FROM inventory_restrictions
					WHERE id = $1
					  AND kind = 'ALLOCATION'
				`,
				allocationID,
			).Scan(&eventID); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return notFound(
						"Allocation",
					)
				}

				return err
			}

			if err := lockRestrictionEvent(
				ctx,
				tx,
				eventID,
			); err != nil {
				return err
			}

			if mode == "CHANNEL" {
				if err := lockChannelPartner(
					ctx,
					tx,
					eventID,
					*partnerID,
				); err != nil {
					return err
				}
			}

			var (
				state          string
				currentMode    string
				currentPartner *uuid.UUID
			)

			if err := tx.QueryRow(
				ctx,
				`
					SELECT
						ir.state,
						a.mode,
						a.partner_id
					FROM inventory_restrictions ir
					JOIN allocations a
					  ON a.restriction_id = ir.id
					WHERE ir.id = $1
					FOR UPDATE OF ir, a
				`,
				allocationID,
			).Scan(
				&state,
				&currentMode,
				&currentPartner,
			); err != nil {
				return err
			}

			if state != "ACTIVE" {
				return validation(
					"only ACTIVE Allocations may be reclassified",
				)
			}

			if currentMode == mode &&
				sameUUIDPointer(
					currentPartner,
					partnerID,
				) {
				return nil
			}

			var (
				activeReservationObligation bool
				gaConsumed                  bool
				commercialHistory           bool
				issuanceHistory             bool
			)

			if err := tx.QueryRow(
				ctx,
				`
					SELECT
						EXISTS (
							SELECT 1
							FROM reservation_items ri
							JOIN reservations r
							  ON r.id = ri.reservation_id
							LEFT JOIN allocation_reserved_units aru
							  ON aru.id =
							     ri.source_allocation_reserved_unit_id
							LEFT JOIN ga_allocation_buckets gab
							  ON gab.id =
							     ri.source_ga_allocation_bucket_id
							WHERE ri.removed_at IS NULL
							  AND r.state IN (
							      'HELD',
							      'COMMITTING',
							      'PAYMENT_RETRY',
							      'RECONCILING'
							  )
							  AND (
							      aru.allocation_id = $1
							      OR gab.allocation_id = $1
							  )
						),
						EXISTS (
							SELECT 1
							FROM ga_allocation_buckets
							WHERE allocation_id = $1
							  AND (
							      active_reserved_quantity > 0
							      OR sold_current_quantity > 0
							      OR issued_current_quantity > 0
							  )
						),
						EXISTS (
							SELECT 1
							FROM sale_items
							WHERE source_allocation_id = $1
						),
						EXISTS (
							SELECT 1
							FROM non_public_issuances
							WHERE allocation_id = $1
						)
				`,
				allocationID,
			).Scan(
				&activeReservationObligation,
				&gaConsumed,
				&commercialHistory,
				&issuanceHistory,
			); err != nil {
				return err
			}

			if activeReservationObligation ||
				gaConsumed ||
				commercialHistory ||
				issuanceHistory {
				return validation(
					"Allocation with protected or historical consumption cannot be reclassified",
				)
			}

			if _, err := tx.Exec(
				ctx,
				`
					UPDATE allocations
					SET
						mode = $2,
						partner_id = $3
					WHERE restriction_id = $1
				`,
				allocationID,
				mode,
				partnerID,
			); err != nil {
				return err
			}

			if _, err := s.audit.Append(
				ctx,
				tx,
				audit.Event{
					EventID:     &eventID,
					PartnerID:   partnerID,
					ActorKind:   audit.ActorUser,
					ActorUserID: &actorID,
					Operation:   "ALLOCATION_RECLASSIFIED",
					EntityType:  "ALLOCATION",
					EntityID:    &allocationID,
					PreviousState: map[string]any{
						"mode":       currentMode,
						"partner_id": currentPartner,
					},
					NewState: map[string]any{
						"mode":       mode,
						"partner_id": partnerID,
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
					FactType:      "inventory.allocation_reclassified",
					AggregateType: "ALLOCATION",
					AggregateID:   &allocationID,
				},
			)

			return err
		},
	)
}

func sameUUIDPointer(
	left *uuid.UUID,
	right *uuid.UUID,
) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}

	return *left == *right
}
