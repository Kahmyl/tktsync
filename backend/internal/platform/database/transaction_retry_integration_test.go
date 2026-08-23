//go:build integration

package database

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRunnerRetriesAnActualPostgreSQLDeadlock(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	runner := NewRunner(pool, 3, time.Millisecond)
	firstLocks := make(chan struct{}, 2)
	startSecondLocks := make(chan struct{})
	var attempts atomic.Int32
	work := func(first, second int64) func(pgx.Tx) error {
		return func(tx pgx.Tx) error {
			attempt := attempts.Add(1)
			if attempt > 2 {
				if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(9111001)); err != nil {
					return err
				}
				_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(9111002))
				return err
			}
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, first); err != nil {
				return err
			}
			firstLocks <- struct{}{}
			<-startSecondLocks
			_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, second)
			return err
		}
	}
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); errs <- runner.Run(ctx, work(9111001, 9111002)) }()
	go func() { defer wg.Done(); errs <- runner.Run(ctx, work(9111002, 9111001)) }()
	<-firstLocks
	<-firstLocks
	close(startSecondLocks)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if attempts.Load() < 3 {
		t.Fatalf("transaction attempts=%d want a deadlock retry", attempts.Load())
	}
}
