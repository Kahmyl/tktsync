package main

import (
	"context"
	"fmt"
	"net/http"

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
			resources.Config.Realtime.Enabled,
		),
	)

	handler := httpserver.Handler(
		resources.Logger,
		resources.Database,
		httpserver.CORS(metricsObserver.Middleware(apiMux), resources.Config.BrowserOrigins),
	)

	server := httpserver.New(
		resources.Config.HTTP.Address(),
		handler,
		resources.Logger,
		resources.Config.Shutdown,
	)

	return server.Run(ctx)
}
