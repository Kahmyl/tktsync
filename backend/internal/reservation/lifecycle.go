package reservation

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

func (s *Service) MaterializeDue(
	ctx context.Context,
	reservationID uuid.UUID,
) error {
	if reservationID == uuid.Nil {
		return apierror.New(
			apierror.CodeValidation,
			"Reservation is required",
		)
	}

	return s.transactions.Run(
		ctx,
		func(tx pgx.Tx) error {
			eventID, err :=
				reservationEventID(
					ctx,
					tx,
					reservationID,
				)
			if err != nil {
				return err
			}

			gate, err := lockEventGate(
				ctx,
				tx,
				eventID,
			)
			if err != nil {
				return err
			}

			record, err :=
				lockReservation(
					ctx,
					tx,
					reservationID,
				)
			if err != nil {
				return err
			}

			if gate.State == "CANCELLED" {
				switch record.State {
				case "COMMITTING":
					attempt, err := lockActiveCheckout(ctx, tx, record.ID)
					if err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return nil
						}
						return err
					}
					now, err := clockTimestamp(ctx, tx)
					if err != nil {
						return err
					}
					_, err = s.transitionToReconciliation(ctx, tx, record, gate.Policy, attempt, now, "", "EVENT_CANCELLED", true)
					return err

				case "HELD", "PAYMENT_RETRY":
					items, err := loadActiveItems(ctx, tx, record.ID)
					if err != nil {
						return err
					}
					allocations, err := s.lockMutationResources(ctx, tx, record.EventID, items, nil)
					if err != nil {
						return err
					}
					now, err := clockTimestamp(ctx, tx)
					if err != nil {
						return err
					}
					return expireReservationLocked(ctx, tx, s, record, items, allocations, now, "EVENT_CANCELLED", true)
				}
			}

			switch record.State {
			case "COMMITTING":
				attempt, err :=
					lockActiveCheckout(
						ctx,
						tx,
						record.ID,
					)
				if err != nil {
					if errors.Is(
						err,
						pgx.ErrNoRows,
					) {
						return nil
					}

					return err
				}

				now, err :=
					clockTimestamp(
						ctx,
						tx,
					)
				if err != nil {
					return err
				}

				if attempt.
					ProtectionExpiresAt.
					After(now) {
					return nil
				}

				_, err =
					s.transitionToReconciliation(
						ctx,
						tx,
						record,
						gate.Policy,
						attempt,
						now,
						"",
						"CHECKOUT_PROTECTION_EXPIRED",
						true,
					)

				return err

			case "HELD",
				"PAYMENT_RETRY",
				"RECONCILING":
				items, err :=
					loadActiveItems(
						ctx,
						tx,
						record.ID,
					)
				if err != nil {
					return err
				}

				allocations, err :=
					s.lockMutationResources(
						ctx,
						tx,
						record.EventID,
						items,
						nil,
					)
				if err != nil {
					return err
				}

				now, err :=
					clockTimestamp(
						ctx,
						tx,
					)
				if err != nil {
					return err
				}

				reason := ""

				switch record.State {
				case "HELD":
					if record.
						HoldExpiresAt.
						After(now) {
						return nil
					}

					reason =
						"HOLD_EXPIRED"

				case "PAYMENT_RETRY":
					if record.
						PaymentRetryExpiresAt !=
						nil &&
						record.
							PaymentRetryExpiresAt.
							After(now) {
						return nil
					}

					reason =
						"PAYMENT_RETRY_EXPIRED"

				case "RECONCILING":
					if record.
						ReconciliationExpiresAt !=
						nil &&
						record.
							ReconciliationExpiresAt.
							After(now) {
						return nil
					}

					reason =
						"RECONCILIATION_EXPIRED"
				}

				return expireReservationLocked(
					ctx,
					tx,
					s,
					record,
					items,
					allocations,
					now,
					reason,
					true,
				)

			default:
				return nil
			}
		},
	)
}

func (s *Service) transitionToReconciliation(
	ctx context.Context,
	tx pgx.Tx,
	record reservationRecord,
	policy eventPolicy,
	attempt checkoutAttemptRecord,
	now time.Time,
	partnerPaymentRef string,
	outcomeCode string,
	system bool,
) (Reconciliation, error) {
	deadline := now.Add(
		time.Duration(
			policy.ReconciliationSeconds,
		) * time.Second,
	)

	if deadline.After(
		record.MaxLifetimeAt,
	) {
		deadline =
			record.MaxLifetimeAt
	}

	if _, err := tx.Exec(
		ctx,
		`
			UPDATE checkout_attempts
			SET
				state = 'UNCERTAIN',
				partner_payment_ref =
				    COALESCE(
				        NULLIF($2, ''),
				        partner_payment_ref
				    ),
				partner_outcome_code =
				    COALESCE(
				        NULLIF($3, ''),
				        partner_outcome_code
				    ),
				completed_at = $4
			WHERE id = $1
			  AND state = 'ACTIVE'
		`,
		attempt.ID,
		partnerPaymentRef,
		outcomeCode,
		now,
	); err != nil {
		return Reconciliation{}, err
	}

	if _, err := tx.Exec(
		ctx,
		`
			UPDATE reservations
			SET
				state = 'RECONCILING',
				reconciliation_expires_at =
				    $2,
				payment_retry_expires_at =
				    NULL,
				updated_at = $3
			WHERE id = $1
		`,
		record.ID,
		deadline,
		now,
	); err != nil {
		return Reconciliation{}, err
	}

	operation :=
		"RESERVATION_PAYMENT_UNCERTAIN"
	fact :=
		"reservation.payment_uncertain"

	previous := map[string]any{
		"state": "COMMITTING",
	}

	next := map[string]any{
		"state":                     "RECONCILING",
		"reconciliation_expires_at": deadline,
	}

	var err error

	if system {
		err = s.appendSystemFact(
			ctx,
			tx,
			record.EventID,
			record.PartnerID,
			record.ID,
			operation,
			fact,
			previous,
			next,
		)
	} else {
		err = s.appendPartnerFact(
			ctx,
			tx,
			record.EventID,
			record.PartnerID,
			record.ID,
			operation,
			fact,
			previous,
			next,
		)
	}
	if err != nil {
		return Reconciliation{}, err
	}

	return Reconciliation{
		ReservationID:           record.ID,
		State:                   "RECONCILING",
		ReconciliationExpiresAt: deadline,
	}, nil
}

func (s *Service) expireWithBusinessError(
	ctx context.Context,
	tx pgx.Tx,
	record reservationRecord,
	now time.Time,
	business error,
	output *error,
	system bool,
) error {
	items, err :=
		loadActiveItems(
			ctx,
			tx,
			record.ID,
		)
	if err != nil {
		return err
	}

	allocations, err :=
		s.lockMutationResources(
			ctx,
			tx,
			record.EventID,
			items,
			nil,
		)
	if err != nil {
		return err
	}

	now, err = clockTimestamp(
		ctx,
		tx,
	)
	if err != nil {
		return err
	}

	reason :=
		"HOLD_EXPIRED"

	switch record.State {
	case "PAYMENT_RETRY":
		reason =
			"PAYMENT_RETRY_EXPIRED"

	case "RECONCILING":
		reason =
			"RECONCILIATION_EXPIRED"
	}

	if err :=
		expireReservationLocked(
			ctx,
			tx,
			s,
			record,
			items,
			allocations,
			now,
			reason,
			system,
		); err != nil {
		return err
	}

	*output = business

	return nil
}

func expireReservationLocked(
	ctx context.Context,
	tx pgx.Tx,
	service *Service,
	record reservationRecord,
	items []activeItem,
	allocations map[uuid.UUID]allocationInfo,
	now time.Time,
	reason string,
	system bool,
) error {
	if err :=
		releaseActiveItems(
			ctx,
			tx,
			items,
			allocations,
			now,
			reason,
			false,
		); err != nil {
		return err
	}

	if record.State ==
		"RECONCILING" {
		if _, err := tx.Exec(
			ctx,
			`
				UPDATE checkout_attempts
				SET
					state = 'ABANDONED',
					completed_at =
					    COALESCE(
					        completed_at,
					        $2
					    )
				WHERE id = (
					SELECT id
					FROM checkout_attempts
					WHERE reservation_id = $1
					  AND state = 'UNCERTAIN'
					ORDER BY attempt_number DESC
					LIMIT 1
				)
			`,
			record.ID,
			now,
		); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(
		ctx,
		`
			UPDATE reservations
			SET
				state = 'EXPIRED',
				expired_at = $2,
				terminal_reason = $3,
				updated_at = $2
			WHERE id = $1
		`,
		record.ID,
		now,
		reason,
	); err != nil {
		return err
	}

	previous := map[string]any{
		"state": record.State,
	}

	next := map[string]any{
		"state":           "EXPIRED",
		"terminal_reason": reason,
	}

	if system {
		return service.
			appendSystemFact(
				ctx,
				tx,
				record.EventID,
				record.PartnerID,
				record.ID,
				"RESERVATION_EXPIRED",
				"reservation.expired",
				previous,
				next,
			)
	}

	return service.
		appendPartnerFact(
			ctx,
			tx,
			record.EventID,
			record.PartnerID,
			record.ID,
			"RESERVATION_EXPIRED",
			"reservation.expired",
			previous,
			next,
		)
}

func reservationEventID(
	ctx context.Context,
	tx pgx.Tx,
	reservationID uuid.UUID,
) (uuid.UUID, error) {
	var eventID uuid.UUID

	err := tx.QueryRow(
		ctx,
		`
			SELECT event_id
			FROM reservations
			WHERE id = $1
		`,
		reservationID,
	).Scan(
		&eventID,
	)
	if err != nil {
		if errors.Is(
			err,
			pgx.ErrNoRows,
		) {
			return uuid.Nil,
				apierror.New(
					apierror.CodeResourceNotFound,
					"Reservation not found",
				)
		}

		return uuid.Nil, err
	}

	return eventID, nil
}

func lockReservation(
	ctx context.Context,
	tx pgx.Tx,
	reservationID uuid.UUID,
) (reservationRecord, error) {
	var (
		record reservationRecord
		retry  pgtype.Timestamptz
		recon  pgtype.Timestamptz
	)

	err := tx.QueryRow(
		ctx,
		`
			SELECT
				id,
				event_id,
				partner_id,
				continuation_token_hash,
				continuation_token_key_version,
				currency,
				state,
				hold_expires_at,
				payment_retry_expires_at,
				reconciliation_expires_at,
				max_lifetime_at
			FROM reservations
			WHERE id = $1
			FOR UPDATE
		`,
		reservationID,
	).Scan(
		&record.ID,
		&record.EventID,
		&record.PartnerID,
		&record.TokenHash,
		&record.TokenKeyVersion,
		&record.Currency,
		&record.State,
		&record.HoldExpiresAt,
		&retry,
		&recon,
		&record.MaxLifetimeAt,
	)
	if err != nil {
		if errors.Is(
			err,
			pgx.ErrNoRows,
		) {
			return reservationRecord{},
				apierror.New(
					apierror.CodeResourceNotFound,
					"Reservation not found",
				)
		}

		return reservationRecord{}, err
	}

	record.PaymentRetryExpiresAt =
		nullableTime(
			retry,
		)

	record.ReconciliationExpiresAt =
		nullableTime(
			recon,
		)

	return record, nil
}

func lockCheckoutAttemptByState(
	ctx context.Context,
	tx pgx.Tx,
	reservationID uuid.UUID,
	checkoutAttemptID uuid.UUID,
	state string,
) (checkoutAttemptRecord, error) {
	var result checkoutAttemptRecord

	err := tx.QueryRow(
		ctx,
		`
			SELECT
				id,
				attempt_number,
				state,
				protection_expires_at
			FROM checkout_attempts
			WHERE reservation_id = $1
			  AND id = $2
			  AND state = $3
			FOR UPDATE
		`,
		reservationID,
		checkoutAttemptID,
		state,
	).Scan(
		&result.ID,
		&result.AttemptNumber,
		&result.State,
		&result.ProtectionExpiresAt,
	)
	if err != nil {
		if errors.Is(
			err,
			pgx.ErrNoRows,
		) {
			return checkoutAttemptRecord{},
				apierror.New(
					apierror.CodeValidation,
					"checkout_attempt_id is not the current protected checkout attempt",
				)
		}

		return checkoutAttemptRecord{},
			err
	}

	return result, nil
}

func lockActiveCheckout(
	ctx context.Context,
	tx pgx.Tx,
	reservationID uuid.UUID,
) (checkoutAttemptRecord, error) {
	var result checkoutAttemptRecord

	err := tx.QueryRow(
		ctx,
		`
			SELECT
				id,
				attempt_number,
				state,
				protection_expires_at
			FROM checkout_attempts
			WHERE reservation_id = $1
			  AND state = 'ACTIVE'
			FOR UPDATE
		`,
		reservationID,
	).Scan(
		&result.ID,
		&result.AttemptNumber,
		&result.State,
		&result.ProtectionExpiresAt,
	)
	if err != nil {
		if errors.Is(
			err,
			pgx.ErrNoRows,
		) {
			return checkoutAttemptRecord{},
				apierror.New(
					apierror.CodeCheckoutWindowExpired,
					"Active checkout attempt was not found",
				)
		}

		return checkoutAttemptRecord{},
			err
	}

	return result, nil
}
