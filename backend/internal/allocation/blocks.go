package allocation

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/outbox"
)

func (s *Service) CreateBlock(
	ctx context.Context,
	actorID uuid.UUID,
	eventID uuid.UUID,
	input BlockInput,
) (uuid.UUID, error) {
	if actorID == uuid.Nil ||
		eventID == uuid.Nil {
		return uuid.Nil, validation(
			"actor and Event are required",
		)
	}

	input.Purpose = normalizePurpose(
		input.Purpose,
	)

	if err := validateTargets(
		input.ReservedUnitIDs,
		input.GATargets,
	); err != nil {
		return uuid.Nil, err
	}

	restrictionID := uuid.New()

	err := s.transactions.Run(
		ctx,
		func(tx pgx.Tx) error {
			if err := lockRestrictionEvent(
				ctx,
				tx,
				eventID,
			); err != nil {
				return err
			}

			if _, err := tx.Exec(
				ctx,
				`
					INSERT INTO inventory_restrictions (
						id,
						event_id,
						kind,
						state,
						purpose,
						reason,
						created_by_user_id,
						created_at
					)
					VALUES (
						$1,$2,'BLOCK','ACTIVE',$3,
						NULLIF($4,''),
						$5,
						clock_timestamp()
					)
				`,
				restrictionID,
				eventID,
				input.Purpose,
				strings.TrimSpace(input.Reason),
				actorID,
			); err != nil {
				return err
			}

			if _, err := tx.Exec(
				ctx,
				`
					INSERT INTO blocks (
						restriction_id
					)
					VALUES ($1)
				`,
				restrictionID,
			); err != nil {
				return err
			}

			reservedIDs := uniqueUUIDs(
				input.ReservedUnitIDs,
			)

			if err := lockAvailableReserved(
				ctx,
				tx,
				eventID,
				reservedIDs,
			); err != nil {
				return err
			}

			gaTargets := normalizeGATargets(
				input.GATargets,
			)

			if err := lockSharedGA(
				ctx,
				tx,
				eventID,
				gaTargets,
			); err != nil {
				return err
			}

			for _, unitID := range reservedIDs {
				membershipID := uuid.New()

				if _, err := tx.Exec(
					ctx,
					`
						INSERT INTO block_reserved_units (
							id,
							block_id,
							reserved_inventory_unit_id,
							assigned_at
						)
						VALUES (
							$1,$2,$3,clock_timestamp()
						)
					`,
					membershipID,
					restrictionID,
					unitID,
				); err != nil {
					return err
				}

				if _, err := tx.Exec(
					ctx,
					`
						INSERT INTO reserved_inventory_claims (
							id,
							reserved_inventory_unit_id,
							claim_type,
							block_reserved_unit_id,
							activated_at
						)
						VALUES (
							gen_random_uuid(),
							$1,
							'BLOCK',
							$2,
							clock_timestamp()
						)
					`,
					unitID,
					membershipID,
				); err != nil {
					return inventoryConflict(err)
				}
			}

			for _, target := range gaTargets {
				if _, err := tx.Exec(
					ctx,
					`
						UPDATE ga_shared_inventory
						SET
							available_quantity =
								available_quantity - $2,
							updated_at =
								clock_timestamp()
						WHERE ga_pool_id = $1
						  AND available_quantity >= $2
					`,
					target.PoolID,
					target.Quantity,
				); err != nil {
					return err
				}

				if _, err := tx.Exec(
					ctx,
					`
						INSERT INTO ga_block_buckets (
							id,
							block_id,
							ga_pool_id,
							assigned_quantity,
							blocked_quantity,
							released_quantity,
							created_at,
							updated_at
						)
						VALUES (
							gen_random_uuid(),
							$1,
							$2,
							$3,
							$3,
							0,
							clock_timestamp(),
							clock_timestamp()
						)
					`,
					restrictionID,
					target.PoolID,
					target.Quantity,
				); err != nil {
					return err
				}
			}

			if _, err := s.audit.Append(
				ctx,
				tx,
				audit.Event{
					EventID:     &eventID,
					ActorKind:   audit.ActorUser,
					ActorUserID: &actorID,
					Operation:   "INVENTORY_BLOCKED",
					EntityType:  "BLOCK",
					EntityID:    &restrictionID,
					Metadata: map[string]any{
						"reserved_units": len(reservedIDs),
						"ga_targets":     len(gaTargets),
						"purpose":        input.Purpose,
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
					FactType:      "inventory.blocked",
					AggregateType: "BLOCK",
					AggregateID:   &restrictionID,
				},
			)

			return err
		},
	)

	if err != nil {
		return uuid.Nil, err
	}

	return restrictionID, nil
}

func (s *Service) ReleaseBlock(
	ctx context.Context,
	actorID uuid.UUID,
	blockID uuid.UUID,
) error {
	if actorID == uuid.Nil ||
		blockID == uuid.Nil {
		return validation(
			"actor and Block are required",
		)
	}

	return s.transactions.Run(
		ctx,
		func(tx pgx.Tx) error {
			eventID, err := restrictionEventID(
				ctx,
				tx,
				blockID,
				"BLOCK",
			)
			if err != nil {
				return err
			}

			if err := lockRestrictionEvent(
				ctx,
				tx,
				eventID,
			); err != nil {
				return err
			}

			var state string

			if err := tx.QueryRow(
				ctx,
				`
					SELECT state
					FROM inventory_restrictions
					WHERE id = $1
					  AND kind = 'BLOCK'
					FOR UPDATE
				`,
				blockID,
			).Scan(&state); err != nil {
				return err
			}

			if state == "RELEASED" {
				return nil
			}

			if state != "ACTIVE" {
				return validation(
					"Block is not releasable",
				)
			}

			reservedIDs, err :=
				blockReservedIDs(
					ctx,
					tx,
					blockID,
				)
			if err != nil {
				return err
			}

			if err := lockReservedRows(
				ctx,
				tx,
				eventID,
				reservedIDs,
			); err != nil {
				return err
			}

			gaTargets, err :=
				blockGATargets(
					ctx,
					tx,
					blockID,
				)
			if err != nil {
				return err
			}

			if err := lockGAPools(
				ctx,
				tx,
				eventID,
				gaTargets,
			); err != nil {
				return err
			}

			if _, err := tx.Exec(
				ctx,
				`
					UPDATE reserved_inventory_claims ric
					SET
						ended_at = clock_timestamp(),
						end_reason = 'BLOCK_RELEASED'
					FROM block_reserved_units bru
					WHERE bru.id =
					      ric.block_reserved_unit_id
					  AND bru.block_id = $1
					  AND ric.claim_type = 'BLOCK'
					  AND ric.ended_at IS NULL
				`,
				blockID,
			); err != nil {
				return err
			}

			if _, err := tx.Exec(
				ctx,
				`
					UPDATE block_reserved_units
					SET released_at =
						COALESCE(
							released_at,
							clock_timestamp()
						)
					WHERE block_id = $1
				`,
				blockID,
			); err != nil {
				return err
			}

			for _, target := range gaTargets {
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
					target.PoolID,
					target.Quantity,
				); err != nil {
					return err
				}

				if _, err := tx.Exec(
					ctx,
					`
						UPDATE ga_block_buckets
						SET
							blocked_quantity = 0,
							released_quantity =
								released_quantity + $3,
							updated_at =
								clock_timestamp()
						WHERE block_id = $1
						  AND ga_pool_id = $2
					`,
					blockID,
					target.PoolID,
					target.Quantity,
				); err != nil {
					return err
				}
			}

			if _, err := tx.Exec(
				ctx,
				`
					UPDATE inventory_restrictions
					SET
						state = 'RELEASED',
						released_at =
							clock_timestamp()
					WHERE id = $1
				`,
				blockID,
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
					Operation:   "BLOCK_RELEASED",
					EntityType:  "BLOCK",
					EntityID:    &blockID,
				},
			); err != nil {
				return err
			}

			_, err = s.outbox.Append(
				ctx,
				tx,
				outbox.Fact{
					EventID:       &eventID,
					FactType:      "inventory.block_released",
					AggregateType: "BLOCK",
					AggregateID:   &blockID,
				},
			)

			return err
		},
	)
}
