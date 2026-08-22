package reservation

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/outbox"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

type eventPolicy struct {
	HoldDurationSeconds                  int
	CheckoutProtectionSeconds            int
	PaymentRetrySeconds                  int
	ReconciliationSeconds                int
	MaxReservationLifetimeSeconds        int
	MaxHoldQuantity                      int
	MaxActiveReservationsPerPartner      int
	MaxActiveReservationsPerBuyerSession int
}

type eventGate struct {
	State        string
	SalesOpenAt  *time.Time
	SalesCloseAt *time.Time
	Policy       eventPolicy
}

type partnerAcquisitionLock struct {
	PartnerState string
	AccessState  string
}

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

func isNonExpandingModification(
	oldItems []activeItem,
	newItems []ItemInput,
) bool {
	key := func(
		inventoryKind string,
		inventoryID uuid.UUID,
		sourceKind string,
		allocationID *uuid.UUID,
	) string {
		allocation := ""

		if allocationID != nil {
			allocation = allocationID.String()
		}

		return inventoryKind +
			"|" +
			inventoryID.String() +
			"|" +
			sourceKind +
			"|" +
			allocation
	}

	available := map[string]int{}

	for _, item := range oldItems {
		var inventoryID uuid.UUID

		switch item.InventoryKind {
		case InventoryReserved:
			if item.ReservedInventoryUnitID == nil {
				return false
			}

			inventoryID = *item.ReservedInventoryUnitID

		case InventoryGA:
			if item.GAPoolID == nil {
				return false
			}

			inventoryID = *item.GAPoolID

		default:
			return false
		}

		available[key(
			item.InventoryKind,
			inventoryID,
			item.SourceKind,
			item.SourceAllocationID,
		)] += item.Quantity
	}

	for _, item := range newItems {
		k := key(
			item.InventoryKind,
			item.InventoryID,
			item.SourceKind,
			item.SourceAllocationID,
		)

		if item.Quantity > available[k] {
			return false
		}

		available[k] -= item.Quantity
	}

	return true
}

func lockEventGate(
	ctx context.Context,
	tx pgx.Tx,
	eventID uuid.UUID,
) (eventGate, error) {
	var (
		state      string
		salesOpen  pgtype.Timestamptz
		salesClose pgtype.Timestamptz
	)

	err := tx.QueryRow(
		ctx,
		`
			SELECT
				state,
				sales_open_at,
				sales_close_at
			FROM events
			WHERE id = $1
			FOR KEY SHARE
		`,
		eventID,
	).Scan(
		&state,
		&salesOpen,
		&salesClose,
	)
	if err != nil {
		if errors.Is(
			err,
			pgx.ErrNoRows,
		) {
			return eventGate{},
				apierror.New(
					apierror.CodeResourceNotFound,
					"Event not found",
				)
		}

		return eventGate{}, err
	}

	var policy eventPolicy

	err = tx.QueryRow(
		ctx,
		`
			SELECT
				hold_duration_seconds,
				checkout_protection_seconds,
				payment_retry_seconds,
				reconciliation_seconds,
				max_reservation_lifetime_seconds,
				max_hold_quantity,
				max_active_reservations_per_partner,
				max_active_reservations_per_buyer_session
			FROM event_transaction_policies
			WHERE event_id = $1
			FOR KEY SHARE
		`,
		eventID,
	).Scan(
		&policy.HoldDurationSeconds,
		&policy.CheckoutProtectionSeconds,
		&policy.PaymentRetrySeconds,
		&policy.ReconciliationSeconds,
		&policy.MaxReservationLifetimeSeconds,
		&policy.MaxHoldQuantity,
		&policy.
			MaxActiveReservationsPerPartner,
		&policy.
			MaxActiveReservationsPerBuyerSession,
	)
	if err != nil {
		if errors.Is(
			err,
			pgx.ErrNoRows,
		) {
			return eventGate{},
				apierror.New(
					apierror.CodeInternal,
					"Event transaction policy is missing",
				)
		}

		return eventGate{}, err
	}

	return eventGate{
		State: state,
		SalesOpenAt: nullableTime(
			salesOpen,
		),
		SalesCloseAt: nullableTime(
			salesClose,
		),
		Policy: policy,
	}, nil
}

func lockPartnerAcquisition(
	ctx context.Context,
	tx pgx.Tx,
	partnerID uuid.UUID,
	eventID uuid.UUID,
) (partnerAcquisitionLock, error) {
	var partnerState string

	err := tx.QueryRow(
		ctx,
		`
			SELECT state
			FROM partners
			WHERE id = $1
			FOR UPDATE
		`,
		partnerID,
	).Scan(
		&partnerState,
	)
	if err != nil {
		if errors.Is(
			err,
			pgx.ErrNoRows,
		) {
			return partnerAcquisitionLock{},
				apierror.New(
					apierror.CodeNotAuthorized,
					"Partner is not authorized",
				)
		}

		return partnerAcquisitionLock{},
			err
	}

	var accessState string

	err = tx.QueryRow(
		ctx,
		`
			SELECT state
			FROM partner_event_access
			WHERE partner_id = $1
			  AND event_id = $2
			FOR KEY SHARE
		`,
		partnerID,
		eventID,
	).Scan(
		&accessState,
	)
	if err != nil {
		if errors.Is(
			err,
			pgx.ErrNoRows,
		) {
			return partnerAcquisitionLock{},
				apierror.New(
					apierror.CodeNotAuthorized,
					"Partner is not authorized for this Event",
				)
		}

		return partnerAcquisitionLock{},
			err
	}

	return partnerAcquisitionLock{
		PartnerState: partnerState,
		AccessState:  accessState,
	}, nil
}

func validateNewAcquisition(
	gate eventGate,
	partner partnerAcquisitionLock,
	now time.Time,
) error {
	if partner.PartnerState !=
		"ACTIVE" {
		return apierror.New(
			apierror.CodePartnerDisabled,
			"Partner is disabled",
		)
	}

	if partner.AccessState !=
		"ACTIVE" {
		return apierror.New(
			apierror.CodePartnerEventAccessDisabled,
			"Partner Event access is disabled",
		)
	}

	switch gate.State {
	case "ON_SALE":
	case "PAUSED":
		return apierror.New(
			apierror.CodeEventPaused,
			"Event sales are paused",
		)

	case "SALES_CLOSED":
		return apierror.New(
			apierror.CodeEventSalesClosed,
			"Event sales are closed",
		)

	case "CANCELLED":
		return apierror.New(
			apierror.CodeEventCancelled,
			"Event is cancelled",
		)

	case "COMPLETED":
		return apierror.New(
			apierror.CodeEventSalesClosed,
			"Event sales are closed",
		)

	default:
		return apierror.New(
			apierror.CodeEventNotOnSale,
			"Event is not on sale",
		)
	}

	if gate.SalesOpenAt != nil &&
		now.Before(
			*gate.SalesOpenAt,
		) {
		return apierror.New(
			apierror.CodeEventNotOnSale,
			"Event sales have not opened",
		)
	}

	if gate.SalesCloseAt != nil &&
		!now.Before(
			*gate.SalesCloseAt,
		) {
		return apierror.New(
			apierror.CodeEventSalesClosed,
			"Event sales are closed",
		)
	}

	return nil
}

func checkActiveReservationLimits(
	ctx context.Context,
	tx pgx.Tx,
	input CreateInput,
	policy eventPolicy,
	now time.Time,
) error {
	var partnerCount int

	if err := tx.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM reservations r
			WHERE r.partner_id = $1
			  AND r.event_id = $2
			  AND (
			      (
			          r.state = 'HELD'
			          AND r.hold_expires_at > $3
			      )
			      OR (
			          r.state = 'PAYMENT_RETRY'
			          AND r.payment_retry_expires_at > $3
			      )
			      OR r.state = 'COMMITTING'
			      OR (
			          r.state = 'RECONCILING'
			          AND r.reconciliation_expires_at > $3
			      )
			  )
		`,
		input.PartnerID,
		input.EventID,
		now,
	).Scan(
		&partnerCount,
	); err != nil {
		return err
	}

	if partnerCount >=
		policy.
			MaxActiveReservationsPerPartner {
		return apierror.New(
			apierror.CodeRateLimited,
			"Partner has reached the Event active Reservation limit",
		)
	}

	if input.
		BuyerSelectionSessionID ==
		nil {
		return nil
	}

	var buyerCount int

	if err := tx.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM reservations r
			WHERE r.buyer_selection_session_id = $1
			  AND r.event_id = $2
			  AND (
			      (
			          r.state = 'HELD'
			          AND r.hold_expires_at > $3
			      )
			      OR (
			          r.state = 'PAYMENT_RETRY'
			          AND r.payment_retry_expires_at > $3
			      )
			      OR r.state = 'COMMITTING'
			      OR (
			          r.state = 'RECONCILING'
			          AND r.reconciliation_expires_at > $3
			      )
			  )
		`,
		*input.
			BuyerSelectionSessionID,
		input.EventID,
		now,
	).Scan(
		&buyerCount,
	); err != nil {
		return err
	}

	if buyerCount >=
		policy.
			MaxActiveReservationsPerBuyerSession {
		return apierror.New(
			apierror.CodeRateLimited,
			"Buyer session has reached the active Reservation limit",
		)
	}

	return nil
}

func clockTimestamp(
	ctx context.Context,
	tx pgx.Tx,
) (time.Time, error) {
	var now time.Time

	err := tx.QueryRow(
		ctx,
		`SELECT clock_timestamp()`,
	).Scan(
		&now,
	)

	return now, err
}

func authTokenHashBytes(
	token string,
) []byte {
	hash := auth.TokenHash(
		token,
	)

	return append(
		[]byte(nil),
		hash[:]...,
	)
}

func (s *Service) appendPartnerFact(
	ctx context.Context,
	tx pgx.Tx,
	eventID uuid.UUID,
	partnerID uuid.UUID,
	reservationID uuid.UUID,
	operation string,
	factType string,
	previousState any,
	newState any,
) error {
	if _, err := s.audit.Append(
		ctx,
		tx,
		audit.Event{
			EventID:        &eventID,
			PartnerID:      &partnerID,
			ActorKind:      "PARTNER",
			ActorPartnerID: &partnerID,
			ReservationID:  &reservationID,
			Operation:      operation,
			EntityType:     "RESERVATION",
			EntityID:       &reservationID,
			PreviousState:  previousState,
			NewState:       newState,
		},
	); err != nil {
		return err
	}

	_, err := s.outbox.Append(
		ctx,
		tx,
		outbox.Fact{
			EventID:       &eventID,
			FactType:      factType,
			AggregateType: "RESERVATION",
			AggregateID:   &reservationID,
		},
	)

	return err
}

func (s *Service) appendSystemFact(
	ctx context.Context,
	tx pgx.Tx,
	eventID uuid.UUID,
	partnerID uuid.UUID,
	reservationID uuid.UUID,
	operation string,
	factType string,
	previousState any,
	newState any,
) error {
	if _, err := s.audit.Append(
		ctx,
		tx,
		audit.Event{
			EventID:       &eventID,
			PartnerID:     &partnerID,
			ActorKind:     "SYSTEM",
			SystemActor:   "RESERVATION_MATERIALIZER",
			ReservationID: &reservationID,
			Operation:     operation,
			EntityType:    "RESERVATION",
			EntityID:      &reservationID,
			PreviousState: previousState,
			NewState:      newState,
		},
	); err != nil {
		return err
	}

	_, err := s.outbox.Append(
		ctx,
		tx,
		outbox.Fact{
			EventID:       &eventID,
			FactType:      factType,
			AggregateType: "RESERVATION",
			AggregateID:   &reservationID,
		},
	)

	return err
}
