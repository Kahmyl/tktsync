package adminapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
)

const (
	defaultAdminPageSize = 50
	maximumAdminPageSize = 100
)

func (h *Handler) registerReadModelRoutes() {
	h.mux.HandleFunc("GET /api/v1/admin/dashboard", h.getDashboard)
	h.mux.HandleFunc("GET /api/v1/admin/events", h.listEvents)
	h.mux.HandleFunc("GET /api/v1/admin/events/{event_id}/configuration", h.getEventConfiguration)
	h.mux.HandleFunc("GET /api/v1/admin/partners", h.listPartners)
	h.mux.HandleFunc("GET /api/v1/admin/partners/{partner_id}", h.getPartner)
	h.mux.HandleFunc("GET /api/v1/admin/tickets", h.listTickets)
	h.mux.HandleFunc("GET /api/v1/admin/tickets/{ticket_id}", h.getTicket)
	h.mux.HandleFunc("GET /api/v1/admin/admissions", h.listAdmissions)
	h.mux.HandleFunc("GET /api/v1/admin/webhook-endpoints", h.listAllWebhookEndpoints)
}

func adminPage(r *http.Request) (int, int, error) {
	limit := defaultAdminPageSize
	offset := 0
	var err error
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > maximumAdminPageSize {
			return 0, 0, apierror.New(apierror.CodeValidation, "limit must be between 1 and 100")
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		offset, err = strconv.Atoi(raw)
		if err != nil || offset < 0 {
			return 0, 0, apierror.New(apierror.CodeValidation, "offset must be zero or greater")
		}
	}
	return limit, offset, nil
}

func (h *Handler) requirePlatformRead(w http.ResponseWriter, r *http.Request) bool {
	if _, err := h.authorizeRead(r, platformAdminAuthorization); err != nil {
		httpserver.WriteError(w, r, err)
		return false
	}
	return true
}

func (h *Handler) getDashboard(w http.ResponseWriter, r *http.Request) {
	if !h.requirePlatformRead(w, r) {
		return
	}

	var activeEvents, ticketsSold, reservationsToday, checkinsToday int64
	err := h.db.QueryRow(r.Context(), `
		SELECT
			(SELECT count(*) FROM events WHERE state IN ('ON_SALE','PAUSED')),
			(SELECT count(*) FROM ticket_entitlements WHERE status='ACTIVE'),
			(SELECT count(*) FROM reservations WHERE created_at >= date_trunc('day', now())),
			(SELECT count(*) FROM admissions WHERE admitted_at >= date_trunc('day', now()))
	`).Scan(&activeEvents, &ticketsSold, &reservationsToday, &checkinsToday)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	rows, err := h.db.Query(r.Context(), `
		SELECT e.id,e.name,e.state,e.starts_at,v.name,
			COALESCE((SELECT count(*) FROM reserved_inventory_units riu WHERE riu.event_id=e.id),0)
			+ COALESCE((SELECT sum(gip.capacity) FROM ga_inventory_pools gip WHERE gip.event_id=e.id),0) AS capacity,
			COALESCE((SELECT count(*) FROM ticket_entitlements te WHERE te.event_id=e.id AND te.status='ACTIVE'),0) AS sold
		FROM events e JOIN venues v ON v.id=e.venue_id
		WHERE e.state NOT IN ('COMPLETED','CANCELLED')
		ORDER BY e.starts_at ASC NULLS LAST,e.created_at DESC
		LIMIT 5
	`)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	upcoming := make([]map[string]any, 0)
	for rows.Next() {
		var id uuid.UUID
		var name, state, venueName string
		var startsAt *time.Time
		var capacity, sold int64
		if err = rows.Scan(&id, &name, &state, &startsAt, &venueName, &capacity, &sold); err != nil {
			rows.Close()
			httpserver.WriteError(w, r, err)
			return
		}
		upcoming = append(upcoming, map[string]any{"id": publicid.Encode(publicid.Event, id), "name": name, "state": state, "starts_at": startsAt, "venue_name": venueName, "capacity": capacity, "sold": sold})
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	attentionRows, err := h.db.Query(r.Context(), `
		SELECT e.id,e.name,e.state,
			EXISTS(SELECT 1 FROM event_layout_snapshots els WHERE els.event_id=e.id AND els.finalized_at IS NOT NULL),
			EXISTS(SELECT 1 FROM event_price_tiers ept WHERE ept.event_id=e.id AND ept.state='ACTIVE'),
			EXISTS(SELECT 1 FROM event_transaction_policies etp WHERE etp.event_id=e.id)
		FROM events e
		WHERE e.state='DRAFT' AND (
			NOT EXISTS(SELECT 1 FROM event_layout_snapshots els WHERE els.event_id=e.id AND els.finalized_at IS NOT NULL)
			OR NOT EXISTS(SELECT 1 FROM event_price_tiers ept WHERE ept.event_id=e.id AND ept.state='ACTIVE')
			OR NOT EXISTS(SELECT 1 FROM event_transaction_policies etp WHERE etp.event_id=e.id)
		)
		ORDER BY e.starts_at ASC NULLS LAST,e.created_at DESC
		LIMIT 5
	`)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	attention := make([]map[string]any, 0)
	for attentionRows.Next() {
		var id uuid.UUID
		var name, state string
		var layout, pricing, policy bool
		if err = attentionRows.Scan(&id, &name, &state, &layout, &pricing, &policy); err != nil {
			attentionRows.Close()
			httpserver.WriteError(w, r, err)
			return
		}
		attention = append(attention, map[string]any{"event_id": publicid.Encode(publicid.Event, id), "event_name": name, "state": state, "layout_ready": layout, "pricing_ready": pricing, "policy_ready": policy})
	}
	attentionRows.Close()
	if err = attentionRows.Err(); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	activityRows, err := h.db.Query(r.Context(), `
		SELECT ae.operation,ae.entity_type,ae.occurred_at,e.id,e.name,p.id,p.name
		FROM audit_events ae
		LEFT JOIN events e ON e.id=ae.event_id
		LEFT JOIN partners p ON p.id=ae.partner_id
		ORDER BY ae.occurred_at DESC,ae.id DESC
		LIMIT 8
	`)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	activity := make([]map[string]any, 0)
	for activityRows.Next() {
		var operation, entityType string
		var occurredAt time.Time
		var eventID, partnerID *uuid.UUID
		var eventName, partnerName *string
		if err = activityRows.Scan(&operation, &entityType, &occurredAt, &eventID, &eventName, &partnerID, &partnerName); err != nil {
			activityRows.Close()
			httpserver.WriteError(w, r, err)
			return
		}
		item := map[string]any{"operation": operation, "entity_type": entityType, "occurred_at": occurredAt, "event_name": eventName, "partner_name": partnerName}
		if eventID != nil {
			item["event_id"] = publicid.Encode(publicid.Event, *eventID)
		}
		if partnerID != nil {
			item["partner_id"] = publicid.Encode(publicid.Partner, *partnerID)
		}
		activity = append(activity, item)
	}
	activityRows.Close()
	if err = activityRows.Err(); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, map[string]any{
		"metrics":         map[string]any{"active_events": activeEvents, "tickets_sold": ticketsSold, "reservations_today": reservationsToday, "checkins_today": checkinsToday},
		"upcoming_events": upcoming,
		"attention":       attention,
		"recent_activity": activity,
	})
}

func (h *Handler) listEvents(w http.ResponseWriter, r *http.Request) {
	if !h.requirePlatformRead(w, r) {
		return
	}
	limit, offset, err := adminPage(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	state := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("state")))
	rows, err := h.db.Query(r.Context(), `
		SELECT e.id,e.venue_id,e.name,e.state,e.starts_at,e.ends_at,e.timezone_name,e.created_at,e.updated_at,v.name,
			COALESCE((SELECT count(*) FROM reserved_inventory_units riu WHERE riu.event_id=e.id),0)
			+ COALESCE((SELECT sum(gip.capacity) FROM ga_inventory_pools gip WHERE gip.event_id=e.id),0) AS capacity,
			COALESCE((SELECT count(*) FROM ticket_entitlements te WHERE te.event_id=e.id AND te.status='ACTIVE'),0) AS sold,
			count(*) OVER()
		FROM events e JOIN venues v ON v.id=e.venue_id
		WHERE ($1='' OR e.name ILIKE '%'||$1||'%' OR v.name ILIKE '%'||$1||'%')
			AND ($2='' OR e.state=$2)
		ORDER BY e.starts_at DESC NULLS LAST,e.created_at DESC,e.id
		LIMIT $3 OFFSET $4
	`, query, state, limit, offset)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	total := int64(0)
	for rows.Next() {
		var id, venueID uuid.UUID
		var name, state, venueName string
		var startsAt, endsAt *time.Time
		var timezoneName *string
		var createdAt, updatedAt time.Time
		var capacity, sold int64
		if err = rows.Scan(&id, &venueID, &name, &state, &startsAt, &endsAt, &timezoneName, &createdAt, &updatedAt, &venueName, &capacity, &sold, &total); err != nil {
			httpserver.WriteError(w, r, err)
			return
		}
		items = append(items, map[string]any{"id": publicid.Encode(publicid.Event, id), "venue_id": publicid.Encode(publicid.Venue, venueID), "venue_name": venueName, "name": name, "state": state, "starts_at": startsAt, "ends_at": endsAt, "timezone_name": timezoneName, "capacity": capacity, "sold": sold, "created_at": createdAt, "updated_at": updatedAt})
	}
	if err = rows.Err(); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
}

func (h *Handler) getEventConfiguration(w http.ResponseWriter, r *http.Request) {
	eventID, err := parsePublicID(r.PathValue("event_id"), publicid.Event, "event_id")
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	if _, err = h.authorizeRead(r, eventReadAuthorization(eventID)); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	result := map[string]any{"price_tiers": []map[string]any{}, "partner_access": []map[string]any{}}
	var layoutID *uuid.UUID
	var layoutVersion *int
	var finalizedAt *time.Time
	err = h.db.QueryRow(r.Context(), `SELECT els.source_layout_version_id,vlv.version_number,els.finalized_at FROM event_layout_snapshots els JOIN venue_layout_versions vlv ON vlv.id=els.source_layout_version_id WHERE els.event_id=$1`, eventID).Scan(&layoutID, &layoutVersion, &finalizedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		httpserver.WriteError(w, r, err)
		return
	}
	if layoutID != nil {
		result["layout"] = map[string]any{"id": publicid.Encode(publicid.VenueLayout, *layoutID), "version_number": layoutVersion, "finalized_at": finalizedAt}
	}

	tierRows, err := h.db.Query(r.Context(), `SELECT id,code,name,amount_minor,currency,state,created_at,updated_at FROM event_price_tiers WHERE event_id=$1 ORDER BY created_at,id`, eventID)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	tiers := make([]map[string]any, 0)
	for tierRows.Next() {
		var id uuid.UUID
		var code, name, currency, state string
		var amount int64
		var createdAt, updatedAt time.Time
		if err = tierRows.Scan(&id, &code, &name, &amount, &currency, &state, &createdAt, &updatedAt); err != nil {
			tierRows.Close()
			httpserver.WriteError(w, r, err)
			return
		}
		tiers = append(tiers, map[string]any{"id": publicid.Encode(publicid.PriceTier, id), "code": code, "name": name, "amount_minor": amount, "currency": currency, "state": state, "created_at": createdAt, "updated_at": updatedAt})
	}
	tierRows.Close()
	if err = tierRows.Err(); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	result["price_tiers"] = tiers

	accessRows, err := h.db.Query(r.Context(), `SELECT p.id,p.name,p.state,pea.state,pea.created_at,pea.disabled_at FROM partners p LEFT JOIN partner_event_access pea ON pea.partner_id=p.id AND pea.event_id=$1 ORDER BY p.name,p.id`, eventID)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	access := make([]map[string]any, 0)
	for accessRows.Next() {
		var partnerID uuid.UUID
		var name, partnerState string
		var accessState *string
		var createdAt, disabledAt *time.Time
		if err = accessRows.Scan(&partnerID, &name, &partnerState, &accessState, &createdAt, &disabledAt); err != nil {
			accessRows.Close()
			httpserver.WriteError(w, r, err)
			return
		}
		access = append(access, map[string]any{"partner_id": publicid.Encode(publicid.Partner, partnerID), "partner_name": name, "partner_state": partnerState, "access_state": accessState, "created_at": createdAt, "disabled_at": disabledAt})
	}
	accessRows.Close()
	if err = accessRows.Err(); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	result["partner_access"] = access

	var policy map[string]any
	var hold, checkout, retry, reconciliation, lifetime, maxHold, perPartner, perBuyer int
	var allow bool
	err = h.db.QueryRow(r.Context(), `SELECT hold_duration_seconds,checkout_protection_seconds,payment_retry_seconds,reconciliation_seconds,max_reservation_lifetime_seconds,max_hold_quantity,max_active_reservations_per_partner,max_active_reservations_per_buyer_session,allow_voided_inventory_rerelease FROM event_transaction_policies WHERE event_id=$1`, eventID).Scan(&hold, &checkout, &retry, &reconciliation, &lifetime, &maxHold, &perPartner, &perBuyer, &allow)
	if err == nil {
		policy = map[string]any{"hold_duration_seconds": hold, "checkout_protection_seconds": checkout, "payment_retry_seconds": retry, "reconciliation_seconds": reconciliation, "max_reservation_lifetime_seconds": lifetime, "max_hold_quantity": maxHold, "max_active_reservations_per_partner": perPartner, "max_active_reservations_per_buyer_session": perBuyer, "allow_voided_inventory_rerelease": allow}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		httpserver.WriteError(w, r, err)
		return
	}
	result["transaction_policy"] = policy
	httpserver.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) listPartners(w http.ResponseWriter, r *http.Request) {
	if !h.requirePlatformRead(w, r) {
		return
	}
	limit, offset, err := adminPage(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	state := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("state")))
	rows, err := h.db.Query(r.Context(), `
		SELECT p.id,p.name,p.state,p.created_at,p.disabled_at,
			(SELECT count(*) FROM partner_event_access pea WHERE pea.partner_id=p.id AND pea.state='ACTIVE'),
			(SELECT count(*) FROM partner_credentials pc WHERE pc.partner_id=p.id AND pc.state='ACTIVE'),
			(SELECT count(*) FROM partner_webhook_endpoints pwe WHERE pwe.partner_id=p.id AND pwe.state='ACTIVE'),
			(SELECT max(ae.occurred_at) FROM audit_events ae WHERE ae.partner_id=p.id OR ae.actor_partner_id=p.id),
			count(*) OVER()
		FROM partners p
		WHERE ($1='' OR p.name ILIKE '%'||$1||'%') AND ($2='' OR p.state=$2)
		ORDER BY p.name,p.id LIMIT $3 OFFSET $4
	`, query, state, limit, offset)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	total := int64(0)
	for rows.Next() {
		var id uuid.UUID
		var name, state string
		var createdAt time.Time
		var disabledAt, lastActivityAt *time.Time
		var events, credentials, endpoints int64
		if err = rows.Scan(&id, &name, &state, &createdAt, &disabledAt, &events, &credentials, &endpoints, &lastActivityAt, &total); err != nil {
			httpserver.WriteError(w, r, err)
			return
		}
		items = append(items, map[string]any{"id": publicid.Encode(publicid.Partner, id), "name": name, "state": state, "active_event_count": events, "active_credential_count": credentials, "active_endpoint_count": endpoints, "last_activity_at": lastActivityAt, "created_at": createdAt, "disabled_at": disabledAt})
	}
	if err = rows.Err(); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
}

func (h *Handler) getPartner(w http.ResponseWriter, r *http.Request) {
	if !h.requirePlatformRead(w, r) {
		return
	}
	partnerID, err := parsePublicID(r.PathValue("partner_id"), publicid.Partner, "partner_id")
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	var name, state string
	var createdAt time.Time
	var disabledAt *time.Time
	err = h.db.QueryRow(r.Context(), `SELECT name,state,created_at,disabled_at FROM partners WHERE id=$1`, partnerID).Scan(&name, &state, &createdAt, &disabledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		httpserver.WriteError(w, r, apierror.New(apierror.CodeResourceNotFound, "partner not found"))
		return
	}
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	credentials := make([]map[string]any, 0)
	rows, err := h.db.Query(r.Context(), `SELECT id,key_id,state,created_at,last_used_at,revoked_at FROM partner_credentials WHERE partner_id=$1 ORDER BY created_at DESC,id`, partnerID)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	for rows.Next() {
		var id uuid.UUID
		var keyID, credentialState string
		var credentialCreated time.Time
		var lastUsedAt, revokedAt *time.Time
		if err = rows.Scan(&id, &keyID, &credentialState, &credentialCreated, &lastUsedAt, &revokedAt); err != nil {
			rows.Close()
			httpserver.WriteError(w, r, err)
			return
		}
		credentials = append(credentials, map[string]any{"id": publicid.Encode(publicid.PartnerCredential, id), "key_id": keyID, "state": credentialState, "created_at": credentialCreated, "last_used_at": lastUsedAt, "revoked_at": revokedAt})
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	access := make([]map[string]any, 0)
	rows, err = h.db.Query(r.Context(), `SELECT e.id,e.name,e.state,pea.state,pea.created_at,pea.disabled_at FROM partner_event_access pea JOIN events e ON e.id=pea.event_id WHERE pea.partner_id=$1 ORDER BY e.starts_at DESC NULLS LAST,e.created_at DESC`, partnerID)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	for rows.Next() {
		var eventID uuid.UUID
		var eventName, eventState, accessState string
		var accessCreated time.Time
		var accessDisabled *time.Time
		if err = rows.Scan(&eventID, &eventName, &eventState, &accessState, &accessCreated, &accessDisabled); err != nil {
			rows.Close()
			httpserver.WriteError(w, r, err)
			return
		}
		access = append(access, map[string]any{"event_id": publicid.Encode(publicid.Event, eventID), "event_name": eventName, "event_state": eventState, "state": accessState, "created_at": accessCreated, "disabled_at": accessDisabled})
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	activity := make([]map[string]any, 0)
	rows, err = h.db.Query(r.Context(), `SELECT operation,entity_type,occurred_at,event_id FROM audit_events WHERE partner_id=$1 OR actor_partner_id=$1 ORDER BY occurred_at DESC,id DESC LIMIT 20`, partnerID)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	for rows.Next() {
		var operation, entityType string
		var occurredAt time.Time
		var eventID *uuid.UUID
		if err = rows.Scan(&operation, &entityType, &occurredAt, &eventID); err != nil {
			rows.Close()
			httpserver.WriteError(w, r, err)
			return
		}
		item := map[string]any{"operation": operation, "entity_type": entityType, "occurred_at": occurredAt}
		if eventID != nil {
			item["event_id"] = publicid.Encode(publicid.Event, *eventID)
		}
		activity = append(activity, item)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"id": publicid.Encode(publicid.Partner, partnerID), "name": name, "state": state, "created_at": createdAt, "disabled_at": disabledAt, "credentials": credentials, "event_access": access, "activity": activity})
}

func (h *Handler) listTickets(w http.ResponseWriter, r *http.Request) {
	if !h.requirePlatformRead(w, r) {
		return
	}
	limit, offset, err := adminPage(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	state := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("state")))
	eventRaw := strings.TrimSpace(r.URL.Query().Get("event_id"))
	var eventID *uuid.UUID
	if eventRaw != "" {
		parsed, parseErr := publicid.Parse(eventRaw, publicid.Event)
		if parseErr != nil {
			httpserver.WriteError(w, r, apierror.New(apierror.CodeValidation, "invalid Event identifier"))
			return
		}
		eventID = &parsed
	}
	var ticketID *uuid.UUID
	textQuery := query
	if strings.HasPrefix(query, string(publicid.Ticket)+"_") {
		if parsed, parseErr := publicid.Parse(query, publicid.Ticket); parseErr == nil {
			ticketID = &parsed
			textQuery = ""
		}
	}
	items, total, err := h.ticketRows(r, eventID, ticketID, state, textQuery, limit, offset)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
}

func (h *Handler) getTicket(w http.ResponseWriter, r *http.Request) {
	ticketID, err := parsePublicID(r.PathValue("ticket_id"), publicid.Ticket, "ticket_id")
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	if !h.requirePlatformRead(w, r) {
		return
	}
	items, _, err := h.ticketRows(r, nil, &ticketID, "", "", 1, 0)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	if len(items) == 0 {
		httpserver.WriteError(w, r, apierror.New(apierror.CodeResourceNotFound, "ticket not found"))
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, items[0])
}

func (h *Handler) ticketRows(r *http.Request, eventID, ticketID *uuid.UUID, state, query string, limit, offset int) ([]map[string]any, int64, error) {
	rows, err := h.db.Query(r.Context(), `
		SELECT te.id,te.event_id,e.name,te.status,te.inventory_kind,te.created_at,te.voided_at,te.void_reason,
			tad.display_name,
			COALESCE(riu.display_label,gip.name),
			qc.id,qc.status,
			a.id,a.status,a.admitted_at,
			count(*) OVER()
		FROM ticket_entitlements te
		JOIN events e ON e.id=te.event_id
		LEFT JOIN ticket_attendee_details tad ON tad.ticket_entitlement_id=te.id
		LEFT JOIN reserved_inventory_units riu ON riu.id=te.reserved_inventory_unit_id
		LEFT JOIN ga_inventory_pools gip ON gip.id=te.ga_pool_id
		LEFT JOIN LATERAL (SELECT id,status FROM qr_credentials WHERE ticket_entitlement_id=te.id ORDER BY issued_at DESC,id DESC LIMIT 1) qc ON true
		LEFT JOIN LATERAL (SELECT id,status,admitted_at FROM admissions WHERE ticket_entitlement_id=te.id ORDER BY admitted_at DESC,id DESC LIMIT 1) a ON true
		WHERE ($1::uuid IS NULL OR te.event_id=$1)
			AND ($2::uuid IS NULL OR te.id=$2)
			AND ($3='' OR te.status=$3)
			AND ($4='' OR tad.display_name ILIKE '%'||$4||'%' OR e.name ILIKE '%'||$4||'%' OR COALESCE(riu.display_label,gip.name,'') ILIKE '%'||$4||'%')
		ORDER BY te.created_at DESC,te.id DESC LIMIT $5 OFFSET $6
	`, eventID, ticketID, state, query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	total := int64(0)
	for rows.Next() {
		var id, eventUUID uuid.UUID
		var eventName, status, inventoryKind string
		var createdAt time.Time
		var voidedAt *time.Time
		var voidReason, attendeeName, displayLabel *string
		var credentialID, admissionID *uuid.UUID
		var credentialState, admissionState *string
		var admittedAt *time.Time
		if err = rows.Scan(&id, &eventUUID, &eventName, &status, &inventoryKind, &createdAt, &voidedAt, &voidReason, &attendeeName, &displayLabel, &credentialID, &credentialState, &admissionID, &admissionState, &admittedAt, &total); err != nil {
			return nil, 0, err
		}
		item := map[string]any{"id": publicid.Encode(publicid.Ticket, id), "event_id": publicid.Encode(publicid.Event, eventUUID), "event_name": eventName, "status": status, "inventory_kind": inventoryKind, "attendee_name": attendeeName, "display_label": displayLabel, "credential_state": credentialState, "admission_state": admissionState, "admitted_at": admittedAt, "created_at": createdAt, "voided_at": voidedAt, "void_reason": voidReason}
		if credentialID != nil {
			item["credential_id"] = publicid.Encode(publicid.Credential, *credentialID)
		}
		if admissionID != nil {
			item["admission_id"] = publicid.Encode(publicid.Admission, *admissionID)
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (h *Handler) listAdmissions(w http.ResponseWriter, r *http.Request) {
	if !h.requirePlatformRead(w, r) {
		return
	}
	limit, offset, err := adminPage(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	eventRaw := strings.TrimSpace(r.URL.Query().Get("event_id"))
	var eventID *uuid.UUID
	if eventRaw != "" {
		parsed, parseErr := publicid.Parse(eventRaw, publicid.Event)
		if parseErr != nil {
			httpserver.WriteError(w, r, apierror.New(apierror.CodeValidation, "invalid Event identifier"))
			return
		}
		eventID = &parsed
	}
	rows, err := h.db.Query(r.Context(), `
		SELECT sa.id,sa.event_id,e.name,sa.result,sa.gate_reference,sa.occurred_at,
			te.id,tad.display_name,COALESCE(riu.display_label,gip.name),a.id,a.status,count(*) OVER()
		FROM scan_attempts sa
		JOIN events e ON e.id=sa.event_id
		LEFT JOIN ticket_entitlements te ON te.id=sa.ticket_entitlement_id
		LEFT JOIN ticket_attendee_details tad ON tad.ticket_entitlement_id=te.id
		LEFT JOIN reserved_inventory_units riu ON riu.id=te.reserved_inventory_unit_id
		LEFT JOIN ga_inventory_pools gip ON gip.id=te.ga_pool_id
		LEFT JOIN admissions a ON a.scan_attempt_id=sa.id
		WHERE ($1::uuid IS NULL OR sa.event_id=$1)
		ORDER BY sa.occurred_at DESC,sa.id DESC LIMIT $2 OFFSET $3
	`, eventID, limit, offset)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	total := int64(0)
	for rows.Next() {
		var scanID, eventUUID uuid.UUID
		var eventName, result string
		var gateReference *string
		var occurredAt time.Time
		var ticketID, admissionID *uuid.UUID
		var attendeeName, displayLabel, admissionState *string
		if err = rows.Scan(&scanID, &eventUUID, &eventName, &result, &gateReference, &occurredAt, &ticketID, &attendeeName, &displayLabel, &admissionID, &admissionState, &total); err != nil {
			httpserver.WriteError(w, r, err)
			return
		}
		item := map[string]any{"id": publicid.Encode(publicid.ScanAttempt, scanID), "event_id": publicid.Encode(publicid.Event, eventUUID), "event_name": eventName, "result": result, "gate_reference": gateReference, "attendee_name": attendeeName, "display_label": displayLabel, "admission_state": admissionState, "occurred_at": occurredAt}
		if ticketID != nil {
			item["ticket_id"] = publicid.Encode(publicid.Ticket, *ticketID)
		}
		if admissionID != nil {
			item["admission_id"] = publicid.Encode(publicid.Admission, *admissionID)
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
}

func (h *Handler) listAllWebhookEndpoints(w http.ResponseWriter, r *http.Request) {
	if !h.requirePlatformRead(w, r) {
		return
	}
	limit, offset, err := adminPage(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	rows, err := h.db.Query(r.Context(), `
		SELECT e.id,e.partner_id,p.name,e.url,e.state,e.created_at,
			COALESCE(array_agg(DISTINCT s.event_type ORDER BY s.event_type) FILTER(WHERE s.event_type IS NOT NULL),'{}'),
			count(DISTINCT d.id) FILTER(WHERE d.state='DELIVERED' AND d.delivered_at >= now()-interval '24 hours'),
			count(DISTINCT d.id) FILTER(WHERE d.state='DEAD_LETTER' AND d.dead_lettered_at >= now()-interval '24 hours'),
			max(COALESCE(d.delivered_at,d.dead_lettered_at,d.created_at)) FILTER(WHERE d.id IS NOT NULL),
			count(*) OVER()
		FROM partner_webhook_endpoints e
		JOIN partners p ON p.id=e.partner_id
		LEFT JOIN partner_webhook_subscriptions s ON s.webhook_endpoint_id=e.id
		LEFT JOIN webhook_deliveries d ON d.webhook_endpoint_id=e.id
		GROUP BY e.id,p.name
		ORDER BY e.created_at DESC,e.id DESC LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	total := int64(0)
	for rows.Next() {
		var id, partnerID uuid.UUID
		var partnerName, url, state string
		var createdAt time.Time
		var subscriptions []string
		var delivered, failed int64
		var lastDeliveryAt *time.Time
		if err = rows.Scan(&id, &partnerID, &partnerName, &url, &state, &createdAt, &subscriptions, &delivered, &failed, &lastDeliveryAt, &total); err != nil {
			httpserver.WriteError(w, r, err)
			return
		}
		items = append(items, map[string]any{"id": publicid.Encode(publicid.WebhookEndpoint, id), "partner_id": publicid.Encode(publicid.Partner, partnerID), "partner_name": partnerName, "url": url, "state": state, "subscriptions": subscriptions, "delivered_24h": delivered, "failed_24h": failed, "last_delivery_at": lastDeliveryAt, "created_at": createdAt})
	}
	if err = rows.Err(); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
}
