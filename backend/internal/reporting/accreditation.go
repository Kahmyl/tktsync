package reporting

import (
	"context"
	"encoding/csv"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
)

type AccreditationSnapshot struct {
	GeneratedAt time.Time
	Event       EventContext
}

func (s *Service) WriteAccreditationCSV(ctx context.Context, eventID uuid.UUID, writer io.Writer) (AccreditationSnapshot, error) {
	var snapshot AccreditationSnapshot
	err := s.readSnapshot(ctx, func(tx pgx.Tx) error {
		var err error
		snapshot.Event, snapshot.GeneratedAt, err = eventContext(ctx, tx, eventID)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT ir.id,ir.purpose,a.mode,npi.id,npi.issued_at,te.id,te.status,te.inventory_kind,
			       COALESCE(riu.display_label,gp.name,''),tad.partner_attendee_ref,tad.display_name,
			       COALESCE(tad.accreditation_data,'{}'::jsonb),adm.status,adm.admitted_at,adm.reversed_at
			FROM events e
			LEFT JOIN non_public_issuances npi ON npi.event_id=e.id
			LEFT JOIN inventory_restrictions ir ON ir.id=npi.allocation_id
			LEFT JOIN allocations a ON a.restriction_id=ir.id
			LEFT JOIN non_public_issuance_items nii ON nii.issuance_id=npi.id
			LEFT JOIN ticket_entitlements te ON te.origin_issuance_item_id=nii.id
			LEFT JOIN reserved_inventory_units riu ON riu.id=te.reserved_inventory_unit_id
			LEFT JOIN ga_inventory_pools gp ON gp.id=te.ga_pool_id
			LEFT JOIN ticket_attendee_details tad ON tad.ticket_entitlement_id=te.id
			LEFT JOIN LATERAL (SELECT status,admitted_at,reversed_at FROM admissions WHERE ticket_entitlement_id=te.id ORDER BY admitted_at DESC,id DESC LIMIT 1) adm ON true
			WHERE e.id=$1 ORDER BY ir.id,npi.issued_at,npi.id,te.id
		`, eventID)
		if err != nil {
			return err
		}
		defer rows.Close()
		csvw := csv.NewWriter(writer)
		if err := csvw.Write([]string{"generated_at", "event_id", "event_name", "event_state", "allocation_id", "allocation_purpose", "allocation_mode", "issuance_id", "issued_at", "ticket_id", "ticket_status", "inventory_kind", "inventory_display", "partner_attendee_ref", "display_name", "accreditation_data", "admission_status", "admitted_at", "reversed_at"}); err != nil {
			return err
		}
		wrote := false
		for rows.Next() {
			var allocationID, issuanceID, ticketID *uuid.UUID
			var purpose, mode, status, kind, display, partnerRef, displayName, admissionStatus *string
			var issuedAt, admittedAt, reversedAt *time.Time
			var accreditation []byte
			if err := rows.Scan(&allocationID, &purpose, &mode, &issuanceID, &issuedAt, &ticketID, &status, &kind, &display, &partnerRef, &displayName, &accreditation, &admissionStatus, &admittedAt, &reversedAt); err != nil {
				return err
			}
			if issuanceID == nil {
				continue
			}
			wrote = true
			cleanAccreditation := sanitizeJSON(accreditation)
			record := []string{snapshot.GeneratedAt.Format(time.RFC3339Nano), snapshot.Event.ID, snapshot.Event.Name, snapshot.Event.State, encodeCSVID(publicid.Allocation, allocationID), valueOrEmpty(purpose), valueOrEmpty(mode), encodeCSVID(publicid.NonPublicIssuance, issuanceID), timeOrEmpty(issuedAt), encodeCSVID(publicid.Ticket, ticketID), valueOrEmpty(status), valueOrEmpty(kind), valueOrEmpty(display), valueOrEmpty(partnerRef), valueOrEmpty(displayName), string(cleanAccreditation), valueOrEmpty(admissionStatus), timeOrEmpty(admittedAt), timeOrEmpty(reversedAt)}
			if err := csvw.Write(record); err != nil {
				return err
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if !wrote {
			if err := csvw.Write([]string{snapshot.GeneratedAt.Format(time.RFC3339Nano), snapshot.Event.ID, snapshot.Event.Name, snapshot.Event.State, "", "", "", "", "", "", "", "", "", "", "", "{}", "", "", ""}); err != nil {
				return err
			}
		}
		csvw.Flush()
		return csvw.Error()
	})
	return snapshot, err
}
func encodeCSVID(kind publicid.Kind, id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return publicid.Encode(kind, *id)
}
func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func timeOrEmpty(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format(time.RFC3339Nano)
}
