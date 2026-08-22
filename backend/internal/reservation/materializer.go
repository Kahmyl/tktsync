package reservation

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Materializer struct {
	db        *pgxpool.Pool
	service   *Service
	batchSize int
}

func NewMaterializer(
	db *pgxpool.Pool,
	service *Service,
	batchSize int,
) *Materializer {
	if batchSize <= 0 {
		batchSize = 100
	}

	return &Materializer{
		db:        db,
		service:   service,
		batchSize: batchSize,
	}
}

func (m *Materializer) RunOnce(
	ctx context.Context,
) error {
	rows, err := m.db.Query(
		ctx,
		`
			SELECT DISTINCT
				r.id,
				r.event_id
			FROM reservations r
			LEFT JOIN checkout_attempts ca
			  ON ca.reservation_id = r.id
			 AND ca.state = 'ACTIVE'
			WHERE (
			    r.state = 'HELD'
			    AND r.hold_expires_at <=
			        clock_timestamp()
			)
			OR (
			    r.state = 'PAYMENT_RETRY'
			    AND (
			        r.payment_retry_expires_at IS NULL
			        OR r.payment_retry_expires_at <=
			           clock_timestamp()
			    )
			)
			OR (
			    r.state = 'RECONCILING'
			    AND (
			        r.reconciliation_expires_at IS NULL
			        OR r.reconciliation_expires_at <=
			           clock_timestamp()
			    )
			)
			OR (
			    r.state = 'COMMITTING'
			    AND ca.protection_expires_at <=
			        clock_timestamp()
			)
			ORDER BY r.event_id, r.id
			LIMIT $1
		`,
		m.batchSize,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	ids := make(
		[]uuid.UUID,
		0,
		m.batchSize,
	)

	for rows.Next() {
		var (
			reservationID uuid.UUID
			eventID       uuid.UUID
		)

		if err := rows.Scan(
			&reservationID,
			&eventID,
		); err != nil {
			return err
		}

		_ = eventID

		ids = append(
			ids,
			reservationID,
		)
	}

	if err := rows.Err(); err != nil {
		return err
	}

	var combined error

	for _, reservationID := range ids {
		if err := m.service.
			MaterializeDue(
				ctx,
				reservationID,
			); err != nil {
			combined =
				errors.Join(
					combined,
					err,
				)
		}
	}

	return combined
}
