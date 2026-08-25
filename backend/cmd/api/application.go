package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/tktsync/tktsync/backend/internal/adminapi"
	"github.com/tktsync/tktsync/backend/internal/admission"
	"github.com/tktsync/tktsync/backend/internal/admissionapi"
	allocsvc "github.com/tktsync/tktsync/backend/internal/allocation"
	"github.com/tktsync/tktsync/backend/internal/auth"
	eventsvc "github.com/tktsync/tktsync/backend/internal/event"
	"github.com/tktsync/tktsync/backend/internal/inventory"
	partnersvc "github.com/tktsync/tktsync/backend/internal/partner"
	"github.com/tktsync/tktsync/backend/internal/partnerapi"
	"github.com/tktsync/tktsync/backend/internal/platform/bootstrap"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
	"github.com/tktsync/tktsync/backend/internal/platform/telemetry"
	"github.com/tktsync/tktsync/backend/internal/realtimeapi"
	"github.com/tktsync/tktsync/backend/internal/reporting"
	"github.com/tktsync/tktsync/backend/internal/reservation"
	"github.com/tktsync/tktsync/backend/internal/selection"
	"github.com/tktsync/tktsync/backend/internal/selectionapi"
	venuesvc "github.com/tktsync/tktsync/backend/internal/venue"
	"github.com/tktsync/tktsync/backend/internal/webhook"
)

func run(ctx context.Context) error {
	resources, err := bootstrap.Start(
		ctx,
		"api",
	)
	if err != nil {
		return fmt.Errorf("bootstrap API: %w", err)
	}
	defer resources.Close()
	telemetryRuntime, err := telemetry.Start(ctx, "tktsync-api", resources.Config.Telemetry, resources.Logger)
	if err != nil {
		return fmt.Errorf("configure telemetry: %w", err)
	}
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutdownErr := telemetryRuntime.Shutdown(shutdown); shutdownErr != nil {
			resources.Logger.Error("telemetry shutdown failed", "operation", "telemetry.shutdown", "error", shutdownErr)
		}
	}()

	replayProtector, err :=
		adminapi.NewReplayProtectorFromEncoded(
			resources.Config.
				PartnerCredentialReplayKey,
		)
	if err != nil {
		return fmt.Errorf("configure credential replay protection: %w", err)
	}

	var humanAuth adminapi.HumanAuthenticator

	if resources.HumanAuth != nil {
		humanAuth = func(
			ctx context.Context,
			token string,
		) (auth.HumanPrincipal, error) {
			return auth.AuthenticateHumanBearer(
				ctx,
				resources.HumanAuth,
				token,
			)
		}
	}

	allocationService :=
		allocsvc.NewService(
			resources.Transactions,
			resources.QRKeys,
		)

	reservationService :=
		reservation.NewService(
			resources.Transactions,
			resources.ReservationKeys,
			resources.QRKeys,
		)
	selectionService := selection.NewService(resources.Database, resources.Transactions, resources.SelectionKeys, resources.Config.SelectorBaseURL)

	realtimeHub := realtimeapi.NewHub(32)
	if resources.Config.Realtime.Enabled {
		listener := realtimeapi.NewListener(
			resources.Database,
			realtimeHub,
			resources.Logger,
		)
		go func() {
			if listenErr := listener.Run(ctx); listenErr != nil && ctx.Err() == nil {
				resources.Logger.Error(
					"realtime listener stopped",
					"operation", "realtime.listen",
					"error", listenErr,
				)
			}
		}()
	}

	admissionService := admission.NewService(
		resources.Transactions,
		resources.QRKeys,
	)

	webhookBox, err := webhook.NewVersionedSecretBox(resources.Config.Webhook.EncryptionKeyVersion, resources.Config.Webhook.EncryptionKey, resources.Config.Webhook.EncryptionKeyring)
	if err != nil {
		return fmt.Errorf("configure webhook encryption: %w", err)
	}
	webhookService := webhook.NewService(resources.Transactions, webhookBox, resources.Config.Webhook.EncryptionKeyVersion, resources.Config.Environment != "production")
	reportingService := reporting.NewService(resources.Database)
	metricsObserver := reporting.NewObserver()
	if resources.Config.Environment == "production" {
		if err := adminapi.ValidateProductionIdentityAdmin(
			resources.Config.Supabase.URL,
			resources.Config.Supabase.SecretKey,
			resources.Config.Supabase.InviteRedirectURL,
		); err != nil {
			return fmt.Errorf("configure Supabase identity administration: %w", err)
		}
	}
	identityAdmin, err := adminapi.NewSupabaseIdentityAdmin(
		resources.Config.Supabase.URL,
		resources.Config.Supabase.SecretKey,
		resources.Config.Supabase.InviteRedirectURL,
	)
	if err != nil {
		return fmt.Errorf("configure Supabase identity administration: %w", err)
	}

	adminHandler, err := adminapi.New(
		adminapi.Dependencies{
			Database:     resources.Database,
			Transactions: resources.Transactions,
			HumanAuth:    humanAuth,
			VenueService: venuesvc.NewService(
				resources.Transactions,
			),
			EventService: eventsvc.NewService(
				resources.Transactions,
			),
			PartnerService: partnersvc.NewService(
				resources.Transactions,
			),
			AllocationService:  allocationService,
			ReservationService: reservationService,
			AdmissionService:   admissionService,
			WebhookService:     webhookService,
			ReplayProtector:    replayProtector,
			ReportingService:   reportingService,
			MetricsObserver:    metricsObserver,
			IdentityAdmin:      identityAdmin,
		},
	)
	if err != nil {
		return fmt.Errorf("configure admin API: %w", err)
	}

	admissionHandler, err := admissionapi.New(
		admissionapi.Dependencies{
			Database:     resources.Database,
			Transactions: resources.Transactions,
			HumanAuth:    admissionapi.HumanAuthenticator(humanAuth),
			Admission:    admissionService,
		},
	)
	if err != nil {
		return fmt.Errorf("configure admission API: %w", err)
	}

	partnerHandler, err := partnerapi.New(
		partnerapi.Dependencies{
			Database:    resources.Database,
			PartnerAuth: resources.PartnerAuth,
			Availability: inventory.NewService(
				resources.Database,
			),
			Transactions: resources.Transactions,
			Reservation:  reservationService,
			Selection:    selectionService,
			Reporting:    reportingService,
			TicketQRPublicBaseURL: resources.Config.
				TicketQRPublicBaseURL,
		},
	)
	if err != nil {
		return fmt.Errorf("configure Partner API: %w", err)
	}

	selectionHandler, err := selectionapi.New(selectionapi.Dependencies{Database: resources.Database, Transactions: resources.Transactions, Selection: selectionService, Reservation: reservationService, Availability: inventory.NewService(resources.Database)})
	if err != nil {
		return fmt.Errorf("configure Selection API: %w", err)
	}

	apiMux := http.NewServeMux()

	apiMux.Handle(
		"/api/v1/admin/",
		adminHandler,
	)

	apiMux.Handle(
		"/api/v1/partner/",
		partnerHandler,
	)
	apiMux.Handle(
		"/api/v1/ticket-qr/",
		partnerHandler,
	)
	apiMux.Handle("/api/v1/selection/", selectionHandler)

	apiMux.Handle(
		"/api/v1/admission/",
		admissionHandler,
	)

	apiMux.Handle(
		"GET /api/v1/realtime/stream",
		realtimeapi.New(
			resources.Database,
			realtimeapi.HumanAuthenticator(humanAuth),
			func(
				ctx context.Context,
				token string,
			) (selection.Session, error) {
				return selectionService.Authenticate(ctx, token)
			},
			realtimeHub,
			resources.Config.Realtime.Enabled,
		),
	)

	readiness := &httpserver.Readiness{}
	handler := telemetryRuntime.HTTPHandler(httpserver.HandlerWithOptions(
		resources.Logger,
		resources.Database,
		httpserver.Options{
			Readiness:              readiness,
			RequestTimeout:         resources.Config.HTTP.RequestTimeout,
			LongRequestTimeout:     resources.Config.HTTP.LongRequestTimeout,
			MaxBodyBytes:           resources.Config.HTTP.MaxBodyBytes,
			MaxInFlight:            resources.Config.HTTP.MaxInFlight,
			RealtimeMaxConnections: resources.Config.Realtime.MaxConnections,
			MetricsEnabled:         resources.Config.HTTP.MetricsEnabled,
			MetricsToken:           resources.Config.HTTP.MetricsToken,
			PoolStats: func() httpserver.PoolSnapshot {
				stat := resources.Database.Stat()
				return httpserver.PoolSnapshot{Acquired: stat.AcquiredConns(), Idle: stat.IdleConns(), Total: stat.TotalConns(), Max: stat.MaxConns(), AcquireCount: uint64(stat.AcquireCount()), EmptyAcquireCount: uint64(stat.EmptyAcquireCount()), AcquireDuration: stat.AcquireDuration()}
			},
		},
		httpserver.CORS(metricsObserver.Middleware(apiMux), resources.Config.BrowserOrigins),
	))

	server := httpserver.New(
		resources.Config.HTTP.Address(),
		handler,
		resources.Logger,
		httpserver.ServerOptions{
			ShutdownTimeout:   resources.Config.Shutdown,
			ReadHeaderTimeout: resources.Config.HTTP.ReadHeaderTimeout,
			IdleTimeout:       resources.Config.HTTP.IdleTimeout,
			MaxHeaderBytes:    resources.Config.HTTP.MaxHeaderBytes,
			Readiness:         readiness,
		},
	)

	return server.Run(ctx)
}
