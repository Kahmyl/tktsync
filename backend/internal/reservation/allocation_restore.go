package reservation

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

func finalAllocationDestination(
	sourceAllocationID uuid.UUID,
	allocations map[uuid.UUID]allocationInfo,
) (*uuid.UUID, error) {
	currentID := sourceAllocationID

	seen := map[uuid.UUID]struct{}{}

	for {
		if _, exists :=
			seen[currentID]; exists {
			return nil,
				apierror.New(
					apierror.CodeInternal,
					"Allocation release destination cycle detected",
				)
		}

		seen[currentID] =
			struct{}{}

		info, ok :=
			allocations[currentID]
		if !ok {
			return nil,
				apierror.New(
					apierror.CodeInternal,
					"Allocation release destination was not locked",
				)
		}

		if info.State == "ACTIVE" {
			value := currentID
			return &value, nil
		}

		if info.DestinationKind ==
			"SHARED" {
			return nil, nil
		}

		if info.
			DestinationAllocationID ==
			nil {
			return nil,
				apierror.New(
					apierror.CodeInternal,
					"Released Allocation has no valid destination",
				)
		}

		currentID =
			*info.
				DestinationAllocationID
	}
}

func ensureAllocationReservedMembership(
	ctx context.Context,
	tx pgx.Tx,
	allocationID uuid.UUID,
	inventoryID uuid.UUID,
	at time.Time,
) (uuid.UUID, error) {
	var (
		id         uuid.UUID
		releasedAt pgtype.Timestamptz
	)

	err := tx.QueryRow(
		ctx,
		`
			SELECT
				id,
				released_at
			FROM allocation_reserved_units
			WHERE allocation_id = $1
			  AND reserved_inventory_unit_id = $2
			FOR UPDATE
		`,
		allocationID,
		inventoryID,
	).Scan(
		&id,
		&releasedAt,
	)
	if err == nil {
		if releasedAt.Valid {
			return uuid.Nil,
				apierror.New(
					apierror.CodeInternal,
					"Destination Allocation membership is historical rather than active",
				)
		}

		return id, nil
	}

	if !errors.Is(
		err,
		pgx.ErrNoRows,
	) {
		return uuid.Nil, err
	}

	id = uuid.New()

	_, err = tx.Exec(
		ctx,
		`
			INSERT INTO allocation_reserved_units (
				id,
				allocation_id,
				reserved_inventory_unit_id,
				assigned_at
			)
			VALUES (
				$1,
				$2,
				$3,
				$4
			)
		`,
		id,
		allocationID,
		inventoryID,
		at,
	)
	if err != nil {
		return uuid.Nil, err
	}

	return id, nil
}

func currentReservedClaim(
	ctx context.Context,
	tx pgx.Tx,
	inventoryID uuid.UUID,
) (
	*uuid.UUID,
	string,
	*uuid.UUID,
	error,
) {
	var (
		id           uuid.UUID
		claimType    string
		allocationID pgtype.UUID
	)

	err := tx.QueryRow(
		ctx,
		`
			SELECT
				id,
				claim_type,
				allocation_reserved_unit_id
			FROM reserved_inventory_claims
			WHERE reserved_inventory_unit_id = $1
			  AND ended_at IS NULL
			FOR UPDATE
		`,
		inventoryID,
	).Scan(
		&id,
		&claimType,
		&allocationID,
	)
	if errors.Is(
		err,
		pgx.ErrNoRows,
	) {
		return nil, "", nil, nil
	}

	if err != nil {
		return nil, "", nil, err
	}

	return &id,
		claimType,
		nullableUUID(
			allocationID,
		),
		nil
}
