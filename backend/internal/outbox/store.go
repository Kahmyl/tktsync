package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type QueryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Fact struct {
	FactID        uuid.UUID
	EventID       *uuid.UUID
	FactType      string
	AggregateType string
	AggregateID   *uuid.UUID
	Payload       any
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

	return id, nil
}
