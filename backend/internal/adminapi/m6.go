package adminapi

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"github.com/google/uuid"
	allocsvc "github.com/tktsync/tktsync/backend/internal/allocation"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
)

type issueNonPublicRequest struct {
	RecipientRef         string                       `json:"recipient_ref,omitempty"`
	Reason               string                       `json:"reason,omitempty"`
	ReservedInventoryIDs []string                     `json:"reserved_inventory_ids,omitempty"`
	GATargets            []gaRestrictionTargetRequest `json:"ga_targets,omitempty"`
}

func (h *Handler) registerM6Routes() {
	if h.allocation != nil {
		h.mux.HandleFunc(
			"POST /api/v1/admin/allocations/{allocation_id}/issuances",
			h.issueNonPublic,
		)
	}

	if h.reservation != nil {
		h.mux.HandleFunc(
			"POST /api/v1/admin/tickets/{ticket_id}/void",
			h.voidTicket,
		)

		h.mux.HandleFunc(
			"POST /api/v1/admin/tickets/{ticket_id}/credentials/reissue",
			h.reissueTicketCredential,
		)
	}
}

func (h *Handler) issueNonPublic(
	w http.ResponseWriter,
	r *http.Request,
) {
	allocationID, err := parsePublicID(
		r.PathValue("allocation_id"),
		publicid.Allocation,
		"allocation_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	var request issueNonPublicRequest

	if err := decodeJSON(
		r,
		&request,
	); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	normalizeIssueNonPublicRequest(
		&request,
	)

	reservedIDs := make(
		[]uuid.UUID,
		0,
		len(request.ReservedInventoryIDs),
	)

	for _, value := range request.ReservedInventoryIDs {
		id, err := parsePublicID(
			value,
			publicid.ReservedInventory,
			"reserved_inventory_ids",
		)
		if err != nil {
			httpserver.WriteError(
				w,
				r,
				err,
			)
			return
		}

		reservedIDs = append(
			reservedIDs,
			id,
		)
	}

	gaTargets := make(
		[]allocsvc.GATarget,
		0,
		len(request.GATargets),
	)

	for _, target := range request.GATargets {
		poolID, err := parsePublicID(
			target.InventoryID,
			publicid.GAPool,
			"ga_targets.inventory_id",
		)
		if err != nil {
			httpserver.WriteError(
				w,
				r,
				err,
			)
			return
		}

		gaTargets = append(
			gaTargets,
			allocsvc.GATarget{
				PoolID:   poolID,
				Quantity: target.Quantity,
			},
		)
	}

	if len(reservedIDs) == 0 &&
		len(gaTargets) == 0 {
		httpserver.WriteError(
			w,
			r,
			apierror.New(
				apierror.CodeValidation,
				"at least one inventory target is required",
			),
		)
		return
	}

	h.runMutation(
		w,
		r,
		"ADMIN_NON_PUBLIC_ISSUE",
		allocationID.String(),
		request,
		platformAdminAuthorization,
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			issued, err :=
				h.allocation.IssueNonPublic(
					ctx,
					userID,
					allocationID,
					allocsvc.
						NonPublicIssuanceInput{
						RecipientRef: request.
							RecipientRef,
						Reason: request.
							Reason,
						ReservedUnitIDs: reservedIDs,
						GATargets:       gaTargets,
					},
				)
			if err != nil {
				return response{}, err
			}

			tickets := make(
				[]map[string]any,
				0,
				len(issued.Tickets),
			)

			for _, ticket := range issued.Tickets {
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

			return jsonResponse(
				http.StatusCreated,
				map[string]any{
					"issuance_id": publicid.Encode(
						publicid.NonPublicIssuance,
						issued.IssuanceID,
					),
					"event_id": publicid.Encode(
						publicid.Event,
						issued.EventID,
					),
					"allocation_id": publicid.Encode(
						publicid.Allocation,
						issued.AllocationID,
					),
					"issued_at": issued.IssuedAt,
					"tickets":   tickets,
				},
			)
		},
	)
}

func normalizeIssueNonPublicRequest(
	request *issueNonPublicRequest,
) {
	request.RecipientRef =
		strings.TrimSpace(
			request.RecipientRef,
		)

	request.Reason =
		strings.TrimSpace(
			request.Reason,
		)

	reserved := make(
		map[string]struct{},
		len(request.ReservedInventoryIDs),
	)

	for _, value := range request.ReservedInventoryIDs {
		value = strings.TrimSpace(value)

		if value == "" {
			continue
		}

		reserved[value] = struct{}{}
	}

	request.ReservedInventoryIDs =
		request.ReservedInventoryIDs[:0]

	for value := range reserved {
		request.ReservedInventoryIDs =
			append(
				request.ReservedInventoryIDs,
				value,
			)
	}

	sort.Strings(
		request.ReservedInventoryIDs,
	)

	quantities := make(
		map[string]int,
		len(request.GATargets),
	)

	for _, target := range request.GATargets {
		inventoryID :=
			strings.TrimSpace(
				target.InventoryID,
			)

		if inventoryID == "" {
			continue
		}

		quantities[inventoryID] +=
			target.Quantity
	}

	request.GATargets =
		request.GATargets[:0]

	for inventoryID, quantity := range quantities {
		request.GATargets = append(
			request.GATargets,
			gaRestrictionTargetRequest{
				InventoryID: inventoryID,
				Quantity:    quantity,
			},
		)
	}

	sort.Slice(
		request.GATargets,
		func(
			i int,
			j int,
		) bool {
			return request.
				GATargets[i].
				InventoryID <
				request.
					GATargets[j].
					InventoryID
		},
	)
}

type voidAdminTicketRequest struct {
	Reason string `json:"reason,omitempty"`
}

func (h *Handler) voidTicket(
	w http.ResponseWriter,
	r *http.Request,
) {
	ticketID, err := parsePublicID(
		r.PathValue("ticket_id"),
		publicid.Ticket,
		"ticket_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	var request voidAdminTicketRequest

	if err := decodeJSON(
		r,
		&request,
	); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	request.Reason =
		strings.TrimSpace(
			request.Reason,
		)

	h.runMutation(
		w,
		r,
		"ADMIN_VOID_TICKET",
		ticketID.String(),
		request,
		platformAdminAuthorization,
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			result, err :=
				h.reservation.VoidAdminTicket(
					ctx,
					userID,
					ticketID,
					request.Reason,
				)
			if err != nil {
				return response{}, err
			}

			return jsonResponse(
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
			)
		},
	)
}

func (h *Handler) reissueTicketCredential(
	w http.ResponseWriter,
	r *http.Request,
) {
	ticketID, err := parsePublicID(
		r.PathValue("ticket_id"),
		publicid.Ticket,
		"ticket_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	request := struct{}{}

	if err := decodeJSON(
		r,
		&request,
	); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	h.runMutation(
		w,
		r,
		"ADMIN_REISSUE_TICKET_CREDENTIAL",
		ticketID.String(),
		request,
		platformAdminAuthorization,
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			result, err :=
				h.reservation.
					ReissueAdminCredential(
						ctx,
						userID,
						ticketID,
					)
			if err != nil {
				return response{}, err
			}

			return jsonResponse(
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
			)
		},
	)
}
