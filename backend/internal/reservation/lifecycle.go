package reservation

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
)

type reservationRecord struct {
	ID                      uuid.UUID
	EventID                 uuid.UUID
	PartnerID               uuid.UUID
	TokenHash               []byte
	TokenKeyVersion         int
	Currency                string
	State                   string
	HoldExpiresAt           time.Time
	PaymentRetryExpiresAt   *time.Time
	ReconciliationExpiresAt *time.Time
	MaxLifetimeAt           time.Time
}

type checkoutAttemptRecord struct {
	ID                  uuid.UUID
	AttemptNumber       int
	State               string
	ProtectionExpiresAt time.Time
}

func (s *Service) BeginCheckout(
	ctx context.Context,
	partnerID uuid.UUID,
	rawToken string,
) (Checkout, error) {
	parts, err :=
		parseReservationToken(
			rawToken,
		)
	if err != nil {
		return Checkout{}, err
	}

	var (
		result      Checkout
		businessErr error
	)

	err = s.transactions.Run(
		ctx,
		func(tx pgx.Tx) error {
			businessErr = nil

			eventID, err :=
				reservationEventID(
					ctx,
					tx,
					parts.ReservationID,
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
					parts.ReservationID,
				)
			if err != nil {
				return err
			}

			if err :=
				s.verifyLockedToken(
					rawToken,
					parts,
					record,
					partnerID,
				); err != nil {
				return err
			}

			now, err := clockTimestamp(
				ctx,
				tx,
			)
			if err != nil {
				return err
			}

			switch gate.State {
			case "ON_SALE", "PAUSED", "SALES_CLOSED":
			case "CANCELLED":
				return apierror.New(
					apierror.CodeEventCancelled,
					"Cancelled Event cannot begin checkout",
				)

			case "COMPLETED":
				return apierror.New(
					apierror.CodeEventSalesClosed,
					"Completed Event cannot begin checkout because sales are closed",
				)

			default:
				return apierror.New(
					apierror.CodeEventNotOnSale,
					"Existing Reservation cannot begin checkout for this Event lifecycle state",
				)
			}

			switch record.State {
			case "HELD":
				if !record.HoldExpiresAt.
					After(now) {
					return s.expireWithBusinessError(
						ctx,
						tx,
						record,
						now,
						apierror.New(
							apierror.CodeHoldExpired,
							"Reservation hold has expired",
						),
						&businessErr,
						false,
					)
				}

			case "PAYMENT_RETRY":
				if record.
					PaymentRetryExpiresAt ==
					nil ||
					!record.
						PaymentRetryExpiresAt.
						After(now) {
					return s.expireWithBusinessError(
						ctx,
						tx,
						record,
						now,
						apierror.New(
							apierror.CodeCheckoutWindowExpired,
							"Payment retry window has expired",
						),
						&businessErr,
						false,
					)
				}

			case "COMMITTING":
				attempt, err :=
					lockActiveCheckout(
						ctx,
						tx,
						record.ID,
					)
				if err != nil {
					return err
				}

				now, err =
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
					return apierror.New(
						apierror.CodeCheckoutAlreadyActive,
						"Checkout is already active",
					)
				}

				reconciliation, err :=
					s.transitionToReconciliation(
						ctx,
						tx,
						record,
						gate.Policy,
						attempt,
						now,
						"",
						"CHECKOUT_PROTECTION_EXPIRED",
						false,
					)
				if err != nil {
					return err
				}

				_ = reconciliation

				businessErr =
					apierror.New(
						apierror.CodePaymentStatusUncertain,
						"Payment outcome is uncertain and inventory remains protected",
					)

				return nil

			case "RECONCILING":
				if record.
					ReconciliationExpiresAt !=
					nil &&
					!record.
						ReconciliationExpiresAt.
						After(now) {
					return s.expireWithBusinessError(
						ctx,
						tx,
						record,
						now,
						apierror.New(
							apierror.CodeReconciliationExpired,
							"Reconciliation window has expired",
						),
						&businessErr,
						false,
					)
				}

				return apierror.New(
					apierror.CodePaymentStatusUncertain,
					"Payment outcome is uncertain and inventory remains protected",
				)

			case "CONFIRMED":
				return apierror.New(
					apierror.CodeAlreadyConfirmed,
					"Reservation is already confirmed",
				)

			case "RELEASED", "EXPIRED":
				return apierror.New(
					apierror.CodeHoldExpired,
					"Reservation is no longer active",
				)

			default:
				return apierror.New(
					apierror.CodeReservationNotModifiable,
					"Reservation cannot enter checkout from its current state",
				)
			}

			attemptNumber := 1

			if err := tx.QueryRow(
				ctx,
				`
					SELECT
						COALESCE(
							MAX(attempt_number),
							0
						) + 1
					FROM checkout_attempts
					WHERE reservation_id = $1
				`,
				record.ID,
			).Scan(
				&attemptNumber,
			); err != nil {
				return err
			}

			commitExpiresAt :=
				now.Add(
					time.Duration(
						gate.Policy.
							CheckoutProtectionSeconds,
					) * time.Second,
				)

			if commitExpiresAt.After(
				record.MaxLifetimeAt,
			) {
				commitExpiresAt =
					record.MaxLifetimeAt
			}

			attemptID := uuid.New()

			if _, err := tx.Exec(
				ctx,
				`
					INSERT INTO checkout_attempts (
						id,
						reservation_id,
						attempt_number,
						state,
						protection_expires_at,
						started_at,
						created_at
					)
					VALUES (
						$1,
						$2,
						$3,
						'ACTIVE',
						$4,
						$5,
						$5
					)
				`,
				attemptID,
				record.ID,
				attemptNumber,
				commitExpiresAt,
				now,
			); err != nil {
				return err
			}

			if _, err := tx.Exec(
				ctx,
				`
					UPDATE reservations
					SET
						state = 'COMMITTING',
						payment_retry_expires_at = NULL,
						reconciliation_expires_at = NULL,
						updated_at = $2
					WHERE id = $1
				`,
				record.ID,
				now,
			); err != nil {
				return err
			}

			if err :=
				s.appendPartnerFact(
					ctx,
					tx,
					record.EventID,
					record.PartnerID,
					record.ID,
					"RESERVATION_CHECKOUT_BEGUN",
					"reservation.checkout_begun",
					map[string]any{
						"state": record.State,
					},
					map[string]any{
						"state":               "COMMITTING",
						"checkout_attempt_id": attemptID,
						"commit_expires_at":   commitExpiresAt,
					},
				); err != nil {
				return err
			}

			result = Checkout{
				ReservationID:     record.ID,
				CheckoutAttemptID: attemptID,
				AttemptNumber:     attemptNumber,
				State:             "COMMITTING",
				CommitExpiresAt:   commitExpiresAt,
			}

			return nil
		},
	)
	if err != nil {
		return Checkout{}, err
	}

	if businessErr != nil {
		return Checkout{}, businessErr
	}

	return result, nil
}

func (s *Service) PaymentFailure(
	ctx context.Context,
	partnerID uuid.UUID,
	rawToken string,
	checkoutAttemptID uuid.UUID,
	partnerPaymentRef string,
	outcomeCode string,
	requestedDisposition string,
) (Retry, error) {
	if checkoutAttemptID == uuid.Nil {
		return Retry{}, apierror.New(
			apierror.CodeValidation,
			"checkout_attempt_id is required",
		)
	}

	requestedDisposition =
		strings.ToUpper(
			strings.TrimSpace(
				requestedDisposition,
			),
		)

	if requestedDisposition != "RETRY" &&
		requestedDisposition != "RELEASE" {
		return Retry{}, apierror.New(
			apierror.CodeValidation,
			"requested_disposition must be RETRY or RELEASE",
		)
	}

	parts, err :=
		parseReservationToken(
			rawToken,
		)
	if err != nil {
		return Retry{}, err
	}

	var (
		result      Retry
		businessErr error
	)

	err = s.transactions.Run(
		ctx,
		func(tx pgx.Tx) error {
			businessErr = nil

			eventID, err :=
				reservationEventID(
					ctx,
					tx,
					parts.ReservationID,
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
					parts.ReservationID,
				)
			if err != nil {
				return err
			}

			if err :=
				s.verifyLockedToken(
					rawToken,
					parts,
					record,
					partnerID,
				); err != nil {
				return err
			}

			if record.State ==
				"CONFIRMED" {
				return apierror.New(
					apierror.CodeAlreadyConfirmed,
					"Reservation is already confirmed",
				)
			}

			if record.State !=
				"COMMITTING" &&
				record.State !=
					"RECONCILING" {
				return apierror.New(
					apierror.CodeReservationNotModifiable,
					"Reservation is not in protected checkout or reconciliation",
				)
			}

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

			attemptState := "ACTIVE"

			if record.State ==
				"RECONCILING" {
				attemptState =
					"UNCERTAIN"
			}

			_, err =
				lockCheckoutAttemptByState(
					ctx,
					tx,
					record.ID,
					checkoutAttemptID,
					attemptState,
				)
			if err != nil {
				return err
			}

			businessCtx :=
				database.WithTransaction(
					ctx,
					tx,
				)

			if record.State ==
				"COMMITTING" {
				retry, err :=
					s.PaymentFailed(
						businessCtx,
						partnerID,
						rawToken,
						partnerPaymentRef,
						outcomeCode,
					)
				if err != nil {
					if apiErr, ok :=
						apierror.As(err); ok &&
						apiErr.Code ==
							apierror.CodePaymentStatusUncertain {
						businessErr = apiErr
						return nil
					}

					return err
				}

				if requestedDisposition ==
					"RELEASE" {
					if err := s.Release(
						businessCtx,
						partnerID,
						rawToken,
					); err != nil {
						if apiErr, ok :=
							apierror.As(err); ok &&
							(apiErr.Code ==
								apierror.CodeHoldExpired ||
								apiErr.Code ==
									apierror.CodeCheckoutWindowExpired ||
								apiErr.Code ==
									apierror.CodeReconciliationExpired ||
								apiErr.Code ==
									apierror.CodePaymentStatusUncertain) {
							businessErr = apiErr
							return nil
						}

						return err
					}

					result = Retry{
						ReservationID: record.ID,
						State:         "RELEASED",
					}

					return nil
				}

				result = retry
				return nil
			}

			now, err := clockTimestamp(
				ctx,
				tx,
			)
			if err != nil {
				return err
			}

			if record.
				ReconciliationExpiresAt ==
				nil ||
				!record.
					ReconciliationExpiresAt.
					After(now) {
				if err :=
					expireReservationLocked(
						ctx,
						tx,
						s,
						record,
						items,
						allocations,
						now,
						"RECONCILIATION_EXPIRED",
						false,
					); err != nil {
					return err
				}

				businessErr =
					apierror.New(
						apierror.CodeReconciliationExpired,
						"Reconciliation window has expired",
					)

				return nil
			}

			if requestedDisposition ==
				"RELEASE" {
				if _, err := tx.Exec(
					ctx,
					`
						UPDATE checkout_attempts
						SET partner_payment_ref =
						    COALESCE(
						        NULLIF($2, ''),
						        partner_payment_ref
						    )
						WHERE id = $1
						  AND reservation_id = $3
						  AND state = 'UNCERTAIN'
					`,
					checkoutAttemptID,
					partnerPaymentRef,
					record.ID,
				); err != nil {
					return err
				}

				if err :=
					s.ResolveNoPayment(
						businessCtx,
						partnerID,
						rawToken,
						outcomeCode,
					); err != nil {
					if apiErr, ok :=
						apierror.As(err); ok &&
						apiErr.Code ==
							apierror.CodeReconciliationExpired {
						businessErr = apiErr
						return nil
					}

					return err
				}

				result = Retry{
					ReservationID: record.ID,
					State:         "RELEASED",
				}

				return nil
			}

			retryExpiresAt :=
				now.Add(
					time.Duration(
						gate.Policy.
							PaymentRetrySeconds,
					) * time.Second,
				)

			if retryExpiresAt.After(
				record.MaxLifetimeAt,
			) {
				retryExpiresAt =
					record.MaxLifetimeAt
			}

			if !retryExpiresAt.After(now) {
				businessErr =
					apierror.New(
						apierror.CodeCheckoutWindowExpired,
						"No payment retry time remains",
					)

				return nil
			}

			commandTag, err := tx.Exec(
				ctx,
				`
					UPDATE checkout_attempts
					SET
						state = 'PAYMENT_FAILED',
						partner_payment_ref =
						    COALESCE(
						        NULLIF($2, ''),
						        partner_payment_ref
						    ),
						partner_outcome_code =
						    NULLIF($3, ''),
						completed_at =
						    COALESCE(
						        completed_at,
						        $4
						    )
					WHERE id = $1
					  AND reservation_id = $5
					  AND state = 'UNCERTAIN'
				`,
				checkoutAttemptID,
				partnerPaymentRef,
				outcomeCode,
				now,
				record.ID,
			)
			if err != nil {
				return err
			}

			if commandTag.RowsAffected() != 1 {
				return apierror.New(
					apierror.CodeValidation,
					"checkout_attempt_id is not the current uncertain checkout attempt",
				)
			}

			commandTag, err = tx.Exec(
				ctx,
				`
					UPDATE reservations
					SET
						state = 'PAYMENT_RETRY',
						payment_retry_expires_at = $2,
						reconciliation_expires_at = NULL,
						updated_at = $3
					WHERE id = $1
					  AND state = 'RECONCILING'
				`,
				record.ID,
				retryExpiresAt,
				now,
			)
			if err != nil {
				return err
			}

			if commandTag.RowsAffected() != 1 {
				return apierror.New(
					apierror.CodeInternal,
					"Reconciling Reservation changed during definitive payment failure",
				)
			}

			if err :=
				s.appendPartnerFact(
					ctx,
					tx,
					record.EventID,
					record.PartnerID,
					record.ID,
					"RESERVATION_PAYMENT_FAILED",
					"reservation.payment_failed",
					map[string]any{
						"state": "RECONCILING",
					},
					map[string]any{
						"state":                    "PAYMENT_RETRY",
						"payment_retry_expires_at": retryExpiresAt,
					},
				); err != nil {
				return err
			}

			result = Retry{
				ReservationID:         record.ID,
				State:                 "PAYMENT_RETRY",
				PaymentRetryExpiresAt: retryExpiresAt,
			}

			return nil
		},
	)
	if err != nil {
		return Retry{}, err
	}

	if businessErr != nil {
		return Retry{}, businessErr
	}

	return result, nil
}

func (s *Service) PaymentFailed(
	ctx context.Context,
	partnerID uuid.UUID,
	rawToken string,
	partnerPaymentRef string,
	outcomeCode string,
) (Retry, error) {
	parts, err :=
		parseReservationToken(
			rawToken,
		)
	if err != nil {
		return Retry{}, err
	}

	var (
		result      Retry
		businessErr error
	)

	err = s.transactions.Run(
		ctx,
		func(tx pgx.Tx) error {
			businessErr = nil

			eventID, err :=
				reservationEventID(
					ctx,
					tx,
					parts.ReservationID,
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
					parts.ReservationID,
				)
			if err != nil {
				return err
			}

			if err :=
				s.verifyLockedToken(
					rawToken,
					parts,
					record,
					partnerID,
				); err != nil {
				return err
			}

			if record.State ==
				"RECONCILING" {
				return apierror.New(
					apierror.CodePaymentStatusUncertain,
					"Payment outcome is already uncertain",
				)
			}

			if record.State ==
				"CONFIRMED" {
				return apierror.New(
					apierror.CodeAlreadyConfirmed,
					"Reservation is already confirmed",
				)
			}

			if record.State !=
				"COMMITTING" {
				return apierror.New(
					apierror.CodeReservationNotModifiable,
					"Reservation is not in protected checkout",
				)
			}

			attempt, err :=
				lockActiveCheckout(
					ctx,
					tx,
					record.ID,
				)
			if err != nil {
				return err
			}

			now, err := clockTimestamp(
				ctx,
				tx,
			)
			if err != nil {
				return err
			}

			if !attempt.
				ProtectionExpiresAt.
				After(now) {
				_, err :=
					s.transitionToReconciliation(
						ctx,
						tx,
						record,
						gate.Policy,
						attempt,
						now,
						partnerPaymentRef,
						"CHECKOUT_PROTECTION_EXPIRED",
						false,
					)
				if err != nil {
					return err
				}

				businessErr =
					apierror.New(
						apierror.CodePaymentStatusUncertain,
						"Checkout protection expired before definitive failure was accepted",
					)

				return nil
			}

			retryExpiresAt :=
				now.Add(
					time.Duration(
						gate.Policy.
							PaymentRetrySeconds,
					) * time.Second,
				)

			if retryExpiresAt.After(
				record.MaxLifetimeAt,
			) {
				retryExpiresAt =
					record.MaxLifetimeAt
			}

			if _, err := tx.Exec(
				ctx,
				`
					UPDATE checkout_attempts
					SET
						state =
						    'PAYMENT_FAILED',
						partner_payment_ref =
						    NULLIF($2, ''),
						partner_outcome_code =
						    NULLIF($3, ''),
						completed_at = $4
					WHERE id = $1
					  AND state = 'ACTIVE'
				`,
				attempt.ID,
				partnerPaymentRef,
				outcomeCode,
				now,
			); err != nil {
				return err
			}

			if _, err := tx.Exec(
				ctx,
				`
					UPDATE reservations
					SET
						state =
						    'PAYMENT_RETRY',
						payment_retry_expires_at =
						    $2,
						reconciliation_expires_at =
						    NULL,
						updated_at = $3
					WHERE id = $1
				`,
				record.ID,
				retryExpiresAt,
				now,
			); err != nil {
				return err
			}

			if err :=
				s.appendPartnerFact(
					ctx,
					tx,
					record.EventID,
					record.PartnerID,
					record.ID,
					"RESERVATION_PAYMENT_FAILED",
					"reservation.payment_failed",
					map[string]any{
						"state": "COMMITTING",
					},
					map[string]any{
						"state":                    "PAYMENT_RETRY",
						"payment_retry_expires_at": retryExpiresAt,
					},
				); err != nil {
				return err
			}

			result = Retry{
				ReservationID:         record.ID,
				State:                 "PAYMENT_RETRY",
				PaymentRetryExpiresAt: retryExpiresAt,
			}

			return nil
		},
	)
	if err != nil {
		return Retry{}, err
	}

	if businessErr != nil {
		return Retry{}, businessErr
	}

	return result, nil
}

func (s *Service) MarkPaymentUncertain(
	ctx context.Context,
	partnerID uuid.UUID,
	rawToken string,
	partnerPaymentRef string,
	outcomeCode string,
) (Reconciliation, error) {
	parts, err :=
		parseReservationToken(
			rawToken,
		)
	if err != nil {
		return Reconciliation{}, err
	}

	var result Reconciliation

	err = s.transactions.Run(
		ctx,
		func(tx pgx.Tx) error {
			eventID, err :=
				reservationEventID(
					ctx,
					tx,
					parts.ReservationID,
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
					parts.ReservationID,
				)
			if err != nil {
				return err
			}

			if err :=
				s.verifyLockedToken(
					rawToken,
					parts,
					record,
					partnerID,
				); err != nil {
				return err
			}

			if record.State ==
				"RECONCILING" {
				if record.
					ReconciliationExpiresAt ==
					nil {
					return apierror.New(
						apierror.CodeInternal,
						"Reconciling Reservation has no deadline",
					)
				}

				result =
					Reconciliation{
						ReservationID: record.ID,
						State:         "RECONCILING",
						ReconciliationExpiresAt: *record.
							ReconciliationExpiresAt,
					}

				return nil
			}

			if record.State ==
				"CONFIRMED" {
				return apierror.New(
					apierror.CodeAlreadyConfirmed,
					"Reservation is already confirmed",
				)
			}

			if record.State !=
				"COMMITTING" {
				return apierror.New(
					apierror.CodeReservationNotModifiable,
					"Reservation is not in protected checkout",
				)
			}

			attempt, err :=
				lockActiveCheckout(
					ctx,
					tx,
					record.ID,
				)
			if err != nil {
				return err
			}

			now, err := clockTimestamp(
				ctx,
				tx,
			)
			if err != nil {
				return err
			}

			value, err :=
				s.transitionToReconciliation(
					ctx,
					tx,
					record,
					gate.Policy,
					attempt,
					now,
					partnerPaymentRef,
					outcomeCode,
					false,
				)
			if err != nil {
				return err
			}

			result = value

			return nil
		},
	)
	if err != nil {
		return Reconciliation{}, err
	}

	return result, nil
}

func (s *Service) ResolveNoPayment(
	ctx context.Context,
	partnerID uuid.UUID,
	rawToken string,
	outcomeCode string,
) error {
	parts, err :=
		parseReservationToken(
			rawToken,
		)
	if err != nil {
		return err
	}

	var businessErr error

	err = s.transactions.Run(
		ctx,
		func(tx pgx.Tx) error {
			businessErr = nil

			eventID, err :=
				reservationEventID(
					ctx,
					tx,
					parts.ReservationID,
				)
			if err != nil {
				return err
			}

			_, err = lockEventGate(
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
					parts.ReservationID,
				)
			if err != nil {
				return err
			}

			if err :=
				s.verifyLockedToken(
					rawToken,
					parts,
					record,
					partnerID,
				); err != nil {
				return err
			}

			if record.State ==
				"RELEASED" {
				return nil
			}

			if record.State ==
				"CONFIRMED" {
				return apierror.New(
					apierror.CodeAlreadyConfirmed,
					"Reservation is already confirmed",
				)
			}

			if record.State !=
				"RECONCILING" {
				return apierror.New(
					apierror.CodeReservationNotModifiable,
					"Reservation is not reconciling",
				)
			}

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

			now, err := clockTimestamp(
				ctx,
				tx,
			)
			if err != nil {
				return err
			}

			if record.
				ReconciliationExpiresAt ==
				nil ||
				!record.
					ReconciliationExpiresAt.
					After(now) {
				if err :=
					expireReservationLocked(
						ctx,
						tx,
						s,
						record,
						items,
						allocations,
						now,
						"RECONCILIATION_EXPIRED",
						false,
					); err != nil {
					return err
				}

				businessErr =
					apierror.New(
						apierror.CodeReconciliationExpired,
						"Reconciliation window has expired",
					)

				return nil
			}

			if err :=
				releaseActiveItems(
					ctx,
					tx,
					items,
					allocations,
					now,
					"RECONCILED_NO_PAYMENT",
					false,
				); err != nil {
				return err
			}

			if _, err := tx.Exec(
				ctx,
				`
					UPDATE checkout_attempts
					SET
						state = 'ABANDONED',
						partner_outcome_code =
						    COALESCE(
						        NULLIF($2, ''),
						        partner_outcome_code
						    ),
						completed_at =
						    COALESCE(
						        completed_at,
						        $3
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
				outcomeCode,
				now,
			); err != nil {
				return err
			}

			if _, err := tx.Exec(
				ctx,
				`
					UPDATE reservations
					SET
						state = 'RELEASED',
						released_at = $2,
						terminal_reason =
						    'RECONCILED_NO_PAYMENT',
						updated_at = $2
					WHERE id = $1
				`,
				record.ID,
				now,
			); err != nil {
				return err
			}

			return s.appendPartnerFact(
				ctx,
				tx,
				record.EventID,
				record.PartnerID,
				record.ID,
				"RESERVATION_RECONCILED_NO_PAYMENT",
				"reservation.reconciled_no_payment",
				map[string]any{
					"state": "RECONCILING",
				},
				map[string]any{
					"state": "RELEASED",
				},
			)
		},
	)
	if err != nil {
		return err
	}

	return businessErr
}

func (s *Service) Release(
	ctx context.Context,
	partnerID uuid.UUID,
	rawToken string,
) error {
	parts, err :=
		parseReservationToken(
			rawToken,
		)
	if err != nil {
		return err
	}

	var businessErr error

	err = s.transactions.Run(
		ctx,
		func(tx pgx.Tx) error {
			businessErr = nil

			eventID, err :=
				reservationEventID(
					ctx,
					tx,
					parts.ReservationID,
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
					parts.ReservationID,
				)
			if err != nil {
				return err
			}

			if err :=
				s.verifyLockedToken(
					rawToken,
					parts,
					record,
					partnerID,
				); err != nil {
				return err
			}

			switch record.State {
			case "RELEASED":
				return nil

			case "EXPIRED":
				return apierror.New(
					apierror.CodeHoldExpired,
					"Reservation is expired",
				)

			case "CONFIRMED":
				return apierror.New(
					apierror.CodeAlreadyConfirmed,
					"Confirmed Reservations cannot be released as holds",
				)

			case "COMMITTING":
				attempt, err :=
					lockActiveCheckout(
						ctx,
						tx,
						record.ID,
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

				if !attempt.
					ProtectionExpiresAt.
					After(now) {
					_, err :=
						s.transitionToReconciliation(
							ctx,
							tx,
							record,
							gate.Policy,
							attempt,
							now,
							"",
							"CHECKOUT_PROTECTION_EXPIRED",
							false,
						)
					if err != nil {
						return err
					}
				}

				return apierror.New(
					apierror.CodePaymentStatusUncertain,
					"Protected checkout cannot be released while payment may have been accepted",
				)

			case "RECONCILING":
				if record.
					ReconciliationExpiresAt !=
					nil {
					now, err :=
						clockTimestamp(
							ctx,
							tx,
						)
					if err != nil {
						return err
					}

					if record.
						ReconciliationExpiresAt.
						After(now) {
						return apierror.New(
							apierror.CodePaymentStatusUncertain,
							"Reconciling inventory remains protected",
						)
					}
				}

			case "HELD", "PAYMENT_RETRY":

			default:
				return apierror.New(
					apierror.CodeReservationNotModifiable,
					"Reservation cannot be released from its current state",
				)
			}

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

			now, err := clockTimestamp(
				ctx,
				tx,
			)
			if err != nil {
				return err
			}

			switch record.State {
			case "HELD":
				if !record.
					HoldExpiresAt.
					After(now) {
					if err :=
						expireReservationLocked(
							ctx,
							tx,
							s,
							record,
							items,
							allocations,
							now,
							"HOLD_EXPIRED",
							false,
						); err != nil {
						return err
					}

					businessErr =
						apierror.New(
							apierror.CodeHoldExpired,
							"Reservation hold has expired",
						)

					return nil
				}

			case "PAYMENT_RETRY":
				if record.
					PaymentRetryExpiresAt ==
					nil ||
					!record.
						PaymentRetryExpiresAt.
						After(now) {
					if err :=
						expireReservationLocked(
							ctx,
							tx,
							s,
							record,
							items,
							allocations,
							now,
							"PAYMENT_RETRY_EXPIRED",
							false,
						); err != nil {
						return err
					}

					businessErr =
						apierror.New(
							apierror.CodeCheckoutWindowExpired,
							"Payment retry window has expired",
						)

					return nil
				}

			case "RECONCILING":
				if err :=
					expireReservationLocked(
						ctx,
						tx,
						s,
						record,
						items,
						allocations,
						now,
						"RECONCILIATION_EXPIRED",
						false,
					); err != nil {
					return err
				}

				businessErr =
					apierror.New(
						apierror.CodeReconciliationExpired,
						"Reconciliation window has expired",
					)

				return nil
			}

			if err :=
				releaseActiveItems(
					ctx,
					tx,
					items,
					allocations,
					now,
					"PARTNER_RELEASED",
					false,
				); err != nil {
				return err
			}

			if _, err := tx.Exec(
				ctx,
				`
					UPDATE reservations
					SET
						state = 'RELEASED',
						released_at = $2,
						terminal_reason =
						    'PARTNER_RELEASED',
						updated_at = $2
					WHERE id = $1
				`,
				record.ID,
				now,
			); err != nil {
				return err
			}

			return s.appendPartnerFact(
				ctx,
				tx,
				record.EventID,
				record.PartnerID,
				record.ID,
				"RESERVATION_RELEASED",
				"reservation.released",
				map[string]any{
					"state": record.State,
				},
				map[string]any{
					"state": "RELEASED",
				},
			)
		},
	)
	if err != nil {
		return err
	}

	return businessErr
}

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
