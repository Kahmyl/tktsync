package reporting

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
)

type AuditFilter struct {
	Limit         int
	Cursor        string
	Operation     string
	EntityType    string
	ActorKind     string
	ReservationID *uuid.UUID
	SaleID        *uuid.UUID
	TicketID      *uuid.UUID
	CorrelationID *uuid.UUID
	From          *time.Time
	To            *time.Time
	Search        string
	PartnerID     *uuid.UUID
}

type AuditEvent struct {
	ID             string          `json:"id"`
	ActorKind      string          `json:"actor_kind"`
	ActorPartnerID *string         `json:"actor_partner_id,omitempty"`
	Operation      string          `json:"operation"`
	EntityType     string          `json:"entity_type"`
	EntityID       *string         `json:"entity_id,omitempty"`
	ReservationID  *string         `json:"reservation_id,omitempty"`
	SaleID         *string         `json:"sale_id,omitempty"`
	TicketID       *string         `json:"ticket_id,omitempty"`
	PreviousState  json.RawMessage `json:"previous_state,omitempty"`
	NewState       json.RawMessage `json:"new_state,omitempty"`
	Reason         *string         `json:"reason,omitempty"`
	CorrelationID  *string         `json:"correlation_id,omitempty"`
	Metadata       json.RawMessage `json:"metadata"`
	OccurredAt     time.Time       `json:"occurred_at"`
}

type AuditPage struct {
	Items      []AuditEvent `json:"items"`
	NextCursor *string      `json:"next_cursor"`
}

type auditCursor struct {
	OccurredAt time.Time `json:"occurred_at"`
	ID         uuid.UUID `json:"id"`
}

func ParseAuditCursor(value string) (*time.Time, *uuid.UUID, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, nil, apierror.New(apierror.CodeValidation, "invalid audit cursor")
	}
	var cursor auditCursor
	if err := json.Unmarshal(raw, &cursor); err != nil || cursor.ID == uuid.Nil || cursor.OccurredAt.IsZero() {
		return nil, nil, apierror.New(apierror.CodeValidation, "invalid audit cursor")
	}
	return &cursor.OccurredAt, &cursor.ID, nil
}

func encodeAuditCursor(at time.Time, id uuid.UUID) string {
	raw, _ := json.Marshal(auditCursor{OccurredAt: at, ID: id})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func (s *Service) Audit(ctx context.Context, eventID uuid.UUID, filter AuditFilter) (AuditPage, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 100 {
		return AuditPage{}, apierror.New(apierror.CodeValidation, "audit limit must not exceed 100")
	}
	cursorTime, cursorID, err := ParseAuditCursor(filter.Cursor)
	if err != nil {
		return AuditPage{}, err
	}
	page := AuditPage{Items: []AuditEvent{}}
	err = s.readSnapshot(ctx, func(tx pgx.Tx) error {
		if _, _, err := eventContext(ctx, tx, eventID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT id, actor_kind, actor_partner_id, operation, entity_type, entity_id,
			       reservation_id, sale_id, ticket_entitlement_id, previous_state, new_state,
			       reason, correlation_id, metadata, occurred_at
			FROM audit_events
			WHERE event_id=$1
			  AND ($2::uuid IS NULL OR partner_id=$2 OR actor_partner_id=$2)
			  AND ($3='' OR operation=$3) AND ($4='' OR entity_type=$4) AND ($5='' OR actor_kind=$5)
			  AND ($6::uuid IS NULL OR reservation_id=$6) AND ($7::uuid IS NULL OR sale_id=$7)
			  AND ($8::uuid IS NULL OR ticket_entitlement_id=$8) AND ($9::uuid IS NULL OR correlation_id=$9)
			  AND ($10::timestamptz IS NULL OR occurred_at >= $10) AND ($11::timestamptz IS NULL OR occurred_at < $11)
			  AND ($12='' OR operation ILIKE '%%'||$12||'%%' OR entity_type ILIKE '%%'||$12||'%%' OR COALESCE(reason,'') ILIKE '%%'||$12||'%%')
			  AND ($13::timestamptz IS NULL OR (occurred_at,id) < ($13,$14::uuid))
			ORDER BY occurred_at DESC,id DESC LIMIT $15
		`, eventID, filter.PartnerID, filter.Operation, filter.EntityType, filter.ActorKind, filter.ReservationID, filter.SaleID, filter.TicketID, filter.CorrelationID, filter.From, filter.To, filter.Search, cursorTime, cursorID, filter.Limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item AuditEvent
			var id uuid.UUID
			var actorPartner, entityID, reservationID, saleID, ticketID, correlationID *uuid.UUID
			var previous, newState, metadata []byte
			if err := rows.Scan(&id, &item.ActorKind, &actorPartner, &item.Operation, &item.EntityType, &entityID, &reservationID, &saleID, &ticketID, &previous, &newState, &item.Reason, &correlationID, &metadata, &item.OccurredAt); err != nil {
				return err
			}
			item.ID = publicid.Encode(publicid.AuditEvent, id)
			if actorPartner != nil {
				value := publicid.Encode(publicid.Partner, *actorPartner)
				item.ActorPartnerID = &value
			}
			item.EntityID = encodeEntityID(item.EntityType, entityID)
			item.ReservationID = encodeOptional(publicid.Reservation, reservationID)
			item.SaleID = encodeOptional(publicid.Sale, saleID)
			item.TicketID = encodeOptional(publicid.Ticket, ticketID)
			if correlationID != nil {
				value := correlationID.String()
				item.CorrelationID = &value
			}
			item.PreviousState = sanitizeJSON(previous)
			item.NewState = sanitizeJSON(newState)
			item.Metadata = sanitizeJSON(metadata)
			page.Items = append(page.Items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return AuditPage{}, err
	}
	if len(page.Items) > filter.Limit {
		last := page.Items[filter.Limit-1]
		cursor := encodeAuditCursor(last.OccurredAt, mustParsePublicID(last.ID, publicid.AuditEvent))
		page.NextCursor = &cursor
		page.Items = page.Items[:filter.Limit]
	}
	return page, nil
}

func encodeOptional(kind publicid.Kind, id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	value := publicid.Encode(kind, *id)
	return &value
}
func encodeEntityID(entityType string, id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	kinds := map[string]publicid.Kind{"EVENT": publicid.Event, "RESERVATION": publicid.Reservation, "SALE": publicid.Sale, "TICKET_ENTITLEMENT": publicid.Ticket, "ADMISSION": publicid.Admission, "ALLOCATION": publicid.Allocation, "BLOCK": publicid.Block, "NON_PUBLIC_ISSUANCE": publicid.NonPublicIssuance, "PARTNER": publicid.Partner}
	kind, ok := kinds[entityType]
	if !ok {
		return nil
	}
	return encodeOptional(kind, id)
}
func mustParsePublicID(value string, kind publicid.Kind) uuid.UUID {
	id, err := publicid.Parse(value, kind)
	if err != nil {
		panic(fmt.Sprintf("invalid generated public id: %v", err))
	}
	return id
}

func sanitizeJSON(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return json.RawMessage(`{}`)
	}
	sanitizeValue(value)
	clean, _ := json.Marshal(value)
	return clean
}
func sanitizeValue(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "ciphertext") || strings.Contains(lower, "credential_raw") || strings.Contains(lower, "idempotency_key") {
				delete(typed, key)
				continue
			}
			sanitizeValue(child)
		}
	case []any:
		for _, child := range typed {
			sanitizeValue(child)
		}
	}
}
