package reservation

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

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
