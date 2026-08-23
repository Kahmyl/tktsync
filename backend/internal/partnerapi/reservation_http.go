package partnerapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
	"github.com/tktsync/tktsync/backend/internal/reservation"
)

const maxPartnerRequestBodyBytes = 1 << 20

type createReservationItemRequest struct {
	OfferID  string `json:"offer_id"`
	Quantity int    `json:"quantity"`
}

type createReservationRequest struct {
	EventID            string                         `json:"event_id"`
	PartnerCustomerRef string                         `json:"partner_customer_ref,omitempty"`
	PartnerOrderRef    string                         `json:"partner_order_ref,omitempty"`
	BuyerSessionRef    string                         `json:"buyer_session_ref,omitempty"`
	Items              []createReservationItemRequest `json:"items"`
}

type adjustReservationQuantityRequest struct {
	ReservationItemID string `json:"reservation_item_id"`
	NewQuantity       int    `json:"new_quantity"`
}

type modifyReservationRequest struct {
	RemoveItemIDs    []string                           `json:"remove_item_ids,omitempty"`
	AdjustQuantities []adjustReservationQuantityRequest `json:"adjust_quantities,omitempty"`
	AddItems         []createReservationItemRequest     `json:"add_items,omitempty"`
}

type paymentFailureRequest struct {
	CheckoutAttemptID    string `json:"checkout_attempt_id"`
	PartnerPaymentRef    string `json:"partner_payment_ref,omitempty"`
	FailureCode          string `json:"failure_code"`
	RequestedDisposition string `json:"requested_disposition"`
}

type partnerQueryer interface {
	QueryRow(
		context.Context,
		string,
		...any,
	) pgx.Row

	Query(
		context.Context,
		string,
		...any,
	) (pgx.Rows, error)
}

func decodePartnerJSON(
	r *http.Request,
	target any,
) error {
	reader := io.LimitReader(
		r.Body,
		maxPartnerRequestBodyBytes+1,
	)

	raw, err := io.ReadAll(
		reader,
	)
	if err != nil {
		return err
	}

	if len(raw) >
		maxPartnerRequestBodyBytes {
		return apierror.New(
			apierror.CodeValidation,
			"request body is too large",
		)
	}

	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte(`{}`)
	}

	decoder := json.NewDecoder(
		bytes.NewReader(raw),
	)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(
		target,
	); err != nil {
		return apierror.New(
			apierror.CodeValidation,
			"request body is invalid",
		)
	}

	if decoder.Decode(
		&struct{}{},
	) != io.EOF {
		return apierror.New(
			apierror.CodeValidation,
			"request body must contain one JSON value",
		)
	}

	return nil
}

func parseReservationID(
	value string,
) (uuid.UUID, error) {
	id, err := publicid.Parse(
		strings.TrimSpace(value),
		publicid.Reservation,
	)
	if err != nil {
		return uuid.Nil,
			apierror.New(
				apierror.CodeValidation,
				"reservation_id is invalid",
			)
	}

	return id, nil
}

func parseReservationItemID(
	value string,
) (uuid.UUID, error) {
	id, err := publicid.Parse(
		strings.TrimSpace(value),
		publicid.ReservationItem,
	)
	if err != nil {
		return uuid.Nil,
			apierror.New(
				apierror.CodeValidation,
				"reservation_item_id is invalid",
			)
	}

	return id, nil
}

func parseCheckoutAttemptID(
	value string,
) (uuid.UUID, error) {
	id, err := publicid.Parse(
		strings.TrimSpace(value),
		publicid.CheckoutAttempt,
	)
	if err != nil {
		return uuid.Nil,
			apierror.New(
				apierror.CodeValidation,
				"checkout_attempt_id is invalid",
			)
	}

	return id, nil
}

func reservationTokenHeader(
	r *http.Request,
) (string, error) {
	token := strings.TrimSpace(
		r.Header.Get(
			"X-TktSync-Reservation-Token",
		),
	)

	if token == "" {
		return "",
			apierror.New(
				apierror.CodeHoldNotOwned,
				"X-TktSync-Reservation-Token header is required",
			)
	}

	return token, nil
}

func normalizeCreateReservationRequest(
	request *createReservationRequest,
) {
	request.EventID =
		strings.TrimSpace(
			request.EventID,
		)

	request.PartnerCustomerRef =
		strings.TrimSpace(
			request.PartnerCustomerRef,
		)

	request.PartnerOrderRef =
		strings.TrimSpace(
			request.PartnerOrderRef,
		)

	request.BuyerSessionRef =
		strings.TrimSpace(
			request.BuyerSessionRef,
		)

	for index := range request.Items {
		request.Items[index].OfferID =
			strings.TrimSpace(
				request.Items[index].
					OfferID,
			)
	}

	sort.Slice(
		request.Items,
		func(i, j int) bool {
			if request.Items[i].
				OfferID !=
				request.Items[j].
					OfferID {
				return request.Items[i].
					OfferID <
					request.Items[j].
						OfferID
			}

			return request.Items[i].
				Quantity <
				request.Items[j].
					Quantity
		},
	)
}

func normalizeModifyReservationRequest(
	request *modifyReservationRequest,
) {
	for index := range request.RemoveItemIDs {
		request.RemoveItemIDs[index] =
			strings.TrimSpace(
				request.RemoveItemIDs[index],
			)
	}

	sort.Strings(
		request.RemoveItemIDs,
	)

	for index := range request.AdjustQuantities {
		request.AdjustQuantities[index].
			ReservationItemID =
			strings.TrimSpace(
				request.AdjustQuantities[index].
					ReservationItemID,
			)
	}

	sort.Slice(
		request.AdjustQuantities,
		func(i, j int) bool {
			return request.
				AdjustQuantities[i].
				ReservationItemID <
				request.
					AdjustQuantities[j].
					ReservationItemID
		},
	)

	for index := range request.AddItems {
		request.AddItems[index].OfferID =
			strings.TrimSpace(
				request.AddItems[index].
					OfferID,
			)
	}

	sort.Slice(
		request.AddItems,
		func(i, j int) bool {
			if request.AddItems[i].
				OfferID !=
				request.AddItems[j].
					OfferID {
				return request.AddItems[i].
					OfferID <
					request.AddItems[j].
						OfferID
			}

			return request.AddItems[i].
				Quantity <
				request.AddItems[j].
					Quantity
		},
	)
}

func (h *Handler) queryer(
	ctx context.Context,
) partnerQueryer {
	if tx, ok :=
		database.TransactionFromContext(
			ctx,
		); ok {
		return tx
	}

	return h.db
}

func (h *Handler) serverTime(
	ctx context.Context,
) (time.Time, error) {
	var now time.Time

	err := h.queryer(ctx).
		QueryRow(
			ctx,
			`SELECT clock_timestamp()`,
		).
		Scan(&now)

	return now, err
}

func (h *Handler) resolvedOfferInput(
	ctx context.Context,
	principal auth.PartnerPrincipal,
	eventID uuid.UUID,
	item createReservationItemRequest,
) (reservation.ItemInput, error) {
	if item.OfferID == "" {
		return reservation.ItemInput{},
			apierror.New(
				apierror.CodeValidation,
				"offer_id is required",
			)
	}

	if item.Quantity <= 0 {
		return reservation.ItemInput{},
			apierror.New(
				apierror.CodeValidation,
				"quantity must be positive",
			)
	}

	resolved, err :=
		h.availability.ResolvePartnerOffer(
			ctx,
			principal.PartnerID,
			eventID,
			item.OfferID,
		)
	if err != nil {
		return reservation.ItemInput{},
			err
	}

	return reservation.ItemInput{
		OfferID:            item.OfferID,
		InventoryKind:      resolved.InventoryKind,
		InventoryID:        resolved.InventoryID,
		Quantity:           item.Quantity,
		SourceKind:         string(resolved.SourceKind),
		SourceAllocationID: resolved.SourceAllocationID,
	}, nil
}
