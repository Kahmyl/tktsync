package allocation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/outbox"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
)

type Service struct {
	transactions *database.Runner
	audit        audit.Store
	outbox       outbox.Store
	qrKeys       *auth.HMACKeyring
}

func NewService(
	transactions *database.Runner,
	qrKeys ...*auth.HMACKeyring,
) *Service {
	var qrKeyring *auth.HMACKeyring

	if len(qrKeys) > 0 {
		qrKeyring = qrKeys[0]
	}

	return &Service{
		transactions: transactions,
		qrKeys:       qrKeyring,
	}
}

type GATarget struct {
	PoolID   uuid.UUID
	Quantity int
}

type BlockInput struct {
	Purpose         string
	Reason          string
	ReservedUnitIDs []uuid.UUID
	GATargets       []GATarget
}

type AllocationInput struct {
	Mode                   string
	PartnerID              *uuid.UUID
	Purpose                string
	Reason                 string
	ReleaseDestinationKind string
	ReleaseDestinationID   *uuid.UUID
	ReservedUnitIDs        []uuid.UUID
	GATargets              []GATarget
}

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

func lockRestrictionEvent(
	ctx context.Context,
	tx pgx.Tx,
	eventID uuid.UUID,
) error {
	var state string

	if err := tx.QueryRow(
		ctx,
		`
			SELECT state
			FROM events
			WHERE id = $1
			FOR KEY SHARE
		`,
		eventID,
	).Scan(&state); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound("Event")
		}
		return err
	}

	switch state {
	case "DRAFT", "ON_SALE", "PAUSED":
		return nil
	default:
		return validation(
			"Event state does not permit inventory restriction changes",
		)
	}
}

func lockAvailableReserved(
	ctx context.Context,
	tx pgx.Tx,
	eventID uuid.UUID,
	ids []uuid.UUID,
) error {
	if err := lockReservedRows(
		ctx,
		tx,
		eventID,
		ids,
	); err != nil {
		return err
	}

	if len(ids) == 0 {
		return nil
	}

	var conflicts int

	if err := tx.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM reserved_inventory_claims
			WHERE reserved_inventory_unit_id =
			      ANY($1::uuid[])
			  AND ended_at IS NULL
		`,
		ids,
	).Scan(&conflicts); err != nil {
		return err
	}

	if conflicts != 0 {
		return apierror.New(
			apierror.CodeInventoryUnavailable,
			"one or more reserved inventory units are unavailable",
		)
	}

	return nil
}

func lockReservedRows(
	ctx context.Context,
	tx pgx.Tx,
	eventID uuid.UUID,
	ids []uuid.UUID,
) error {
	if len(ids) == 0 {
		return nil
	}

	ids = database.SortUUIDs(ids)

	rows, err := tx.Query(
		ctx,
		`
			SELECT id
			FROM reserved_inventory_units
			WHERE event_id = $1
			  AND id = ANY($2::uuid[])
			ORDER BY id
			FOR UPDATE
		`,
		eventID,
		ids,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	count := 0

	for rows.Next() {
		var id uuid.UUID

		if err := rows.Scan(&id); err != nil {
			return err
		}

		count++
	}

	if err := rows.Err(); err != nil {
		return err
	}

	if count != len(ids) {
		return notFound(
			"reserved inventory unit",
		)
	}

	return nil
}

func lockSharedGA(
	ctx context.Context,
	tx pgx.Tx,
	eventID uuid.UUID,
	targets []GATarget,
) error {
	if err := lockGAPools(
		ctx,
		tx,
		eventID,
		targets,
	); err != nil {
		return err
	}

	for _, target := range targets {
		var available int

		if err := tx.QueryRow(
			ctx,
			`
				SELECT available_quantity
				FROM ga_shared_inventory
				WHERE ga_pool_id = $1
				FOR UPDATE
			`,
			target.PoolID,
		).Scan(&available); err != nil {
			return err
		}

		if available < target.Quantity {
			return apierror.New(
				apierror.CodeInsufficientGAQuantity,
				"insufficient GA quantity",
			)
		}
	}

	return nil
}

func lockGAPools(
	ctx context.Context,
	tx pgx.Tx,
	eventID uuid.UUID,
	targets []GATarget,
) error {
	if len(targets) == 0 {
		return nil
	}

	ids := make(
		[]uuid.UUID,
		0,
		len(targets),
	)

	for _, target := range targets {
		ids = append(
			ids,
			target.PoolID,
		)
	}

	ids = database.SortUUIDs(
		uniqueUUIDs(ids),
	)

	rows, err := tx.Query(
		ctx,
		`
			SELECT id
			FROM ga_inventory_pools
			WHERE event_id = $1
			  AND id = ANY($2::uuid[])
			ORDER BY id
			FOR UPDATE
		`,
		eventID,
		ids,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	count := 0

	for rows.Next() {
		var id uuid.UUID

		if err := rows.Scan(&id); err != nil {
			return err
		}

		count++
	}

	if err := rows.Err(); err != nil {
		return err
	}

	if count != len(ids) {
		return notFound("GA pool")
	}

	return nil
}

func lockChannelPartner(
	ctx context.Context,
	tx pgx.Tx,
	eventID uuid.UUID,
	partnerID uuid.UUID,
) error {
	var partnerState string

	if err := tx.QueryRow(
		ctx,
		`
			SELECT state
			FROM partners
			WHERE id = $1
			FOR KEY SHARE
		`,
		partnerID,
	).Scan(&partnerState); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound("Partner")
		}
		return err
	}

	if partnerState != "ACTIVE" {
		return apierror.New(
			apierror.CodePartnerDisabled,
			"Partner is disabled",
		)
	}

	var accessState string

	if err := tx.QueryRow(
		ctx,
		`
			SELECT state
			FROM partner_event_access
			WHERE partner_id = $1
			  AND event_id = $2
			FOR KEY SHARE
		`,
		partnerID,
		eventID,
	).Scan(&accessState); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apierror.New(
				apierror.CodeNotAuthorized,
				"Partner is not authorized for this Event",
			)
		}
		return err
	}

	if accessState != "ACTIVE" {
		return apierror.New(
			apierror.CodePartnerEventAccessDisabled,
			"Partner Event access is disabled",
		)
	}

	return nil
}

func validateDestinationAllocation(
	ctx context.Context,
	tx pgx.Tx,
	eventID uuid.UUID,
	allocationID uuid.UUID,
) error {
	var state string

	if err := tx.QueryRow(
		ctx,
		`
			SELECT ir.state
			FROM inventory_restrictions ir
			JOIN allocations a
			  ON a.restriction_id = ir.id
			WHERE ir.id = $1
			  AND ir.event_id = $2
			  AND ir.kind = 'ALLOCATION'
			FOR KEY SHARE OF ir, a
		`,
		allocationID,
		eventID,
	).Scan(&state); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return validation(
				"release destination Allocation is invalid",
			)
		}
		return err
	}

	if state != "ACTIVE" {
		return validation(
			"release destination Allocation must be ACTIVE",
		)
	}

	return nil
}

func restrictionEventID(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
	kind string,
) (uuid.UUID, error) {
	var eventID uuid.UUID

	if err := tx.QueryRow(
		ctx,
		`
			SELECT event_id
			FROM inventory_restrictions
			WHERE id = $1
			  AND kind = $2
		`,
		id,
		kind,
	).Scan(&eventID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, notFound(kind)
		}
		return uuid.Nil, err
	}

	return eventID, nil
}

func blockReservedIDs(
	ctx context.Context,
	tx pgx.Tx,
	blockID uuid.UUID,
) ([]uuid.UUID, error) {
	return membershipReservedIDs(
		ctx,
		tx,
		`
			SELECT reserved_inventory_unit_id
			FROM block_reserved_units
			WHERE block_id = $1
			ORDER BY reserved_inventory_unit_id
		`,
		blockID,
	)
}

func allocationReservedIDs(
	ctx context.Context,
	tx pgx.Tx,
	allocationID uuid.UUID,
) ([]uuid.UUID, error) {
	return membershipReservedIDs(
		ctx,
		tx,
		`
			SELECT reserved_inventory_unit_id
			FROM allocation_reserved_units
			WHERE allocation_id = $1
			ORDER BY reserved_inventory_unit_id
		`,
		allocationID,
	)
}

func membershipReservedIDs(
	ctx context.Context,
	tx pgx.Tx,
	statement string,
	id uuid.UUID,
) ([]uuid.UUID, error) {
	rows, err := tx.Query(
		ctx,
		statement,
		id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(
		[]uuid.UUID,
		0,
	)

	for rows.Next() {
		var unitID uuid.UUID

		if err := rows.Scan(
			&unitID,
		); err != nil {
			return nil, err
		}

		result = append(
			result,
			unitID,
		)
	}

	return result, rows.Err()
}

func blockGATargets(
	ctx context.Context,
	tx pgx.Tx,
	blockID uuid.UUID,
) ([]GATarget, error) {
	rows, err := tx.Query(
		ctx,
		`
			SELECT
				ga_pool_id,
				blocked_quantity
			FROM ga_block_buckets
			WHERE block_id = $1
			  AND blocked_quantity > 0
			ORDER BY ga_pool_id
		`,
		blockID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanGATargets(rows)
}

func allocationAvailableGATargets(
	ctx context.Context,
	tx pgx.Tx,
	allocationID uuid.UUID,
) ([]GATarget, error) {
	rows, err := tx.Query(
		ctx,
		`
			SELECT
				ga_pool_id,
				available_quantity
			FROM ga_allocation_buckets
			WHERE allocation_id = $1
			  AND available_quantity > 0
			ORDER BY ga_pool_id
		`,
		allocationID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanGATargets(rows)
}

func scanGATargets(
	rows pgx.Rows,
) ([]GATarget, error) {
	result := make(
		[]GATarget,
		0,
	)

	for rows.Next() {
		var target GATarget

		if err := rows.Scan(
			&target.PoolID,
			&target.Quantity,
		); err != nil {
			return nil, err
		}

		result = append(
			result,
			target,
		)
	}

	return result, rows.Err()
}

func upsertAllocationMembership(
	ctx context.Context,
	tx pgx.Tx,
	allocationID uuid.UUID,
	unitID uuid.UUID,
) (uuid.UUID, error) {
	var id uuid.UUID

	err := tx.QueryRow(
		ctx,
		`
			INSERT INTO allocation_reserved_units (
				id,
				allocation_id,
				reserved_inventory_unit_id,
				assigned_at,
				released_at
			)
			VALUES (
				gen_random_uuid(),
				$1,$2,
				clock_timestamp(),
				NULL
			)
			ON CONFLICT (
				allocation_id,
				reserved_inventory_unit_id
			)
			DO UPDATE SET
				released_at = NULL
			RETURNING id
		`,
		allocationID,
		unitID,
	).Scan(&id)

	return id, err
}

func moveGAAvailableToAllocation(
	ctx context.Context,
	tx pgx.Tx,
	destinationID uuid.UUID,
	target GATarget,
) error {
	_, err := tx.Exec(
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
			ON CONFLICT (
				allocation_id,
				ga_pool_id
			)
			DO UPDATE SET
				assigned_quantity =
					ga_allocation_buckets.
						assigned_quantity +
					EXCLUDED.assigned_quantity,
				available_quantity =
					ga_allocation_buckets.
						available_quantity +
					EXCLUDED.available_quantity,
				updated_at =
					clock_timestamp()
		`,
		destinationID,
		target.PoolID,
		target.Quantity,
	)

	return err
}

func validateTargets(
	reserved []uuid.UUID,
	ga []GATarget,
) error {
	if len(reserved) == 0 &&
		len(ga) == 0 {
		return validation(
			"at least one inventory target is required",
		)
	}

	for _, id := range reserved {
		if id == uuid.Nil {
			return validation(
				"reserved inventory target is invalid",
			)
		}
	}

	for _, target := range ga {
		if target.PoolID == uuid.Nil ||
			target.Quantity <= 0 {
			return validation(
				"GA targets require a valid pool and positive quantity",
			)
		}
	}

	return nil
}

func normalizeGATargets(
	values []GATarget,
) []GATarget {
	quantities := map[uuid.UUID]int{}

	for _, value := range values {
		quantities[value.PoolID] +=
			value.Quantity
	}

	ids := make(
		[]uuid.UUID,
		0,
		len(quantities),
	)

	for id := range quantities {
		ids = append(ids, id)
	}

	sort.Slice(
		ids,
		func(i, j int) bool {
			return ids[i].String() <
				ids[j].String()
		},
	)

	result := make(
		[]GATarget,
		0,
		len(ids),
	)

	for _, id := range ids {
		result = append(
			result,
			GATarget{
				PoolID:   id,
				Quantity: quantities[id],
			},
		)
	}

	return result
}

func uniqueUUIDs(
	values []uuid.UUID,
) []uuid.UUID {
	seen := map[uuid.UUID]struct{}{}
	result := make(
		[]uuid.UUID,
		0,
		len(values),
	)

	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}

		seen[value] = struct{}{}
		result = append(
			result,
			value,
		)
	}

	return database.SortUUIDs(result)
}

func normalizePurpose(
	value string,
) string {
	value = strings.ToUpper(
		strings.TrimSpace(value),
	)

	if value == "" {
		return "OTHER"
	}

	return value
}

func inventoryConflict(
	err error,
) error {
	if err == nil {
		return nil
	}

	return apierror.New(
		apierror.CodeInventoryUnavailable,
		"one or more inventory targets are unavailable",
	)
}

func validation(
	message string,
) error {
	return apierror.New(
		apierror.CodeValidation,
		message,
	)
}

func notFound(
	resource string,
) error {
	return apierror.New(
		apierror.CodeResourceNotFound,
		fmt.Sprintf(
			"%s not found",
			resource,
		),
	)
}
