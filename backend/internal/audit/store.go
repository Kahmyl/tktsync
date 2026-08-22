package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type QueryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type ActorKind string

const (
	ActorUser         ActorKind = "USER"
	ActorPartner      ActorKind = "PARTNER"
	ActorBuyerSession ActorKind = "BUYER_SESSION"
	ActorSystem       ActorKind = "SYSTEM"
)

type Event struct {
	EventID             *uuid.UUID
	PartnerID           *uuid.UUID
	ActorKind           ActorKind
	ActorUserID         *uuid.UUID
	ActorPartnerID      *uuid.UUID
	ActorBuyerSessionID *uuid.UUID
	SystemActor         string
	Operation           string
	EntityType          string
	EntityID            *uuid.UUID
	ReservationID       *uuid.UUID
	SaleID              *uuid.UUID
	TicketEntitlementID *uuid.UUID
	TicketID            *uuid.UUID
	PreviousState       any
	NewState            any
	Reason              string
	IdempotencyKeyHash  []byte
	CorrelationID       *uuid.UUID
	Metadata            any
}

type Store struct{}

func (Store) Append(
	ctx context.Context,
	q QueryRower,
	event Event,
) (uuid.UUID, error) {
	if event.Operation == "" || event.EntityType == "" {
		return uuid.Nil, errors.New("audit operation and entity type are required")
	}

	previousState, err := marshalNullable(event.PreviousState)
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal previous state: %w", err)
	}

	newState, err := marshalNullable(event.NewState)
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal new state: %w", err)
	}

	metadata, err := marshalMetadata(event.Metadata)
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal audit metadata: %w", err)
	}

	ticketEntitlementID := event.TicketEntitlementID
	if ticketEntitlementID == nil {
		ticketEntitlementID = event.TicketID
	}

	id := uuid.New()

	err = q.QueryRow(
		ctx,
		`
			INSERT INTO audit_events (
				id,
				event_id,
				partner_id,
				actor_kind,
				actor_user_id,
				actor_partner_id,
				actor_buyer_session_id,
				system_actor,
				operation,
				entity_type,
				entity_id,
				reservation_id,
				sale_id,
				ticket_entitlement_id,
				previous_state,
				new_state,
				reason,
				idempotency_key_hash,
				correlation_id,
				metadata,
				occurred_at
			)
			VALUES (
				$1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),
				$9,$10,$11,$12,$13,$14,$15,$16,
				NULLIF($17,''),$18,$19,$20,clock_timestamp()
			)
			RETURNING id
		`,
		id,
		event.EventID,
		event.PartnerID,
		string(event.ActorKind),
		event.ActorUserID,
		event.ActorPartnerID,
		event.ActorBuyerSessionID,
		event.SystemActor,
		event.Operation,
		event.EntityType,
		event.EntityID,
		event.ReservationID,
		event.SaleID,
		ticketEntitlementID,
		previousState,
		newState,
		event.Reason,
		event.IdempotencyKeyHash,
		event.CorrelationID,
		metadata,
	).Scan(&id)

	if err != nil {
		return uuid.Nil, err
	}

	return id, nil
}

func marshalNullable(value any) (any, error) {
	if value == nil {
		return nil, nil
	}

	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}

	return json.RawMessage(raw), nil
}

func marshalMetadata(value any) (json.RawMessage, error) {
	if value == nil {
		return json.RawMessage(`{}`), nil
	}

	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}

	return json.RawMessage(raw), nil
}
