package realtimeapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
)

type HumanAuthenticator func(context.Context, string) (auth.HumanPrincipal, error)
type Handler struct {
	db        *pgxpool.Pool
	humanAuth HumanAuthenticator
	heartbeat time.Duration
}

func New(db *pgxpool.Pool, humanAuth HumanAuthenticator) *Handler {
	return &Handler{db: db, humanAuth: humanAuth, heartbeat: 15 * time.Second}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Path != "/api/v1/realtime/stream" {
		http.NotFound(w, r)
		return
	}
	h.stream(w, r)
}

func (h *Handler) stream(w http.ResponseWriter, r *http.Request) {
	if h.humanAuth == nil {
		httpserver.WriteError(w, r, apierror.New(apierror.CodeAuthorityTemporarilyUnavailable, "human authentication is not configured"))
		return
	}
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(header, "Bearer ") {
		httpserver.WriteError(w, r, apierror.WithStatus(apierror.CodeNotAuthorized, "authentication is required", http.StatusUnauthorized))
		return
	}
	principal, err := h.humanAuth(r.Context(), strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
	if err != nil {
		httpserver.WriteError(w, r, apierror.WithStatus(apierror.CodeNotAuthorized, "authentication failed", http.StatusUnauthorized))
		return
	}
	eventID, err := publicid.Parse(strings.TrimSpace(r.URL.Query().Get("event_id")), publicid.Event)
	if err != nil {
		httpserver.WriteError(w, r, apierror.New(apierror.CodeValidation, "event_id is invalid"))
		return
	}
	audience := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("audience")))
	roles := []string{"EVENT_MANAGER", "BOX_OFFICE", "GATE_SUPERVISOR", "VIEWER"}
	if audience == "scanner" {
		roles = []string{"SCANNER", "GATE_SUPERVISOR", "EVENT_MANAGER"}
	} else if audience != "admin" {
		httpserver.WriteError(w, r, apierror.New(apierror.CodeValidation, "audience must be admin or scanner"))
		return
	}
	authorizer := auth.NewAuthorizer(h.db)
	user, err := authorizer.ResolveHuman(r.Context(), principal)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	if err = authorizer.RequireHumanEventRole(r.Context(), user.ID, eventID, roles...); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpserver.WriteError(w, r, errors.New("streaming is unavailable"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	var sequence int64
	if err = h.db.QueryRow(r.Context(), `SELECT COALESCE(MAX(enqueue_sequence),0) FROM outbox_events WHERE processed_at IS NOT NULL`).Scan(&sequence); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	writeSSE(w, "resync", map[string]any{"reason": "connected", "server_time": time.Now().UTC()})
	flusher.Flush()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	heartbeat := time.NewTicker(h.heartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		case <-ticker.C:
			rows, queryErr := h.db.Query(r.Context(), `SELECT enqueue_sequence,fact_id,fact_type,created_at FROM outbox_events WHERE processed_at IS NOT NULL AND event_id=$1 AND enqueue_sequence>$2 ORDER BY enqueue_sequence LIMIT 100`, eventID, sequence)
			if queryErr != nil {
				return
			}
			for rows.Next() {
				var next int64
				var factID [16]byte
				var factType string
				var created time.Time
				if scanErr := rows.Scan(&next, &factID, &factType, &created); scanErr != nil {
					rows.Close()
					return
				}
				sequence = next
				if audience == "scanner" && !strings.HasPrefix(factType, "event.") {
					continue
				}
				writeSSE(w, "invalidate", map[string]any{"id": publicid.Encode(publicid.EventFact, factID), "type": factType, "event_id": publicid.Encode(publicid.Event, eventID), "occurred_at": created})
			}
			rows.Close()
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, event string, value any) {
	raw, _ := json.Marshal(value)
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw)
}
