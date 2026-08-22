package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type TxBeginner interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type QueryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Runner struct {
	db          TxBeginner
	maxAttempts int
	retryBase   time.Duration
}

func NewRunner(db TxBeginner, maxAttempts int, retryBase time.Duration) *Runner {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	if retryBase <= 0 {
		retryBase = 10 * time.Millisecond
	}

	return &Runner{
		db:          db,
		maxAttempts: maxAttempts,
		retryBase:   retryBase,
	}
}

// Run executes work inside a fresh READ COMMITTED PostgreSQL transaction.
//
// work MAY be invoked more than once when PostgreSQL reports a deadlock or
// serialization failure. Therefore work must contain only transactional
// PostgreSQL mutations and retry-safe in-process computation. It must never
// perform payment calls, webhooks, messages, irreversible external writes,
// or any other network side effect.
//
// Unknown commit outcomes are not retried unless PostgreSQL definitively
// reports a retryable transaction failure.
func (r *Runner) Run(ctx context.Context, work func(pgx.Tx) error) error {
	if tx, ok := TransactionFromContext(ctx); ok {
		return work(tx)
	}

	var lastErr error

	for attempt := 1; attempt <= r.maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		tx, err := r.db.BeginTx(ctx, pgx.TxOptions{
			IsoLevel: pgx.ReadCommitted,
		})
		if err != nil {
			if !IsRetryableTransactionError(err) || attempt == r.maxAttempts {
				return fmt.Errorf("begin transaction: %w", err)
			}
			lastErr = err
			if err := r.wait(ctx, attempt); err != nil {
				return err
			}
			continue
		}

		err = work(tx)
		if err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))

			if !IsRetryableTransactionError(err) || attempt == r.maxAttempts {
				return err
			}

			lastErr = err
			if err := r.wait(ctx, attempt); err != nil {
				return err
			}
			continue
		}

		err = tx.Commit(ctx)
		if err == nil {
			return nil
		}

		if !IsRetryableTransactionError(err) || attempt == r.maxAttempts {
			return fmt.Errorf("commit transaction: %w", err)
		}

		lastErr = err
		if err := r.wait(ctx, attempt); err != nil {
			return err
		}
	}

	return fmt.Errorf("transaction attempts exhausted: %w", lastErr)
}

func (r *Runner) wait(ctx context.Context, attempt int) error {
	delay := r.retryBase

	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= 250*time.Millisecond {
			delay = 250 * time.Millisecond
			break
		}
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func IsRetryableTransactionError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}

	switch pgErr.Code {
	case "40P01", "40001":
		return true
	default:
		return false
	}
}

func AuthoritativeTime(ctx context.Context, q QueryRower) (time.Time, error) {
	var now time.Time
	if err := q.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return time.Time{}, fmt.Errorf("read authoritative database time: %w", err)
	}

	return now, nil
}

type EventLockMode int

const (
	EventLockKeyShare EventLockMode = iota + 1
	EventLockUpdate
)

func LockEvent(ctx context.Context, q QueryRower, eventID uuid.UUID, mode EventLockMode) error {
	var statement string

	switch mode {
	case EventLockKeyShare:
		statement = `SELECT id FROM events WHERE id = $1 FOR KEY SHARE`
	case EventLockUpdate:
		statement = `SELECT id FROM events WHERE id = $1 FOR UPDATE`
	default:
		return errors.New("invalid Event lock mode")
	}

	var locked uuid.UUID
	if err := q.QueryRow(ctx, statement, eventID).Scan(&locked); err != nil {
		return err
	}

	return nil
}

func SortUUIDs(values []uuid.UUID) []uuid.UUID {
	out := append([]uuid.UUID(nil), values...)

	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].String() < out[j-1].String(); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}

	return out
}
