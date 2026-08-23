package partnerapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
	"github.com/tktsync/tktsync/backend/internal/reservation"
)

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
