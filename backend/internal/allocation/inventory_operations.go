package allocation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
)

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
