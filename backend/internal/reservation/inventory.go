package reservation

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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
