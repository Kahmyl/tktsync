package adminapi

import (
	"context"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
	"github.com/tktsync/tktsync/backend/internal/reporting"
)

func (h *Handler) registerReportingRoutes() {
	h.mux.HandleFunc("GET /api/v1/admin/events/{event_id}/reports/inventory", h.getInventoryReport)
	h.mux.HandleFunc("GET /api/v1/admin/events/{event_id}/reports/sales", h.getSalesReport)
	h.mux.HandleFunc("GET /api/v1/admin/events/{event_id}/reports/admission", h.getAdmissionReport)
	h.mux.HandleFunc("GET /api/v1/admin/events/{event_id}/audit", h.getAuditEvents)
	h.mux.HandleFunc("GET /api/v1/admin/events/{event_id}/accreditation-export", h.exportAccreditation)
	h.mux.HandleFunc("GET /api/v1/admin/events/{event_id}/metrics", h.getOperationalMetrics)
}

func reportAuthorization(eventID uuid.UUID) authorizeFunc {
	return func(ctx context.Context, authorizer *auth.Authorizer, userID uuid.UUID) error {
		return authorizer.RequireHumanEventRole(ctx, userID, eventID, "EVENT_MANAGER", "BOX_OFFICE", "GATE_SUPERVISOR", "VIEWER")
	}
}

func auditAuthorization(eventID uuid.UUID) authorizeFunc {
	return func(ctx context.Context, authorizer *auth.Authorizer, userID uuid.UUID) error {
		return authorizer.RequireHumanEventRole(ctx, userID, eventID, "EVENT_MANAGER")
	}
}

func accreditationAuthorization(eventID uuid.UUID) authorizeFunc {
	return func(ctx context.Context, authorizer *auth.Authorizer, userID uuid.UUID) error {
		return authorizer.RequireHumanEventRole(ctx, userID, eventID, "EVENT_MANAGER", "BOX_OFFICE")
	}
}

func (h *Handler) authorizeReporting(w http.ResponseWriter, r *http.Request, authorization func(uuid.UUID) authorizeFunc) (uuid.UUID, bool) {
	eventID, err := publicid.Parse(r.PathValue("event_id"), publicid.Event)
	if err != nil {
		httpserver.WriteError(w, r, apierror.New(apierror.CodeValidation, "invalid Event identifier"))
		return uuid.Nil, false
	}
	if _, err := h.authorizeRead(r, authorization(eventID)); err != nil {
		httpserver.WriteError(w, r, err)
		return uuid.Nil, false
	}
	return eventID, true
}

func (h *Handler) getInventoryReport(w http.ResponseWriter, r *http.Request) {
	eventID, ok := h.authorizeReporting(w, r, reportAuthorization)
	if !ok {
		return
	}
	report, err := h.reporting.Inventory(r.Context(), eventID, nil)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, report)
}
func (h *Handler) getSalesReport(w http.ResponseWriter, r *http.Request) {
	eventID, ok := h.authorizeReporting(w, r, reportAuthorization)
	if !ok {
		return
	}
	report, err := h.reporting.Sales(r.Context(), eventID, nil)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, report)
}
func (h *Handler) getAdmissionReport(w http.ResponseWriter, r *http.Request) {
	eventID, ok := h.authorizeReporting(w, r, reportAuthorization)
	if !ok {
		return
	}
	report, err := h.reporting.Admissions(r.Context(), eventID)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, report)
}
func (h *Handler) getAuditEvents(w http.ResponseWriter, r *http.Request) {
	eventID, ok := h.authorizeReporting(w, r, auditAuthorization)
	if !ok {
		return
	}
	filter, err := parseAuditFilter(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	page, err := h.reporting.Audit(r.Context(), eventID, filter)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, page)
}

func parseAuditFilter(r *http.Request) (reporting.AuditFilter, error) {
	q := r.URL.Query()
	filter := reporting.AuditFilter{Operation: strings.TrimSpace(q.Get("operation")), EntityType: strings.TrimSpace(q.Get("entity_type")), ActorKind: strings.TrimSpace(q.Get("actor_kind")), Cursor: strings.TrimSpace(q.Get("cursor")), Search: strings.TrimSpace(q.Get("search"))}
	if raw := q.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 {
			return filter, apierror.New(apierror.CodeValidation, "audit limit must be a positive integer")
		}
		filter.Limit = limit
	}
	var err error
	if filter.ReservationID, err = parseOptionalPublicID(q.Get("reservation_id"), publicid.Reservation); err != nil {
		return filter, err
	}
	if filter.SaleID, err = parseOptionalPublicID(q.Get("sale_id"), publicid.Sale); err != nil {
		return filter, err
	}
	if filter.TicketID, err = parseOptionalPublicID(q.Get("ticket_id"), publicid.Ticket); err != nil {
		return filter, err
	}
	if raw := strings.TrimSpace(q.Get("correlation_id")); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return filter, apierror.New(apierror.CodeValidation, "invalid correlation identifier")
		}
		filter.CorrelationID = &id
	}
	if filter.From, err = parseOptionalTime(q.Get("from")); err != nil {
		return filter, err
	}
	if filter.To, err = parseOptionalTime(q.Get("to")); err != nil {
		return filter, err
	}
	return filter, nil
}
func parseOptionalPublicID(raw string, kind publicid.Kind) (*uuid.UUID, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	id, err := publicid.Parse(raw, kind)
	if err != nil {
		return nil, apierror.New(apierror.CodeValidation, "invalid audit resource identifier")
	}
	return &id, nil
}
func parseOptionalTime(raw string) (*time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, apierror.New(apierror.CodeValidation, "audit time filters must be RFC3339")
	}
	return &value, nil
}

func (h *Handler) exportAccreditation(w http.ResponseWriter, r *http.Request) {
	eventID, ok := h.authorizeReporting(w, r, accreditationAuthorization)
	if !ok {
		return
	}
	temporary, err := os.CreateTemp("", "tktsync-accreditation-*.csv")
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	name := temporary.Name()
	defer func() { _ = temporary.Close(); _ = os.Remove(name) }()
	snapshot, err := h.reporting.WriteAccreditationCSV(r.Context(), eventID, temporary)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	if _, err = temporary.Seek(0, io.SeekStart); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	filename := strings.ToLower(snapshot.Event.Name)
	filename = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, filename)
	filename = strings.Trim(strings.Join(strings.FieldsFunc(filename, func(r rune) bool { return r == '-' }), "-"), "-")
	if filename == "" {
		filename = "event"
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`-accreditation-`+snapshot.GeneratedAt.Format("2006-01-02")+`.csv"`)
	w.Header().Set("X-TktSync-Generated-At", snapshot.GeneratedAt.Format(time.RFC3339Nano))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, temporary)
}
func (h *Handler) getOperationalMetrics(w http.ResponseWriter, r *http.Request) {
	eventID, ok := h.authorizeReporting(w, r, reportAuthorization)
	if !ok {
		return
	}
	metrics, err := h.reporting.Metrics(r.Context(), eventID, h.metricsObserver)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, metrics)
}
