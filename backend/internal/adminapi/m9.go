package adminapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
)

func (h *Handler) registerM9Routes() {
	h.mux.HandleFunc("PUT /api/v1/admin/partners/{partner_id}/allowed-return-urls", h.setPartnerAllowedReturnURLs)
}

func (h *Handler) setPartnerAllowedReturnURLs(w http.ResponseWriter, r *http.Request) {
	partnerID, err := parsePublicID(r.PathValue("partner_id"), publicid.Partner, "partner_id")
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	var request struct {
		URLs []string `json:"urls"`
	}
	if err = decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	h.runMutation(w, r, "ADMIN_SET_PARTNER_RETURN_URLS", partnerID.String(), request, platformAdminAuthorization, false, func(ctx context.Context, userID uuid.UUID) (response, error) {
		urls, setErr := h.partner.SetAllowedReturnURLs(ctx, userID, partnerID, request.URLs)
		if setErr != nil {
			return response{}, setErr
		}
		return jsonResponse(http.StatusOK, map[string]any{"partner_id": publicid.Encode(publicid.Partner, partnerID), "urls": urls})
	})
}
