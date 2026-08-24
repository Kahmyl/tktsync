package realtimeapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
	"github.com/tktsync/tktsync/backend/internal/selection"
)

type HumanAuthenticator func(context.Context, string) (auth.HumanPrincipal, error)
type SelectionAuthenticator func(context.Context, string) (selection.Session, error)

type Handler struct {
	db            *pgxpool.Pool
	humanAuth     HumanAuthenticator
	selectionAuth SelectionAuthenticator
	hub           *Hub
	heartbeat     time.Duration
	enabled       bool
}

func New(
	db *pgxpool.Pool,
	humanAuth HumanAuthenticator,
	selectionAuth SelectionAuthenticator,
	hub *Hub,
	enabled ...bool,
) *Handler {
	isEnabled := true
	if len(enabled) > 0 {
		isEnabled = enabled[0]
	}

	return &Handler{
		db:            db,
		humanAuth:     humanAuth,
		selectionAuth: selectionAuth,
		hub:           hub,
		heartbeat:     15 * time.Second,
		enabled:       isEnabled,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Path != "/api/v1/realtime/stream" {
		http.NotFound(w, r)
		return
	}
	h.stream(w, r)
}

func (h *Handler) stream(w http.ResponseWriter, r *http.Request) {
	if !h.enabled {
		httpserver.WriteError(
			w,
			r,
			apierror.New(
				apierror.CodeAuthorityTemporarilyUnavailable,
				"realtime streaming is disabled",
			),
		)
		return
	}

	if h.hub == nil {
		httpserver.WriteError(
			w,
			r,
			apierror.New(
				apierror.CodeAuthorityTemporarilyUnavailable,
				"realtime streaming is unavailable",
			),
		)
		return
	}

	eventID, err := publicid.Parse(
		strings.TrimSpace(r.URL.Query().Get("event_id")),
		publicid.Event,
	)
	if err != nil {
		httpserver.WriteError(
			w,
			r,
			apierror.New(apierror.CodeValidation, "event_id is invalid"),
		)
		return
	}

	audience := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("audience")))
	token, err := bearer(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	switch audience {
	case "admin", "scanner":
		if err = h.authorizeHuman(r, token, eventID, audience); err != nil {
			httpserver.WriteError(w, r, err)
			return
		}
	case "selection":
		if h.selectionAuth == nil {
			httpserver.WriteError(
				w,
				r,
				apierror.New(
					apierror.CodeAuthorityTemporarilyUnavailable,
					"selection authentication is not configured",
				),
			)
			return
		}
		session, authErr := h.selectionAuth(r.Context(), token)
		if authErr != nil || session.EventID != eventID {
			httpserver.WriteError(
				w,
				r,
				apierror.WithStatus(
					apierror.CodeNotAuthorized,
					"selection capability is invalid or expired",
					http.StatusUnauthorized,
				),
			)
			return
		}
	default:
		httpserver.WriteError(
			w,
			r,
			apierror.New(
				apierror.CodeValidation,
				"audience must be admin, scanner, or selection",
			),
		)
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

	events, unsubscribe := h.hub.Subscribe(eventID)
	defer unsubscribe()

	writeSSE(w, "resync", map[string]any{
		"reason":      "connected",
		"event_id":    publicid.Encode(publicid.Event, eventID),
		"server_time": time.Now().UTC(),
	})
	flusher.Flush()

	heartbeat := time.NewTicker(h.heartbeat)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		case fact := <-events:
			if fact.Resync {
				writeSSE(w, "resync", map[string]any{
					"reason":   "subscriber_backpressure",
					"event_id": publicid.Encode(publicid.Event, eventID),
				})
				flusher.Flush()
				continue
			}

			switch audience {
			case "scanner":
				if !strings.HasPrefix(fact.FactType, "event.") {
					continue
				}
				writeHumanInvalidation(w, fact)
			case "admin":
				writeHumanInvalidation(w, fact)
			case "selection":
				eventType := "availability.changed"
				if strings.HasPrefix(fact.FactType, "event.") {
					eventType = "event.changed"
				}
				writeSSE(w, "invalidate", map[string]any{
					"type":     eventType,
					"event_id": publicid.Encode(publicid.Event, fact.EventID),
				})
			}

			flusher.Flush()
		}
	}
}

func (h *Handler) authorizeHuman(
	r *http.Request,
	token string,
	eventID uuid.UUID,
	audience string,
) error {
	if h.humanAuth == nil {
		return apierror.New(
			apierror.CodeAuthorityTemporarilyUnavailable,
			"human authentication is not configured",
		)
	}

	principal, err := h.humanAuth(r.Context(), token)
	if err != nil {
		return apierror.WithStatus(
			apierror.CodeNotAuthorized,
			"authentication failed",
			http.StatusUnauthorized,
		)
	}

	roles := []string{"EVENT_MANAGER", "BOX_OFFICE", "GATE_SUPERVISOR", "VIEWER"}
	if audience == "scanner" {
		roles = []string{"SCANNER", "GATE_SUPERVISOR", "EVENT_MANAGER"}
	}

	authorizer := auth.NewAuthorizer(h.db)
	user, err := authorizer.ResolveHuman(r.Context(), principal)
	if err != nil {
		return err
	}

	return authorizer.RequireHumanEventRole(
		r.Context(),
		user.ID,
		eventID,
		roles...,
	)
}

func bearer(r *http.Request) (string, error) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(header, "Bearer ") {
		return "", apierror.WithStatus(
			apierror.CodeNotAuthorized,
			"authentication is required",
			http.StatusUnauthorized,
		)
	}

	token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	if token == "" {
		return "", apierror.WithStatus(
			apierror.CodeNotAuthorized,
			"authentication is required",
			http.StatusUnauthorized,
		)
	}

	return token, nil
}

func writeHumanInvalidation(w http.ResponseWriter, fact Fact) {
	body := map[string]any{
		"id":       publicid.Encode(publicid.EventFact, fact.FactID),
		"type":     fact.FactType,
		"event_id": publicid.Encode(publicid.Event, fact.EventID),
	}
	writeSSE(w, "invalidate", body)
}

func writeSSE(w http.ResponseWriter, event string, value any) {
	raw, _ := json.Marshal(value)
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw)
}
