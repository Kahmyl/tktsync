package reservation

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

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
