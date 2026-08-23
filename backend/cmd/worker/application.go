package main

import (
	"context"
	"fmt"
	"time"

	"github.com/tktsync/tktsync/backend/internal/outbox"
	"github.com/tktsync/tktsync/backend/internal/platform/bootstrap"
	"github.com/tktsync/tktsync/backend/internal/platform/telemetry"
	platformworker "github.com/tktsync/tktsync/backend/internal/platform/worker"
	"github.com/tktsync/tktsync/backend/internal/reservation"
	"github.com/tktsync/tktsync/backend/internal/webhook"
)

func run(ctx context.Context) error {
	resources, err := bootstrap.Start(ctx, "worker")
	if err != nil {
		return fmt.Errorf("bootstrap worker: %w", err)
	}
	defer resources.Close()

	telemetryRuntime, err := telemetry.Start(
		ctx,
		"tktsync-worker",
		resources.Config.Telemetry,
		resources.Logger,
	)
	if err != nil {
		return fmt.Errorf("configure telemetry: %w", err)
	}
	defer func() {
		shutdown, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()
		if shutdownErr := telemetryRuntime.Shutdown(shutdown); shutdownErr != nil {
			resources.Logger.Error(
				"telemetry shutdown failed",
				"operation", "telemetry.shutdown",
				"error", shutdownErr,
			)
		}
	}()

	reservationService := reservation.NewService(
		resources.Transactions,
		resources.ReservationKeys,
	)

	reservationMaterializer := reservation.NewMaterializer(
		resources.Database,
		reservationService,
		resources.Config.Worker.ReservationBatchSize,
	)

	workloads := []platformworker.Workload{
		{
			Name:        "reservation",
			Job:         reservationMaterializer,
			Concurrency: resources.Config.Worker.ReservationConcurrency,
		},
		{
			Name: "outbox",
			Job: outbox.NewDispatcher(
				resources.Transactions,
				resources.Config.Worker.OutboxBatchSize,
			),
			Concurrency: resources.Config.Worker.OutboxConcurrency,
		},
	}

	if resources.Config.Webhook.Enabled {
		box, boxErr := webhook.NewVersionedSecretBox(
			resources.Config.Webhook.EncryptionKeyVersion,
			resources.Config.Webhook.EncryptionKey,
			resources.Config.Webhook.EncryptionKeyring,
		)
		if boxErr != nil {
			return fmt.Errorf("configure webhook worker: %w", boxErr)
		}
		if box == nil || resources.Config.Webhook.EncryptionKeyVersion <= 0 {
			return fmt.Errorf(
				"configure webhook worker: active encryption key is required",
			)
		}

		workloads = append(workloads, platformworker.Workload{
			Name: "webhook",
			Job: webhook.NewDeliveryWorker(
				resources.Transactions,
				box,
				resources.Config.Environment != "production",
				resources.Config.Worker.WebhookBatchSize,
				8,
				resources.Config.Worker.WebhookTimeout,
			),
			Concurrency: resources.Config.Worker.WebhookConcurrency,
		})
	}

	runner := platformworker.NewWorkloads(
		resources.Logger,
		resources.Database,
		resources.Config.Worker.PollInterval,
		resources.Config.Worker.ShutdownTimeout,
		workloads...,
	)

	return runner.Run(ctx)
}
