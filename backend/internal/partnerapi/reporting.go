package partnerapi

import (
	"net/http"
	"strconv"

	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
	"github.com/tktsync/tktsync/backend/internal/reporting"
)

func (h *Handler) getInventoryReport(w http.ResponseWriter, r *http.Request) {
	eventID, err := parseEventID(r.PathValue("event_id"))
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	principal, err := h.authenticateEvent(r, eventID)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	report, err := h.reporting.Inventory(r.Context(), eventID, &principal.PartnerID)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, report)
}

func (h *Handler) getSalesReport(w http.ResponseWriter, r *http.Request) {
	eventID, err := parseEventID(r.PathValue("event_id"))
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	principal, err := h.authenticateEvent(r, eventID)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	report, err := h.reporting.Sales(r.Context(), eventID, &principal.PartnerID)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, report)
}

func (h *Handler) getActivity(w http.ResponseWriter, r *http.Request) {
	eventID, err := parseEventID(r.PathValue("event_id"))
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	principal, err := h.authenticateEvent(r, eventID)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	filter := reporting.AuditFilter{PartnerID: &principal.PartnerID, Cursor: r.URL.Query().Get("cursor")}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 {
			httpserver.WriteError(w, r, apierror.New(apierror.CodeValidation, "activity limit must be a positive integer"))
			return
		}
		filter.Limit = value
	}
	page, err := h.reporting.Audit(r.Context(), eventID, filter)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, page)
}
