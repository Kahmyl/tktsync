package realtimeapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tktsync/tktsync/backend/internal/outbox"
)

type Listener struct {
	db     *pgxpool.Pool
	hub    *Hub
	logger *slog.Logger
}

type wireNotice struct {
	FactID        uuid.UUID  `json:"fact_id"`
	EventID       uuid.UUID  `json:"event_id"`
	FactType      string     `json:"fact_type"`
	AggregateType string     `json:"aggregate_type"`
	AggregateID   *uuid.UUID `json:"aggregate_id,omitempty"`
}

func NewListener(
	db *pgxpool.Pool,
	hub *Hub,
	logger *slog.Logger,
) *Listener {
	return &Listener{db: db, hub: hub, logger: logger}
}

func (l *Listener) Run(ctx context.Context) error {
	if l == nil || l.db == nil || l.hub == nil {
		return errors.New("realtime listener dependencies are incomplete")
	}

	for ctx.Err() == nil {
		err := l.listen(ctx)
		if ctx.Err() != nil {
			return nil
		}

		if l.logger != nil {
			l.logger.Warn(
				"realtime listener reconnecting",
				"operation", "realtime.listen",
				"error", err,
			)
		}

		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}

	return nil
}

func (l *Listener) listen(ctx context.Context) error {
	conn, err := l.db.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err = conn.Exec(ctx, "LISTEN "+outbox.RealtimeChannel); err != nil {
		return err
	}

	for {
		notification, waitErr := conn.Conn().WaitForNotification(ctx)
		if waitErr != nil {
			return waitErr
		}

		var notice wireNotice
		if json.Unmarshal([]byte(notification.Payload), &notice) != nil {
			if l.logger != nil {
				l.logger.Warn(
					"invalid realtime notification ignored",
					"operation", "realtime.notify",
				)
			}
			continue
		}

		l.hub.Publish(Fact{
			FactID:        notice.FactID,
			EventID:       notice.EventID,
			FactType:      notice.FactType,
			AggregateType: notice.AggregateType,
			AggregateID:   notice.AggregateID,
		})
	}
}
