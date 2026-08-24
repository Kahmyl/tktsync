package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const RealtimeChannel = "tktsync_realtime"

type QueryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type Fact struct {
	FactID        uuid.UUID
	EventID       *uuid.UUID
	FactType      string
	AggregateType string
	AggregateID   *uuid.UUID
	Payload       any
}

type realtimeNotice struct {
	FactID        uuid.UUID  `json:"fact_id"`
	EventID       uuid.UUID  `json:"event_id"`
	FactType      string     `json:"fact_type"`
	AggregateType string     `json:"aggregate_type"`
	AggregateID   *uuid.UUID `json:"aggregate_id,omitempty"`
}

type Store struct{}

func (Store) Append(
	ctx context.Context,
	q QueryRower,
	fact Fact,
) (uuid.UUID, error) {
	if fact.FactType == "" || fact.AggregateType == "" {
		return uuid.Nil, errors.New("outbox fact type and aggregate type are required")
	}

	if fact.FactID == uuid.Nil {
		fact.FactID = uuid.New()
	}

	payload := fact.Payload
	if payload == nil {
		payload = map[string]any{}
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal outbox payload: %w", err)
	}

	id := uuid.New()

	err = q.QueryRow(
		ctx,
		`
			INSERT INTO outbox_events (
				id,
				fact_id,
				event_id,
				fact_type,
				aggregate_type,
				aggregate_id,
				payload,
				created_at,
				attempt_count
			)
			VALUES (
				$1,$2,$3,$4,$5,$6,$7,clock_timestamp(),0
			)
			RETURNING id
		`,
		id,
		fact.FactID,
		fact.EventID,
		fact.FactType,
		fact.AggregateType,
		fact.AggregateID,
		json.RawMessage(raw),
	).Scan(&id)
	if err != nil {
		return uuid.Nil, err
	}

	if fact.EventID != nil {
		notice, marshalErr := json.Marshal(realtimeNotice{
			FactID:        fact.FactID,
			EventID:       *fact.EventID,
			FactType:      fact.FactType,
			AggregateType: fact.AggregateType,
			AggregateID:   fact.AggregateID,
		})
		if marshalErr != nil {
			return uuid.Nil, marshalErr
		}
		if len(notice) > 7000 {
			return uuid.Nil, errors.New("realtime invalidation payload is too large")
		}
		if _, err = q.Exec(
			ctx,
			`SELECT pg_notify('tktsync_realtime', $1)`,
			string(notice),
		); err != nil {
			return uuid.Nil, fmt.Errorf("publish realtime invalidation: %w", err)
		}
	}

	return id, nil
}
