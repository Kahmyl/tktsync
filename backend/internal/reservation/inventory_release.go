package reservation

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/outbox"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

type InventoryReleaseInput struct {
	DestinationKind         string
	DestinationAllocationID *uuid.UUID
	Reason                  string
}

type ReleasedTicketInventory struct {
	TicketID                uuid.UUID
	ReleasedAt              time.Time
	DestinationKind         string
	DestinationAllocationID *uuid.UUID
	Reason                  string
}

type releasableTicket struct {
	EventID                        uuid.UUID
	Status                         string
	InventoryKind                  string
	ReservedInventoryUnitID        *uuid.UUID
	GAPoolID                       *uuid.UUID
	InventoryReleasedAt            *time.Time
	OriginSaleItemID               *uuid.UUID
	OriginIssuanceItemID           *uuid.UUID
	SourceAllocationID             *uuid.UUID
	SourceAllocationReservedUnitID *uuid.UUID
	SourceGAAllocationBucketID     *uuid.UUID
}

type releaseActor struct {
	kind      audit.ActorKind
	userID    *uuid.UUID
	partnerID *uuid.UUID
}

func (s *Service) ReReleasePartnerTicketInventory(ctx context.Context, partnerID, ticketID uuid.UUID, input InventoryReleaseInput) (ReleasedTicketInventory, error) {
	if partnerID == uuid.Nil || ticketID == uuid.Nil {
		return ReleasedTicketInventory{}, apierror.New(apierror.CodeValidation, "Partner and Ticket are required")
	}
	return s.reReleaseTicketInventory(ctx, ticketID, input, releaseActor{kind: audit.ActorPartner, partnerID: &partnerID})
}

func (s *Service) ReReleaseAdminTicketInventory(ctx context.Context, actorID, ticketID uuid.UUID, input InventoryReleaseInput) (ReleasedTicketInventory, error) {
	if actorID == uuid.Nil || ticketID == uuid.Nil {
		return ReleasedTicketInventory{}, apierror.New(apierror.CodeValidation, "Actor and Ticket are required")
	}
	return s.reReleaseTicketInventory(ctx, ticketID, input, releaseActor{kind: audit.ActorUser, userID: &actorID})
}

func normalizeInventoryReleaseInput(input InventoryReleaseInput) (InventoryReleaseInput, error) {
	input.DestinationKind = strings.ToUpper(strings.TrimSpace(input.DestinationKind))
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Reason == "" {
		return input, apierror.New(apierror.CodeValidation, "reason is required")
	}
	switch input.DestinationKind {
	case SourceShared:
		if input.DestinationAllocationID != nil {
			return input, apierror.New(apierror.CodeValidation, "SHARED destination cannot identify an Allocation")
		}
	case SourceAllocation:
		if input.DestinationAllocationID == nil || *input.DestinationAllocationID == uuid.Nil {
			return input, apierror.New(apierror.CodeValidation, "ALLOCATION destination requires destination_allocation_id")
		}
	default:
		return input, apierror.New(apierror.CodeValidation, "destination_kind must be SHARED or ALLOCATION")
	}
	return input, nil
}

func (s *Service) reReleaseTicketInventory(ctx context.Context, ticketID uuid.UUID, input InventoryReleaseInput, actor releaseActor) (ReleasedTicketInventory, error) {
	input, err := normalizeInventoryReleaseInput(input)
	if err != nil {
		return ReleasedTicketInventory{}, err
	}

	var result ReleasedTicketInventory
	err = s.transactions.Run(ctx, func(tx pgx.Tx) error {
		var eventID uuid.UUID
		if actor.partnerID != nil {
			eventID, err = partnerCommercialTicketEventID(ctx, tx, *actor.partnerID, ticketID)
		} else {
			eventID, err = adminTicketEventID(ctx, tx, ticketID)
		}
		if err != nil {
			return err
		}
		if err = lockTicketEvent(ctx, tx, eventID); err != nil {
			return err
		}

		var policyAllows bool
		if err = tx.QueryRow(ctx, `SELECT allow_voided_inventory_rerelease FROM event_transaction_policies WHERE event_id = $1 FOR KEY SHARE`, eventID).Scan(&policyAllows); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apierror.New(apierror.CodeNotAuthorized, "Event does not permit voided inventory re-release")
			}
			return err
		}
		if !policyAllows {
			return apierror.New(apierror.CodeNotAuthorized, "Event policy does not permit voided inventory re-release")
		}

		ticket, err := lockReleasableTicket(ctx, tx, ticketID, eventID)
		if err != nil {
			return err
		}
		if ticket.Status != "VOIDED" {
			return apierror.New(apierror.CodeTicketInvalid, "Ticket must be voided before inventory re-release")
		}
		if ticket.InventoryReleasedAt != nil {
			return apierror.New(apierror.CodeInventoryUnavailable, "Ticket inventory has already been re-released")
		}

		if input.DestinationAllocationID != nil {
			if err = validateReleaseDestinationAllocation(ctx, tx, eventID, *input.DestinationAllocationID, actor.partnerID); err != nil {
				return err
			}
		}

		now, err := ticketClock(ctx, tx)
		if err != nil {
			return err
		}
		if err = releaseTicketCapacity(ctx, tx, ticket, input, now); err != nil {
			return err
		}

		commandTag, err := tx.Exec(ctx, `
			UPDATE ticket_entitlements
			SET inventory_released_at = $2,
			    inventory_release_reason = $3,
			    inventory_release_destination_kind = $4,
			    inventory_release_destination_allocation_id = $5
			WHERE id = $1 AND status = 'VOIDED' AND inventory_released_at IS NULL
		`, ticketID, now, input.Reason, input.DestinationKind, input.DestinationAllocationID)
		if err != nil {
			return err
		}
		if commandTag.RowsAffected() != 1 {
			return apierror.New(apierror.CodeInventoryUnavailable, "Ticket inventory changed during re-release")
		}

		auditEvent := audit.Event{
			EventID: &eventID, PartnerID: actor.partnerID, ActorKind: actor.kind,
			ActorUserID: actor.userID, ActorPartnerID: actor.partnerID,
			Operation: "TICKET_INVENTORY_RELEASED", EntityType: "TICKET", EntityID: &ticketID,
			TicketEntitlementID: &ticketID, Reason: input.Reason,
			PreviousState: map[string]any{"inventory_released": false},
			NewState:      map[string]any{"inventory_released": true, "destination_kind": input.DestinationKind, "destination_allocation_id": input.DestinationAllocationID},
		}
		if _, err = s.audit.Append(ctx, tx, auditEvent); err != nil {
			return err
		}
		if _, err = s.outbox.Append(ctx, tx, outbox.Fact{EventID: &eventID, FactType: "ticket.inventory_released", AggregateType: "TICKET", AggregateID: &ticketID, Payload: map[string]any{"destination_kind": input.DestinationKind, "destination_allocation_id": input.DestinationAllocationID}}); err != nil {
			return err
		}

		result = ReleasedTicketInventory{TicketID: ticketID, ReleasedAt: now, DestinationKind: input.DestinationKind, DestinationAllocationID: input.DestinationAllocationID, Reason: input.Reason}
		return nil
	})
	if err != nil {
		return ReleasedTicketInventory{}, err
	}
	return result, nil
}

func lockReleasableTicket(ctx context.Context, tx pgx.Tx, ticketID, eventID uuid.UUID) (releasableTicket, error) {
	var t releasableTicket
	err := tx.QueryRow(ctx, `
		SELECT t.event_id, t.status, t.inventory_kind, t.reserved_inventory_unit_id,
		       t.ga_pool_id, t.inventory_released_at, t.origin_sale_item_id,
		       t.origin_issuance_item_id,
		       COALESCE(si.source_allocation_id, ni.allocation_id),
		       COALESCE(ri.source_allocation_reserved_unit_id, nii.allocation_reserved_unit_id),
		       COALESCE(ri.source_ga_allocation_bucket_id, nii.ga_allocation_bucket_id)
		FROM ticket_entitlements t
		LEFT JOIN sale_items si ON si.id = t.origin_sale_item_id
		LEFT JOIN reservation_items ri ON ri.id = si.reservation_item_id
		LEFT JOIN non_public_issuance_items nii ON nii.id = t.origin_issuance_item_id
		LEFT JOIN non_public_issuances ni ON ni.id = nii.issuance_id
		WHERE t.id = $1 AND t.event_id = $2
		FOR UPDATE OF t
	`, ticketID, eventID).Scan(&t.EventID, &t.Status, &t.InventoryKind, &t.ReservedInventoryUnitID, &t.GAPoolID, &t.InventoryReleasedAt, &t.OriginSaleItemID, &t.OriginIssuanceItemID, &t.SourceAllocationID, &t.SourceAllocationReservedUnitID, &t.SourceGAAllocationBucketID)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, apierror.New(apierror.CodeResourceNotFound, "Ticket not found")
	}
	return t, err
}

func validateReleaseDestinationAllocation(ctx context.Context, tx pgx.Tx, eventID, allocationID uuid.UUID, partnerID *uuid.UUID) error {
	var mode string
	var owner *uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT a.mode, a.partner_id
		FROM allocations a
		JOIN inventory_restrictions ir ON ir.id = a.restriction_id
		WHERE a.restriction_id = $1 AND ir.event_id = $2 AND ir.state = 'ACTIVE'
		FOR KEY SHARE OF ir
	`, allocationID, eventID).Scan(&mode, &owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return apierror.New(apierror.CodeInventoryUnavailable, "Destination Allocation is not active for this Event")
	}
	if err != nil {
		return err
	}
	if partnerID != nil && (mode != "CHANNEL" || owner == nil || *owner != *partnerID) {
		return apierror.New(apierror.CodeNotAuthorized, "Partner cannot re-release inventory to this Allocation")
	}
	return nil
}

func releaseTicketCapacity(ctx context.Context, tx pgx.Tx, t releasableTicket, input InventoryReleaseInput, now time.Time) error {
	switch t.InventoryKind {
	case InventoryReserved:
		return releaseReservedTicketCapacity(ctx, tx, t, input, now)
	case InventoryGA:
		return releaseGATicketCapacity(ctx, tx, t, input, now)
	default:
		return apierror.New(apierror.CodeInternal, "Ticket has unsupported inventory kind")
	}
}

func releaseReservedTicketCapacity(ctx context.Context, tx pgx.Tx, t releasableTicket, input InventoryReleaseInput, now time.Time) error {
	if t.ReservedInventoryUnitID == nil {
		return apierror.New(apierror.CodeInternal, "Reserved Ticket has no inventory identity")
	}
	var claimID uuid.UUID
	var err error
	if t.OriginSaleItemID != nil {
		err = tx.QueryRow(ctx, `SELECT id FROM reserved_inventory_claims WHERE reserved_inventory_unit_id=$1 AND claim_type='SALE' AND sale_item_id=$2 AND ended_at IS NULL FOR UPDATE`, *t.ReservedInventoryUnitID, *t.OriginSaleItemID).Scan(&claimID)
	} else {
		err = tx.QueryRow(ctx, `SELECT id FROM reserved_inventory_claims WHERE reserved_inventory_unit_id=$1 AND claim_type='ISSUANCE' AND issuance_item_id=$2 AND ended_at IS NULL FOR UPDATE`, *t.ReservedInventoryUnitID, *t.OriginIssuanceItemID).Scan(&claimID)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return apierror.New(apierror.CodeInventoryUnavailable, "Ticket no longer owns current Reserved inventory")
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE reserved_inventory_claims SET ended_at=$2,end_reason='TICKET_INVENTORY_RELEASED' WHERE id=$1 AND ended_at IS NULL`, claimID, now); err != nil {
		return err
	}
	if input.DestinationKind == SourceShared {
		return nil
	}
	var membershipID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id FROM allocation_reserved_units WHERE allocation_id=$1 AND reserved_inventory_unit_id=$2 AND released_at IS NULL FOR UPDATE`, *input.DestinationAllocationID, *t.ReservedInventoryUnitID).Scan(&membershipID)
	if errors.Is(err, pgx.ErrNoRows) {
		return apierror.New(apierror.CodeInventoryUnavailable, "Reserved unit is not an active member of the destination Allocation")
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO reserved_inventory_claims (id,reserved_inventory_unit_id,claim_type,allocation_reserved_unit_id,activated_at) VALUES ($1,$2,'ALLOCATION',$3,$4)`, uuid.New(), *t.ReservedInventoryUnitID, membershipID, now)
	return err
}

func releaseGATicketCapacity(ctx context.Context, tx pgx.Tx, t releasableTicket, input InventoryReleaseInput, now time.Time) error {
	if t.GAPoolID == nil {
		return apierror.New(apierror.CodeInternal, "GA Ticket has no pool identity")
	}
	if _, err := tx.Exec(ctx, `SELECT id FROM ga_inventory_pools WHERE id=$1 FOR UPDATE`, *t.GAPoolID); err != nil {
		return err
	}
	column := "sold_current_quantity"
	if t.OriginIssuanceItemID != nil {
		column = "issued_current_quantity"
	}
	sameAllocation := input.DestinationKind == SourceAllocation && t.SourceAllocationID != nil && *t.SourceAllocationID == *input.DestinationAllocationID
	if t.SourceGAAllocationBucketID == nil {
		if column != "sold_current_quantity" {
			return apierror.New(apierror.CodeInternal, "Issued GA Ticket has no Allocation source")
		}
		tag, err := tx.Exec(ctx, `UPDATE ga_shared_inventory SET sold_current_quantity=sold_current_quantity-1,updated_at=$2 WHERE ga_pool_id=$1 AND sold_current_quantity>=1`, *t.GAPoolID, now)
		if err != nil || tag.RowsAffected() != 1 {
			if err != nil {
				return err
			}
			return apierror.New(apierror.CodeInventoryUnavailable, "Ticket no longer owns current shared GA capacity")
		}
	} else {
		setDestination := "released_quantity=released_quantity+1"
		if sameAllocation {
			setDestination = "available_quantity=available_quantity+1"
		}
		query := `UPDATE ga_allocation_buckets SET ` + column + `=` + column + `-1,` + setDestination + `,updated_at=$2 WHERE id=$1 AND ` + column + `>=1`
		tag, err := tx.Exec(ctx, query, *t.SourceGAAllocationBucketID, now)
		if err != nil || tag.RowsAffected() != 1 {
			if err != nil {
				return err
			}
			return apierror.New(apierror.CodeInventoryUnavailable, "Ticket no longer owns current Allocation GA capacity")
		}
	}
	if sameAllocation {
		return nil
	}
	if input.DestinationKind == SourceShared {
		_, err := tx.Exec(ctx, `UPDATE ga_shared_inventory SET available_quantity=available_quantity+1,updated_at=$2 WHERE ga_pool_id=$1`, *t.GAPoolID, now)
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE ga_allocation_buckets SET assigned_quantity=assigned_quantity+1,available_quantity=available_quantity+1,updated_at=$3 WHERE allocation_id=$1 AND ga_pool_id=$2`, *input.DestinationAllocationID, *t.GAPoolID, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return apierror.New(apierror.CodeInventoryUnavailable, "GA pool is not configured on the destination Allocation")
	}
	return nil
}
