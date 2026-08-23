package admissionapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
)

func (h *Handler) authenticateHuman(r *http.Request) (auth.HumanPrincipal, error) {
	if h.humanAuth == nil {
		return auth.HumanPrincipal{}, apierror.New(apierror.CodeAuthorityTemporarilyUnavailable, "human authentication is not configured")
	}
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(header, "Bearer ") {
		return auth.HumanPrincipal{}, apierror.WithStatus(apierror.CodeNotAuthorized, "authentication is required", http.StatusUnauthorized)
	}
	principal, err := h.humanAuth(r.Context(), strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
	if err != nil {
		return auth.HumanPrincipal{}, apierror.WithStatus(apierror.CodeNotAuthorized, "authentication failed", http.StatusUnauthorized)
	}
	return principal, nil
}

func (h *Handler) events(w http.ResponseWriter, r *http.Request) {
	principal, err := h.authenticateHuman(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	authorizer := auth.NewAuthorizer(h.db)
	user, err := authorizer.ResolveHuman(r.Context(), principal)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	items, err := h.authorizedEvents(r.Context(), user.ID)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) authorizedEvents(ctx context.Context, userID uuid.UUID) ([]map[string]any, error) {
	rows, err := h.db.Query(ctx, `
		SELECT e.id,e.name,e.state,e.starts_at,e.ends_at,e.timezone_name,v.name,v.address_text
		FROM events e
		JOIN venues v ON v.id=e.venue_id
		WHERE EXISTS (
			SELECT 1 FROM platform_user_roles pur
			WHERE pur.user_id=$1 AND pur.role='PLATFORM_ADMIN'
		) OR EXISTS (
			SELECT 1 FROM event_staff_assignments esa
			WHERE esa.user_id=$1 AND esa.event_id=e.id AND esa.state='ACTIVE'
			AND esa.role=ANY($2::text[])
		)
		ORDER BY CASE WHEN e.state IN ('ON_SALE','SALES_CLOSED','PAUSED') THEN 0 ELSE 1 END,
			e.starts_at DESC NULLS LAST,e.name
		LIMIT 100`, userID, []string{"SCANNER", "GATE_SUPERVISOR", "EVENT_MANAGER"})
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var id uuid.UUID
		var name, state, venueName string
		var startsAt, endsAt *time.Time
		var timezoneName, addressText *string
		if err = rows.Scan(&id, &name, &state, &startsAt, &endsAt, &timezoneName, &venueName, &addressText); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{
			"id": publicid.Encode(publicid.Event, id), "name": name, "state": state,
			"starts_at": startsAt, "ends_at": endsAt, "timezone_name": timezoneName,
			"venue_name": venueName, "address_text": addressText,
		})
	}
	return items, rows.Err()
}
