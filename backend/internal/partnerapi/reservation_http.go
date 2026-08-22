package partnerapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/inventory"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
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

func (h *Handler) createReservation(
	w http.ResponseWriter,
	r *http.Request,
) {
	var request createReservationRequest

	if err := decodePartnerJSON(
		r,
		&request,
	); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	normalizeCreateReservationRequest(
		&request,
	)

	eventID, err := parseEventID(
		request.EventID,
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	if len(request.Items) == 0 {
		httpserver.WriteError(
			w,
			r,
			apierror.New(
				apierror.CodeValidation,
				"items must contain at least one offer",
			),
		)
		return
	}

	canonical, err :=
		canonicalPartnerMutation(
			"PARTNER_CREATE_RESERVATION",
			publicid.Encode(
				publicid.Event,
				eventID,
			),
			request,
			"",
		)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	h.runPartnerMutation(
		w,
		r,
		"PARTNER_CREATE_RESERVATION",
		canonical,
		func(
			ctx context.Context,
			principal auth.PartnerPrincipal,
		) (partnerMutationResponse, error) {
			inputs := make(
				[]reservation.ItemInput,
				0,
				len(request.Items),
			)

			for _, item := range request.Items {
				input, err :=
					h.resolvedOfferInput(
						ctx,
						principal,
						eventID,
						item,
					)
				if err != nil {
					return partnerMutationResponse{},
						err
				}

				inputs = append(
					inputs,
					input,
				)
			}

			created, err :=
				h.reservation.Create(
					ctx,
					reservation.CreateInput{
						EventID:            eventID,
						PartnerID:          principal.PartnerID,
						PartnerCustomerRef: request.PartnerCustomerRef,
						PartnerOrderRef:    request.PartnerOrderRef,
						BuyerSessionRef:    request.BuyerSessionRef,
						Items:              inputs,
					},
				)
			if err != nil {
				return partnerMutationResponse{},
					err
			}

			snapshot, err :=
				h.loadPartnerReservation(
					ctx,
					principal.PartnerID,
					created.ReservationID,
				)
			if err != nil {
				return partnerMutationResponse{},
					err
			}

			id := created.ReservationID

			return partnerJSONResponse(
				http.StatusCreated,
				map[string]any{
					"reservation":       snapshot,
					"reservation_token": created.Token,
				},
				&id,
				true,
			)
		},
	)
}

func (h *Handler) getReservation(
	w http.ResponseWriter,
	r *http.Request,
) {
	reservationID, err :=
		parseReservationID(
			r.PathValue(
				"reservation_id",
			),
		)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	principal, err :=
		h.authenticatePartner(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	if err :=
		h.requireReservationOwnership(
			r.Context(),
			principal.PartnerID,
			reservationID,
		); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	if h.reservation != nil {
		if err :=
			h.reservation.MaterializeDue(
				r.Context(),
				reservationID,
			); err != nil {
			httpserver.WriteError(
				w,
				r,
				err,
			)
			return
		}
	}

	snapshot, err :=
		h.loadPartnerReservation(
			r.Context(),
			principal.PartnerID,
			reservationID,
		)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	httpserver.WriteJSON(
		w,
		http.StatusOK,
		snapshot,
	)
}

func (h *Handler) modifyReservation(
	w http.ResponseWriter,
	r *http.Request,
) {
	reservationID, err :=
		parseReservationID(
			r.PathValue(
				"reservation_id",
			),
		)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	token, err :=
		reservationTokenHeader(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	var request modifyReservationRequest

	if err := decodePartnerJSON(
		r,
		&request,
	); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	normalizeModifyReservationRequest(
		&request,
	)

	canonical, err :=
		canonicalPartnerMutation(
			"PARTNER_MODIFY_RESERVATION",
			publicid.Encode(
				publicid.Reservation,
				reservationID,
			),
			request,
			token,
		)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	h.runPartnerMutation(
		w,
		r,
		"PARTNER_MODIFY_RESERVATION",
		canonical,
		func(
			ctx context.Context,
			principal auth.PartnerPrincipal,
		) (partnerMutationResponse, error) {
			current, eventID, err :=
				h.loadReservationItemInputs(
					ctx,
					principal.PartnerID,
					reservationID,
				)
			if err != nil {
				return partnerMutationResponse{},
					err
			}

			byID := make(
				map[uuid.UUID]int,
				len(current),
			)

			for index, item := range current {
				if item.ReservationItemID ==
					nil {
					return partnerMutationResponse{},
						apierror.New(
							apierror.CodeInternal,
							"Active ReservationItem has no identity",
						)
				}

				byID[*item.
					ReservationItemID] =
					index
			}

			removed :=
				map[uuid.UUID]struct{}{}

			for _, rawID := range request.RemoveItemIDs {
				itemID, err :=
					parseReservationItemID(
						rawID,
					)
				if err != nil {
					return partnerMutationResponse{},
						err
				}

				if _, exists :=
					byID[itemID]; !exists {
					return partnerMutationResponse{},
						apierror.New(
							apierror.CodeValidation,
							"remove_item_ids contains an item that is not active on this Reservation",
						)
				}

				removed[itemID] =
					struct{}{}
			}

			for _, adjustment := range request.AdjustQuantities {
				itemID, err :=
					parseReservationItemID(
						adjustment.
							ReservationItemID,
					)
				if err != nil {
					return partnerMutationResponse{},
						err
				}

				if _, exists :=
					removed[itemID]; exists {
					return partnerMutationResponse{},
						apierror.New(
							apierror.CodeValidation,
							"Reservation item cannot be removed and adjusted in the same request",
						)
				}

				index, exists :=
					byID[itemID]
				if !exists {
					return partnerMutationResponse{},
						apierror.New(
							apierror.CodeValidation,
							"adjust_quantities contains an item that is not active on this Reservation",
						)
				}

				if adjustment.
					NewQuantity <= 0 {
					return partnerMutationResponse{},
						apierror.New(
							apierror.CodeValidation,
							"new_quantity must be positive",
						)
				}

				current[index].Quantity =
					adjustment.
						NewQuantity
			}

			finalItems := make(
				[]reservation.ItemInput,
				0,
				len(current)+
					len(request.AddItems),
			)

			for _, item := range current {
				if _, exists :=
					removed[*item.
						ReservationItemID]; exists {
					continue
				}

				finalItems = append(
					finalItems,
					item,
				)
			}

			for _, added := range request.AddItems {
				input, err :=
					h.resolvedOfferInput(
						ctx,
						principal,
						eventID,
						added,
					)
				if err != nil {
					return partnerMutationResponse{},
						err
				}

				finalItems = append(
					finalItems,
					input,
				)
			}

			if _, err :=
				h.reservation.Modify(
					ctx,
					principal.PartnerID,
					token,
					finalItems,
				); err != nil {
				return partnerMutationResponse{},
					err
			}

			snapshot, err :=
				h.loadPartnerReservation(
					ctx,
					principal.PartnerID,
					reservationID,
				)
			if err != nil {
				return partnerMutationResponse{},
					err
			}

			id := reservationID

			return partnerJSONResponse(
				http.StatusOK,
				snapshot,
				&id,
				false,
			)
		},
	)
}

func (h *Handler) beginReservationCheckout(
	w http.ResponseWriter,
	r *http.Request,
) {
	reservationID, err :=
		parseReservationID(
			r.PathValue(
				"reservation_id",
			),
		)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	token, err :=
		reservationTokenHeader(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	var request struct{}

	if err := decodePartnerJSON(
		r,
		&request,
	); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	canonical, err :=
		canonicalPartnerMutation(
			"PARTNER_BEGIN_CHECKOUT",
			publicid.Encode(
				publicid.Reservation,
				reservationID,
			),
			request,
			token,
		)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	h.runPartnerMutation(
		w,
		r,
		"PARTNER_BEGIN_CHECKOUT",
		canonical,
		func(
			ctx context.Context,
			principal auth.PartnerPrincipal,
		) (partnerMutationResponse, error) {
			checkout, err :=
				h.reservation.BeginCheckout(
					ctx,
					principal.PartnerID,
					token,
				)
			if err != nil {
				return partnerMutationResponse{},
					err
			}

			if checkout.ReservationID !=
				reservationID {
				return partnerMutationResponse{},
					apierror.New(
						apierror.CodeHoldNotOwned,
						"Reservation token does not match requested Reservation",
					)
			}

			serverTime, err :=
				h.serverTime(ctx)
			if err != nil {
				return partnerMutationResponse{},
					err
			}

			id := reservationID

			return partnerJSONResponse(
				http.StatusOK,
				map[string]any{
					"reservation_id": publicid.Encode(
						publicid.Reservation,
						reservationID,
					),
					"status": "COMMITTING",
					"checkout_attempt": map[string]any{
						"id": publicid.Encode(
							publicid.CheckoutAttempt,
							checkout.CheckoutAttemptID,
						),
						"status": "ACTIVE",
						"checkout_expires_at": checkout.
							CommitExpiresAt,
					},
					"server_time": serverTime,
				},
				&id,
				false,
			)
		},
	)
}

func (h *Handler) reservationPaymentFailure(
	w http.ResponseWriter,
	r *http.Request,
) {
	reservationID, err :=
		parseReservationID(
			r.PathValue(
				"reservation_id",
			),
		)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	token, err :=
		reservationTokenHeader(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	var request paymentFailureRequest

	if err := decodePartnerJSON(
		r,
		&request,
	); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	request.CheckoutAttemptID =
		strings.TrimSpace(
			request.CheckoutAttemptID,
		)
	request.PartnerPaymentRef =
		strings.TrimSpace(
			request.PartnerPaymentRef,
		)
	request.FailureCode =
		strings.TrimSpace(
			request.FailureCode,
		)
	request.RequestedDisposition =
		strings.ToUpper(
			strings.TrimSpace(
				request.
					RequestedDisposition,
			),
		)

	checkoutAttemptID, err :=
		parseCheckoutAttemptID(
			request.CheckoutAttemptID,
		)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	request.CheckoutAttemptID =
		publicid.Encode(
			publicid.CheckoutAttempt,
			checkoutAttemptID,
		)

	if request.FailureCode == "" {
		httpserver.WriteError(
			w,
			r,
			apierror.New(
				apierror.CodeValidation,
				"failure_code is required",
			),
		)
		return
	}

	canonical, err :=
		canonicalPartnerMutation(
			"PARTNER_PAYMENT_FAILURE",
			publicid.Encode(
				publicid.Reservation,
				reservationID,
			),
			request,
			token,
		)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	h.runPartnerMutation(
		w,
		r,
		"PARTNER_PAYMENT_FAILURE",
		canonical,
		func(
			ctx context.Context,
			principal auth.PartnerPrincipal,
		) (partnerMutationResponse, error) {
			result, err :=
				h.reservation.PaymentFailure(
					ctx,
					principal.PartnerID,
					token,
					checkoutAttemptID,
					request.PartnerPaymentRef,
					request.FailureCode,
					request.RequestedDisposition,
				)
			if err != nil {
				return partnerMutationResponse{},
					err
			}

			if result.ReservationID !=
				reservationID {
				return partnerMutationResponse{},
					apierror.New(
						apierror.CodeHoldNotOwned,
						"Reservation token does not match requested Reservation",
					)
			}

			serverTime, err :=
				h.serverTime(ctx)
			if err != nil {
				return partnerMutationResponse{},
					err
			}

			body := map[string]any{
				"reservation_id": publicid.Encode(
					publicid.Reservation,
					reservationID,
				),
				"status":      result.State,
				"server_time": serverTime,
			}

			if result.State ==
				"PAYMENT_RETRY" {
				body["payment_retry_expires_at"] =
					result.
						PaymentRetryExpiresAt
			}

			id := reservationID

			return partnerJSONResponse(
				http.StatusOK,
				body,
				&id,
				false,
			)
		},
	)
}

func (h *Handler) releaseReservation(
	w http.ResponseWriter,
	r *http.Request,
) {
	reservationID, err :=
		parseReservationID(
			r.PathValue(
				"reservation_id",
			),
		)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	token, err :=
		reservationTokenHeader(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	var request struct{}

	if err := decodePartnerJSON(
		r,
		&request,
	); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	canonical, err :=
		canonicalPartnerMutation(
			"PARTNER_RELEASE_RESERVATION",
			publicid.Encode(
				publicid.Reservation,
				reservationID,
			),
			request,
			token,
		)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	h.runPartnerMutation(
		w,
		r,
		"PARTNER_RELEASE_RESERVATION",
		canonical,
		func(
			ctx context.Context,
			principal auth.PartnerPrincipal,
		) (partnerMutationResponse, error) {
			if err :=
				h.reservation.Release(
					ctx,
					principal.PartnerID,
					token,
				); err != nil {
				return partnerMutationResponse{},
					err
			}

			serverTime, err :=
				h.serverTime(ctx)
			if err != nil {
				return partnerMutationResponse{},
					err
			}

			id := reservationID

			return partnerJSONResponse(
				http.StatusOK,
				map[string]any{
					"reservation_id": publicid.Encode(
						publicid.Reservation,
						reservationID,
					),
					"status":      "RELEASED",
					"server_time": serverTime,
				},
				&id,
				false,
			)
		},
	)
}

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
