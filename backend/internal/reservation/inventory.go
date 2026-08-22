package reservation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	inventorysvc "github.com/tktsync/tktsync/backend/internal/inventory"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

type allocationInfo struct {
	ID                      uuid.UUID
	State                   string
	Mode                    string
	PartnerID               *uuid.UUID
	DestinationKind         string
	DestinationAllocationID *uuid.UUID
}

type activeItem struct {
	ID                             uuid.UUID
	ReservationID                  uuid.UUID
	EventID                        uuid.UUID
	InventoryKind                  string
	ReservedInventoryUnitID        *uuid.UUID
	GAPoolID                       *uuid.UUID
	Quantity                       int
	SourceKind                     string
	SourceAllocationID             *uuid.UUID
	SourceAllocationReservedUnitID *uuid.UUID
	SourceGAAllocationBucketID     *uuid.UUID
	PriceTierID                    *uuid.UUID
	UnitAmountMinor                int64
	Currency                       string
	PriceTierLabelSnapshot         *string
	CommercialTerms                json.RawMessage
}

type resolvedItem struct {
	Input                          ItemInput
	PriceTierID                    uuid.UUID
	UnitAmountMinor                int64
	Currency                       string
	PriceTierLabel                 string
	CommercialTerms                json.RawMessage
	SourceAllocationReservedUnitID *uuid.UUID
	SourceGAAllocationBucketID     *uuid.UUID
}

func normalizeItems(
	items []ItemInput,
) ([]ItemInput, error) {
	if len(items) == 0 {
		return nil, apierror.New(
			apierror.CodeValidation,
			"Reservation requires at least one inventory item",
		)
	}

	reservedSeen := map[uuid.UUID]struct{}{}
	reservationItemSeen := map[uuid.UUID]struct{}{}
	gaByKey := map[string]ItemInput{}
	normalized := make(
		[]ItemInput,
		0,
		len(items),
	)

	for _, input := range items {
		input.OfferID = strings.TrimSpace(
			input.OfferID,
		)

		input.InventoryKind = strings.ToUpper(
			strings.TrimSpace(
				input.InventoryKind,
			),
		)

		input.SourceKind = strings.ToUpper(
			strings.TrimSpace(
				input.SourceKind,
			),
		)

		if input.ReservationItemID != nil {
			if *input.ReservationItemID ==
				uuid.Nil {
				return nil, apierror.New(
					apierror.CodeValidation,
					"reservation_item_id is invalid",
				)
			}

			if _, exists :=
				reservationItemSeen[*input.ReservationItemID]; exists {
				return nil, apierror.New(
					apierror.CodeValidation,
					"Reservation item cannot appear more than once",
				)
			}

			reservationItemSeen[*input.ReservationItemID] = struct{}{}
		}

		if input.InventoryID == uuid.Nil {
			return nil, apierror.New(
				apierror.CodeValidation,
				"inventory_id is required",
			)
		}

		switch input.InventoryKind {
		case InventoryReserved:
			if input.Quantity != 1 {
				return nil, apierror.New(
					apierror.CodeValidation,
					"Reserved inventory quantity must equal one",
				)
			}

			if _, exists :=
				reservedSeen[input.InventoryID]; exists {
				return nil, apierror.New(
					apierror.CodeValidation,
					"Reserved inventory cannot appear more than once",
				)
			}

			reservedSeen[input.InventoryID] =
				struct{}{}

		case InventoryGA:
			if input.Quantity <= 0 {
				return nil, apierror.New(
					apierror.CodeValidation,
					"GA quantity must be positive",
				)
			}

		default:
			return nil, apierror.New(
				apierror.CodeValidation,
				"inventory_kind must be RESERVED or GA",
			)
		}

		switch input.SourceKind {
		case SourceShared:
			if input.SourceAllocationID != nil {
				return nil, apierror.New(
					apierror.CodeValidation,
					"SHARED inventory cannot identify an Allocation",
				)
			}

		case SourceAllocation:
			if input.SourceAllocationID == nil ||
				*input.SourceAllocationID ==
					uuid.Nil {
				return nil, apierror.New(
					apierror.CodeValidation,
					"ALLOCATION inventory requires a source Allocation",
				)
			}

		default:
			return nil, apierror.New(
				apierror.CodeValidation,
				"source_kind must be SHARED or ALLOCATION",
			)
		}

		if input.InventoryKind ==
			InventoryGA &&
			input.ReservationItemID == nil {
			allocation := ""

			if input.SourceAllocationID != nil {
				allocation =
					input.SourceAllocationID.
						String()
			}

			key := strings.Join(
				[]string{
					input.InventoryID.String(),
					input.SourceKind,
					allocation,
					input.OfferID,
				},
				"|",
			)

			if existing, ok :=
				gaByKey[key]; ok {
				existing.Quantity +=
					input.Quantity

				gaByKey[key] = existing
				continue
			}

			gaByKey[key] = input
			continue
		}

		normalized = append(
			normalized,
			input,
		)
	}

	for _, input := range gaByKey {
		normalized = append(
			normalized,
			input,
		)
	}

	sort.Slice(
		normalized,
		func(i, j int) bool {
			left := normalized[i]
			right := normalized[j]

			if left.InventoryKind !=
				right.InventoryKind {
				return left.InventoryKind <
					right.InventoryKind
			}

			if left.InventoryID !=
				right.InventoryID {
				return left.InventoryID.
					String() <
					right.InventoryID.
						String()
			}

			if left.SourceKind !=
				right.SourceKind {
				return left.SourceKind <
					right.SourceKind
			}

			leftAllocation := ""
			rightAllocation := ""

			if left.SourceAllocationID != nil {
				leftAllocation =
					left.SourceAllocationID.
						String()
			}

			if right.SourceAllocationID != nil {
				rightAllocation =
					right.SourceAllocationID.
						String()
			}

			if leftAllocation !=
				rightAllocation {
				return leftAllocation <
					rightAllocation
			}

			leftItemID := ""
			rightItemID := ""

			if left.ReservationItemID != nil {
				leftItemID =
					left.ReservationItemID.
						String()
			}

			if right.ReservationItemID != nil {
				rightItemID =
					right.ReservationItemID.
						String()
			}

			if leftItemID != rightItemID {
				return leftItemID <
					rightItemID
			}

			return left.OfferID <
				right.OfferID
		},
	)

	return normalized, nil
}

func totalQuantity(
	items []ItemInput,
) int {
	total := 0

	for _, item := range items {
		total += item.Quantity
	}

	return total
}

func loadActiveItems(
	ctx context.Context,
	tx pgx.Tx,
	reservationID uuid.UUID,
) ([]activeItem, error) {
	rows, err := tx.Query(
		ctx,
		`
			SELECT
				ri.id,
				ri.inventory_kind,
				ri.reserved_inventory_unit_id,
				ri.ga_pool_id,
				ri.quantity,
				ri.source_kind,
				ri.source_allocation_reserved_unit_id,
				ri.source_ga_allocation_bucket_id,
				COALESCE(
					aru.allocation_id,
					gab.allocation_id
				),
				ri.event_id,
				ri.price_tier_id,
				ri.unit_amount_minor,
				ri.currency,
				ri.price_tier_label_snapshot,
				ri.commercial_terms
			FROM reservation_items ri
			LEFT JOIN allocation_reserved_units aru
			  ON aru.id =
			     ri.source_allocation_reserved_unit_id
			LEFT JOIN ga_allocation_buckets gab
			  ON gab.id =
			     ri.source_ga_allocation_bucket_id
			WHERE ri.reservation_id = $1
			  AND ri.removed_at IS NULL
			ORDER BY ri.created_at, ri.id
		`,
		reservationID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(
		[]activeItem,
		0,
	)

	for rows.Next() {
		var (
			itemID          uuid.UUID
			inventoryKind   string
			reservedID      pgtype.UUID
			gaPoolID        pgtype.UUID
			quantity        int
			sourceKind      string
			sourceReserved  pgtype.UUID
			sourceGA        pgtype.UUID
			allocationID    pgtype.UUID
			eventID         uuid.UUID
			priceTierID     pgtype.UUID
			unitAmountMinor int64
			currency        string
			priceTierLabel  pgtype.Text
			commercialTerms []byte
		)

		if err := rows.Scan(
			&itemID,
			&inventoryKind,
			&reservedID,
			&gaPoolID,
			&quantity,
			&sourceKind,
			&sourceReserved,
			&sourceGA,
			&allocationID,
			&eventID,
			&priceTierID,
			&unitAmountMinor,
			&currency,
			&priceTierLabel,
			&commercialTerms,
		); err != nil {
			return nil, err
		}

		var priceTierLabelSnapshot *string

		if priceTierLabel.Valid {
			value := priceTierLabel.String
			priceTierLabelSnapshot = &value
		}

		result = append(
			result,
			activeItem{
				ID:            itemID,
				ReservationID: reservationID,
				EventID:       eventID,
				InventoryKind: inventoryKind,
				ReservedInventoryUnitID: nullableUUID(
					reservedID,
				),
				GAPoolID: nullableUUID(
					gaPoolID,
				),
				Quantity:   quantity,
				SourceKind: sourceKind,
				SourceAllocationID: nullableUUID(
					allocationID,
				),
				SourceAllocationReservedUnitID: nullableUUID(
					sourceReserved,
				),
				SourceGAAllocationBucketID: nullableUUID(
					sourceGA,
				),
				PriceTierID: nullableUUID(
					priceTierID,
				),
				UnitAmountMinor:        unitAmountMinor,
				Currency:               currency,
				PriceTierLabelSnapshot: priceTierLabelSnapshot,
				CommercialTerms: append(
					json.RawMessage(nil),
					commercialTerms...,
				),
			},
		)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

type partialGARelease struct {
	Item      activeItem
	Quantity  int
	Remaining int
}

type reservationModificationPlan struct {
	FullReleases    []activeItem
	PartialReleases []partialGARelease
	Acquisitions    []ItemInput
}

func modificationItemKey(
	inventoryKind string,
	inventoryID uuid.UUID,
	sourceKind string,
	allocationID *uuid.UUID,
) string {
	allocation := ""

	if allocationID != nil {
		allocation = allocationID.String()
	}

	return inventoryKind +
		"|" +
		inventoryID.String() +
		"|" +
		sourceKind +
		"|" +
		allocation
}

func activeModificationKey(
	item activeItem,
) (string, error) {
	var inventoryID uuid.UUID

	switch item.InventoryKind {
	case InventoryReserved:
		if item.ReservedInventoryUnitID == nil {
			return "", apierror.New(
				apierror.CodeInternal,
				"Reserved ReservationItem has no inventory identity",
			)
		}

		inventoryID = *item.ReservedInventoryUnitID

	case InventoryGA:
		if item.GAPoolID == nil {
			return "", apierror.New(
				apierror.CodeInternal,
				"GA ReservationItem has no pool identity",
			)
		}

		inventoryID = *item.GAPoolID

	default:
		return "", apierror.New(
			apierror.CodeInternal,
			"ReservationItem has an unsupported inventory kind",
		)
	}

	return modificationItemKey(
		item.InventoryKind,
		inventoryID,
		item.SourceKind,
		item.SourceAllocationID,
	), nil
}

func inputModificationKey(
	item ItemInput,
) string {
	return modificationItemKey(
		item.InventoryKind,
		item.InventoryID,
		item.SourceKind,
		item.SourceAllocationID,
	)
}

func planReservationModification(
	oldItems []activeItem,
	newItems []ItemInput,
) (reservationModificationPlan, error) {
	hasExplicitIdentity := false

	for _, item := range newItems {
		if item.ReservationItemID != nil {
			hasExplicitIdentity = true
			break
		}
	}

	if !hasExplicitIdentity {
		return planReservationModificationByComposition(
			oldItems,
			newItems,
		)
	}

	return planReservationModificationByIdentity(
		oldItems,
		newItems,
	)
}

func planReservationModificationByIdentity(
	oldItems []activeItem,
	newItems []ItemInput,
) (reservationModificationPlan, error) {
	oldByID := make(
		map[uuid.UUID]activeItem,
		len(oldItems),
	)

	for _, item := range oldItems {
		oldByID[item.ID] = item
	}

	retained := map[uuid.UUID]struct{}{}

	plan := reservationModificationPlan{
		FullReleases: make(
			[]activeItem,
			0,
		),
		PartialReleases: make(
			[]partialGARelease,
			0,
		),
		Acquisitions: make(
			[]ItemInput,
			0,
		),
	}

	for _, desired := range newItems {
		if desired.ReservationItemID == nil {
			plan.Acquisitions = append(
				plan.Acquisitions,
				desired,
			)

			continue
		}

		itemID := *desired.ReservationItemID
		current, exists := oldByID[itemID]

		if !exists {
			return reservationModificationPlan{},
				apierror.New(
					apierror.CodeValidation,
					"Reservation item is not active on this Reservation",
				)
		}

		if _, exists := retained[itemID]; exists {
			return reservationModificationPlan{},
				apierror.New(
					apierror.CodeValidation,
					"Reservation item cannot appear more than once",
				)
		}

		retained[itemID] = struct{}{}

		currentKey, err :=
			activeModificationKey(
				current,
			)
		if err != nil {
			return reservationModificationPlan{},
				err
		}

		desiredKey :=
			inputModificationKey(
				desired,
			)

		if currentKey != desiredKey {
			return reservationModificationPlan{},
				apierror.New(
					apierror.CodeValidation,
					"Existing Reservation item identity cannot change inventory or source",
				)
		}

		switch current.InventoryKind {
		case InventoryReserved:
			if desired.Quantity != 1 {
				return reservationModificationPlan{},
					apierror.New(
						apierror.CodeValidation,
						"Reserved inventory quantity must equal one",
					)
			}

		case InventoryGA:
			switch {
			case desired.Quantity ==
				current.Quantity:

			case desired.Quantity <
				current.Quantity:
				plan.PartialReleases =
					append(
						plan.PartialReleases,
						partialGARelease{
							Item: current,
							Quantity: current.Quantity -
								desired.Quantity,
							Remaining: desired.Quantity,
						},
					)

			case desired.Quantity >
				current.Quantity:
				acquisition := desired
				acquisition.ReservationItemID =
					nil
				acquisition.Quantity =
					desired.Quantity -
						current.Quantity

				plan.Acquisitions =
					append(
						plan.Acquisitions,
						acquisition,
					)
			}

		default:
			return reservationModificationPlan{},
				apierror.New(
					apierror.CodeInternal,
					"ReservationItem has an unsupported inventory kind",
				)
		}
	}

	for _, current := range oldItems {
		if _, exists :=
			retained[current.ID]; exists {
			continue
		}

		plan.FullReleases = append(
			plan.FullReleases,
			current,
		)
	}

	return plan, nil
}

func planReservationModificationByComposition(
	oldItems []activeItem,
	newItems []ItemInput,
) (reservationModificationPlan, error) {
	oldGroups := map[string][]activeItem{}
	newByKey := map[string]ItemInput{}

	for _, item := range oldItems {
		key, err := activeModificationKey(item)
		if err != nil {
			return reservationModificationPlan{}, err
		}

		oldGroups[key] = append(
			oldGroups[key],
			item,
		)
	}

	for _, item := range newItems {
		newByKey[inputModificationKey(item)] =
			item
	}

	keys := make(
		[]string,
		0,
		len(oldGroups),
	)

	for key := range oldGroups {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	plan := reservationModificationPlan{
		FullReleases: make(
			[]activeItem,
			0,
		),
		PartialReleases: make(
			[]partialGARelease,
			0,
		),
		Acquisitions: make(
			[]ItemInput,
			0,
		),
	}

	for _, key := range keys {
		group := oldGroups[key]
		desired, exists := newByKey[key]

		if group[0].InventoryKind ==
			InventoryReserved {
			if exists {
				delete(newByKey, key)
				continue
			}

			plan.FullReleases = append(
				plan.FullReleases,
				group...,
			)

			continue
		}

		oldQuantity := 0

		for _, item := range group {
			oldQuantity += item.Quantity
		}

		desiredQuantity := 0

		if exists {
			desiredQuantity = desired.Quantity
			delete(newByKey, key)
		}

		if desiredQuantity >= oldQuantity {
			if desiredQuantity >
				oldQuantity {
				acquisition := desired
				acquisition.Quantity =
					desiredQuantity -
						oldQuantity

				plan.Acquisitions =
					append(
						plan.Acquisitions,
						acquisition,
					)
			}

			continue
		}

		remaining := desiredQuantity

		for _, item := range group {
			if remaining >= item.Quantity {
				remaining -= item.Quantity
				continue
			}

			if remaining > 0 {
				plan.PartialReleases =
					append(
						plan.PartialReleases,
						partialGARelease{
							Item: item,
							Quantity: item.Quantity -
								remaining,
							Remaining: remaining,
						},
					)

				remaining = 0
				continue
			}

			plan.FullReleases = append(
				plan.FullReleases,
				item,
			)
		}
	}

	remainingKeys := make(
		[]string,
		0,
		len(newByKey),
	)

	for key := range newByKey {
		remainingKeys = append(
			remainingKeys,
			key,
		)
	}

	sort.Strings(remainingKeys)

	for _, key := range remainingKeys {
		plan.Acquisitions = append(
			plan.Acquisitions,
			newByKey[key],
		)
	}

	return plan, nil
}

func releasePartialGAItem(
	ctx context.Context,
	tx pgx.Tx,
	release partialGARelease,
	allocations map[uuid.UUID]allocationInfo,
	at time.Time,
) error {
	if release.Item.InventoryKind !=
		InventoryGA ||
		release.Item.ReservationID == uuid.Nil ||
		release.Item.EventID == uuid.Nil ||
		release.Item.GAPoolID == nil ||
		release.Quantity <= 0 ||
		release.Remaining <= 0 ||
		release.Quantity+
			release.Remaining !=
			release.Item.Quantity {
		return apierror.New(
			apierror.CodeInternal,
			"Partial GA Reservation release is invalid",
		)
	}

	var removedID uuid.UUID

	err := tx.QueryRow(
		ctx,
		`
			UPDATE reservation_items
			SET removed_at = $2
			WHERE id = $1
			  AND removed_at IS NULL
			  AND inventory_kind = 'GA'
			RETURNING id
		`,
		release.Item.ID,
		at,
	).Scan(&removedID)

	if errors.Is(err, pgx.ErrNoRows) {
		return apierror.New(
			apierror.CodeInternal,
			"GA ReservationItem changed during modification",
		)
	}

	if err != nil {
		return err
	}

	part := release.Item
	part.Quantity = release.Quantity

	if err := releaseActiveItems(
		ctx,
		tx,
		[]activeItem{part},
		allocations,
		at,
		"RESERVATION_QUANTITY_DECREASED",
		false,
	); err != nil {
		return err
	}

	_, err = tx.Exec(
		ctx,
		`
			INSERT INTO reservation_items (
				id,
				reservation_id,
				event_id,
				inventory_kind,
				ga_pool_id,
				quantity,
				source_kind,
				source_ga_allocation_bucket_id,
				price_tier_id,
				unit_amount_minor,
				currency,
				price_tier_label_snapshot,
				commercial_terms,
				created_at
			)
			VALUES (
				$1,
				$2,
				$3,
				'GA',
				$4,
				$5,
				$6,
				$7,
				$8,
				$9,
				$10,
				$11,
				$12,
				$13
			)
		`,
		uuid.New(),
		release.Item.ReservationID,
		release.Item.EventID,
		*release.Item.GAPoolID,
		release.Remaining,
		release.Item.SourceKind,
		release.Item.SourceGAAllocationBucketID,
		release.Item.PriceTierID,
		release.Item.UnitAmountMinor,
		release.Item.Currency,
		release.Item.PriceTierLabelSnapshot,
		release.Item.CommercialTerms,
		at,
	)

	return err
}

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

func (s *Service) resolveItems(
	ctx context.Context,
	tx pgx.Tx,
	eventID uuid.UUID,
	partnerID uuid.UUID,
	inputs []ItemInput,
	allocations map[uuid.UUID]allocationInfo,
) ([]resolvedItem, error) {
	result := make(
		[]resolvedItem,
		0,
		len(inputs),
	)

	for _, input := range inputs {
		var (
			priceTierID uuid.UUID
			amountMinor int64
			currency    string
			label       string
			priceState  string
		)

		switch input.InventoryKind {
		case InventoryReserved:
			err := tx.QueryRow(
				ctx,
				`
					SELECT
						pt.id,
						pt.amount_minor,
						pt.currency,
						pt.name,
						pt.state
					FROM reserved_inventory_units riu
					JOIN event_sections es
					  ON es.id =
					     riu.event_section_id
					JOIN event_price_tiers pt
					  ON pt.id = COALESCE(
					      riu.price_tier_override_id,
					      es.default_price_tier_id
					  )
					 AND pt.event_id =
					     riu.event_id
					WHERE riu.id = $1
					  AND riu.event_id = $2
				`,
				input.InventoryID,
				eventID,
			).Scan(
				&priceTierID,
				&amountMinor,
				&currency,
				&label,
				&priceState,
			)
			if err != nil {
				if errors.Is(
					err,
					pgx.ErrNoRows,
				) {
					return nil,
						apierror.New(
							apierror.CodeInventoryUnavailable,
							"Reserved inventory has no effective commercial price",
						)
				}

				return nil, err
			}

		case InventoryGA:
			err := tx.QueryRow(
				ctx,
				`
					SELECT
						pt.id,
						pt.amount_minor,
						pt.currency,
						pt.name,
						pt.state
					FROM ga_inventory_pools gp
					JOIN event_price_tiers pt
					  ON pt.id =
					     gp.price_tier_id
					 AND pt.event_id =
					     gp.event_id
					WHERE gp.id = $1
					  AND gp.event_id = $2
				`,
				input.InventoryID,
				eventID,
			).Scan(
				&priceTierID,
				&amountMinor,
				&currency,
				&label,
				&priceState,
			)
			if err != nil {
				if errors.Is(
					err,
					pgx.ErrNoRows,
				) {
					return nil,
						apierror.New(
							apierror.CodeInventoryUnavailable,
							"GA inventory has no effective commercial price",
						)
				}

				return nil, err
			}
		}

		if priceState != "ACTIVE" {
			return nil,
				apierror.New(
					apierror.CodeInventoryUnavailable,
					"Inventory price tier is not active",
				)
		}

		resolved := resolvedItem{
			Input:           input,
			PriceTierID:     priceTierID,
			UnitAmountMinor: amountMinor,
			Currency:        currency,
			PriceTierLabel:  label,
		}

		terms, err := json.Marshal(
			map[string]any{
				"price_authority": "EVENT_PRICE_TIER",
				"source_kind":     input.SourceKind,
			},
		)
		if err != nil {
			return nil, err
		}

		resolved.CommercialTerms =
			terms

		if input.SourceKind ==
			SourceAllocation {
			info, ok :=
				allocations[*input.SourceAllocationID]
			if !ok ||
				info.State != "ACTIVE" ||
				info.Mode != "CHANNEL" ||
				info.PartnerID == nil ||
				*info.PartnerID !=
					partnerID {
				return nil,
					apierror.New(
						apierror.CodeInventoryNotEligibleForPartner,
						"Inventory is not eligible for this Partner",
					)
			}

			switch input.InventoryKind {
			case InventoryReserved:
				var (
					membershipID uuid.UUID
					releasedAt   pgtype.Timestamptz
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
					*input.SourceAllocationID,
					input.InventoryID,
				).Scan(
					&membershipID,
					&releasedAt,
				)
				if err != nil ||
					releasedAt.Valid {
					if err != nil &&
						!errors.Is(
							err,
							pgx.ErrNoRows,
						) {
						return nil, err
					}

					return nil,
						apierror.New(
							apierror.CodeInventoryNotEligibleForPartner,
							"Reserved inventory is not currently assigned to this Partner Allocation",
						)
				}

				resolved.
					SourceAllocationReservedUnitID =
					&membershipID

			case InventoryGA:
				var bucketID uuid.UUID

				err := tx.QueryRow(
					ctx,
					`
						SELECT id
						FROM ga_allocation_buckets
						WHERE allocation_id = $1
						  AND ga_pool_id = $2
						FOR UPDATE
					`,
					*input.SourceAllocationID,
					input.InventoryID,
				).Scan(
					&bucketID,
				)
				if err != nil {
					if errors.Is(
						err,
						pgx.ErrNoRows,
					) {
						return nil,
							apierror.New(
								apierror.CodeInventoryNotEligibleForPartner,
								"GA inventory is not assigned to this Partner Allocation",
							)
					}

					return nil, err
				}

				resolved.
					SourceGAAllocationBucketID =
					&bucketID
			}
		}

		if strings.TrimSpace(input.OfferID) != "" {
			sourceID := uuid.Nil

			if input.SourceKind == SourceAllocation {
				switch input.InventoryKind {
				case InventoryReserved:
					if resolved.SourceAllocationReservedUnitID == nil {
						return nil, apierror.New(
							apierror.CodeInventoryUnavailable,
							"Offer source is no longer available",
						)
					}

					sourceID = *resolved.SourceAllocationReservedUnitID

				case InventoryGA:
					if resolved.SourceGAAllocationBucketID == nil {
						return nil, apierror.New(
							apierror.CodeInventoryUnavailable,
							"Offer source is no longer available",
						)
					}

					sourceID = *resolved.SourceGAAllocationBucketID
				}
			}

			expectedOfferID := inventorysvc.OfferID(
				eventID,
				partnerID,
				input.InventoryKind,
				input.InventoryID,
				inventorysvc.OfferSourceKind(input.SourceKind),
				sourceID,
				priceTierID,
				amountMinor,
				currency,
			)

			if expectedOfferID != strings.TrimSpace(input.OfferID) {
				return nil, apierror.New(
					apierror.CodeInventoryUnavailable,
					"Offer is stale or invalid",
				)
			}
		}

		result = append(
			result,
			resolved,
		)
	}

	return result, nil
}

func acquireResolvedItems(
	ctx context.Context,
	tx pgx.Tx,
	reservationID uuid.UUID,
	eventID uuid.UUID,
	items []resolvedItem,
	acceptedAt time.Time,
) error {
	for _, item := range items {
		itemID := uuid.New()

		switch item.Input.InventoryKind {
		case InventoryReserved:
			claimID, claimType,
				allocationMembershipID,
				err :=
				currentReservedClaim(
					ctx,
					tx,
					item.Input.InventoryID,
				)
			if err != nil {
				return err
			}

			switch item.Input.SourceKind {
			case SourceShared:
				if claimID != nil {
					return apierror.New(
						apierror.CodeInventoryUnavailable,
						"Reserved inventory is no longer available from the requested source",
					)
				}

			case SourceAllocation:
				if claimID == nil ||
					claimType !=
						"ALLOCATION" ||
					allocationMembershipID ==
						nil ||
					item.
						SourceAllocationReservedUnitID ==
						nil ||
					*allocationMembershipID !=
						*item.
							SourceAllocationReservedUnitID {
					return apierror.New(
						apierror.CodeInventoryUnavailable,
						"Reserved allocation inventory is no longer available",
					)
				}

				if _, err := tx.Exec(
					ctx,
					`
						UPDATE reserved_inventory_claims
						SET
							ended_at = $2,
							end_reason =
							    'RESERVATION_ACQUIRED'
						WHERE id = $1
						  AND ended_at IS NULL
					`,
					*claimID,
					acceptedAt,
				); err != nil {
					return err
				}
			}

			if _, err := tx.Exec(
				ctx,
				`
					INSERT INTO reservation_items (
						id,
						reservation_id,
						event_id,
						inventory_kind,
						reserved_inventory_unit_id,
						quantity,
						source_kind,
						source_allocation_reserved_unit_id,
						price_tier_id,
						unit_amount_minor,
						currency,
						price_tier_label_snapshot,
						commercial_terms,
						created_at
					)
					VALUES (
						$1,
						$2,
						$3,
						'RESERVED',
						$4,
						1,
						$5,
						$6,
						$7,
						$8,
						$9,
						$10,
						$11,
						$12
					)
				`,
				itemID,
				reservationID,
				eventID,
				item.Input.InventoryID,
				item.Input.SourceKind,
				item.
					SourceAllocationReservedUnitID,
				item.PriceTierID,
				item.UnitAmountMinor,
				item.Currency,
				item.PriceTierLabel,
				item.CommercialTerms,
				acceptedAt,
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
						reservation_item_id,
						activated_at
					)
					VALUES (
						$1,
						$2,
						'RESERVATION',
						$3,
						$4
					)
				`,
				uuid.New(),
				item.Input.InventoryID,
				itemID,
				acceptedAt,
			); err != nil {
				return translateInventoryWriteError(
					err,
				)
			}

		case InventoryGA:
			switch item.Input.SourceKind {
			case SourceShared:
				var poolID uuid.UUID

				err := tx.QueryRow(
					ctx,
					`
						UPDATE ga_shared_inventory
						SET
							available_quantity =
							    available_quantity - $2,
							active_reserved_quantity =
							    active_reserved_quantity + $2,
							updated_at = $3
						WHERE ga_pool_id = $1
						  AND available_quantity >= $2
						RETURNING ga_pool_id
					`,
					item.Input.InventoryID,
					item.Input.Quantity,
					acceptedAt,
				).Scan(
					&poolID,
				)
				if err != nil {
					if errors.Is(
						err,
						pgx.ErrNoRows,
					) {
						return apierror.New(
							apierror.CodeInsufficientGAQuantity,
							"Requested GA quantity is no longer available",
						)
					}

					return err
				}

			case SourceAllocation:
				if item.
					SourceGAAllocationBucketID ==
					nil {
					return apierror.New(
						apierror.CodeInventoryNotEligibleForPartner,
						"GA Allocation source is missing",
					)
				}

				var bucketID uuid.UUID

				err := tx.QueryRow(
					ctx,
					`
						UPDATE ga_allocation_buckets
						SET
							available_quantity =
							    available_quantity - $2,
							active_reserved_quantity =
							    active_reserved_quantity + $2,
							updated_at = $3
						WHERE id = $1
						  AND available_quantity >= $2
						RETURNING id
					`,
					*item.
						SourceGAAllocationBucketID,
					item.Input.Quantity,
					acceptedAt,
				).Scan(
					&bucketID,
				)
				if err != nil {
					if errors.Is(
						err,
						pgx.ErrNoRows,
					) {
						return apierror.New(
							apierror.CodeInsufficientGAQuantity,
							"Requested GA Allocation quantity is no longer available",
						)
					}

					return err
				}
			}

			if _, err := tx.Exec(
				ctx,
				`
					INSERT INTO reservation_items (
						id,
						reservation_id,
						event_id,
						inventory_kind,
						ga_pool_id,
						quantity,
						source_kind,
						source_ga_allocation_bucket_id,
						price_tier_id,
						unit_amount_minor,
						currency,
						price_tier_label_snapshot,
						commercial_terms,
						created_at
					)
					VALUES (
						$1,
						$2,
						$3,
						'GA',
						$4,
						$5,
						$6,
						$7,
						$8,
						$9,
						$10,
						$11,
						$12,
						$13
					)
				`,
				itemID,
				reservationID,
				eventID,
				item.Input.InventoryID,
				item.Input.Quantity,
				item.Input.SourceKind,
				item.
					SourceGAAllocationBucketID,
				item.PriceTierID,
				item.UnitAmountMinor,
				item.Currency,
				item.PriceTierLabel,
				item.CommercialTerms,
				acceptedAt,
			); err != nil {
				return err
			}
		}
	}

	return nil
}

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

func translateInventoryWriteError(
	err error,
) error {
	if err == nil {
		return nil
	}

	if strings.Contains(
		err.Error(),
		"reserved_inventory_one_active_claim_uq",
	) {
		return apierror.New(
			apierror.CodeInventoryUnavailable,
			"Reserved inventory was acquired concurrently",
		)
	}

	return err
}

func sortedUUIDSet(
	values map[uuid.UUID]struct{},
) []uuid.UUID {
	result := make(
		[]uuid.UUID,
		0,
		len(values),
	)

	for value := range values {
		result = append(
			result,
			value,
		)
	}

	sort.Slice(
		result,
		func(i, j int) bool {
			return result[i].String() <
				result[j].String()
		},
	)

	return result
}

func nullableUUID(
	value pgtype.UUID,
) *uuid.UUID {
	if !value.Valid {
		return nil
	}

	result := uuid.UUID(
		value.Bytes,
	)

	return &result
}

func nullableTime(
	value pgtype.Timestamptz,
) *time.Time {
	if !value.Valid {
		return nil
	}

	result := value.Time

	return &result
}

func requireSingleCurrency(
	items []resolvedItem,
) (string, error) {
	if len(items) == 0 {
		return "", apierror.New(
			apierror.CodeValidation,
			"Reservation requires inventory",
		)
	}

	currency := items[0].Currency

	for _, item := range items[1:] {
		if item.Currency != currency {
			return "",
				apierror.New(
					apierror.CodeCurrencyMismatch,
					"All Reservation items must use one currency",
				)
		}
	}

	return currency, nil
}

func allocationPartnerMatches(
	info allocationInfo,
	partnerID uuid.UUID,
) bool {
	return info.PartnerID != nil &&
		*info.PartnerID == partnerID
}

func requireAllocationActiveForPartner(
	info allocationInfo,
	partnerID uuid.UUID,
) error {
	if info.State != "ACTIVE" ||
		info.Mode != "CHANNEL" ||
		!allocationPartnerMatches(
			info,
			partnerID,
		) {
		return apierror.New(
			apierror.CodeInventoryNotEligibleForPartner,
			"Inventory Allocation is not eligible for this Partner",
		)
	}

	return nil
}

func internalf(
	format string,
	args ...any,
) error {
	return apierror.New(
		apierror.CodeInternal,
		fmt.Sprintf(
			format,
			args...,
		),
	)
}
