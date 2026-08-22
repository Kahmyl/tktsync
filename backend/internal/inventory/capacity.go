package inventory

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/outbox"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
)

type CapacityService struct {
	transactions *database.Runner
	audit        audit.Store
	outbox       outbox.Store
}

func NewCapacityService(
	transactions *database.Runner,
) *CapacityService {
	return &CapacityService{
		transactions: transactions,
	}
}

func (s *CapacityService) SetGACapacity(
	ctx context.Context,
	actorID uuid.UUID,
	eventID uuid.UUID,
	poolID uuid.UUID,
	newCapacity int,
) error {
	if actorID == uuid.Nil ||
		eventID == uuid.Nil ||
		poolID == uuid.Nil {
		return apierror.New(
			apierror.CodeValidation,
			"actor, Event and GA pool are required",
		)
	}

	if newCapacity < 0 {
		return apierror.New(
			apierror.CodeValidation,
			"GA capacity cannot be negative",
		)
	}

	return s.transactions.Run(
		ctx,
		func(tx pgx.Tx) error {
			var eventState string

			if err := tx.QueryRow(
				ctx,
				`
					SELECT state
					FROM events
					WHERE id = $1
					FOR KEY SHARE
				`,
				eventID,
			).Scan(&eventState); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return apierror.New(
						apierror.CodeResourceNotFound,
						"Event not found",
					)
				}
				return err
			}

			switch eventState {
			case "DRAFT", "ON_SALE", "PAUSED":
			default:
				return apierror.New(
					apierror.CodeValidation,
					"Event state does not permit GA capacity mutation",
				)
			}

			var currentCapacity int

			if err := tx.QueryRow(
				ctx,
				`
					SELECT capacity
					FROM ga_inventory_pools
					WHERE id = $1
					  AND event_id = $2
					FOR UPDATE
				`,
				poolID,
				eventID,
			).Scan(&currentCapacity); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return apierror.New(
						apierror.CodeResourceNotFound,
						"GA pool not found",
					)
				}
				return err
			}

			var available int

			if err := tx.QueryRow(
				ctx,
				`
					SELECT available_quantity
					FROM ga_shared_inventory
					WHERE ga_pool_id = $1
					FOR UPDATE
				`,
				poolID,
			).Scan(&available); err != nil {
				return err
			}

			delta := newCapacity -
				currentCapacity

			if delta < 0 &&
				available < -delta {
				return apierror.New(
					apierror.CodeInventoryUnavailable,
					"GA capacity cannot be reduced below current obligations",
				)
			}

			if _, err := tx.Exec(
				ctx,
				`
					UPDATE ga_inventory_pools
					SET capacity = $2
					WHERE id = $1
				`,
				poolID,
				newCapacity,
			); err != nil {
				return err
			}

			if _, err := tx.Exec(
				ctx,
				`
					UPDATE ga_shared_inventory
					SET
						available_quantity =
							available_quantity + $2,
						updated_at =
							clock_timestamp()
					WHERE ga_pool_id = $1
				`,
				poolID,
				delta,
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
					Operation:   "GA_CAPACITY_CHANGED",
					EntityType:  "GA_INVENTORY_POOL",
					EntityID:    &poolID,
					PreviousState: map[string]any{
						"capacity": currentCapacity,
					},
					NewState: map[string]any{
						"capacity": newCapacity,
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
					FactType:      "inventory.ga_capacity_changed",
					AggregateType: "GA_INVENTORY_POOL",
					AggregateID:   &poolID,
					Payload: map[string]any{
						"previous_capacity": currentCapacity,
						"new_capacity":      newCapacity,
					},
				},
			)

			return err
		},
	)
}
