package partnerapi

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/inventory"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
	"github.com/tktsync/tktsync/backend/internal/reservation"
)

func (h *Handler) requireReservationOwnership(
	ctx context.Context,
	partnerID uuid.UUID,
	reservationID uuid.UUID,
) error {
	var id uuid.UUID

	err := h.queryer(ctx).
		QueryRow(
			ctx,
			`
				SELECT id
				FROM reservations
				WHERE id = $1
				  AND partner_id = $2
			`,
			reservationID,
			partnerID,
		).
		Scan(&id)

	if errors.Is(err, pgx.ErrNoRows) {
		return apierror.New(
			apierror.CodeResourceNotFound,
			"Reservation not found",
		)
	}

	return err
}

func (h *Handler) loadReservationItemInputs(
	ctx context.Context,
	partnerID uuid.UUID,
	reservationID uuid.UUID,
) ([]reservation.ItemInput, uuid.UUID, error) {
	q := h.queryer(ctx)

	var eventID uuid.UUID

	err := q.QueryRow(
		ctx,
		`
			SELECT event_id
			FROM reservations
			WHERE id = $1
			  AND partner_id = $2
		`,
		reservationID,
		partnerID,
	).Scan(&eventID)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil,
			uuid.Nil,
			apierror.New(
				apierror.CodeResourceNotFound,
				"Reservation not found",
			)
	}

	if err != nil {
		return nil, uuid.Nil, err
	}

	rows, err := q.Query(
		ctx,
		`
			SELECT
				ri.id,
				ri.inventory_kind,
				ri.reserved_inventory_unit_id,
				ri.ga_pool_id,
				ri.quantity,
				ri.source_kind,
				COALESCE(
					aru.allocation_id,
					gab.allocation_id
				)
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
		return nil, uuid.Nil, err
	}
	defer rows.Close()

	result := make(
		[]reservation.ItemInput,
		0,
	)

	for rows.Next() {
		var (
			itemID             uuid.UUID
			inventoryKind      string
			reservedInventory  *uuid.UUID
			gaPool             *uuid.UUID
			quantity           int
			sourceKind         string
			sourceAllocationID *uuid.UUID
		)

		if err := rows.Scan(
			&itemID,
			&inventoryKind,
			&reservedInventory,
			&gaPool,
			&quantity,
			&sourceKind,
			&sourceAllocationID,
		); err != nil {
			return nil, uuid.Nil, err
		}

		var inventoryID uuid.UUID

		switch inventoryKind {
		case reservation.InventoryReserved:
			if reservedInventory == nil {
				return nil,
					uuid.Nil,
					apierror.New(
						apierror.CodeInternal,
						"Reserved ReservationItem has no inventory identity",
					)
			}

			inventoryID =
				*reservedInventory

		case reservation.InventoryGA:
			if gaPool == nil {
				return nil,
					uuid.Nil,
					apierror.New(
						apierror.CodeInternal,
						"GA ReservationItem has no pool identity",
					)
			}

			inventoryID = *gaPool

		default:
			return nil,
				uuid.Nil,
				apierror.New(
					apierror.CodeInternal,
					"ReservationItem has unsupported inventory kind",
				)
		}

		itemIDCopy := itemID

		result = append(
			result,
			reservation.ItemInput{
				ReservationItemID:  &itemIDCopy,
				InventoryKind:      inventoryKind,
				InventoryID:        inventoryID,
				Quantity:           quantity,
				SourceKind:         sourceKind,
				SourceAllocationID: sourceAllocationID,
			},
		)
	}

	if err := rows.Err(); err != nil {
		return nil, uuid.Nil, err
	}

	return result, eventID, nil
}

func (h *Handler) loadPartnerReservation(
	ctx context.Context,
	partnerID uuid.UUID,
	reservationID uuid.UUID,
) (map[string]any, error) {
	q := h.queryer(ctx)

	var (
		eventID                 uuid.UUID
		state                   string
		currency                string
		holdExpiresAt           time.Time
		paymentRetryExpiresAt   *time.Time
		reconciliationExpiresAt *time.Time
		maxLifetimeAt           time.Time
		partnerCustomerRef      *string
		partnerOrderRef         *string
		buyerSessionRef         *string
		confirmedAt             *time.Time
		releasedAt              *time.Time
		expiredAt               *time.Time
		serverTime              time.Time
	)

	err := q.QueryRow(
		ctx,
		`
			SELECT
				event_id,
				state,
				currency,
				hold_expires_at,
				payment_retry_expires_at,
				reconciliation_expires_at,
				max_lifetime_at,
				partner_customer_ref,
				partner_order_ref,
				buyer_session_ref,
				confirmed_at,
				released_at,
				expired_at,
				clock_timestamp()
			FROM reservations
			WHERE id = $1
			  AND partner_id = $2
		`,
		reservationID,
		partnerID,
	).Scan(
		&eventID,
		&state,
		&currency,
		&holdExpiresAt,
		&paymentRetryExpiresAt,
		&reconciliationExpiresAt,
		&maxLifetimeAt,
		&partnerCustomerRef,
		&partnerOrderRef,
		&buyerSessionRef,
		&confirmedAt,
		&releasedAt,
		&expiredAt,
		&serverTime,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil,
			apierror.New(
				apierror.CodeResourceNotFound,
				"Reservation not found",
			)
	}

	if err != nil {
		return nil, err
	}

	rows, err := q.Query(
		ctx,
		`
			SELECT
				id,
				inventory_kind,
				reserved_inventory_unit_id,
				ga_pool_id,
				quantity,
				unit_amount_minor,
				currency,
				price_tier_label_snapshot,
				commercial_terms
			FROM reservation_items
			WHERE reservation_id = $1
			  AND removed_at IS NULL
			ORDER BY created_at, id
		`,
		reservationID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make(
		[]map[string]any,
		0,
	)

	var totalAmount int64

	for rows.Next() {
		var (
			itemID            uuid.UUID
			inventoryKind     string
			reservedInventory *uuid.UUID
			gaPool            *uuid.UUID
			quantity          int
			unitAmountMinor   int64
			itemCurrency      string
			priceTierLabel    *string
			commercialTerms   []byte
		)

		if err := rows.Scan(
			&itemID,
			&inventoryKind,
			&reservedInventory,
			&gaPool,
			&quantity,
			&unitAmountMinor,
			&itemCurrency,
			&priceTierLabel,
			&commercialTerms,
		); err != nil {
			return nil, err
		}

		var inventoryID string

		switch inventoryKind {
		case reservation.InventoryReserved:
			if reservedInventory == nil {
				return nil,
					apierror.New(
						apierror.CodeInternal,
						"Reserved ReservationItem has no inventory identity",
					)
			}

			inventoryID =
				publicid.Encode(
					publicid.ReservedInventory,
					*reservedInventory,
				)

		case reservation.InventoryGA:
			if gaPool == nil {
				return nil,
					apierror.New(
						apierror.CodeInternal,
						"GA ReservationItem has no pool identity",
					)
			}

			inventoryID =
				publicid.Encode(
					publicid.GAPool,
					*gaPool,
				)

		default:
			return nil,
				apierror.New(
					apierror.CodeInternal,
					"ReservationItem has unsupported inventory kind",
				)
		}

		terms := any(
			map[string]any{},
		)

		if len(commercialTerms) > 0 {
			var decoded any

			if err := json.Unmarshal(
				commercialTerms,
				&decoded,
			); err == nil {
				terms = decoded
			}
		}

		items = append(
			items,
			map[string]any{
				"id": publicid.Encode(
					publicid.ReservationItem,
					itemID,
				),
				"inventory_kind":    inventoryKind,
				"inventory_id":      inventoryID,
				"quantity":          quantity,
				"unit_amount_minor": unitAmountMinor,
				"currency":          itemCurrency,
				"price_tier_label":  priceTierLabel,
				"commercial_terms":  terms,
			},
		)

		totalAmount +=
			unitAmountMinor *
				int64(quantity)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := map[string]any{
		"id": publicid.Encode(
			publicid.Reservation,
			reservationID,
		),
		"event_id": publicid.Encode(
			publicid.Event,
			eventID,
		),
		"status":          state,
		"currency":        currency,
		"hold_expires_at": holdExpiresAt,
		"max_lifetime_at": maxLifetimeAt,
		"server_time":     serverTime,
		"items":           items,
		"total": map[string]any{
			"amount_minor": totalAmount,
			"currency":     currency,
		},
	}

	if paymentRetryExpiresAt != nil {
		result["payment_retry_expires_at"] =
			*paymentRetryExpiresAt
	}

	if reconciliationExpiresAt != nil {
		result["reconciliation_expires_at"] =
			*reconciliationExpiresAt
	}

	if partnerCustomerRef != nil {
		result["partner_customer_ref"] =
			*partnerCustomerRef
	}

	if partnerOrderRef != nil {
		result["partner_order_ref"] =
			*partnerOrderRef
	}

	if buyerSessionRef != nil {
		result["buyer_session_ref"] =
			*buyerSessionRef
	}

	if confirmedAt != nil {
		result["confirmed_at"] =
			*confirmedAt
	}

	if releasedAt != nil {
		result["released_at"] =
			*releasedAt
	}

	if expiredAt != nil {
		result["expired_at"] =
			*expiredAt
	}

	return result, nil
}

var _ inventory.OfferSourceKind
