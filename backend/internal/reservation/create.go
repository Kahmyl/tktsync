package reservation

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

func (s *Service) Create(
	ctx context.Context,
	input CreateInput,
) (Created, error) {
	if input.EventID == uuid.Nil ||
		input.PartnerID == uuid.Nil {
		return Created{},
			apierror.New(
				apierror.CodeValidation,
				"Event and Partner are required",
			)
	}

	items, err := normalizeItems(
		input.Items,
	)
	if err != nil {
		return Created{}, err
	}

	if s.keys == nil {
		return Created{},
			apierror.New(
				apierror.CodeInternal,
				"Reservation token authority is not configured",
			)
	}

	reservationID := uuid.New()
	version := s.keys.ActiveVersion()

	token, err := s.buildToken(
		reservationID,
		input.PartnerID,
		input.EventID,
		version,
	)
	if err != nil {
		return Created{}, err
	}

	tokenHash := authTokenHashBytes(
		token,
	)

	var result Created

	err = s.transactions.Run(
		ctx,
		func(tx pgx.Tx) error {
			gate, err := lockEventGate(
				ctx,
				tx,
				input.EventID,
			)
			if err != nil {
				return err
			}

			partnerLock, err :=
				lockPartnerAcquisition(
					ctx,
					tx,
					input.PartnerID,
					input.EventID,
				)
			if err != nil {
				return err
			}

			if input.
				BuyerSelectionSessionID !=
				nil {
				var sessionID uuid.UUID

				err := tx.QueryRow(
					ctx,
					`
						SELECT id
						FROM buyer_selection_sessions
						WHERE id = $1
						  AND partner_id = $2
						  AND event_id = $3
						  AND state = 'ACTIVE'
						  AND expires_at > clock_timestamp()
						FOR UPDATE
					`,
					*input.
						BuyerSelectionSessionID,
					input.PartnerID,
					input.EventID,
				).Scan(
					&sessionID,
				)
				if err != nil {
					if errors.Is(
						err,
						pgx.ErrNoRows,
					) {
						return apierror.New(
							apierror.CodeNotAuthorized,
							"Buyer selection session is not valid for this Partner and Event",
						)
					}

					return err
				}
			}

			allocations, err :=
				s.lockMutationResources(
					ctx,
					tx,
					input.EventID,
					nil,
					items,
				)
			if err != nil {
				return err
			}

			acceptedAt, err :=
				clockTimestamp(
					ctx,
					tx,
				)
			if err != nil {
				return err
			}

			if err :=
				validateNewAcquisition(
					gate,
					partnerLock,
					acceptedAt,
				); err != nil {
				return err
			}

			if totalQuantity(items) >
				gate.Policy.
					MaxHoldQuantity {
				return apierror.New(
					apierror.CodeValidation,
					"Reservation exceeds Event maximum hold quantity",
				)
			}

			if err :=
				checkActiveReservationLimits(
					ctx,
					tx,
					input,
					gate.Policy,
					acceptedAt,
				); err != nil {
				return err
			}

			resolved, err :=
				s.resolveItems(
					ctx,
					tx,
					input.EventID,
					input.PartnerID,
					items,
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

			holdExpiresAt :=
				acceptedAt.Add(
					time.Duration(
						gate.Policy.
							HoldDurationSeconds,
					) * time.Second,
				)

			maxLifetimeAt :=
				acceptedAt.Add(
					time.Duration(
						gate.Policy.
							MaxReservationLifetimeSeconds,
					) * time.Second,
				)

			if holdExpiresAt.After(
				maxLifetimeAt,
			) {
				holdExpiresAt =
					maxLifetimeAt
			}

			if _, err := tx.Exec(
				ctx,
				`
					INSERT INTO reservations (
						id,
						event_id,
						partner_id,
						buyer_selection_session_id,
						partner_customer_ref,
						partner_order_ref,
						buyer_session_ref,
						continuation_token_hash,
						continuation_token_key_version,
						currency,
						state,
						hold_expires_at,
						max_lifetime_at,
						created_at,
						updated_at
					)
					VALUES (
						$1,
						$2,
						$3,
						$4,
						NULLIF($5, ''),
						NULLIF($6, ''),
						NULLIF($7, ''),
						$8,
						$9,
						$10,
						'HELD',
						$11,
						$12,
						$13,
						$13
					)
				`,
				reservationID,
				input.EventID,
				input.PartnerID,
				input.
					BuyerSelectionSessionID,
				input.PartnerCustomerRef,
				input.PartnerOrderRef,
				input.BuyerSessionRef,
				tokenHash,
				version,
				currency,
				holdExpiresAt,
				maxLifetimeAt,
				acceptedAt,
			); err != nil {
				return err
			}

			if err :=
				acquireResolvedItems(
					ctx,
					tx,
					reservationID,
					input.EventID,
					resolved,
					acceptedAt,
				); err != nil {
				return err
			}

			if err :=
				s.appendPartnerFact(
					ctx,
					tx,
					input.EventID,
					input.PartnerID,
					reservationID,
					"RESERVATION_CREATED",
					"reservation.created",
					nil,
					map[string]any{
						"state":           "HELD",
						"hold_expires_at": holdExpiresAt,
						"max_lifetime_at": maxLifetimeAt,
					},
				); err != nil {
				return err
			}

			result = Created{
				ReservationID: reservationID,
				Token:         token,
				State:         "HELD",
				Currency:      currency,
				HoldExpiresAt: holdExpiresAt,
				MaxLifetimeAt: maxLifetimeAt,
			}

			return nil
		},
	)
	if err != nil {
		return Created{}, err
	}

	return result, nil
}
