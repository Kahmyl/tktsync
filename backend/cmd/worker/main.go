package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tktsync/tktsync/backend/internal/outbox"
	"github.com/tktsync/tktsync/backend/internal/platform/bootstrap"
	platformworker "github.com/tktsync/tktsync/backend/internal/platform/worker"
	"github.com/tktsync/tktsync/backend/internal/reservation"
	"github.com/tktsync/tktsync/backend/internal/webhook"
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

	jobs := []platformworker.Job{
		reservationMaterializer,
		outbox.NewDispatcher(resources.Transactions, 100),
	}
	if resources.Config.Webhook.Enabled {
		box, boxErr := webhook.NewSecretBox(resources.Config.Webhook.EncryptionKey)
		if boxErr != nil || box == nil || resources.Config.Webhook.EncryptionKeyVersion <= 0 {
			resources.Logger.Error("webhook worker configuration failed", "operation", "webhook.worker.configure", "error", boxErr)
			os.Exit(1)
		}
		jobs = append(jobs, webhook.NewDeliveryWorker(resources.Transactions, box, resources.Config.Environment != "production", 50, 8, 5*time.Second))
	}

	runner := platformworker.New(
		resources.Logger,
		resources.Database,
		resources.Config.Worker.PollInterval,
		resources.Config.Worker.ShutdownTimeout,
		jobs...,
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
