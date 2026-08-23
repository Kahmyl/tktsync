package reservation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	inventorysvc "github.com/tktsync/tktsync/backend/internal/inventory"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

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
