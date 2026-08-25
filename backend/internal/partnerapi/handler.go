package partnerapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/inventory"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
	"github.com/tktsync/tktsync/backend/internal/reporting"
	"github.com/tktsync/tktsync/backend/internal/reservation"
	"github.com/tktsync/tktsync/backend/internal/selection"
)

type Dependencies struct {
	Database              *pgxpool.Pool
	PartnerAuth           *auth.PartnerAuthenticator
	Availability          *inventory.Service
	Transactions          *database.Runner
	Reservation           *reservation.Service
	Selection             *selection.Service
	Reporting             *reporting.Service
	TicketQRPublicBaseURL string
}

type Handler struct {
	db                    *pgxpool.Pool
	partnerAuth           *auth.PartnerAuthenticator
	availability          *inventory.Service
	transactions          *database.Runner
	reservation           *reservation.Service
	selection             *selection.Service
	reporting             *reporting.Service
	ticketQRPublicBaseURL string
	mux                   *http.ServeMux
}

func New(
	deps Dependencies,
) (*Handler, error) {
	if deps.Database == nil ||
		deps.PartnerAuth == nil ||
		deps.Availability == nil {
		return nil, errors.New(
			"Partner API dependencies are incomplete",
		)
	}

	if (deps.Transactions == nil) !=
		(deps.Reservation == nil) {
		return nil, errors.New(
			"Partner Reservation API dependencies must be configured together",
		)
	}

	ticketQRPublicBaseURL := strings.TrimRight(
		strings.TrimSpace(deps.TicketQRPublicBaseURL),
		"/",
	)
	if ticketQRPublicBaseURL == "" {
		ticketQRPublicBaseURL = "http://localhost:8080"
	}

	h := &Handler{
		db:                    deps.Database,
		partnerAuth:           deps.PartnerAuth,
		availability:          deps.Availability,
		transactions:          deps.Transactions,
		reservation:           deps.Reservation,
		selection:             deps.Selection,
		reporting:             deps.Reporting,
		ticketQRPublicBaseURL: ticketQRPublicBaseURL,
		mux:                   http.NewServeMux(),
	}
	if h.reporting == nil {
		h.reporting = reporting.NewService(deps.Database)
	}

	h.registerRoutes()

	return h, nil
}

func (h *Handler) ServeHTTP(
	w http.ResponseWriter,
	r *http.Request,
) {
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) registerRoutes() {
	h.mux.HandleFunc(
		"GET /api/v1/partner/events/{event_id}",
		h.getEvent,
	)

	h.mux.HandleFunc(
		"GET /api/v1/partner/events/{event_id}/layout",
		h.getLayout,
	)

	h.mux.HandleFunc(
		"GET /api/v1/partner/events/{event_id}/availability",
		h.getAvailability,
	)
	h.mux.HandleFunc("GET /api/v1/partner/events/{event_id}/reports/inventory", h.getInventoryReport)
	h.mux.HandleFunc("GET /api/v1/partner/events/{event_id}/reports/sales", h.getSalesReport)
	h.mux.HandleFunc("GET /api/v1/partner/events/{event_id}/activity", h.getActivity)

	if h.reservation != nil {
		if h.selection != nil {
			h.mux.HandleFunc("POST /api/v1/partner/selection-sessions", h.createSelectionSession)
		}
		h.mux.HandleFunc(
			"POST /api/v1/partner/reservations",
			h.createReservation,
		)
		h.mux.HandleFunc(
			"GET /api/v1/partner/reservations/{reservation_id}",
			h.getReservation,
		)
		h.mux.HandleFunc(
			"PATCH /api/v1/partner/reservations/{reservation_id}",
			h.modifyReservation,
		)
		h.mux.HandleFunc(
			"POST /api/v1/partner/reservations/{reservation_id}/checkout",
			h.beginReservationCheckout,
		)
		h.mux.HandleFunc(
			"POST /api/v1/partner/reservations/{reservation_id}/payment-failure",
			h.reservationPaymentFailure,
		)
		h.mux.HandleFunc(
			"POST /api/v1/partner/reservations/{reservation_id}/confirm",
			h.confirmReservation,
		)

		h.mux.HandleFunc(
			"POST /api/v1/partner/reservations/{reservation_id}/release",
			h.releaseReservation,
		)

		h.mux.HandleFunc(
			"GET /api/v1/partner/tickets/{ticket_id}/credential",
			h.getTicketCredential,
		)
		h.mux.HandleFunc(
			"GET /api/v1/partner/tickets/{ticket_id}/qr",
			h.getTicketQR,
		)
		h.mux.HandleFunc(
			"GET /api/v1/ticket-qr/{capability}",
			h.getHostedTicketQR,
		)

		h.mux.HandleFunc(
			"POST /api/v1/partner/tickets/{ticket_id}/void",
			h.voidTicket,
		)

		h.mux.HandleFunc(
			"POST /api/v1/partner/tickets/{ticket_id}/credentials/reissue",
			h.reissueTicketCredential,
		)

		h.mux.HandleFunc(
			"POST /api/v1/partner/tickets/{ticket_id}/inventory/re-release",
			h.reReleaseTicketInventory,
		)
	}
}

func (h *Handler) authenticateEvent(
	r *http.Request,
	eventID uuid.UUID,
) (auth.PartnerPrincipal, error) {
	header := strings.TrimSpace(
		r.Header.Get("Authorization"),
	)

	const prefix = "Bearer "

	if !strings.HasPrefix(
		header,
		prefix,
	) {
		return auth.PartnerPrincipal{},
			apierror.WithStatus(
				apierror.CodeNotAuthorized,
				"authentication is required",
				http.StatusUnauthorized,
			)
	}

	raw := strings.TrimSpace(
		strings.TrimPrefix(
			header,
			prefix,
		),
	)

	if raw == "" {
		return auth.PartnerPrincipal{},
			apierror.WithStatus(
				apierror.CodeNotAuthorized,
				"authentication is required",
				http.StatusUnauthorized,
			)
	}

	principal, err :=
		h.partnerAuth.Authenticate(
			r.Context(),
			raw,
		)
	if err != nil {
		return auth.PartnerPrincipal{}, err
	}

	authorizer := auth.NewAuthorizer(
		h.db,
	)

	if err := authorizer.RequirePartnerEventAccess(
		r.Context(),
		principal,
		eventID,
	); err != nil {
		return auth.PartnerPrincipal{}, err
	}

	return principal, nil
}

func (h *Handler) getEvent(
	w http.ResponseWriter,
	r *http.Request,
) {
	eventID, err := parseEventID(
		r.PathValue("event_id"),
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	if _, err := h.authenticateEvent(
		r,
		eventID,
	); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	var (
		name             string
		state            string
		startsAt         any
		endsAt           any
		salesOpenAt      any
		salesCloseAt     any
		admissionOpenAt  any
		admissionCloseAt any
		timezoneName     *string
		serverTime       any
	)

	err = h.db.QueryRow(
		r.Context(),
		`
			SELECT
				name,
				state,
				starts_at,
				ends_at,
				sales_open_at,
				sales_close_at,
				admission_open_at,
				admission_close_at,
				timezone_name,
				clock_timestamp()
			FROM events
			WHERE id = $1
		`,
		eventID,
	).Scan(
		&name,
		&state,
		&startsAt,
		&endsAt,
		&salesOpenAt,
		&salesCloseAt,
		&admissionOpenAt,
		&admissionCloseAt,
		&timezoneName,
		&serverTime,
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	httpserver.WriteJSON(
		w,
		http.StatusOK,
		map[string]any{
			"id": publicid.Encode(
				publicid.Event,
				eventID,
			),
			"name":               name,
			"state":              state,
			"starts_at":          startsAt,
			"ends_at":            endsAt,
			"sales_open_at":      salesOpenAt,
			"sales_close_at":     salesCloseAt,
			"admission_open_at":  admissionOpenAt,
			"admission_close_at": admissionCloseAt,
			"timezone_name":      timezoneName,
			"server_time":        serverTime,
		},
	)
}

func (h *Handler) getLayout(
	w http.ResponseWriter,
	r *http.Request,
) {
	eventID, err := parseEventID(
		r.PathValue("event_id"),
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	if _, err := h.authenticateEvent(
		r,
		eventID,
	); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	var geometry []byte

	err = h.db.QueryRow(
		r.Context(),
		`
			SELECT
				COALESCE(
					snapshot_json -> 'geometry',
					'{}'::jsonb
				)
			FROM event_layout_snapshots
			WHERE event_id = $1
		`,
		eventID,
	).Scan(&geometry)
	if err != nil {
		if errors.Is(
			err,
			pgx.ErrNoRows,
		) {
			httpserver.WriteError(
				w,
				r,
				apierror.New(
					apierror.CodeResourceNotFound,
					"Event layout not found",
				),
			)
			return
		}

		httpserver.WriteError(w, r, err)
		return
	}

	sections, err := h.layoutSections(
		r.Context(),
		eventID,
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	reserved, err := h.layoutReserved(
		r.Context(),
		eventID,
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	gaPools, err := h.layoutGA(
		r.Context(),
		eventID,
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	httpserver.WriteJSON(
		w,
		http.StatusOK,
		map[string]any{
			"event_id": publicid.Encode(
				publicid.Event,
				eventID,
			),
			"geometry":       json.RawMessage(geometry),
			"sections":       sections,
			"reserved_units": reserved,
			"ga_pools":       gaPools,
		},
	)
}

func (h *Handler) layoutSections(
	ctx context.Context,
	eventID uuid.UUID,
) ([]map[string]any, error) {
	rows, err := h.db.Query(
		ctx,
		`
			SELECT
				id,
				snapshot_object_key,
				name,
				sort_order
			FROM event_sections
			WHERE event_id = $1
			ORDER BY sort_order, snapshot_object_key
		`,
		eventID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(
		[]map[string]any,
		0,
	)

	for rows.Next() {
		var (
			id        uuid.UUID
			objectKey string
			name      string
			sortOrder int
		)

		if err := rows.Scan(
			&id,
			&objectKey,
			&name,
			&sortOrder,
		); err != nil {
			return nil, err
		}

		result = append(
			result,
			map[string]any{
				"id": publicid.Encode(
					publicid.EventSection,
					id,
				),
				"object_key": objectKey,
				"name":       name,
				"sort_order": sortOrder,
			},
		)
	}

	return result, rows.Err()
}

func (h *Handler) layoutReserved(
	ctx context.Context,
	eventID uuid.UUID,
) ([]map[string]any, error) {
	rows, err := h.db.Query(
		ctx,
		`
			SELECT
				riu.id,
				riu.event_section_id,
				riu.snapshot_object_key,
				COALESCE(riu.row_label, ''),
				riu.seat_label,
				COALESCE(riu.table_label, ''),
				riu.display_label,
				pt.amount_minor,
				pt.currency
			FROM reserved_inventory_units riu
			JOIN event_sections es
			  ON es.id = riu.event_section_id
			JOIN event_price_tiers pt
			  ON pt.id = COALESCE(
			      riu.price_tier_override_id,
			      es.default_price_tier_id
			  )
			WHERE riu.event_id = $1
			ORDER BY es.sort_order,
			         riu.snapshot_object_key,
			         riu.id
		`,
		eventID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(
		[]map[string]any,
		0,
	)

	for rows.Next() {
		var (
			id          uuid.UUID
			sectionID   uuid.UUID
			objectKey   string
			row         string
			seat        string
			table       string
			display     string
			amountMinor int64
			currency    string
		)

		if err := rows.Scan(
			&id,
			&sectionID,
			&objectKey,
			&row,
			&seat,
			&table,
			&display,
			&amountMinor,
			&currency,
		); err != nil {
			return nil, err
		}

		result = append(
			result,
			map[string]any{
				"inventory_id": publicid.Encode(
					publicid.ReservedInventory,
					id,
				),
				"section_id": publicid.Encode(
					publicid.EventSection,
					sectionID,
				),
				"object_key":    objectKey,
				"row":           row,
				"seat":          seat,
				"table":         table,
				"display_label": display,
				"price": map[string]any{
					"amount_minor": amountMinor,
					"currency":     currency,
				},
			},
		)
	}

	return result, rows.Err()
}

func (h *Handler) layoutGA(
	ctx context.Context,
	eventID uuid.UUID,
) ([]map[string]any, error) {
	rows, err := h.db.Query(
		ctx,
		`
			SELECT
				gp.id,
				gp.event_section_id,
				gp.snapshot_object_key,
				gp.name,
				gp.capacity,
				pt.amount_minor,
				pt.currency
			FROM ga_inventory_pools gp
			JOIN event_price_tiers pt
			  ON pt.id = gp.price_tier_id
			WHERE gp.event_id = $1
			ORDER BY gp.snapshot_object_key, gp.id
		`,
		eventID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(
		[]map[string]any,
		0,
	)

	for rows.Next() {
		var (
			id          uuid.UUID
			sectionID   uuid.UUID
			objectKey   string
			name        string
			capacity    int
			amountMinor int64
			currency    string
		)

		if err := rows.Scan(
			&id,
			&sectionID,
			&objectKey,
			&name,
			&capacity,
			&amountMinor,
			&currency,
		); err != nil {
			return nil, err
		}

		result = append(
			result,
			map[string]any{
				"inventory_id": publicid.Encode(
					publicid.GAPool,
					id,
				),
				"section_id": publicid.Encode(
					publicid.EventSection,
					sectionID,
				),
				"object_key": objectKey,
				"name":       name,
				"capacity":   capacity,
				"price": map[string]any{
					"amount_minor": amountMinor,
					"currency":     currency,
				},
			},
		)
	}

	return result, rows.Err()
}

func (h *Handler) getAvailability(
	w http.ResponseWriter,
	r *http.Request,
) {
	eventID, err := parseEventID(
		r.PathValue("event_id"),
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	principal, err := h.authenticateEvent(
		r,
		eventID,
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	result, err :=
		h.availability.PartnerAvailability(
			r.Context(),
			principal.PartnerID,
			eventID,
		)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	reserved := make(
		[]map[string]any,
		0,
		len(result.ReservedUnits),
	)

	for _, item := range result.ReservedUnits {
		entry := map[string]any{
			"inventory_id": publicid.Encode(
				publicid.ReservedInventory,
				item.InventoryID,
			),
			"section_id": publicid.Encode(
				publicid.EventSection,
				item.SectionID,
			),
			"row":         item.Row,
			"seat":        item.Seat,
			"sellability": item.Sellability,
		}

		if item.Offer != nil {
			entry["offer"] = map[string]any{
				"offer_id": item.Offer.OfferID,
				"price": map[string]any{
					"amount_minor": item.Offer.Price.
						AmountMinor,
					"currency": item.Offer.Price.
						Currency,
				},
			}
		}

		reserved = append(
			reserved,
			entry,
		)
	}

	gaPools := make(
		[]map[string]any,
		0,
		len(result.GAPools),
	)

	for _, pool := range result.GAPools {
		offers := make(
			[]map[string]any,
			0,
			len(pool.Offers),
		)

		for _, offer := range pool.Offers {
			offers = append(
				offers,
				map[string]any{
					"offer_id":           offer.OfferID,
					"available_quantity": offer.AvailableQuantity,
					"price": map[string]any{
						"amount_minor": offer.Price.
							AmountMinor,
						"currency": offer.Price.
							Currency,
					},
				},
			)
		}

		gaPools = append(
			gaPools,
			map[string]any{
				"inventory_id": publicid.Encode(
					publicid.GAPool,
					pool.InventoryID,
				),
				"name":   pool.Name,
				"offers": offers,
			},
		)
	}

	httpserver.WriteJSON(
		w,
		http.StatusOK,
		map[string]any{
			"event_id": publicid.Encode(
				publicid.Event,
				result.EventID,
			),
			"as_of":          result.AsOf,
			"server_time":    result.ServerTime,
			"reserved_units": reserved,
			"ga_pools":       gaPools,
		},
	)
}

func parseEventID(
	value string,
) (uuid.UUID, error) {
	id, err := publicid.Parse(
		value,
		publicid.Event,
	)
	if err != nil {
		return uuid.Nil, apierror.New(
			apierror.CodeValidation,
			"event_id is invalid",
		)
	}

	return id, nil
}
