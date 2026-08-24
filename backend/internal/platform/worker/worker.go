package worker

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

type Pinger interface{ Ping(context.Context) error }

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Job interface {
	RunOnce(context.Context) error
}

type ProgressJob interface {
	RunOnceWithProgress(context.Context) (bool, error)
}

type Workload struct {
	Name        string
	Job         Job
	Concurrency int
}

type workloadRuntime struct {
	Workload
	active atomic.Int64
}

type Runner struct {
	logger       *slog.Logger
	database     Pinger
	pollInterval time.Duration
	shutdown     time.Duration
	workloads    []*workloadRuntime
	active       sync.WaitGroup
}

func New(
	logger *slog.Logger,
	database Pinger,
	pollInterval time.Duration,
	shutdown time.Duration,
	concurrency int,
	jobs ...Job,
) *Runner {
	if concurrency < 1 {
		concurrency = 1
	}

	workloads := make([]Workload, 0, len(jobs))
	for i, job := range jobs {
		c := 1
		if len(jobs) == 1 {
			c = concurrency
		}
		workloads = append(workloads, Workload{
			Name:        fmt.Sprintf("job-%d", i+1),
			Job:         job,
			Concurrency: c,
		})
	}

	return NewWorkloads(
		logger,
		database,
		pollInterval,
		shutdown,
		workloads...,
	)
}

func NewWorkloads(
	logger *slog.Logger,
	database Pinger,
	pollInterval time.Duration,
	shutdown time.Duration,
	workloads ...Workload,
) *Runner {
	if pollInterval <= 0 {
		pollInterval = time.Second
	}

	result := &Runner{
		logger:       logger,
		database:     database,
		pollInterval: pollInterval,
		shutdown:     shutdown,
	}

	for _, workload := range workloads {
		if workload.Job == nil {
			continue
		}
		if workload.Concurrency < 1 {
			workload.Concurrency = 1
		}
		if workload.Name == "" {
			workload.Name = reflect.TypeOf(workload.Job).String()
		}
		result.workloads = append(result.workloads, &workloadRuntime{Workload: workload})
	}

	return result
}

func (r *Runner) Run(ctx context.Context) error {
	startup, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := r.database.Ping(startup); err != nil {
		return fmt.Errorf("database startup readiness: %w", err)
	}

	workContext, cancelWork := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelWork()

	stop := make(chan struct{})

	for _, workload := range r.workloads {
		for i := 0; i < workload.Concurrency; i++ {
			r.active.Add(1)
			go r.runLoop(workContext, stop, workload)
		}
	}

	r.logger.Info(
		"worker ready",
		"operation", "worker.start",
		"poll_interval", r.pollInterval,
		"workloads", len(r.workloads),
	)

	r.logBacklog(workContext)
	backlogTicker := time.NewTicker(30 * time.Second)
	defer backlogTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			close(stop)
			r.logger.Info(
				"worker draining",
				"operation", "worker.shutdown",
				"grace_period", r.shutdown,
			)
			return r.drain(cancelWork)
		case <-backlogTicker.C:
			r.logBacklog(workContext)
		}
	}
}

func (r *Runner) runLoop(
	ctx context.Context,
	stop <-chan struct{},
	workload *workloadRuntime,
) {
	defer r.active.Done()

	for {
		select {
		case <-stop:
			return
		default:
		}

		worked, err := r.runJob(ctx, workload)
		if err != nil {
			r.logger.Error(
				"worker job failed",
				"operation", "worker.job",
				"workload", workload.Name,
				"error", err,
			)
		}

		if err == nil && worked {
			continue
		}

		timer := time.NewTimer(r.pollInterval)
		select {
		case <-stop:
			timer.Stop()
			return
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (r *Runner) runJob(
	ctx context.Context,
	workload *workloadRuntime,
) (worked bool, err error) {
	ctx, span := otel.Tracer("tktsync.worker").Start(ctx, "worker.run")
	span.SetAttributes(attribute.String("job.type", workload.Name))
	defer span.End()

	workload.active.Add(1)
	defer workload.active.Add(-1)

	started := time.Now()

	defer func() {
		if recovered := recover(); recovered != nil {
			r.logger.Error(
				"worker job panic recovered",
				"operation", "worker.job",
				"workload", workload.Name,
				"panic_type", fmt.Sprintf("%T", recovered),
				"stack", string(debug.Stack()),
			)
			worked = false
			err = fmt.Errorf("worker job panic recovered")
		}

		r.logger.Debug(
			"worker job completed",
			"operation", "worker.job",
			"workload", workload.Name,
			"duration", time.Since(started),
			"worked", worked,
		)
	}()

	if progress, ok := workload.Job.(ProgressJob); ok {
		return progress.RunOnceWithProgress(ctx)
	}

	if err = workload.Job.RunOnce(ctx); err != nil {
		return false, err
	}

	return false, nil
}

func (r *Runner) logBacklog(ctx context.Context) {
	database, ok := r.database.(queryRower)
	if !ok {
		return
	}

	var reservationPending, outboxPending, webhookPending int64
	var reservationOldest, outboxOldest, webhookOldest float64

	err := database.QueryRow(ctx, `
		WITH due_reservations AS (
			SELECT
				r.id,
				CASE
					WHEN e.state = 'CANCELLED' THEN r.updated_at
					WHEN r.state = 'HELD' THEN r.hold_expires_at
					WHEN r.state = 'PAYMENT_RETRY' THEN COALESCE(r.payment_retry_expires_at, r.updated_at)
					WHEN r.state = 'RECONCILING' THEN COALESCE(r.reconciliation_expires_at, r.updated_at)
					WHEN r.state = 'COMMITTING' THEN ca.protection_expires_at
				END AS due_at
			FROM reservations r
			JOIN events e ON e.id = r.event_id
			LEFT JOIN checkout_attempts ca
			  ON ca.reservation_id = r.id
			 AND ca.state = 'ACTIVE'
			WHERE
				(
					r.state = 'HELD'
					AND r.hold_expires_at <= clock_timestamp()
				)
				OR (
					r.state = 'PAYMENT_RETRY'
					AND COALESCE(r.payment_retry_expires_at, r.updated_at) <= clock_timestamp()
				)
				OR (
					r.state = 'RECONCILING'
					AND COALESCE(r.reconciliation_expires_at, r.updated_at) <= clock_timestamp()
				)
				OR (
					r.state = 'COMMITTING'
					AND ca.protection_expires_at <= clock_timestamp()
				)
				OR (
					e.state = 'CANCELLED'
					AND r.state IN ('HELD','PAYMENT_RETRY','COMMITTING')
				)
		),
		claimable_outbox AS (
			SELECT created_at
			FROM outbox_events
			WHERE processed_at IS NULL
			  AND (
				next_attempt_at IS NULL
				OR next_attempt_at <= clock_timestamp()
			  )
		),
		claimable_webhooks AS (
			SELECT created_at
			FROM webhook_deliveries
			WHERE state = 'PENDING'
			  AND (
				next_attempt_at IS NULL
				OR next_attempt_at <= clock_timestamp()
			  )
			  AND (
				lease_until IS NULL
				OR lease_until < clock_timestamp()
			  )
		)
		SELECT
			(SELECT COUNT(*) FROM due_reservations),
			COALESCE((
				SELECT EXTRACT(EPOCH FROM clock_timestamp() - MIN(due_at))
				FROM due_reservations
			), 0),
			(SELECT COUNT(*) FROM claimable_outbox),
			COALESCE((
				SELECT EXTRACT(EPOCH FROM clock_timestamp() - MIN(created_at))
				FROM claimable_outbox
			), 0),
			(SELECT COUNT(*) FROM claimable_webhooks),
			COALESCE((
				SELECT EXTRACT(EPOCH FROM clock_timestamp() - MIN(created_at))
				FROM claimable_webhooks
			), 0)
	`).Scan(
		&reservationPending,
		&reservationOldest,
		&outboxPending,
		&outboxOldest,
		&webhookPending,
		&webhookOldest,
	)
	if err != nil {
		r.logger.Warn(
			"worker backlog probe failed",
			"operation", "worker.backlog",
			"error", err,
		)
		return
	}

	args := []any{
		"operation", "worker.backlog",
		"reservation_claimable", reservationPending,
		"reservation_oldest_claimable_seconds", reservationOldest,
		"outbox_claimable", outboxPending,
		"outbox_oldest_claimable_seconds", outboxOldest,
		"webhook_claimable", webhookPending,
		"webhook_oldest_claimable_seconds", webhookOldest,
	}

	for _, workload := range r.workloads {
		args = append(
			args,
			workload.Name+"_active_workers", workload.active.Load(),
			workload.Name+"_worker_capacity", workload.Concurrency,
		)
	}

	r.logger.Info("worker backlog", args...)
}

func (r *Runner) drain(cancelWork context.CancelFunc) error {
	finished := make(chan struct{})
	go func() {
		r.active.Wait()
		close(finished)
	}()

	timer := time.NewTimer(r.shutdown)
	defer timer.Stop()

	select {
	case <-finished:
		return nil
	case <-timer.C:
		cancelWork()
		select {
		case <-finished:
			return fmt.Errorf(
				"worker cleanup exceeded %s; remaining jobs cancelled",
				r.shutdown,
			)
		case <-time.After(time.Second):
			return fmt.Errorf(
				"worker cleanup exceeded %s and jobs did not stop after cancellation",
				r.shutdown,
			)
		}
	}
}
