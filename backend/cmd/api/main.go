package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

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
	"github.com/tktsync/tktsync/backend/internal/reservation"
	"github.com/tktsync/tktsync/backend/internal/selection"
	"github.com/tktsync/tktsync/backend/internal/selectionapi"
	venuesvc "github.com/tktsync/tktsync/backend/internal/venue"
	"github.com/tktsync/tktsync/backend/internal/webhook"
)

func main() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	resources, err := bootstrap.Start(
		ctx,
		"api",
	)
	if err != nil {
		slog.Error(
			"API bootstrap failed",
			"error",
			err,
		)
		os.Exit(1)
	}
	defer resources.Close()

	replayProtector, err :=
		adminapi.NewReplayProtectorFromEncoded(
			resources.Config.
				PartnerCredentialReplayKey,
		)
	if err != nil {
		resources.Logger.Error(
			"credential replay protection configuration failed",
			"operation",
			"adminapi.replay.configure",
			"error",
			err,
		)
		os.Exit(1)
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

	webhookBox, err := webhook.NewSecretBox(resources.Config.Webhook.EncryptionKey)
	if err != nil {
		resources.Logger.Error("webhook encryption configuration failed", "operation", "webhook.configure", "error", err)
		os.Exit(1)
	}
	webhookService := webhook.NewService(resources.Transactions, webhookBox, resources.Config.Webhook.EncryptionKeyVersion, resources.Config.Environment != "production")

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
		},
	)
	if err != nil {
		resources.Logger.Error(
			"admin API configuration failed",
			"operation",
			"adminapi.configure",
			"error",
			err,
		)
		os.Exit(1)
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
		resources.Logger.Error("admission API configuration failed", "operation", "admissionapi.configure", "error", err)
		os.Exit(1)
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
		},
	)
	if err != nil {
		resources.Logger.Error(
			"Partner API configuration failed",
			"operation",
			"partnerapi.configure",
			"error",
			err,
		)
		os.Exit(1)
	}

	selectionHandler, err := selectionapi.New(selectionapi.Dependencies{Database: resources.Database, Transactions: resources.Transactions, Selection: selectionService, Reservation: reservationService, Availability: inventory.NewService(resources.Database)})
	if err != nil {
		resources.Logger.Error("Selection API configuration failed", "operation", "selectionapi.configure", "error", err)
		os.Exit(1)
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
		realtimeapi.New(resources.Database, realtimeapi.HumanAuthenticator(humanAuth)),
	)

	handler := httpserver.Handler(
		resources.Logger,
		resources.Database,
		httpserver.CORS(apiMux, resources.Config.BrowserOrigins),
	)

	server := httpserver.New(
		resources.Config.HTTP.Address(),
		handler,
		resources.Logger,
		resources.Config.Shutdown,
	)

	if err := server.Run(ctx); err != nil {
		resources.Logger.Error(
			"API stopped with error",
			"operation",
			"http.run",
			"error",
			err,
		)
		os.Exit(1)
	}
}
