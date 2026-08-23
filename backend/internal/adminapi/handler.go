package adminapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	admissionsvc "github.com/tktsync/tktsync/backend/internal/admission"
	allocsvc "github.com/tktsync/tktsync/backend/internal/allocation"
	"github.com/tktsync/tktsync/backend/internal/auth"
	eventsvc "github.com/tktsync/tktsync/backend/internal/event"
	"github.com/tktsync/tktsync/backend/internal/idempotency"
	partnersvc "github.com/tktsync/tktsync/backend/internal/partner"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
	"github.com/tktsync/tktsync/backend/internal/reporting"
	"github.com/tktsync/tktsync/backend/internal/reservation"
	venuesvc "github.com/tktsync/tktsync/backend/internal/venue"
	webhooksvc "github.com/tktsync/tktsync/backend/internal/webhook"
)

const maxRequestBodyBytes = 1 << 20

type HumanAuthenticator func(
	context.Context,
	string,
) (auth.HumanPrincipal, error)

type Dependencies struct {
	Database           *pgxpool.Pool
	Transactions       *database.Runner
	HumanAuth          HumanAuthenticator
	VenueService       *venuesvc.Service
	EventService       *eventsvc.Service
	PartnerService     *partnersvc.Service
	AllocationService  *allocsvc.Service
	ReservationService *reservation.Service
	AdmissionService   *admissionsvc.Service
	WebhookService     *webhooksvc.Service
	ReplayProtector    *ReplayProtector
	ReportingService   *reporting.Service
	MetricsObserver    *reporting.Observer
}

type Handler struct {
	db              *pgxpool.Pool
	transactions    *database.Runner
	humanAuth       HumanAuthenticator
	venue           *venuesvc.Service
	event           *eventsvc.Service
	partner         *partnersvc.Service
	allocation      *allocsvc.Service
	reservation     *reservation.Service
	admission       *admissionsvc.Service
	webhook         *webhooksvc.Service
	replayProtector *ReplayProtector
	reporting       *reporting.Service
	metricsObserver *reporting.Observer
	idempotency     idempotency.Store
	mux             *http.ServeMux
}

func New(
	deps Dependencies,
) (*Handler, error) {
	if deps.Database == nil ||
		deps.Transactions == nil ||
		deps.VenueService == nil ||
		deps.EventService == nil ||
		deps.PartnerService == nil {
		return nil, errors.New(
			"admin API dependencies are incomplete",
		)
	}

	h := &Handler{
		db:              deps.Database,
		transactions:    deps.Transactions,
		humanAuth:       deps.HumanAuth,
		venue:           deps.VenueService,
		event:           deps.EventService,
		partner:         deps.PartnerService,
		allocation:      deps.AllocationService,
		reservation:     deps.ReservationService,
		admission:       deps.AdmissionService,
		webhook:         deps.WebhookService,
		replayProtector: deps.ReplayProtector,
		reporting:       deps.ReportingService,
		metricsObserver: deps.MetricsObserver,
		mux:             http.NewServeMux(),
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
		"POST /api/v1/admin/venues",
		h.createVenue,
	)
	h.mux.HandleFunc(
		"GET /api/v1/admin/venues",
		h.listVenues,
	)
	h.mux.HandleFunc(
		"GET /api/v1/admin/venues/{venue_id}",
		h.getVenue,
	)
	h.mux.HandleFunc(
		"POST /api/v1/admin/venues/{venue_id}/layout-versions",
		h.createLayoutVersion,
	)
	h.mux.HandleFunc(
		"GET /api/v1/admin/venues/{venue_id}/layout-versions",
		h.listLayoutVersions,
	)
	h.mux.HandleFunc(
		"GET /api/v1/admin/venue-layouts/{layout_id}",
		h.getLayout,
	)
	h.mux.HandleFunc(
		"PATCH /api/v1/admin/venue-layouts/{layout_id}",
		h.replaceLayout,
	)
	h.mux.HandleFunc(
		"POST /api/v1/admin/venue-layouts/{layout_id}/publish",
		h.publishLayout,
	)

	h.mux.HandleFunc(
		"POST /api/v1/admin/events",
		h.createEvent,
	)
	h.mux.HandleFunc(
		"GET /api/v1/admin/events/{event_id}",
		h.getEvent,
	)
	h.mux.HandleFunc(
		"PATCH /api/v1/admin/events/{event_id}",
		h.updateEvent,
	)
	h.mux.HandleFunc(
		"POST /api/v1/admin/events/{event_id}/materialize-layout",
		h.materializeLayout,
	)
	h.mux.HandleFunc(
		"GET /api/v1/admin/events/{event_id}/inventory",
		h.getEventInventory,
	)
	h.mux.HandleFunc(
		"PUT /api/v1/admin/events/{event_id}/transaction-policy",
		h.configureTransactionPolicy,
	)
	h.mux.HandleFunc(
		"POST /api/v1/admin/events/{event_id}/price-tiers",
		h.createPriceTier,
	)
	h.mux.HandleFunc(
		"PATCH /api/v1/admin/events/{event_id}/price-tiers/{price_tier_id}",
		h.updatePriceTier,
	)
	h.mux.HandleFunc(
		"POST /api/v1/admin/events/{event_id}/pricing/assignments",
		h.assignPricing,
	)
	h.mux.HandleFunc(
		"POST /api/v1/admin/events/{event_id}/open-sales",
		h.openSales,
	)
	h.mux.HandleFunc("POST /api/v1/admin/events/{event_id}/pause-sales", h.pauseSales)
	h.mux.HandleFunc("POST /api/v1/admin/events/{event_id}/resume-sales", h.resumeSales)
	h.mux.HandleFunc("POST /api/v1/admin/events/{event_id}/close-sales", h.closeSales)
	h.mux.HandleFunc("POST /api/v1/admin/events/{event_id}/cancel", h.cancelEvent)
	h.mux.HandleFunc("POST /api/v1/admin/events/{event_id}/complete", h.completeEvent)

	h.mux.HandleFunc(
		"POST /api/v1/admin/partners",
		h.createPartner,
	)
	h.mux.HandleFunc(
		"POST /api/v1/admin/partners/{partner_id}/credentials",
		h.createPartnerCredential,
	)
	h.mux.HandleFunc(
		"POST /api/v1/admin/partner-credentials/{credential_id}/revoke",
		h.revokePartnerCredential,
	)
	h.mux.HandleFunc(
		"POST /api/v1/admin/partners/{partner_id}/disable",
		h.disablePartner,
	)
	h.mux.HandleFunc(
		"POST /api/v1/admin/partners/{partner_id}/enable",
		h.enablePartner,
	)
	h.mux.HandleFunc(
		"POST /api/v1/admin/events/{event_id}/partners/{partner_id}/access",
		h.enablePartnerEventAccess,
	)
	h.mux.HandleFunc(
		"POST /api/v1/admin/events/{event_id}/partners/{partner_id}/access/disable",
		h.disablePartnerEventAccess,
	)

	h.registerAllocationRoutes()
	h.registerTicketingRoutes()
	h.registerAdmissionRoutes()
	h.registerAsyncDeliveryRoutes()
	h.registerSelectionRoutes()
	h.registerReportingRoutes()
}

func (h *Handler) authenticate(
	r *http.Request,
) (auth.HumanPrincipal, error) {
	if h.humanAuth == nil {
		return auth.HumanPrincipal{}, apierror.New(
			apierror.CodeAuthorityTemporarilyUnavailable,
			"human authentication is not configured",
		)
	}

	header := strings.TrimSpace(
		r.Header.Get("Authorization"),
	)

	const prefix = "Bearer "

	if !strings.HasPrefix(header, prefix) {
		return auth.HumanPrincipal{}, apierror.New(
			apierror.CodeNotAuthorized,
			"authentication is required",
		)
	}

	token := strings.TrimSpace(
		strings.TrimPrefix(header, prefix),
	)
	if token == "" {
		return auth.HumanPrincipal{}, apierror.New(
			apierror.CodeNotAuthorized,
			"authentication is required",
		)
	}

	principal, err := h.humanAuth(
		r.Context(),
		token,
	)
	if err != nil {
		return auth.HumanPrincipal{}, apierror.New(
			apierror.CodeNotAuthorized,
			"authentication failed",
		)
	}

	return principal, nil
}

func platformAdminAuthorization(
	ctx context.Context,
	authorizer *auth.Authorizer,
	userID uuid.UUID,
) error {
	return authorizer.RequirePlatformAdmin(
		ctx,
		userID,
	)
}

func eventManagerAuthorization(
	eventID uuid.UUID,
) authorizeFunc {
	return func(
		ctx context.Context,
		authorizer *auth.Authorizer,
		userID uuid.UUID,
	) error {
		return authorizer.RequireHumanEventRole(
			ctx,
			userID,
			eventID,
			"EVENT_MANAGER",
		)
	}
}

func eventReadAuthorization(
	eventID uuid.UUID,
) authorizeFunc {
	return func(
		ctx context.Context,
		authorizer *auth.Authorizer,
		userID uuid.UUID,
	) error {
		return authorizer.RequireHumanEventRole(
			ctx,
			userID,
			eventID,
			"EVENT_MANAGER",
			"BOX_OFFICE",
			"GATE_SUPERVISOR",
			"SCANNER",
			"VIEWER",
		)
	}
}

func (h *Handler) authorizeRead(
	r *http.Request,
	authorize authorizeFunc,
) (uuid.UUID, error) {
	principal, err := h.authenticate(r)
	if err != nil {
		return uuid.Nil, err
	}

	authorizer := auth.NewAuthorizer(h.db)

	user, err := authorizer.ResolveHuman(
		r.Context(),
		principal,
	)
	if err != nil {
		return uuid.Nil, err
	}

	if authorize != nil {
		if err := authorize(
			r.Context(),
			authorizer,
			user.ID,
		); err != nil {
			return uuid.Nil, err
		}
	}

	return user.ID, nil
}

func (h *Handler) runMutation(
	w http.ResponseWriter,
	r *http.Request,
	operation string,
	pathIdentity string,
	requestBody any,
	authorize authorizeFunc,
	protectReplay bool,
	mutate mutationFunc,
) {
	principal, err := h.authenticate(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	canonicalBody, err := json.Marshal(requestBody)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	canonical := []byte(
		operation +
			"\n" +
			pathIdentity +
			"\n" +
			string(canonicalBody),
	)

	result, err := h.executeMutation(
		r.Context(),
		principal,
		strings.TrimSpace(
			r.Header.Get("Idempotency-Key"),
		),
		operation,
		canonical,
		authorize,
		protectReplay,
		mutate,
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	if protectReplay {
		w.Header().Set(
			"Cache-Control",
			"no-store",
		)
	}

	if len(result.Body) == 0 {
		result.Body = json.RawMessage(`{}`)
	}

	httpserver.WriteJSON(
		w,
		result.Status,
		result.Body,
	)
}

func decodeJSON(
	r *http.Request,
	target any,
) error {
	reader := io.LimitReader(
		r.Body,
		maxRequestBodyBytes+1,
	)

	raw, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	if len(raw) > maxRequestBodyBytes {
		return apierror.New(
			apierror.CodeValidation,
			"request body is too large",
		)
	}

	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte(`{}`)
	}

	decoder := json.NewDecoder(
		bytes.NewReader(raw),
	)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		return apierror.New(
			apierror.CodeValidation,
			"request body is invalid",
		)
	}

	if decoder.Decode(&struct{}{}) != io.EOF {
		return apierror.New(
			apierror.CodeValidation,
			"request body must contain one JSON value",
		)
	}

	return nil
}

func jsonResponse(
	status int,
	value any,
) (response, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return response{}, err
	}

	return response{
		Status: status,
		Body:   raw,
	}, nil
}

func rawJSON(
	raw []byte,
) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(raw)
}

func parsePublicID(
	value string,
	kind publicid.Kind,
	label string,
) (uuid.UUID, error) {
	id, err := publicid.Parse(
		value,
		kind,
	)
	if err != nil {
		return uuid.Nil, apierror.New(
			apierror.CodeValidation,
			label+" is invalid",
		)
	}

	return id, nil
}

func (h *Handler) createVenue(
	w http.ResponseWriter,
	r *http.Request,
) {
	var request createVenueRequest
	if err := decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	h.runMutation(
		w,
		r,
		"ADMIN_CREATE_VENUE",
		"",
		request,
		platformAdminAuthorization,
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			id, err := h.venue.CreateVenue(
				ctx,
				userID,
				venuesvc.CreateVenueInput{
					Name:        request.Name,
					AddressText: request.AddressText,
					Metadata:    request.Metadata,
				},
			)
			if err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusCreated,
				map[string]any{
					"id": publicid.Encode(
						publicid.Venue,
						id,
					),
				},
			)
		},
	)
}

func (h *Handler) listVenues(
	w http.ResponseWriter,
	r *http.Request,
) {
	if _, err := h.authorizeRead(
		r,
		platformAdminAuthorization,
	); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	rows, err := h.db.Query(
		r.Context(),
		`
			SELECT
				id,
				name,
				address_text,
				created_at,
				updated_at
			FROM venues
			ORDER BY name, id
		`,
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)

	for rows.Next() {
		var (
			id          uuid.UUID
			name        string
			addressText *string
			createdAt   any
			updatedAt   any
		)

		if err := rows.Scan(
			&id,
			&name,
			&addressText,
			&createdAt,
			&updatedAt,
		); err != nil {
			httpserver.WriteError(w, r, err)
			return
		}

		items = append(
			items,
			map[string]any{
				"id": publicid.Encode(
					publicid.Venue,
					id,
				),
				"name":         name,
				"address_text": addressText,
				"created_at":   createdAt,
				"updated_at":   updatedAt,
			},
		)
	}

	if err := rows.Err(); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	httpserver.WriteJSON(
		w,
		http.StatusOK,
		map[string]any{
			"venues": items,
		},
	)
}

func (h *Handler) getVenue(
	w http.ResponseWriter,
	r *http.Request,
) {
	if _, err := h.authorizeRead(
		r,
		platformAdminAuthorization,
	); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	id, err := parsePublicID(
		r.PathValue("venue_id"),
		publicid.Venue,
		"venue_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	result, err := h.venueResponse(
		r.Context(),
		id,
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	httpserver.WriteJSON(
		w,
		http.StatusOK,
		result,
	)
}

func (h *Handler) createLayoutVersion(
	w http.ResponseWriter,
	r *http.Request,
) {
	venueID, err := parsePublicID(
		r.PathValue("venue_id"),
		publicid.Venue,
		"venue_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	request := struct{}{}

	h.runMutation(
		w,
		r,
		"ADMIN_CREATE_LAYOUT_VERSION",
		venueID.String(),
		request,
		platformAdminAuthorization,
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			id, version, err :=
				h.venue.CreateLayoutVersion(
					ctx,
					userID,
					venueID,
				)
			if err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusCreated,
				map[string]any{
					"id": publicid.Encode(
						publicid.VenueLayout,
						id,
					),
					"venue_id": publicid.Encode(
						publicid.Venue,
						venueID,
					),
					"version_number": version,
					"state":          "DRAFT",
				},
			)
		},
	)
}

func (h *Handler) listLayoutVersions(
	w http.ResponseWriter,
	r *http.Request,
) {
	if _, err := h.authorizeRead(
		r,
		platformAdminAuthorization,
	); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	venueID, err := parsePublicID(
		r.PathValue("venue_id"),
		publicid.Venue,
		"venue_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	rows, err := h.db.Query(
		r.Context(),
		`
			SELECT
				id,
				version_number,
				state,
				published_at,
				retired_at,
				created_at
			FROM venue_layout_versions
			WHERE venue_id = $1
			ORDER BY version_number
		`,
		venueID,
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)

	for rows.Next() {
		var (
			id            uuid.UUID
			versionNumber int
			state         string
			publishedAt   any
			retiredAt     any
			createdAt     any
		)

		if err := rows.Scan(
			&id,
			&versionNumber,
			&state,
			&publishedAt,
			&retiredAt,
			&createdAt,
		); err != nil {
			httpserver.WriteError(w, r, err)
			return
		}

		items = append(
			items,
			map[string]any{
				"id": publicid.Encode(
					publicid.VenueLayout,
					id,
				),
				"version_number": versionNumber,
				"state":          state,
				"published_at":   publishedAt,
				"retired_at":     retiredAt,
				"created_at":     createdAt,
			},
		)
	}

	httpserver.WriteJSON(
		w,
		http.StatusOK,
		map[string]any{
			"layout_versions": items,
		},
	)
}

func (h *Handler) getLayout(
	w http.ResponseWriter,
	r *http.Request,
) {
	if _, err := h.authorizeRead(
		r,
		platformAdminAuthorization,
	); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	layoutID, err := parsePublicID(
		r.PathValue("layout_id"),
		publicid.VenueLayout,
		"layout_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	var (
		venueID     uuid.UUID
		version     int
		state       string
		geometry    []byte
		contentHash []byte
		publishedAt any
		retiredAt   any
		createdAt   any
	)

	err = h.db.QueryRow(
		r.Context(),
		`
			SELECT
				venue_id,
				version_number,
				state,
				geometry_json,
				content_hash,
				published_at,
				retired_at,
				created_at
			FROM venue_layout_versions
			WHERE id = $1
		`,
		layoutID,
	).Scan(
		&venueID,
		&version,
		&state,
		&geometry,
		&contentHash,
		&publishedAt,
		&retiredAt,
		&createdAt,
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
				publicid.VenueLayout,
				layoutID,
			),
			"venue_id": publicid.Encode(
				publicid.Venue,
				venueID,
			),
			"version_number": version,
			"state":          state,
			"geometry":       rawJSON(geometry),
			"content_hash":   contentHash,
			"published_at":   publishedAt,
			"retired_at":     retiredAt,
			"created_at":     createdAt,
		},
	)
}

func (h *Handler) replaceLayout(
	w http.ResponseWriter,
	r *http.Request,
) {
	layoutID, err := parsePublicID(
		r.PathValue("layout_id"),
		publicid.VenueLayout,
		"layout_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	var request replaceLayoutRequest
	if err := decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	sections := make(
		[]venuesvc.SectionInput,
		0,
		len(request.Sections),
	)
	for _, item := range request.Sections {
		sections = append(
			sections,
			venuesvc.SectionInput{
				ObjectKey: item.ObjectKey,
				Name:      item.Name,
				Kind:      item.Kind,
				SortOrder: item.SortOrder,
				Metadata:  item.Metadata,
			},
		)
	}

	rows := make(
		[]venuesvc.RowInput,
		0,
		len(request.Rows),
	)
	for _, item := range request.Rows {
		rows = append(
			rows,
			venuesvc.RowInput{
				ObjectKey:  item.ObjectKey,
				SectionKey: item.SectionKey,
				Label:      item.Label,
				SortOrder:  item.SortOrder,
				Metadata:   item.Metadata,
			},
		)
	}

	tables := make(
		[]venuesvc.TableInput,
		0,
		len(request.Tables),
	)
	for _, item := range request.Tables {
		tables = append(
			tables,
			venuesvc.TableInput{
				ObjectKey:  item.ObjectKey,
				SectionKey: item.SectionKey,
				Label:      item.Label,
				Metadata:   item.Metadata,
			},
		)
	}

	seats := make(
		[]venuesvc.SeatInput,
		0,
		len(request.Seats),
	)
	for _, item := range request.Seats {
		seats = append(
			seats,
			venuesvc.SeatInput{
				ObjectKey:  item.ObjectKey,
				SectionKey: item.SectionKey,
				RowKey:     item.RowKey,
				TableKey:   item.TableKey,
				SeatLabel:  item.SeatLabel,
				SortOrder:  item.SortOrder,
				Metadata:   item.Metadata,
			},
		)
	}

	gaZones := make(
		[]venuesvc.GAZoneInput,
		0,
		len(request.GAZones),
	)
	for _, item := range request.GAZones {
		gaZones = append(
			gaZones,
			venuesvc.GAZoneInput{
				ObjectKey:       item.ObjectKey,
				SectionKey:      item.SectionKey,
				Name:            item.Name,
				DefaultCapacity: item.DefaultCapacity,
				Metadata:        item.Metadata,
			},
		)
	}

	h.runMutation(
		w,
		r,
		"ADMIN_REPLACE_LAYOUT_DRAFT",
		layoutID.String(),
		request,
		platformAdminAuthorization,
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			err := h.venue.ReplaceDraftLayout(
				ctx,
				userID,
				layoutID,
				venuesvc.ReplaceLayoutInput{
					Geometry: request.Geometry,
					Sections: sections,
					Rows:     rows,
					Tables:   tables,
					Seats:    seats,
					GAZones:  gaZones,
				},
			)
			if err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusOK,
				map[string]any{
					"id": publicid.Encode(
						publicid.VenueLayout,
						layoutID,
					),
					"state": "DRAFT",
				},
			)
		},
	)
}

func (h *Handler) publishLayout(
	w http.ResponseWriter,
	r *http.Request,
) {
	layoutID, err := parsePublicID(
		r.PathValue("layout_id"),
		publicid.VenueLayout,
		"layout_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	request := struct{}{}

	h.runMutation(
		w,
		r,
		"ADMIN_PUBLISH_LAYOUT",
		layoutID.String(),
		request,
		platformAdminAuthorization,
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			if err := h.venue.PublishLayout(
				ctx,
				userID,
				layoutID,
			); err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusOK,
				map[string]any{
					"id": publicid.Encode(
						publicid.VenueLayout,
						layoutID,
					),
					"state": "PUBLISHED",
				},
			)
		},
	)
}

func (h *Handler) createEvent(
	w http.ResponseWriter,
	r *http.Request,
) {
	var request createEventRequest
	if err := decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	venueID, err := parsePublicID(
		request.VenueID,
		publicid.Venue,
		"venue_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	h.runMutation(
		w,
		r,
		"ADMIN_CREATE_EVENT",
		venueID.String(),
		request,
		platformAdminAuthorization,
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			id, err := h.event.Create(
				ctx,
				userID,
				eventsvc.CreateInput{
					VenueID:          venueID,
					Name:             request.Name,
					StartsAt:         request.StartsAt,
					EndsAt:           request.EndsAt,
					SalesOpenAt:      request.SalesOpenAt,
					SalesCloseAt:     request.SalesCloseAt,
					AdmissionOpenAt:  request.AdmissionOpenAt,
					AdmissionCloseAt: request.AdmissionCloseAt,
					TimezoneName:     request.TimezoneName,
				},
			)
			if err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusCreated,
				map[string]any{
					"id": publicid.Encode(
						publicid.Event,
						id,
					),
					"state": "DRAFT",
				},
			)
		},
	)
}

func (h *Handler) getEvent(
	w http.ResponseWriter,
	r *http.Request,
) {
	eventID, err := parsePublicID(
		r.PathValue("event_id"),
		publicid.Event,
		"event_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	if _, err := h.authorizeRead(
		r,
		eventReadAuthorization(eventID),
	); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	result, err := h.eventResponse(
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
		result,
	)
}

func (h *Handler) updateEvent(
	w http.ResponseWriter,
	r *http.Request,
) {
	eventID, err := parsePublicID(
		r.PathValue("event_id"),
		publicid.Event,
		"event_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	var request updateEventRequest
	if err := decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	h.runMutation(
		w,
		r,
		"ADMIN_UPDATE_EVENT_CONFIGURATION",
		eventID.String(),
		request,
		eventManagerAuthorization(eventID),
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			if err := h.event.UpdateConfiguration(
				ctx,
				userID,
				eventID,
				eventsvc.UpdateConfigurationInput{
					Name:             request.Name,
					StartsAt:         request.StartsAt,
					EndsAt:           request.EndsAt,
					SalesOpenAt:      request.SalesOpenAt,
					SalesCloseAt:     request.SalesCloseAt,
					AdmissionOpenAt:  request.AdmissionOpenAt,
					AdmissionCloseAt: request.AdmissionCloseAt,
					TimezoneName:     request.TimezoneName,
				},
			); err != nil {
				return response{}, err
			}

			value, err := h.eventResponse(
				ctx,
				eventID,
			)
			if err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusOK,
				value,
			)
		},
	)
}

func (h *Handler) materializeLayout(
	w http.ResponseWriter,
	r *http.Request,
) {
	eventID, err := parsePublicID(
		r.PathValue("event_id"),
		publicid.Event,
		"event_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	var request materializeLayoutRequest
	if err := decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	layoutID, err := parsePublicID(
		request.LayoutID,
		publicid.VenueLayout,
		"layout_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	h.runMutation(
		w,
		r,
		"ADMIN_MATERIALIZE_EVENT_LAYOUT",
		eventID.String()+":"+layoutID.String(),
		request,
		eventManagerAuthorization(eventID),
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			if err := h.event.MaterializeLayout(
				ctx,
				userID,
				eventID,
				layoutID,
			); err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusOK,
				map[string]any{
					"event_id": publicid.Encode(
						publicid.Event,
						eventID,
					),
					"layout_id": publicid.Encode(
						publicid.VenueLayout,
						layoutID,
					),
					"materialized": true,
				},
			)
		},
	)
}

func (h *Handler) getEventInventory(
	w http.ResponseWriter,
	r *http.Request,
) {
	eventID, err := parsePublicID(
		r.PathValue("event_id"),
		publicid.Event,
		"event_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	if _, err := h.authorizeRead(
		r,
		eventReadAuthorization(eventID),
	); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	rows, err := h.db.Query(
		r.Context(),
		`
			SELECT
				'RESERVED'::text,
				riu.id,
				riu.snapshot_object_key,
				riu.display_label,
				1::integer,
				COALESCE(
					riu.price_tier_override_id,
					es.default_price_tier_id
				)
			FROM reserved_inventory_units riu
			JOIN event_sections es
			  ON es.id = riu.event_section_id
			WHERE riu.event_id = $1

			UNION ALL

			SELECT
				'GA'::text,
				gp.id,
				gp.snapshot_object_key,
				gp.name,
				gp.capacity,
				gp.price_tier_id
			FROM ga_inventory_pools gp
			WHERE gp.event_id = $1

			ORDER BY 1, 3
		`,
		eventID,
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)

	for rows.Next() {
		var (
			kind        string
			id          uuid.UUID
			objectKey   string
			label       string
			quantity    int
			priceTierID *uuid.UUID
		)

		if err := rows.Scan(
			&kind,
			&id,
			&objectKey,
			&label,
			&quantity,
			&priceTierID,
		); err != nil {
			httpserver.WriteError(w, r, err)
			return
		}

		idKind := publicid.ReservedInventory
		if kind == "GA" {
			idKind = publicid.GAPool
		}

		var encodedPriceTier any
		if priceTierID != nil {
			encodedPriceTier = publicid.Encode(
				publicid.PriceTier,
				*priceTierID,
			)
		}

		items = append(
			items,
			map[string]any{
				"kind":                kind,
				"id":                  publicid.Encode(idKind, id),
				"snapshot_object_key": objectKey,
				"label":               label,
				"quantity":            quantity,
				"price_tier_id":       encodedPriceTier,
			},
		)
	}

	httpserver.WriteJSON(
		w,
		http.StatusOK,
		map[string]any{
			"event_id": publicid.Encode(
				publicid.Event,
				eventID,
			),
			"inventory": items,
		},
	)
}

func (h *Handler) configureTransactionPolicy(
	w http.ResponseWriter,
	r *http.Request,
) {
	eventID, err := parsePublicID(
		r.PathValue("event_id"),
		publicid.Event,
		"event_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	var request transactionPolicyRequest
	if err := decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	h.runMutation(
		w,
		r,
		"ADMIN_CONFIGURE_EVENT_TRANSACTION_POLICY",
		eventID.String(),
		request,
		eventManagerAuthorization(eventID),
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			err := h.event.ConfigureTransactionPolicy(
				ctx,
				userID,
				eventID,
				eventsvc.TransactionPolicyInput{
					HoldDurationSeconds:                  request.HoldDurationSeconds,
					CheckoutProtectionSeconds:            request.CheckoutProtectionSeconds,
					PaymentRetrySeconds:                  request.PaymentRetrySeconds,
					ReconciliationSeconds:                request.ReconciliationSeconds,
					MaxReservationLifetimeSeconds:        request.MaxReservationLifetimeSeconds,
					MaxHoldQuantity:                      request.MaxHoldQuantity,
					MaxActiveReservationsPerPartner:      request.MaxActiveReservationsPerPartner,
					MaxActiveReservationsPerBuyerSession: request.MaxActiveReservationsPerBuyerSession,
					AllowVoidedInventoryRerelease:        request.AllowVoidedInventoryRerelease,
				},
			)
			if err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusOK,
				map[string]any{
					"event_id": publicid.Encode(
						publicid.Event,
						eventID,
					),
					"configured": true,
				},
			)
		},
	)
}

func (h *Handler) createPriceTier(
	w http.ResponseWriter,
	r *http.Request,
) {
	eventID, err := parsePublicID(
		r.PathValue("event_id"),
		publicid.Event,
		"event_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	var request createPriceTierRequest
	if err := decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	h.runMutation(
		w,
		r,
		"ADMIN_CREATE_EVENT_PRICE_TIER",
		eventID.String(),
		request,
		eventManagerAuthorization(eventID),
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			id, err := h.event.CreatePriceTier(
				ctx,
				userID,
				eventID,
				eventsvc.PriceTierInput{
					Code:        request.Code,
					Name:        request.Name,
					AmountMinor: request.AmountMinor,
					Currency:    request.Currency,
				},
			)
			if err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusCreated,
				map[string]any{
					"id": publicid.Encode(
						publicid.PriceTier,
						id,
					),
				},
			)
		},
	)
}

func (h *Handler) updatePriceTier(
	w http.ResponseWriter,
	r *http.Request,
) {
	eventID, err := parsePublicID(
		r.PathValue("event_id"),
		publicid.Event,
		"event_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	priceTierID, err := parsePublicID(
		r.PathValue("price_tier_id"),
		publicid.PriceTier,
		"price_tier_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	var request updatePriceTierRequest
	if err := decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	h.runMutation(
		w,
		r,
		"ADMIN_UPDATE_EVENT_PRICE_TIER",
		eventID.String()+":"+priceTierID.String(),
		request,
		eventManagerAuthorization(eventID),
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			err := h.event.UpdatePriceTier(
				ctx,
				userID,
				eventID,
				priceTierID,
				eventsvc.UpdatePriceTierInput{
					Name:        request.Name,
					AmountMinor: request.AmountMinor,
					State:       request.State,
				},
			)
			if err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusOK,
				map[string]any{
					"id": publicid.Encode(
						publicid.PriceTier,
						priceTierID,
					),
					"updated": true,
				},
			)
		},
	)
}

func (h *Handler) assignPricing(
	w http.ResponseWriter,
	r *http.Request,
) {
	eventID, err := parsePublicID(
		r.PathValue("event_id"),
		publicid.Event,
		"event_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	var request pricingAssignmentRequest
	if err := decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	priceTierID, err := parsePublicID(
		request.PriceTierID,
		publicid.PriceTier,
		"price_tier_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	h.runMutation(
		w,
		r,
		"ADMIN_ASSIGN_EVENT_PRICING",
		eventID.String()+":"+priceTierID.String(),
		request,
		eventManagerAuthorization(eventID),
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			err := h.event.AssignPricing(
				ctx,
				userID,
				eventID,
				eventsvc.PricingAssignmentInput{
					PriceTierID:        priceTierID,
					SectionObjectKeys:  request.SectionObjectKeys,
					ReservedObjectKeys: request.ReservedObjectKeys,
					GAPoolObjectKeys:   request.GAPoolObjectKeys,
				},
			)
			if err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusOK,
				map[string]any{
					"event_id": publicid.Encode(
						publicid.Event,
						eventID,
					),
					"assigned": true,
				},
			)
		},
	)
}

func (h *Handler) openSales(
	w http.ResponseWriter,
	r *http.Request,
) {
	eventID, err := parsePublicID(
		r.PathValue("event_id"),
		publicid.Event,
		"event_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	request := struct{}{}

	h.runMutation(
		w,
		r,
		"ADMIN_OPEN_EVENT_SALES",
		eventID.String(),
		request,
		eventManagerAuthorization(eventID),
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			if err := h.event.OpenSales(
				ctx,
				userID,
				eventID,
			); err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusOK,
				map[string]any{
					"event_id": publicid.Encode(
						publicid.Event,
						eventID,
					),
					"state": "ON_SALE",
				},
			)
		},
	)
}

func (h *Handler) pauseSales(w http.ResponseWriter, r *http.Request) {
	h.runEventLifecycleCommand(w, r, "ADMIN_PAUSE_EVENT_SALES", "PAUSED", nil, func(ctx context.Context, actorID, eventID uuid.UUID) error {
		return h.event.PauseSales(ctx, actorID, eventID)
	})
}

func (h *Handler) resumeSales(w http.ResponseWriter, r *http.Request) {
	h.runEventLifecycleCommand(w, r, "ADMIN_RESUME_EVENT_SALES", "ON_SALE", nil, func(ctx context.Context, actorID, eventID uuid.UUID) error {
		return h.event.ResumeSales(ctx, actorID, eventID)
	})
}

func (h *Handler) closeSales(w http.ResponseWriter, r *http.Request) {
	h.runEventLifecycleCommand(w, r, "ADMIN_CLOSE_EVENT_SALES", "SALES_CLOSED", nil, func(ctx context.Context, actorID, eventID uuid.UUID) error {
		return h.event.CloseSales(ctx, actorID, eventID)
	})
}

func (h *Handler) completeEvent(w http.ResponseWriter, r *http.Request) {
	h.runEventLifecycleCommand(w, r, "ADMIN_COMPLETE_EVENT", "COMPLETED", nil, func(ctx context.Context, actorID, eventID uuid.UUID) error {
		return h.event.CompleteEvent(ctx, actorID, eventID)
	})
}

func (h *Handler) cancelEvent(w http.ResponseWriter, r *http.Request) {
	var request cancelEventRequest
	if err := decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" {
		httpserver.WriteError(w, r, apierror.New(apierror.CodeValidation, "reason is required"))
		return
	}
	h.runEventLifecycleCommand(w, r, "ADMIN_CANCEL_EVENT", "CANCELLED", request, func(ctx context.Context, actorID, eventID uuid.UUID) error {
		return h.event.CancelEvent(ctx, actorID, eventID, request.Reason)
	})
}

func (h *Handler) runEventLifecycleCommand(
	w http.ResponseWriter,
	r *http.Request,
	operation string,
	nextState string,
	request any,
	command func(context.Context, uuid.UUID, uuid.UUID) error,
) {
	eventID, err := parsePublicID(r.PathValue("event_id"), publicid.Event, "event_id")
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	if request == nil {
		request = struct{}{}
	}
	h.runMutation(w, r, operation, eventID.String(), request, eventManagerAuthorization(eventID), false, func(ctx context.Context, userID uuid.UUID) (response, error) {
		if err := command(ctx, userID, eventID); err != nil {
			return response{}, err
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"event_id": publicid.Encode(publicid.Event, eventID),
			"state":    nextState,
		})
	})
}

func (h *Handler) createPartner(
	w http.ResponseWriter,
	r *http.Request,
) {
	var request createPartnerRequest
	if err := decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	h.runMutation(
		w,
		r,
		"ADMIN_CREATE_PARTNER",
		"",
		request,
		platformAdminAuthorization,
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			id, err := h.partner.Create(
				ctx,
				userID,
				request.Name,
			)
			if err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusCreated,
				map[string]any{
					"id": publicid.Encode(
						publicid.Partner,
						id,
					),
					"state": "ACTIVE",
				},
			)
		},
	)
}

func (h *Handler) createPartnerCredential(
	w http.ResponseWriter,
	r *http.Request,
) {
	partnerID, err := parsePublicID(
		r.PathValue("partner_id"),
		publicid.Partner,
		"partner_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	request := struct{}{}

	h.runMutation(
		w,
		r,
		"ADMIN_CREATE_PARTNER_CREDENTIAL",
		partnerID.String(),
		request,
		platformAdminAuthorization,
		true,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			if h.replayProtector == nil {
				return response{}, apierror.New(
					apierror.
						CodeAuthorityTemporarilyUnavailable,
					"credential replay protection is not configured",
				)
			}

			credentialID, rawCredential, err :=
				h.partner.CreateCredential(
					ctx,
					userID,
					partnerID,
				)
			if err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusCreated,
				map[string]any{
					"id": publicid.Encode(
						publicid.PartnerCredential,
						credentialID,
					),
					"partner_id": publicid.Encode(
						publicid.Partner,
						partnerID,
					),
					"credential": rawCredential,
				},
			)
		},
	)
}

func (h *Handler) revokePartnerCredential(
	w http.ResponseWriter,
	r *http.Request,
) {
	credentialID, err := parsePublicID(
		r.PathValue("credential_id"),
		publicid.PartnerCredential,
		"credential_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	request := struct{}{}

	h.runMutation(
		w,
		r,
		"ADMIN_REVOKE_PARTNER_CREDENTIAL",
		credentialID.String(),
		request,
		platformAdminAuthorization,
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			if err := h.partner.RevokeCredential(
				ctx,
				userID,
				credentialID,
			); err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusOK,
				map[string]any{
					"id": publicid.Encode(
						publicid.PartnerCredential,
						credentialID,
					),
					"state": "REVOKED",
				},
			)
		},
	)
}

func (h *Handler) disablePartner(
	w http.ResponseWriter,
	r *http.Request,
) {
	h.setPartnerEnabled(w, r, false)
}

func (h *Handler) enablePartner(
	w http.ResponseWriter,
	r *http.Request,
) {
	h.setPartnerEnabled(w, r, true)
}

func (h *Handler) setPartnerEnabled(
	w http.ResponseWriter,
	r *http.Request,
	enabled bool,
) {
	partnerID, err := parsePublicID(
		r.PathValue("partner_id"),
		publicid.Partner,
		"partner_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	operation := "ADMIN_DISABLE_PARTNER"
	state := "DISABLED"

	if enabled {
		operation = "ADMIN_ENABLE_PARTNER"
		state = "ACTIVE"
	}

	request := struct{}{}

	h.runMutation(
		w,
		r,
		operation,
		partnerID.String(),
		request,
		platformAdminAuthorization,
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			if err := h.partner.SetEnabled(
				ctx,
				userID,
				partnerID,
				enabled,
			); err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusOK,
				map[string]any{
					"id": publicid.Encode(
						publicid.Partner,
						partnerID,
					),
					"state": state,
				},
			)
		},
	)
}

func (h *Handler) enablePartnerEventAccess(
	w http.ResponseWriter,
	r *http.Request,
) {
	h.setPartnerEventAccess(w, r, true)
}

func (h *Handler) disablePartnerEventAccess(
	w http.ResponseWriter,
	r *http.Request,
) {
	h.setPartnerEventAccess(w, r, false)
}

func (h *Handler) setPartnerEventAccess(
	w http.ResponseWriter,
	r *http.Request,
	enabled bool,
) {
	eventID, err := parsePublicID(
		r.PathValue("event_id"),
		publicid.Event,
		"event_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	partnerID, err := parsePublicID(
		r.PathValue("partner_id"),
		publicid.Partner,
		"partner_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	operation := "ADMIN_DISABLE_PARTNER_EVENT_ACCESS"
	state := "DISABLED"

	if enabled {
		operation = "ADMIN_ENABLE_PARTNER_EVENT_ACCESS"
		state = "ACTIVE"
	}

	request := struct{}{}

	h.runMutation(
		w,
		r,
		operation,
		eventID.String()+":"+partnerID.String(),
		request,
		platformAdminAuthorization,
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			var err error

			if enabled {
				err = h.partner.GrantEventAccess(
					ctx,
					userID,
					eventID,
					partnerID,
				)
			} else {
				err = h.partner.DisableEventAccess(
					ctx,
					userID,
					eventID,
					partnerID,
				)
			}

			if err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusOK,
				map[string]any{
					"event_id": publicid.Encode(
						publicid.Event,
						eventID,
					),
					"partner_id": publicid.Encode(
						publicid.Partner,
						partnerID,
					),
					"state": state,
				},
			)
		},
	)
}
