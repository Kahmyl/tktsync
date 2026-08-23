package reservation

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

func (s *Service) Modify(
	ctx context.Context,
	partnerID uuid.UUID,
	rawToken string,
	items []ItemInput,
) (Modified, error) {
	parts, err :=
		parseReservationToken(
			rawToken,
		)
	if err != nil {
		return Modified{}, err
	}

	normalized, err :=
		normalizeItems(
			items,
		)
	if err != nil {
		return Modified{}, err
	}

	var (
		result      Modified
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

			if record.State != "HELD" {
				return apierror.New(
					apierror.CodeReservationNotModifiable,
					"Only HELD Reservations may be modified",
				)
			}

			oldItems, err :=
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
					oldItems,
					normalized,
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

			if gate.State == "CANCELLED" {
				if err :=
					expireReservationLocked(
						ctx,
						tx,
						s,
						record,
						oldItems,
						allocations,
						now,
						"EVENT_CANCELLED",
						false,
					); err != nil {
					return err
				}

				businessErr =
					apierror.New(
						apierror.CodeEventCancelled,
						"Event is cancelled",
					)

				return nil
			}

			if gate.State == "COMPLETED" {
				return apierror.New(
					apierror.CodeEventSalesClosed,
					"Event sales are closed",
				)
			}

			if !record.HoldExpiresAt.
				After(now) {
				if err :=
					expireReservationLocked(
						ctx,
						tx,
						s,
						record,
						oldItems,
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

			switch gate.State {
			case "PAUSED":
				if !isNonExpandingModification(
					oldItems,
					normalized,
				) {
					return apierror.New(
						apierror.CodeEventPaused,
						"Event is paused; Reservation modification may only release inventory",
					)
				}

			case "SALES_CLOSED":
				if !isNonExpandingModification(
					oldItems,
					normalized,
				) {
					return apierror.New(
						apierror.CodeEventSalesClosed,
						"Event sales are closed; Reservation modification may only release inventory",
					)
				}
			}

			if totalQuantity(
				normalized,
			) > gate.Policy.
				MaxHoldQuantity {
				return apierror.New(
					apierror.CodeValidation,
					"Reservation exceeds Event maximum hold quantity",
				)
			}

			plan, err :=
				planReservationModification(
					oldItems,
					normalized,
				)
			if err != nil {
				return err
			}

			var resolved []resolvedItem

			if len(plan.Acquisitions) > 0 {
				resolved, err =
					s.resolveItems(
						ctx,
						tx,
						record.EventID,
						record.PartnerID,
						plan.Acquisitions,
						allocations,
					)
				if err != nil {
					return err
				}

				currency, err :=
					requireSingleCurrency(
						resolved,
					)
				if err != nil {
					return err
				}

				if currency != record.Currency {
					return apierror.New(
						apierror.CodeCurrencyMismatch,
						"Reservation currency cannot change during modification",
					)
				}
			}

			if len(plan.FullReleases) > 0 {
				if err :=
					releaseActiveItems(
						ctx,
						tx,
						plan.FullReleases,
						allocations,
						now,
						"RESERVATION_MODIFIED",
						true,
					); err != nil {
					return err
				}
			}

			for _, partial := range plan.PartialReleases {
				if err :=
					releasePartialGAItem(
						ctx,
						tx,
						partial,
						allocations,
						now,
					); err != nil {
					return err
				}
			}

			if len(resolved) > 0 {
				if err :=
					acquireResolvedItems(
						ctx,
						tx,
						record.ID,
						record.EventID,
						resolved,
						now,
					); err != nil {
					return err
				}
			}

			if _, err := tx.Exec(
				ctx,
				`
					UPDATE reservations
					SET updated_at = $2
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
					"RESERVATION_MODIFIED",
					"reservation.modified",
					map[string]any{
						"state": record.State,
					},
					map[string]any{
						"state": "HELD",
						"hold_expires_at": record.
							HoldExpiresAt,
					},
				); err != nil {
				return err
			}

			result = Modified{
				ReservationID: record.ID,
				State:         "HELD",
				Currency:      record.Currency,
				HoldExpiresAt: record.
					HoldExpiresAt,
			}

			return nil
		},
	)
	if err != nil {
		return Modified{}, err
	}

	if businessErr != nil {
		return Modified{}, businessErr
	}

	return result, nil
}
