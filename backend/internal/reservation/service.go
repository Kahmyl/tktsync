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
