package reservation

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

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
