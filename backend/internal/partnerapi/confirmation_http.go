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

type confirmReservationRequest struct {
	CheckoutAttemptID string `json:"checkout_attempt_id"`
	PartnerOrderRef   string `json:"partner_order_ref,omitempty"`
	PartnerPaymentRef string `json:"partner_payment_ref,omitempty"`
}

func normalizeConfirmReservationRequest(
	request *confirmReservationRequest,
) {
	request.CheckoutAttemptID =
		strings.TrimSpace(
			request.CheckoutAttemptID,
		)

	request.PartnerOrderRef =
		strings.TrimSpace(
			request.PartnerOrderRef,
		)

	request.PartnerPaymentRef =
		strings.TrimSpace(
			request.PartnerPaymentRef,
		)
}

func parseTicketID(
	value string,
) (uuid.UUID, error) {
	id, err := publicid.Parse(
		strings.TrimSpace(value),
		publicid.Ticket,
	)
	if err != nil {
		return uuid.Nil,
			apierror.New(
				apierror.CodeValidation,
				"ticket_id is invalid",
			)
	}

	return id, nil
}

func (h *Handler) confirmReservation(
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
		httpserver.WriteError(
			w,
			r,
			err,
		)
		return
	}

	token, err :=
		reservationTokenHeader(r)
	if err != nil {
		httpserver.WriteError(
			w,
			r,
			err,
		)
		return
	}

	var request confirmReservationRequest

	if err := decodePartnerJSON(
		r,
		&request,
	); err != nil {
		httpserver.WriteError(
			w,
			r,
			err,
		)
		return
	}

	normalizeConfirmReservationRequest(
		&request,
	)

	checkoutAttemptID, err :=
		parseCheckoutAttemptID(
			request.CheckoutAttemptID,
		)
	if err != nil {
		httpserver.WriteError(
			w,
			r,
			err,
		)
		return
	}

	request.CheckoutAttemptID =
		publicid.Encode(
			publicid.CheckoutAttempt,
			checkoutAttemptID,
		)

	canonical, err :=
		canonicalPartnerMutation(
			"PARTNER_CONFIRM_RESERVATION",
			publicid.Encode(
				publicid.Reservation,
				reservationID,
			),
			request,
			token,
		)
	if err != nil {
		httpserver.WriteError(
			w,
			r,
			err,
		)
		return
	}

	h.runPartnerMutation(
		w,
		r,
		"PARTNER_CONFIRM_RESERVATION",
		canonical,
		func(
			ctx context.Context,
			principal auth.PartnerPrincipal,
		) (
			partnerMutationResponse,
			error,
		) {
			confirmed, err :=
				h.reservation.Confirm(
					ctx,
					principal.PartnerID,
					token,
					reservation.ConfirmInput{
						CheckoutAttemptID: checkoutAttemptID,
						PartnerOrderRef: request.
							PartnerOrderRef,
						PartnerPaymentRef: request.
							PartnerPaymentRef,
					},
				)
			if err != nil {
				return partnerMutationResponse{},
					err
			}

			tickets := make(
				[]map[string]any,
				0,
				len(confirmed.Tickets),
			)

			for _, ticket := range confirmed.Tickets {
				tickets = append(
					tickets,
					map[string]any{
						"id": publicid.Encode(
							publicid.Ticket,
							ticket.TicketID,
						),
						"status": ticket.State,
						"credential_id": publicid.Encode(
							publicid.Credential,
							ticket.CredentialID,
						),
					},
				)
			}

			id := confirmed.
				ReservationID

			return partnerJSONResponse(
				http.StatusOK,
				map[string]any{
					"reservation_id": publicid.Encode(
						publicid.Reservation,
						confirmed.ReservationID,
					),
					"status": confirmed.State,
					"sale": map[string]any{
						"id": publicid.Encode(
							publicid.Sale,
							confirmed.SaleID,
						),
						"confirmed_at": confirmed.
							ConfirmedAt,
						"partner_order_ref": confirmed.
							PartnerOrderRef,
						"partner_payment_ref": confirmed.
							PartnerPaymentRef,
					},
					"tickets": tickets,
				},
				&id,
				false,
			)
		},
	)
}

func (h *Handler) getTicketCredential(
	w http.ResponseWriter,
	r *http.Request,
) {
	if h.reservation == nil {
		httpserver.WriteError(
			w,
			r,
			apierror.New(
				apierror.CodeAuthorityTemporarilyUnavailable,
				"Ticket credential authority is not configured",
			),
		)
		return
	}

	ticketID, err :=
		parseTicketID(
			r.PathValue(
				"ticket_id",
			),
		)
	if err != nil {
		httpserver.WriteError(
			w,
			r,
			err,
		)
		return
	}

	principal, err :=
		h.authenticatePartner(r)
	if err != nil {
		httpserver.WriteError(
			w,
			r,
			err,
		)
		return
	}

	credential, err :=
		h.reservation.
			RecoverActiveCredential(
				r.Context(),
				principal.PartnerID,
				ticketID,
			)
	if err != nil {
		httpserver.WriteError(
			w,
			r,
			err,
		)
		return
	}

	qrURL, err := h.ticketQRURL(credential.TicketID)
	if err != nil {
		httpserver.WriteError(
			w,
			r,
			err,
		)
		return
	}

	w.Header().Set(
		"Cache-Control",
		"no-store",
	)

	httpserver.WriteJSON(
		w,
		http.StatusOK,
		map[string]any{
			"ticket_id": publicid.Encode(
				publicid.Ticket,
				credential.TicketID,
			),
			"credential_id": publicid.Encode(
				publicid.Credential,
				credential.CredentialID,
			),
			"status": credential.State,
			"qr_payload": credential.
				QRPayload,
			"qr_url": qrURL,
		},
	)
}

type voidTicketRequest struct {
	Reason string `json:"reason,omitempty"`
}

func (h *Handler) voidTicket(
	w http.ResponseWriter,
	r *http.Request,
) {
	ticketID, err :=
		parseTicketID(
			r.PathValue("ticket_id"),
		)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	var request voidTicketRequest

	if r.ContentLength != 0 {
		if err := decodePartnerJSON(
			r,
			&request,
		); err != nil {
			httpserver.WriteError(
				w,
				r,
				err,
			)
			return
		}
	}

	request.Reason =
		strings.TrimSpace(
			request.Reason,
		)

	canonical, err :=
		canonicalPartnerMutation(
			"PARTNER_VOID_TICKET",
			publicid.Encode(
				publicid.Ticket,
				ticketID,
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
		"PARTNER_VOID_TICKET",
		canonical,
		func(
			ctx context.Context,
			principal auth.PartnerPrincipal,
		) (
			partnerMutationResponse,
			error,
		) {
			result, err :=
				h.reservation.
					VoidPartnerTicket(
						ctx,
						principal.PartnerID,
						ticketID,
						request.Reason,
					)
			if err != nil {
				return partnerMutationResponse{},
					err
			}

			return partnerJSONResponse(
				http.StatusOK,
				map[string]any{
					"ticket_id": publicid.Encode(
						publicid.Ticket,
						result.TicketID,
					),
					"status":      result.State,
					"voided_at":   result.VoidedAt,
					"void_reason": result.VoidReason,
				},
				nil,
				false,
			)
		},
	)
}

func (h *Handler) reissueTicketCredential(
	w http.ResponseWriter,
	r *http.Request,
) {
	ticketID, err :=
		parseTicketID(
			r.PathValue("ticket_id"),
		)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	request := struct{}{}

	if r.ContentLength != 0 {
		if err := decodePartnerJSON(
			r,
			&request,
		); err != nil {
			httpserver.WriteError(
				w,
				r,
				err,
			)
			return
		}
	}

	canonical, err :=
		canonicalPartnerMutation(
			"PARTNER_REISSUE_TICKET_CREDENTIAL",
			publicid.Encode(
				publicid.Ticket,
				ticketID,
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
		"PARTNER_REISSUE_TICKET_CREDENTIAL",
		canonical,
		func(
			ctx context.Context,
			principal auth.PartnerPrincipal,
		) (
			partnerMutationResponse,
			error,
		) {
			result, err :=
				h.reservation.
					ReissuePartnerCredential(
						ctx,
						principal.PartnerID,
						ticketID,
					)
			if err != nil {
				return partnerMutationResponse{},
					err
			}

			return partnerJSONResponse(
				http.StatusOK,
				map[string]any{
					"ticket_id": publicid.Encode(
						publicid.Ticket,
						result.TicketID,
					),
					"credential_id": publicid.Encode(
						publicid.Credential,
						result.CredentialID,
					),
					"status":    result.State,
					"issued_at": result.IssuedAt,
				},
				nil,
				false,
			)
		},
	)
}

type releasePartnerTicketInventoryRequest struct {
	DestinationKind         string `json:"destination_kind"`
	DestinationAllocationID string `json:"destination_allocation_id,omitempty"`
	Reason                  string `json:"reason"`
}

func (h *Handler) reReleaseTicketInventory(w http.ResponseWriter, r *http.Request) {
	ticketID, err := parseTicketID(r.PathValue("ticket_id"))
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	var request releasePartnerTicketInventoryRequest
	if err = decodePartnerJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	request.DestinationKind = strings.ToUpper(strings.TrimSpace(request.DestinationKind))
	request.DestinationAllocationID = strings.TrimSpace(request.DestinationAllocationID)
	request.Reason = strings.TrimSpace(request.Reason)
	var destinationAllocationID *uuid.UUID
	if request.DestinationAllocationID != "" {
		id, parseErr := publicid.Parse(request.DestinationAllocationID, publicid.Allocation)
		if parseErr != nil {
			httpserver.WriteError(w, r, apierror.New(apierror.CodeValidation, "destination_allocation_id is invalid"))
			return
		}
		destinationAllocationID = &id
		request.DestinationAllocationID = publicid.Encode(publicid.Allocation, id)
	}
	canonical, err := canonicalPartnerMutation("PARTNER_RERELEASE_TICKET_INVENTORY", publicid.Encode(publicid.Ticket, ticketID), request, "")
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	h.runPartnerMutation(w, r, "PARTNER_RERELEASE_TICKET_INVENTORY", canonical, func(ctx context.Context, principal auth.PartnerPrincipal) (partnerMutationResponse, error) {
		result, releaseErr := h.reservation.ReReleasePartnerTicketInventory(ctx, principal.PartnerID, ticketID, reservation.InventoryReleaseInput{DestinationKind: request.DestinationKind, DestinationAllocationID: destinationAllocationID, Reason: request.Reason})
		if releaseErr != nil {
			return partnerMutationResponse{}, releaseErr
		}
		body := map[string]any{"ticket_id": publicid.Encode(publicid.Ticket, result.TicketID), "released_at": result.ReleasedAt, "destination_kind": result.DestinationKind, "reason": result.Reason}
		if result.DestinationAllocationID != nil {
			body["destination_allocation_id"] = publicid.Encode(publicid.Allocation, *result.DestinationAllocationID)
		}
		return partnerJSONResponse(http.StatusOK, body, nil, false)
	})
}
