package reservation

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
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

func (m *Materializer) RunOnce(ctx context.Context) error {
	_, err := m.RunOnceWithProgress(ctx)
	return err
}

func (m *Materializer) RunOnceWithProgress(
	ctx context.Context,
) (bool, error) {
	rows, err := m.db.Query(
		ctx,
		`
			SELECT DISTINCT r.event_id
			FROM reservations r
			JOIN events e
			  ON e.id = r.event_id
			LEFT JOIN checkout_attempts ca
			  ON ca.reservation_id = r.id
			 AND ca.state = 'ACTIVE'
			WHERE
				(
					r.state = 'HELD'
					AND r.hold_expires_at <= clock_timestamp()
				)
				OR (
					r.state = 'PAYMENT_RETRY'
					AND (
						r.payment_retry_expires_at IS NULL
						OR r.payment_retry_expires_at <= clock_timestamp()
					)
				)
				OR (
					r.state = 'RECONCILING'
					AND (
						r.reconciliation_expires_at IS NULL
						OR r.reconciliation_expires_at <= clock_timestamp()
					)
				)
				OR (
					r.state = 'COMMITTING'
					AND ca.protection_expires_at <= clock_timestamp()
				)
				OR (
					e.state = 'CANCELLED'
					AND r.state IN ('HELD','PAYMENT_RETRY','COMMITTING')
				)
			ORDER BY r.event_id
			LIMIT $1
		`,
		m.batchSize,
	)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	eventIDs := make([]uuid.UUID, 0, m.batchSize)
	for rows.Next() {
		var eventID uuid.UUID
		if err := rows.Scan(&eventID); err != nil {
			return false, err
		}
		eventIDs = append(eventIDs, eventID)
	}
	if err := rows.Err(); err != nil {
		return false, err
	}

	worked := false
	var combined error

	for _, eventID := range eventIDs {
		processed, processErr := m.processNextForEvent(ctx, eventID)
		if processed {
			worked = true
		}
		if processErr != nil {
			combined = errors.Join(combined, processErr)
		}
	}

	return worked, combined
}

func (m *Materializer) processNextForEvent(
	ctx context.Context,
	eventID uuid.UUID,
) (bool, error) {
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() {
		_ = tx.Rollback(context.Background())
	}()

	if _, err = lockEventGate(ctx, tx, eventID); err != nil {
		return false, err
	}

	var reservationID uuid.UUID
	err = tx.QueryRow(
		ctx,
		`
			SELECT r.id
			FROM reservations r
			JOIN events e
			  ON e.id = r.event_id
			LEFT JOIN checkout_attempts ca
			  ON ca.reservation_id = r.id
			 AND ca.state = 'ACTIVE'
			WHERE r.event_id = $1
			  AND (
				(
					r.state = 'HELD'
					AND r.hold_expires_at <= clock_timestamp()
				)
				OR (
					r.state = 'PAYMENT_RETRY'
					AND (
						r.payment_retry_expires_at IS NULL
						OR r.payment_retry_expires_at <= clock_timestamp()
					)
				)
				OR (
					r.state = 'RECONCILING'
					AND (
						r.reconciliation_expires_at IS NULL
						OR r.reconciliation_expires_at <= clock_timestamp()
					)
				)
				OR (
					r.state = 'COMMITTING'
					AND ca.protection_expires_at <= clock_timestamp()
				)
				OR (
					e.state = 'CANCELLED'
					AND r.state IN ('HELD','PAYMENT_RETRY','COMMITTING')
				)
			  )
			ORDER BY r.id
			FOR UPDATE OF r
			SKIP LOCKED
			LIMIT 1
		`,
		eventID,
	).Scan(&reservationID)
	if errors.Is(err, pgx.ErrNoRows) {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return false, commitErr
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}

	txContext := database.WithTransaction(ctx, tx)
	if err = m.service.MaterializeDue(txContext, reservationID); err != nil {
		return true, err
	}

	if err = tx.Commit(ctx); err != nil {
		return true, err
	}

	return true, nil
}
