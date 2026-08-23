package reservation

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
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
