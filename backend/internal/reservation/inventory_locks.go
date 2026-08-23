package reservation

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

func (s *Service) lockMutationResources(
	ctx context.Context,
	tx pgx.Tx,
	eventID uuid.UUID,
	oldItems []activeItem,
	newItems []ItemInput,
) (map[uuid.UUID]allocationInfo, error) {
	allocationIDs := map[uuid.UUID]struct{}{}

	for _, item := range oldItems {
		if item.SourceAllocationID == nil {
			continue
		}

		chain, err :=
			allocationChainIDs(
				ctx,
				tx,
				*item.SourceAllocationID,
			)
		if err != nil {
			return nil, err
		}

		for _, id := range chain {
			allocationIDs[id] =
				struct{}{}
		}
	}

	for _, item := range newItems {
		if item.SourceAllocationID != nil {
			allocationIDs[*item.SourceAllocationID] = struct{}{}
		}
	}

	sortedAllocations :=
		sortedUUIDSet(
			allocationIDs,
		)

	allocations := make(
		map[uuid.UUID]allocationInfo,
		len(sortedAllocations),
	)

	for _, allocationID := range sortedAllocations {
		info, err := lockAllocation(
			ctx,
			tx,
			allocationID,
			eventID,
		)
		if err != nil {
			return nil, err
		}

		allocations[allocationID] = info
	}

	reservedIDs := map[uuid.UUID]struct{}{}

	gaPoolIDs := map[uuid.UUID]struct{}{}

	for _, item := range oldItems {
		if item.ReservedInventoryUnitID != nil {
			reservedIDs[*item.ReservedInventoryUnitID] = struct{}{}
		}

		if item.GAPoolID != nil {
			gaPoolIDs[*item.GAPoolID] = struct{}{}
		}
	}

	for _, item := range newItems {
		switch item.InventoryKind {
		case InventoryReserved:
			reservedIDs[item.InventoryID] = struct{}{}

		case InventoryGA:
			gaPoolIDs[item.InventoryID] = struct{}{}
		}
	}

	sortedReserved :=
		sortedUUIDSet(
			reservedIDs,
		)

	sortedGAPools :=
		sortedUUIDSet(
			gaPoolIDs,
		)

	for _, inventoryID := range sortedReserved {
		var lockedEventID uuid.UUID

		err := tx.QueryRow(
			ctx,
			`
				SELECT event_id
				FROM reserved_inventory_units
				WHERE id = $1
				FOR UPDATE
			`,
			inventoryID,
		).Scan(
			&lockedEventID,
		)
		if err != nil {
			if errors.Is(
				err,
				pgx.ErrNoRows,
			) {
				return nil,
					apierror.New(
						apierror.CodeInventoryUnavailable,
						"Reserved inventory is unavailable",
					)
			}

			return nil, err
		}

		if lockedEventID != eventID {
			return nil,
				apierror.New(
					apierror.CodeInventoryUnavailable,
					"Reserved inventory does not belong to the Event",
				)
		}
	}

	for _, poolID := range sortedGAPools {
		var lockedEventID uuid.UUID

		err := tx.QueryRow(
			ctx,
			`
				SELECT event_id
				FROM ga_inventory_pools
				WHERE id = $1
				FOR UPDATE
			`,
			poolID,
		).Scan(
			&lockedEventID,
		)
		if err != nil {
			if errors.Is(
				err,
				pgx.ErrNoRows,
			) {
				return nil,
					apierror.New(
						apierror.CodeInventoryUnavailable,
						"GA inventory is unavailable",
					)
			}

			return nil, err
		}

		if lockedEventID != eventID {
			return nil,
				apierror.New(
					apierror.CodeInventoryUnavailable,
					"GA inventory does not belong to the Event",
				)
		}
	}

	for _, inventoryID := range sortedReserved {
		rows, err := tx.Query(
			ctx,
			`
				SELECT id
				FROM reserved_inventory_claims
				WHERE reserved_inventory_unit_id = $1
				  AND ended_at IS NULL
				ORDER BY id
				FOR UPDATE
			`,
			inventoryID,
		)
		if err != nil {
			return nil, err
		}

		for rows.Next() {
			var ignored uuid.UUID

			if err := rows.Scan(
				&ignored,
			); err != nil {
				rows.Close()
				return nil, err
			}
		}

		err = rows.Err()
		rows.Close()

		if err != nil {
			return nil, err
		}
	}

	for _, poolID := range sortedGAPools {
		var ignored uuid.UUID

		if err := tx.QueryRow(
			ctx,
			`
				SELECT ga_pool_id
				FROM ga_shared_inventory
				WHERE ga_pool_id = $1
				FOR UPDATE
			`,
			poolID,
		).Scan(
			&ignored,
		); err != nil {
			return nil, err
		}

		for _, allocationID := range sortedAllocations {
			rows, err := tx.Query(
				ctx,
				`
					SELECT id
					FROM ga_allocation_buckets
					WHERE ga_pool_id = $1
					  AND allocation_id = $2
					FOR UPDATE
				`,
				poolID,
				allocationID,
			)
			if err != nil {
				return nil, err
			}

			for rows.Next() {
				var bucketID uuid.UUID

				if err := rows.Scan(
					&bucketID,
				); err != nil {
					rows.Close()
					return nil, err
				}
			}

			err = rows.Err()
			rows.Close()

			if err != nil {
				return nil, err
			}
		}
	}

	return allocations, nil
}

func allocationChainIDs(
	ctx context.Context,
	tx pgx.Tx,
	start uuid.UUID,
) ([]uuid.UUID, error) {
	result := make(
		[]uuid.UUID,
		0,
		4,
	)

	seen := map[uuid.UUID]struct{}{}

	current := start

	for current != uuid.Nil {
		if _, exists :=
			seen[current]; exists {
			return nil, apierror.New(
				apierror.CodeInternal,
				"Allocation release destination cycle detected",
			)
		}

		seen[current] =
			struct{}{}

		result = append(
			result,
			current,
		)

		var (
			kind string
			dest pgtype.UUID
		)

		err := tx.QueryRow(
			ctx,
			`
				SELECT
					release_destination_kind,
					release_destination_allocation_id
				FROM allocations
				WHERE restriction_id = $1
			`,
			current,
		).Scan(
			&kind,
			&dest,
		)
		if err != nil {
			if errors.Is(
				err,
				pgx.ErrNoRows,
			) {
				return nil,
					apierror.New(
						apierror.CodeInternal,
						"Reservation source Allocation is missing",
					)
			}

			return nil, err
		}

		if kind == "SHARED" ||
			!dest.Valid {
			break
		}

		current = uuid.UUID(
			dest.Bytes,
		)
	}

	return result, nil
}

func lockAllocation(
	ctx context.Context,
	tx pgx.Tx,
	allocationID uuid.UUID,
	eventID uuid.UUID,
) (allocationInfo, error) {
	var (
		state       string
		mode        string
		partner     pgtype.UUID
		destKind    string
		dest        pgtype.UUID
		lockedEvent uuid.UUID
	)

	err := tx.QueryRow(
		ctx,
		`
			SELECT
				ir.event_id,
				ir.state,
				a.mode,
				a.partner_id,
				a.release_destination_kind,
				a.release_destination_allocation_id
			FROM inventory_restrictions ir
			JOIN allocations a
			  ON a.restriction_id = ir.id
			WHERE ir.id = $1
			  AND ir.kind = 'ALLOCATION'
			FOR UPDATE OF ir, a
		`,
		allocationID,
	).Scan(
		&lockedEvent,
		&state,
		&mode,
		&partner,
		&destKind,
		&dest,
	)
	if err != nil {
		if errors.Is(
			err,
			pgx.ErrNoRows,
		) {
			return allocationInfo{},
				apierror.New(
					apierror.CodeInventoryNotEligibleForPartner,
					"Source Allocation is unavailable",
				)
		}

		return allocationInfo{}, err
	}

	if lockedEvent != eventID {
		return allocationInfo{},
			apierror.New(
				apierror.CodeInventoryNotEligibleForPartner,
				"Source Allocation does not belong to the Event",
			)
	}

	return allocationInfo{
		ID:    allocationID,
		State: state,
		Mode:  mode,
		PartnerID: nullableUUID(
			partner,
		),
		DestinationKind: destKind,
		DestinationAllocationID: nullableUUID(
			dest,
		),
	}, nil
}
