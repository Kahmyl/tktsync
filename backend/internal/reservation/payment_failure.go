package reservation

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
)

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
