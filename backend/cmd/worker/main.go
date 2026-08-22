package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/tktsync/tktsync/backend/internal/platform/bootstrap"
	platformworker "github.com/tktsync/tktsync/backend/internal/platform/worker"
	"github.com/tktsync/tktsync/backend/internal/reservation"
)

func main() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	resources, err :=
		bootstrap.Start(
			ctx,
			"worker",
		)
	if err != nil {
		slog.Error(
			"worker bootstrap failed",
			"error",
			err,
		)
		os.Exit(1)
	}
	defer resources.Close()

	reservationService :=
		reservation.NewService(
			resources.Transactions,
			resources.ReservationKeys,
		)

	reservationMaterializer :=
		reservation.NewMaterializer(
			resources.Database,
			reservationService,
			100,
		)

	runner := platformworker.New(
		resources.Logger,
		resources.Database,
		resources.Config.Worker.PollInterval,
		resources.Config.Worker.ShutdownTimeout,
		reservationMaterializer,
	)

	if err := runner.Run(ctx); err != nil {
		resources.Logger.Error(
			"worker stopped with error",
			"operation",
			"worker.run",
			"error",
			err,
		)
		os.Exit(1)
	}
}
