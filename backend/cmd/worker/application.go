package main

import (
	"context"
	"fmt"
	"time"

	"github.com/tktsync/tktsync/backend/internal/outbox"
	"github.com/tktsync/tktsync/backend/internal/platform/bootstrap"
	platformworker "github.com/tktsync/tktsync/backend/internal/platform/worker"
	"github.com/tktsync/tktsync/backend/internal/reservation"
	"github.com/tktsync/tktsync/backend/internal/webhook"
)

func run(ctx context.Context) error {
	resources, err :=
		bootstrap.Start(
			ctx,
			"worker",
		)
	if err != nil {
		return fmt.Errorf("bootstrap worker: %w", err)
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
		box, boxErr := webhook.NewVersionedSecretBox(resources.Config.Webhook.EncryptionKeyVersion, resources.Config.Webhook.EncryptionKey, resources.Config.Webhook.EncryptionKeyring)
		if boxErr != nil {
			return fmt.Errorf("configure webhook worker: %w", boxErr)
		}
		if box == nil || resources.Config.Webhook.EncryptionKeyVersion <= 0 {
			return fmt.Errorf("configure webhook worker: active encryption key is required")
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

	return runner.Run(ctx)
}
