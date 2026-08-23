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
	h.registerReadModelRoutes()

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
