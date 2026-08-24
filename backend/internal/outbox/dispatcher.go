package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
)

type Dispatcher struct {
	transactions *database.Runner
	batchSize    int
	retryDelay   func(int) time.Duration
}

func NewDispatcher(transactions *database.Runner, batchSize int) *Dispatcher {
	if batchSize <= 0 {
		batchSize = 100
	}
	return &Dispatcher{transactions: transactions, batchSize: batchSize, retryDelay: jitteredRetryDelay}
}

type dispatchFact struct {
	ID            uuid.UUID
	FactID        uuid.UUID
	EventID       *uuid.UUID
	FactType      string
	AggregateType string
	AggregateID   *uuid.UUID
	Payload       json.RawMessage
	Attempt       int
}

func (d *Dispatcher) RunOnce(ctx context.Context) error {
	_, err := d.RunOnceWithProgress(ctx)
	return err
}

func (d *Dispatcher) RunOnceWithProgress(ctx context.Context) (bool, error) {
	worked := false
	for i := 0; i < d.batchSize; i++ {
		processed, err := d.processOne(ctx)
		if err != nil {
			return worked, err
		}
		if !processed {
			return worked, nil
		}
		worked = true
	}
	return worked, nil
}

func (d *Dispatcher) processOne(ctx context.Context) (bool, error) {
	processed := false
	var claimedID uuid.UUID
	claimedAttempt := 0
	err := d.transactions.Run(ctx, func(tx pgx.Tx) error {
		var fact dispatchFact
		err := tx.QueryRow(ctx, `SELECT id,fact_id,event_id,fact_type,aggregate_type,aggregate_id,payload,attempt_count FROM outbox_events WHERE processed_at IS NULL AND (next_attempt_at IS NULL OR next_attempt_at<=clock_timestamp()) ORDER BY enqueue_sequence FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&fact.ID, &fact.FactID, &fact.EventID, &fact.FactType, &fact.AggregateType, &fact.AggregateID, &fact.Payload, &fact.Attempt)
		if err == pgx.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		claimedID = fact.ID
		claimedAttempt = fact.Attempt + 1
		processed = true
		partnerIDs, err := eligiblePartners(ctx, tx, fact)
		if err != nil {
			return err
		}
		for _, partnerID := range partnerIDs {
			if _, err := tx.Exec(ctx, `
				INSERT INTO webhook_deliveries(id,webhook_endpoint_id,outbox_event_id,state,attempt_count,next_attempt_at,created_at)
				SELECT gen_random_uuid(),e.id,$1,'PENDING',0,clock_timestamp(),clock_timestamp()
				FROM partner_webhook_endpoints e JOIN partner_webhook_subscriptions s ON s.webhook_endpoint_id=e.id
				WHERE e.partner_id=$2 AND e.state='ACTIVE' AND s.event_type=$3
				ON CONFLICT(webhook_endpoint_id,outbox_event_id) DO NOTHING
			`, fact.ID, partnerID, fact.FactType); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE outbox_events SET processed_at=clock_timestamp(),attempt_count=attempt_count+1,next_attempt_at=NULL,last_error=NULL WHERE id=$1`, fact.ID)
		return err
	})
	if err != nil && claimedID != uuid.Nil {
		message := err.Error()
		if len(message) > 512 {
			message = message[:512]
		}
		retryErr := d.transactions.Run(ctx, func(tx pgx.Tx) error {
			delay := d.retryDelay(claimedAttempt)
			_, updateErr := tx.Exec(ctx, `UPDATE outbox_events SET attempt_count=attempt_count+1,next_attempt_at=clock_timestamp()+$3::interval,last_error=$2 WHERE id=$1 AND processed_at IS NULL`, claimedID, message, fmt.Sprintf("%f seconds", delay.Seconds()))
			return updateErr
		})
		if retryErr != nil {
			err = errors.Join(err, fmt.Errorf("schedule outbox retry: %w", retryErr))
		}
	}
	return processed, err
}

func eligiblePartners(ctx context.Context, tx pgx.Tx, fact dispatchFact) ([]uuid.UUID, error) {
	if fact.AggregateID == nil {
		return nil, nil
	}
	query := ""
	args := []any{*fact.AggregateID}
	switch fact.AggregateType {
	case "RESERVATION":
		query = `SELECT partner_id FROM reservations WHERE id=$1`
	case "TICKET":
		query = `SELECT DISTINCT s.partner_id FROM ticket_entitlements t JOIN sale_items si ON si.id=t.origin_sale_item_id JOIN sales s ON s.id=si.sale_id WHERE t.id=$1`
	case "ADMISSION":
		query = `SELECT DISTINCT s.partner_id FROM admissions a JOIN ticket_entitlements t ON t.id=a.ticket_entitlement_id JOIN sale_items si ON si.id=t.origin_sale_item_id JOIN sales s ON s.id=si.sale_id WHERE a.id=$1`
	case "PARTNER":
		query = `SELECT id FROM partners WHERE id=$1`
	case "EVENT":
		query = `SELECT partner_id FROM partner_event_access WHERE event_id=$1 AND state='ACTIVE'`
	default:
		return nil, nil
	}
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func retryDelay(attempt int, random float64) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if random < 0 {
		random = 0
	} else if random > 1 {
		random = 1
	}
	base := time.Second * time.Duration(1<<min(attempt-1, 9))
	if base > 5*time.Minute {
		base = 5 * time.Minute
	}
	delay := time.Duration(float64(base) * (0.8 + 0.4*random))
	if delay < time.Second {
		return time.Second
	}
	if delay > 5*time.Minute {
		return 5 * time.Minute
	}
	return delay
}

func jitteredRetryDelay(attempt int) time.Duration { return retryDelay(attempt, rand.Float64()) }
