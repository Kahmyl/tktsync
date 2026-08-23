package worker

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"log/slog"
	"reflect"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

type Pinger interface{ Ping(context.Context) error }
type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}
type Job interface{ RunOnce(context.Context) error }

type Runner struct {
	logger                 *slog.Logger
	database               Pinger
	pollInterval, shutdown time.Duration
	concurrency            int
	jobs                   []Job
	active                 sync.WaitGroup
	slots                  chan struct{}
	next                   atomic.Uint64
}

func New(logger *slog.Logger, database Pinger, pollInterval, shutdown time.Duration, concurrency int, jobs ...Job) *Runner {
	if concurrency < 1 {
		concurrency = 1
	}
	return &Runner{logger: logger, database: database, pollInterval: pollInterval, shutdown: shutdown, concurrency: concurrency, jobs: append([]Job(nil), jobs...), slots: make(chan struct{}, concurrency)}
}

func (r *Runner) Run(ctx context.Context) error {
	startup, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := r.database.Ping(startup); err != nil {
		return fmt.Errorf("database startup readiness: %w", err)
	}
	workContext, cancelWork := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelWork()
	r.logger.Info("worker ready", "operation", "worker.start", "poll_interval", r.pollInterval, "jobs", len(r.jobs), "concurrency", r.concurrency)
	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()
	r.schedule(workContext)
	r.logBacklog(workContext)
	nextBacklog := time.Now().Add(30 * time.Second)
	for {
		select {
		case <-ctx.Done():
			r.logger.Info("worker draining", "operation", "worker.shutdown", "active", len(r.slots), "grace_period", r.shutdown)
			return r.drain(cancelWork)
		case <-ticker.C:
			r.schedule(workContext)
			if time.Now().After(nextBacklog) {
				r.logBacklog(workContext)
				nextBacklog = time.Now().Add(30 * time.Second)
			}
		}
	}
}

func (r *Runner) logBacklog(ctx context.Context) {
	database, ok := r.database.(queryRower)
	if !ok {
		return
	}
	var reservationPending, outboxPending, webhookPending int64
	var reservationOldest, outboxOldest, webhookOldest float64
	err := database.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM reservations WHERE (state='HELD' AND hold_expires_at<=clock_timestamp()) OR (state='PAYMENT_RETRY' AND COALESCE(payment_retry_expires_at,clock_timestamp())<=clock_timestamp()) OR (state='RECONCILING' AND COALESCE(reconciliation_expires_at,clock_timestamp())<=clock_timestamp())),
			COALESCE((SELECT EXTRACT(EPOCH FROM clock_timestamp()-MIN(updated_at)) FROM reservations WHERE state IN ('HELD','PAYMENT_RETRY','RECONCILING')),0),
			(SELECT COUNT(*) FROM outbox_events WHERE processed_at IS NULL),
			COALESCE((SELECT EXTRACT(EPOCH FROM clock_timestamp()-MIN(created_at)) FROM outbox_events WHERE processed_at IS NULL),0),
			(SELECT COUNT(*) FROM webhook_deliveries WHERE state='PENDING'),
			COALESCE((SELECT EXTRACT(EPOCH FROM clock_timestamp()-MIN(created_at)) FROM webhook_deliveries WHERE state='PENDING'),0)
	`).Scan(&reservationPending, &reservationOldest, &outboxPending, &outboxOldest, &webhookPending, &webhookOldest)
	if err != nil {
		r.logger.Warn("worker backlog probe failed", "operation", "worker.backlog", "error", err)
		return
	}
	r.logger.Info("worker backlog", "operation", "worker.backlog", "active_slots", len(r.slots), "slot_capacity", r.concurrency, "reservation_pending", reservationPending, "reservation_oldest_seconds", reservationOldest, "outbox_pending", outboxPending, "outbox_oldest_seconds", outboxOldest, "webhook_pending", webhookPending, "webhook_oldest_seconds", webhookOldest)
}

func (r *Runner) schedule(ctx context.Context) {
	if ctx.Err() != nil || len(r.jobs) == 0 {
		return
	}
	for i := 0; i < r.concurrency; i++ {
		select {
		case r.slots <- struct{}{}:
			index := int(r.next.Add(1)-1) % len(r.jobs)
			r.active.Add(1)
			go r.runJob(ctx, r.jobs[index])
		default:
			return
		}
	}
}

func (r *Runner) runJob(ctx context.Context, job Job) {
	defer func() { <-r.slots; r.active.Done() }()
	name := reflect.TypeOf(job).String()
	ctx, span := otel.Tracer("tktsync.worker").Start(ctx, "worker.run")
	span.SetAttributes(attribute.String("job.type", name))
	defer span.End()
	started := time.Now()
	defer func() {
		if recovered := recover(); recovered != nil {
			r.logger.Error("worker job panic recovered", "operation", "worker.job", "job", name, "panic", fmt.Sprint(recovered), "stack", string(debug.Stack()))
		}
	}()
	if err := job.RunOnce(ctx); err != nil {
		r.logger.Error("worker job failed", "operation", "worker.job", "job", name, "duration", time.Since(started), "error", err)
		return
	}
	r.logger.Debug("worker job completed", "operation", "worker.job", "job", name, "duration", time.Since(started))
}

func (r *Runner) drain(cancelWork context.CancelFunc) error {
	finished := make(chan struct{})
	go func() { r.active.Wait(); close(finished) }()
	timer := time.NewTimer(r.shutdown)
	defer timer.Stop()
	select {
	case <-finished:
		return nil
	case <-timer.C:
		cancelWork()
		select {
		case <-finished:
			return fmt.Errorf("worker cleanup exceeded %s; remaining jobs cancelled", r.shutdown)
		case <-time.After(time.Second):
			return fmt.Errorf("worker cleanup exceeded %s and jobs did not stop after cancellation", r.shutdown)
		}
	}
}
