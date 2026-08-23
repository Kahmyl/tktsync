package reservation

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

func releaseActiveItems(
	ctx context.Context,
	tx pgx.Tx,
	items []activeItem,
	allocations map[uuid.UUID]allocationInfo,
	at time.Time,
	reason string,
	markRemoved bool,
) error {
	for _, item := range items {
		if markRemoved {
			if _, err := tx.Exec(
				ctx,
				`
					UPDATE reservation_items
					SET removed_at = $2
					WHERE id = $1
					  AND removed_at IS NULL
				`,
				item.ID,
				at,
			); err != nil {
				return err
			}
		}

		switch item.InventoryKind {
		case InventoryReserved:
			if item.ReservedInventoryUnitID ==
				nil {
				return apierror.New(
					apierror.CodeInternal,
					"Reserved ReservationItem has no inventory identity",
				)
			}

			tag, err := tx.Exec(
				ctx,
				`
					UPDATE reserved_inventory_claims
					SET
						ended_at = $2,
						end_reason = $3
					WHERE reservation_item_id = $1
					  AND claim_type =
					      'RESERVATION'
					  AND ended_at IS NULL
				`,
				item.ID,
				at,
				reason,
			)
			if err != nil {
				return err
			}

			if tag.RowsAffected() != 1 {
				return apierror.New(
					apierror.CodeInternal,
					"Active Reservation claim is missing",
				)
			}

			if item.SourceKind ==
				SourceAllocation {
				if item.SourceAllocationID ==
					nil ||
					item.
						SourceAllocationReservedUnitID ==
						nil {
					return apierror.New(
						apierror.CodeInternal,
						"Allocation-sourced Reserved item is incomplete",
					)
				}

				if err :=
					restoreReservedDestination(
						ctx,
						tx,
						*item.
							ReservedInventoryUnitID,
						*item.
							SourceAllocationID,
						*item.
							SourceAllocationReservedUnitID,
						allocations,
						at,
					); err != nil {
					return err
				}
			}

		case InventoryGA:
			if item.GAPoolID == nil {
				return apierror.New(
					apierror.CodeInternal,
					"GA ReservationItem has no pool identity",
				)
			}

			switch item.SourceKind {
			case SourceShared:
				var poolID uuid.UUID

				err := tx.QueryRow(
					ctx,
					`
						UPDATE ga_shared_inventory
						SET
							active_reserved_quantity =
							    active_reserved_quantity - $2,
							available_quantity =
							    available_quantity + $2,
							updated_at = $3
						WHERE ga_pool_id = $1
						  AND active_reserved_quantity >= $2
						RETURNING ga_pool_id
					`,
					*item.GAPoolID,
					item.Quantity,
					at,
				).Scan(
					&poolID,
				)
				if err != nil {
					return err
				}

			case SourceAllocation:
				if item.SourceAllocationID ==
					nil ||
					item.
						SourceGAAllocationBucketID ==
						nil {
					return apierror.New(
						apierror.CodeInternal,
						"Allocation-sourced GA item is incomplete",
					)
				}

				if err :=
					restoreGADestination(
						ctx,
						tx,
						*item.GAPoolID,
						item.Quantity,
						*item.
							SourceAllocationID,
						*item.
							SourceGAAllocationBucketID,
						allocations,
						at,
					); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func restoreReservedDestination(
	ctx context.Context,
	tx pgx.Tx,
	inventoryID uuid.UUID,
	sourceAllocationID uuid.UUID,
	sourceMembershipID uuid.UUID,
	allocations map[uuid.UUID]allocationInfo,
	at time.Time,
) error {
	destination, err :=
		finalAllocationDestination(
			sourceAllocationID,
			allocations,
		)
	if err != nil {
		return err
	}

	if destination == nil {
		return nil
	}

	membershipID :=
		sourceMembershipID

	if *destination !=
		sourceAllocationID {
		value, err :=
			ensureAllocationReservedMembership(
				ctx,
				tx,
				*destination,
				inventoryID,
				at,
			)
		if err != nil {
			return err
		}

		membershipID = value
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
				$1,
				$2,
				'ALLOCATION',
				$3,
				$4
			)
		`,
		uuid.New(),
		inventoryID,
		membershipID,
		at,
	); err != nil {
		return translateInventoryWriteError(
			err,
		)
	}

	return nil
}

func restoreGADestination(
	ctx context.Context,
	tx pgx.Tx,
	poolID uuid.UUID,
	quantity int,
	sourceAllocationID uuid.UUID,
	sourceBucketID uuid.UUID,
	allocations map[uuid.UUID]allocationInfo,
	at time.Time,
) error {
	source, ok :=
		allocations[sourceAllocationID]
	if !ok {
		return apierror.New(
			apierror.CodeInternal,
			"Reservation source Allocation was not locked",
		)
	}

	if source.State == "ACTIVE" {
		var bucketID uuid.UUID

		err := tx.QueryRow(
			ctx,
			`
				UPDATE ga_allocation_buckets
				SET
					active_reserved_quantity =
					    active_reserved_quantity - $2,
					available_quantity =
					    available_quantity + $2,
					updated_at = $3
				WHERE id = $1
				  AND active_reserved_quantity >= $2
				RETURNING id
			`,
			sourceBucketID,
			quantity,
			at,
		).Scan(
			&bucketID,
		)

		return err
	}

	var sourceID uuid.UUID

	err := tx.QueryRow(
		ctx,
		`
			UPDATE ga_allocation_buckets
			SET
				active_reserved_quantity =
				    active_reserved_quantity - $2,
				released_quantity =
				    released_quantity + $2,
				updated_at = $3
			WHERE id = $1
			  AND active_reserved_quantity >= $2
			RETURNING id
		`,
		sourceBucketID,
		quantity,
		at,
	).Scan(
		&sourceID,
	)
	if err != nil {
		return err
	}

	destination, err :=
		finalAllocationDestination(
			sourceAllocationID,
			allocations,
		)
	if err != nil {
		return err
	}

	if destination == nil {
		var sharedID uuid.UUID

		return tx.QueryRow(
			ctx,
			`
				UPDATE ga_shared_inventory
				SET
					available_quantity =
					    available_quantity + $2,
					updated_at = $3
				WHERE ga_pool_id = $1
				RETURNING ga_pool_id
			`,
			poolID,
			quantity,
			at,
		).Scan(
			&sharedID,
		)
	}

	var existingID uuid.UUID

	err = tx.QueryRow(
		ctx,
		`
			UPDATE ga_allocation_buckets
			SET
				assigned_quantity =
				    assigned_quantity + $3,
				available_quantity =
				    available_quantity + $3,
				updated_at = $4
			WHERE allocation_id = $1
			  AND ga_pool_id = $2
			RETURNING id
		`,
		*destination,
		poolID,
		quantity,
		at,
	).Scan(
		&existingID,
	)
	if err == nil {
		return nil
	}

	if !errors.Is(
		err,
		pgx.ErrNoRows,
	) {
		return err
	}

	_, err = tx.Exec(
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
				$1,
				$2,
				$3,
				$4,
				$4,
				0,
				0,
				0,
				0,
				$5,
				$5
			)
		`,
		uuid.New(),
		*destination,
		poolID,
		quantity,
		at,
	)

	return err
}
