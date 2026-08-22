package partnerapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
)

type createSelectionSessionRequest struct {
	EventID         string `json:"event_id"`
	BuyerSessionRef string `json:"buyer_session_ref,omitempty"`
	ReturnURL       string `json:"return_url"`
}

func (h *Handler) createSelectionSession(w http.ResponseWriter, r *http.Request) {
	var request createSelectionSessionRequest
	if err := decodePartnerJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	request.EventID = strings.TrimSpace(request.EventID)
	request.BuyerSessionRef = strings.TrimSpace(request.BuyerSessionRef)
	request.ReturnURL = strings.TrimSpace(request.ReturnURL)
	eventID, err := parseEventID(request.EventID)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	canonical, err := canonicalPartnerMutation("PARTNER_CREATE_SELECTION_SESSION", publicid.Encode(publicid.Event, eventID), request, "")
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	h.runPartnerMutation(w, r, "PARTNER_CREATE_SELECTION_SESSION", canonical, func(ctx context.Context, principal auth.PartnerPrincipal) (partnerMutationResponse, error) {
		if h.selection == nil {
			return partnerMutationResponse{}, apierror.New(apierror.CodeAuthorityTemporarilyUnavailable, "Selection API is not configured")
		}
		created, createErr := h.selection.Create(ctx, principal.PartnerID, eventID, request.BuyerSessionRef, request.ReturnURL)
		if createErr != nil {
			return partnerMutationResponse{}, createErr
		}
		response, marshalErr := partnerJSONResponse(http.StatusCreated, map[string]any{
			"selection_session_id": publicid.Encode(publicid.SelectionSession, created.ID),
			"selection_url":        created.SelectionURL,
			"expires_at":           created.ExpiresAt,
		}, &created.ID, false)
		response.RecoverSelectionURL = true
		response.EntityType = "BUYER_SELECTION_SESSION"
		return response, marshalErr
	})
}
