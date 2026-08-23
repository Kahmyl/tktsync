//go:build integration

package idempotency

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
)

func TestConcurrentClaimExecutesOnce(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer pool.Close()

	var schemaExists bool
	if err := pool.QueryRow(
		ctx,
		`SELECT to_regclass('public.idempotency_operations') IS NOT NULL`,
	).Scan(&schemaExists); err != nil {
		t.Fatalf("check schema: %v", err)
	}

	if !schemaExists {
		t.Fatal("authoritative schema is not applied")
	}

	partnerID := uuid.New()

	if _, err := pool.Exec(
		ctx,
		`
			INSERT INTO partners (id, name, state)
			VALUES ($1, $2, 'ACTIVE')
		`,
		partnerID,
		"Platform Idempotency Integration",
	); err != nil {
		t.Fatalf("create Partner fixture: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cleanupCancel()

		_, _ = pool.Exec(
			cleanupCtx,
			`DELETE FROM idempotency_operations WHERE partner_id = $1`,
			partnerID,
		)
		_, _ = pool.Exec(
			cleanupCtx,
			`DELETE FROM partners WHERE id = $1`,
			partnerID,
		)
	})

	runner := database.NewRunner(pool, 3, 5*time.Millisecond)
	store := Store{}

	scope := Scope{
		Kind: ScopePartner,
		ID:   partnerID,
	}

	key := "platform-" + uuid.NewString()
	hash := Fingerprint(
		[]byte(`{"operation":"concurrent-idempotency-test","value":1}`),
	)

	var businessExecutions atomic.Int32

	const workers = 24

	start := make(chan struct{})
	errCh := make(chan error, workers)

	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			<-start

			err := runner.Run(ctx, func(tx pgx.Tx) error {
				claim, err := store.Claim(
					ctx,
					tx,
					scope,
					"PLATFORM_CONCURRENCY_TEST",
					key,
					hash,
				)
				if err != nil {
					return err
				}

				if claim.Owner {
					businessExecutions.Add(1)

					time.Sleep(20 * time.Millisecond)

					return store.CompleteSuccess(
						ctx,
						tx,
						claim.ID,
						"OK",
						"PLATFORM_TEST",
						nil,
						map[string]any{
							"ok": true,
						},
					)
				}

				if claim.Replay == nil {
					return errors.New("non-owner received no replay result")
				}

				if claim.Replay.ExecutionState != "SUCCEEDED" {
					return errors.New("replayed operation was not SUCCEEDED")
				}

				var payload map[string]any
				if err := json.Unmarshal(claim.Replay.Payload, &payload); err != nil {
					return err
				}

				if payload["ok"] != true {
					return errors.New("unexpected replay payload")
				}

				return nil
			})

			errCh <- err
		}()
	}

	close(start)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent request failed: %v", err)
		}
	}

	if got := businessExecutions.Load(); got != 1 {
		t.Fatalf("business mutation executed %d times, want exactly 1", got)
	}

	var count int
	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM idempotency_operations
			WHERE partner_id = $1
			  AND operation_type = 'PLATFORM_CONCURRENCY_TEST'
			  AND idempotency_key = $2
		`,
		partnerID,
		key,
	).Scan(&count); err != nil {
		t.Fatalf("count idempotency records: %v", err)
	}

	if count != 1 {
		t.Fatalf("persisted %d idempotency records, want 1", count)
	}

	conflictHash := Fingerprint(
		[]byte(`{"operation":"concurrent-idempotency-test","value":2}`),
	)

	err = runner.Run(ctx, func(tx pgx.Tx) error {
		_, err := store.Claim(
			ctx,
			tx,
			scope,
			"PLATFORM_CONCURRENCY_TEST",
			key,
			conflictHash,
		)
		return err
	})

	if err == nil {
		t.Fatal("expected changed intent to produce IDEMPOTENCY_CONFLICT")
	}

	apiErr, ok := apierror.As(err)
	if !ok || apiErr.Code != apierror.CodeIdempotencyConflict {
		t.Fatalf("unexpected conflict error: %v", err)
	}
}

func TestClaimRollbackDoesNotPersistInProgress(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required")
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		15*time.Second,
	)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer pool.Close()

	partnerID := uuid.New()

	if _, err := pool.Exec(
		ctx,
		`
			INSERT INTO partners (id, name, state)
			VALUES ($1, $2, 'ACTIVE')
		`,
		partnerID,
		"Platform Rollback Integration",
	); err != nil {
		t.Fatalf("create Partner fixture: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cleanupCancel()

		_, _ = pool.Exec(
			cleanupCtx,
			`DELETE FROM idempotency_operations WHERE partner_id = $1`,
			partnerID,
		)

		_, _ = pool.Exec(
			cleanupCtx,
			`DELETE FROM partners WHERE id = $1`,
			partnerID,
		)
	})

	runner := database.NewRunner(
		pool,
		3,
		5*time.Millisecond,
	)

	store := Store{}

	scope := Scope{
		Kind: ScopePartner,
		ID:   partnerID,
	}

	key := "platform-rollback-" + uuid.NewString()
	hash := Fingerprint(
		[]byte(`{"operation":"rollback-test"}`),
	)

	sentinel := errors.New("forced rollback")

	err = runner.Run(
		ctx,
		func(tx pgx.Tx) error {
			claim, err := store.Claim(
				ctx,
				tx,
				scope,
				"PLATFORM_ROLLBACK_TEST",
				key,
				hash,
			)
			if err != nil {
				return err
			}

			if !claim.Owner {
				return errors.New("first request unexpectedly lost claim")
			}

			return sentinel
		},
	)

	if !errors.Is(err, sentinel) {
		t.Fatalf("got %v, want forced rollback", err)
	}

	var count int

	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM idempotency_operations
			WHERE partner_id = $1
			  AND operation_type = 'PLATFORM_ROLLBACK_TEST'
			  AND idempotency_key = $2
		`,
		partnerID,
		key,
	).Scan(&count); err != nil {
		t.Fatalf("count rolled-back idempotency operations: %v", err)
	}

	if count != 0 {
		t.Fatalf(
			"rollback persisted %d IN_PROGRESS operations, want 0",
			count,
		)
	}
}
