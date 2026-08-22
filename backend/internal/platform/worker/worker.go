package worker

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type Pinger interface {
	Ping(context.Context) error
}

type Job interface {
	RunOnce(context.Context) error
}

type Runner struct {
	logger       *slog.Logger
	database     Pinger
	pollInterval time.Duration
	shutdown     time.Duration
	jobs         []Job
	active       sync.WaitGroup
}

func New(
	logger *slog.Logger,
	database Pinger,
	pollInterval time.Duration,
	shutdown time.Duration,
	jobs ...Job,
) *Runner {
	return &Runner{
		logger:       logger,
		database:     database,
		pollInterval: pollInterval,
		shutdown:     shutdown,
		jobs:         append([]Job(nil), jobs...),
	}
}

func (r *Runner) Run(
	ctx context.Context,
) error {
	startup, cancel :=
		context.WithTimeout(
			ctx,
			5*time.Second,
		)
	defer cancel()

	if err := r.database.Ping(
		startup,
	); err != nil {
		return fmt.Errorf(
			"database startup readiness: %w",
			err,
		)
	}

	r.logger.Info(
		"worker ready",
		"operation",
		"worker.start",
		"poll_interval",
		r.pollInterval,
		"jobs",
		len(r.jobs),
	)

	ticker := time.NewTicker(
		r.pollInterval,
	)
	defer ticker.Stop()

	r.runJobs(ctx)

	for {
		select {
		case <-ctx.Done():
			r.logger.Info(
				"worker shutting down",
				"operation",
				"worker.shutdown",
			)

			return r.waitForJobs()

		case <-ticker.C:
			r.runJobs(ctx)
		}
	}
}

func (r *Runner) runJobs(
	ctx context.Context,
) {
	for _, job := range r.jobs {
		if ctx.Err() != nil {
			return
		}

		r.active.Add(1)

		err := job.RunOnce(ctx)

		r.active.Done()

		if err != nil {
			r.logger.Error(
				"worker job failed",
				"operation",
				"worker.job",
				"error",
				err,
			)
		}
	}
}

func (r *Runner) waitForJobs() error {
	finished := make(
		chan struct{},
	)

	go func() {
		r.active.Wait()
		close(finished)
	}()

	select {
	case <-finished:
		return nil

	case <-time.After(
		r.shutdown,
	):
		return fmt.Errorf(
			"worker cleanup exceeded %s",
			r.shutdown,
		)
	}
}
