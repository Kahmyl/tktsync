//go:build integration

package audit_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/outbox"
)

func TestAuditAndOutboxRollbackTogether(t *testing.T) {
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
		t.Fatalf("open database: %v", err)
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}

	defer func() {
		_ = tx.Rollback(context.Background())
	}()

	correlationID := uuid.New()

	auditID, err := (audit.Store{}).Append(
		ctx,
		tx,
		audit.Event{
			ActorKind:     audit.ActorSystem,
			SystemActor:   "platform.integration",
			Operation:     "PLATFORM_ATOMICITY_TEST",
			EntityType:    "PLATFORM",
			CorrelationID: &correlationID,
			Metadata: map[string]any{
				"test": true,
			},
		},
	)
	if err != nil {
		t.Fatalf("append audit event: %v", err)
	}

	factID := uuid.New()

	outboxID, err := (outbox.Store{}).Append(
		ctx,
		tx,
		outbox.Fact{
			FactID:        factID,
			FactType:      "platform.atomicity_test",
			AggregateType: "PLATFORM",
			Payload: map[string]any{
				"test": true,
			},
		},
	)
	if err != nil {
		t.Fatalf("append outbox event: %v", err)
	}

	var auditCount int
	if err := tx.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM audit_events WHERE id = $1`,
		auditID,
	).Scan(&auditCount); err != nil {
		t.Fatalf("count audit event inside transaction: %v", err)
	}

	var outboxCount int
	if err := tx.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM outbox_events WHERE id = $1`,
		outboxID,
	).Scan(&outboxCount); err != nil {
		t.Fatalf("count outbox event inside transaction: %v", err)
	}

	if auditCount != 1 || outboxCount != 1 {
		t.Fatalf(
			"expected audit/outbox visible inside transaction, got %d/%d",
			auditCount,
			outboxCount,
		)
	}

	rollbackReason := errors.New("intentional Platform rollback")
	_ = rollbackReason

	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback transaction: %v", err)
	}

	if err := pool.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM audit_events WHERE id = $1`,
		auditID,
	).Scan(&auditCount); err != nil {
		t.Fatalf("count audit event after rollback: %v", err)
	}

	if err := pool.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM outbox_events WHERE id = $1`,
		outboxID,
	).Scan(&outboxCount); err != nil {
		t.Fatalf("count outbox event after rollback: %v", err)
	}

	if auditCount != 0 || outboxCount != 0 {
		t.Fatalf(
			"audit/outbox escaped rollback: %d/%d",
			auditCount,
			outboxCount,
		)
	}
}
