package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/tktsync/tktsync/backend/internal/adminapi"
	allocsvc "github.com/tktsync/tktsync/backend/internal/allocation"
	"github.com/tktsync/tktsync/backend/internal/auth"
	eventsvc "github.com/tktsync/tktsync/backend/internal/event"
	"github.com/tktsync/tktsync/backend/internal/inventory"
	partnersvc "github.com/tktsync/tktsync/backend/internal/partner"
	"github.com/tktsync/tktsync/backend/internal/partnerapi"
	"github.com/tktsync/tktsync/backend/internal/platform/bootstrap"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
	"github.com/tktsync/tktsync/backend/internal/reservation"
	venuesvc "github.com/tktsync/tktsync/backend/internal/venue"
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

	partnerHandler, err := partnerapi.New(
		partnerapi.Dependencies{
			Database:    resources.Database,
			PartnerAuth: resources.PartnerAuth,
			Availability: inventory.NewService(
				resources.Database,
			),
			Transactions: resources.Transactions,
			Reservation:  reservationService,
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

	apiMux := http.NewServeMux()

	apiMux.Handle(
		"/api/v1/admin/",
		adminHandler,
	)

	apiMux.Handle(
		"/api/v1/partner/",
		partnerHandler,
	)

	handler := httpserver.Handler(
		resources.Logger,
		resources.Database,
		apiMux,
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
