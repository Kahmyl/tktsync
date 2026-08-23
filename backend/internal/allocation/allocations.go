package allocation

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/outbox"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

func (s *Service) CreateAllocation(
	ctx context.Context,
	actorID uuid.UUID,
	eventID uuid.UUID,
	input AllocationInput,
) (uuid.UUID, error) {
	if actorID == uuid.Nil ||
		eventID == uuid.Nil {
		return uuid.Nil, validation(
			"actor and Event are required",
		)
	}

	input.Mode = strings.ToUpper(
		strings.TrimSpace(input.Mode),
	)
	input.Purpose = normalizePurpose(
		input.Purpose,
	)
	input.ReleaseDestinationKind =
		strings.ToUpper(
			strings.TrimSpace(
				input.ReleaseDestinationKind,
			),
		)

	if input.ReleaseDestinationKind == "" {
		input.ReleaseDestinationKind = "SHARED"
	}

	if input.Mode != "CHANNEL" &&
		input.Mode != "NON_PUBLIC" {
		return uuid.Nil, validation(
			"Allocation mode must be CHANNEL or NON_PUBLIC",
		)
	}

	if input.Mode == "CHANNEL" &&
		(input.PartnerID == nil ||
			*input.PartnerID == uuid.Nil) {
		return uuid.Nil, validation(
			"CHANNEL Allocation requires a Partner",
		)
	}

	if input.Mode == "NON_PUBLIC" &&
		input.PartnerID != nil {
		return uuid.Nil, validation(
			"NON_PUBLIC Allocation cannot identify a Partner",
		)
	}

	if input.ReleaseDestinationKind != "SHARED" &&
		input.ReleaseDestinationKind != "ALLOCATION" {
		return uuid.Nil, validation(
			"release destination must be SHARED or ALLOCATION",
		)
	}

	if input.ReleaseDestinationKind ==
		"ALLOCATION" &&
		(input.ReleaseDestinationID == nil ||
			*input.ReleaseDestinationID ==
				uuid.Nil) {
		return uuid.Nil, validation(
			"ALLOCATION release destination requires an Allocation",
		)
	}

	if err := validateTargets(
		input.ReservedUnitIDs,
		input.GATargets,
	); err != nil {
		return uuid.Nil, err
	}

	allocationID := uuid.New()

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

			if input.Mode == "CHANNEL" {
				if err := lockChannelPartner(
					ctx,
					tx,
					eventID,
					*input.PartnerID,
				); err != nil {
					return err
				}
			}

			if input.ReleaseDestinationKind ==
				"ALLOCATION" {
				if err := validateDestinationAllocation(
					ctx,
					tx,
					eventID,
					*input.ReleaseDestinationID,
				); err != nil {
					return err
				}
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
						$1,$2,'ALLOCATION','ACTIVE',
						$3,NULLIF($4,''),
						$5,clock_timestamp()
					)
				`,
				allocationID,
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
					INSERT INTO allocations (
						restriction_id,
						mode,
						partner_id,
						release_destination_kind,
						release_destination_allocation_id
					)
					VALUES (
						$1,$2,$3,$4,$5
					)
				`,
				allocationID,
				input.Mode,
				input.PartnerID,
				input.ReleaseDestinationKind,
				input.ReleaseDestinationID,
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
						INSERT INTO allocation_reserved_units (
							id,
							allocation_id,
							reserved_inventory_unit_id,
							assigned_at
						)
						VALUES (
							$1,$2,$3,clock_timestamp()
						)
					`,
					membershipID,
					allocationID,
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
							allocation_reserved_unit_id,
							activated_at
						)
						VALUES (
							gen_random_uuid(),
							$1,
							'ALLOCATION',
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
				result, err := tx.Exec(
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
				)
				if err != nil {
					return err
				}

				if result.RowsAffected() != 1 {
					return apierror.New(
						apierror.CodeInsufficientGAQuantity,
						"insufficient GA quantity",
					)
				}

				if _, err := tx.Exec(
					ctx,
					`
						INSERT INTO ga_allocation_buckets (
							id,
							allocation_id,
							ga_pool_id,
							assigned_quantity,
							available_quantity,
							active_reserved_quantity,
							sold_current_quantity,
							issued_current_quantity,
							released_quantity,
							created_at,
							updated_at
						)
						VALUES (
							gen_random_uuid(),
							$1,$2,$3,$3,0,0,0,0,
							clock_timestamp(),
							clock_timestamp()
						)
					`,
					allocationID,
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
					PartnerID:   input.PartnerID,
					ActorKind:   audit.ActorUser,
					ActorUserID: &actorID,
					Operation:   "INVENTORY_ALLOCATED",
					EntityType:  "ALLOCATION",
					EntityID:    &allocationID,
					Metadata: map[string]any{
						"mode":           input.Mode,
						"purpose":        input.Purpose,
						"reserved_units": len(reservedIDs),
						"ga_targets":     len(gaTargets),
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
					FactType:      "inventory.allocated",
					AggregateType: "ALLOCATION",
					AggregateID:   &allocationID,
				},
			)

			return err
		},
	)

	if err != nil {
		return uuid.Nil, err
	}

	return allocationID, nil
}

func (s *Service) ReleaseAllocation(
	ctx context.Context,
	actorID uuid.UUID,
	allocationID uuid.UUID,
) error {
	if actorID == uuid.Nil ||
		allocationID == uuid.Nil {
		return validation(
			"actor and Allocation are required",
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

			var (
				state           string
				destinationKind string
				destinationID   *uuid.UUID
			)

			if err := tx.QueryRow(
				ctx,
				`
					SELECT
						ir.state,
						a.release_destination_kind,
						a.release_destination_allocation_id
					FROM inventory_restrictions ir
					JOIN allocations a
					  ON a.restriction_id = ir.id
					WHERE ir.id = $1
					FOR UPDATE OF ir, a
				`,
				allocationID,
			).Scan(
				&state,
				&destinationKind,
				&destinationID,
			); err != nil {
				return err
			}

			if state == "RELEASED" {
				return nil
			}

			if state != "ACTIVE" {
				return validation(
					"Allocation is not releasable",
				)
			}

			if destinationKind ==
				"ALLOCATION" {
				if destinationID == nil {
					return validation(
						"Allocation release destination is invalid",
					)
				}

				if err := validateDestinationAllocation(
					ctx,
					tx,
					eventID,
					*destinationID,
				); err != nil {
					return err
				}
			}

			reservedIDs, err :=
				allocationReservedIDs(
					ctx,
					tx,
					allocationID,
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
				allocationAvailableGATargets(
					ctx,
					tx,
					allocationID,
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

			rows, err := tx.Query(
				ctx,
				`
					SELECT
						aru.id,
						aru.reserved_inventory_unit_id,
						EXISTS (
							SELECT 1
							FROM reserved_inventory_claims ric
							WHERE ric.allocation_reserved_unit_id =
							      aru.id
							  AND ric.claim_type =
							      'ALLOCATION'
							  AND ric.ended_at IS NULL
						)
					FROM allocation_reserved_units aru
					WHERE aru.allocation_id = $1
					ORDER BY aru.reserved_inventory_unit_id
				`,
				allocationID,
			)
			if err != nil {
				return err
			}

			type reservedRelease struct {
				membershipID uuid.UUID
				unitID       uuid.UUID
				current      bool
			}

			releases := make(
				[]reservedRelease,
				0,
			)

			for rows.Next() {
				var item reservedRelease

				if err := rows.Scan(
					&item.membershipID,
					&item.unitID,
					&item.current,
				); err != nil {
					rows.Close()
					return err
				}

				releases = append(
					releases,
					item,
				)
			}

			if err := rows.Err(); err != nil {
				rows.Close()
				return err
			}

			rows.Close()

			for _, item := range releases {
				if !item.current {
					continue
				}

				if _, err := tx.Exec(
					ctx,
					`
						UPDATE reserved_inventory_claims
						SET
							ended_at =
								clock_timestamp(),
							end_reason =
								'ALLOCATION_RELEASED'
						WHERE allocation_reserved_unit_id =
						      $1
						  AND claim_type =
						      'ALLOCATION'
						  AND ended_at IS NULL
					`,
					item.membershipID,
				); err != nil {
					return err
				}

				if destinationKind ==
					"ALLOCATION" {
					destinationMembershipID,
						err :=
						upsertAllocationMembership(
							ctx,
							tx,
							*destinationID,
							item.unitID,
						)
					if err != nil {
						return err
					}

					if _, err := tx.Exec(
						ctx,
						`
							INSERT INTO reserved_inventory_claims (
								id,
								reserved_inventory_unit_id,
								claim_type,
								allocation_reserved_unit_id,
								activated_at
							)
							VALUES (
								gen_random_uuid(),
								$1,
								'ALLOCATION',
								$2,
								clock_timestamp()
							)
						`,
						item.unitID,
						destinationMembershipID,
					); err != nil {
						return err
					}
				}
			}

			if _, err := tx.Exec(
				ctx,
				`
					UPDATE allocation_reserved_units
					SET released_at =
						COALESCE(
							released_at,
							clock_timestamp()
						)
					WHERE allocation_id = $1
				`,
				allocationID,
			); err != nil {
				return err
			}

			for _, target := range gaTargets {
				if target.Quantity == 0 {
					continue
				}

				if destinationKind ==
					"SHARED" {
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
				} else {
					if err := moveGAAvailableToAllocation(
						ctx,
						tx,
						*destinationID,
						target,
					); err != nil {
						return err
					}
				}

				if _, err := tx.Exec(
					ctx,
					`
						UPDATE ga_allocation_buckets
						SET
							available_quantity = 0,
							released_quantity =
								released_quantity + $3,
							updated_at =
								clock_timestamp()
						WHERE allocation_id = $1
						  AND ga_pool_id = $2
					`,
					allocationID,
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
				allocationID,
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
					Operation:   "ALLOCATION_RELEASED",
					EntityType:  "ALLOCATION",
					EntityID:    &allocationID,
				},
			); err != nil {
				return err
			}

			_, err = s.outbox.Append(
				ctx,
				tx,
				outbox.Fact{
					EventID:       &eventID,
					FactType:      "inventory.allocation_released",
					AggregateType: "ALLOCATION",
					AggregateID:   &allocationID,
				},
			)

			return err
		},
	)
}
