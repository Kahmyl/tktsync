package reporting

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
			SELECT te.status,te.inventory_kind,es.name,riu.row_label,riu.seat_label,
			       COALESCE(gp.name,''),tad.display_name,adm.status,adm.admitted_at,te.created_at
			FROM ticket_entitlements te
			LEFT JOIN reserved_inventory_units riu ON riu.id=te.reserved_inventory_unit_id
			LEFT JOIN event_sections es ON es.id=riu.event_section_id
			LEFT JOIN ga_inventory_pools gp ON gp.id=te.ga_pool_id
			LEFT JOIN ticket_attendee_details tad ON tad.ticket_entitlement_id=te.id
			LEFT JOIN LATERAL (SELECT status,admitted_at FROM admissions WHERE ticket_entitlement_id=te.id ORDER BY admitted_at DESC,id DESC LIMIT 1) adm ON true
			WHERE te.event_id=$1 ORDER BY COALESCE(es.name,gp.name,''),riu.row_label,riu.seat_label,te.created_at,te.id
		`, eventID)
		if err != nil {
			return err
		}
		defer rows.Close()
		csvw := csv.NewWriter(writer)
		if err := csvw.Write([]string{"ticket", "attendee_name", "event", "section_or_area", "row", "seat", "ticket_status", "admission_status", "admission_timestamp", "issued_at"}); err != nil {
			return err
		}
		wrote := false
		rowNumber := 0
		for rows.Next() {
			var status, kind string
			var section, row, seat, ga, displayName, admissionStatus *string
			var admittedAt *time.Time
			var issuedAt time.Time
			if err := rows.Scan(&status, &kind, &section, &row, &seat, &ga, &displayName, &admissionStatus, &admittedAt, &issuedAt); err != nil {
				return err
			}
			wrote = true
			area := valueOrEmpty(section)
			if kind == "GA" {
				area = valueOrEmpty(ga)
			}
			record := []string{fmt.Sprintf("Ticket %d", rowNumber+1), valueOrEmpty(displayName), snapshot.Event.Name, area, valueOrEmpty(row), valueOrEmpty(seat), status, valueOrEmpty(admissionStatus), timeOrEmpty(admittedAt), issuedAt.Format(time.RFC3339Nano)}
			if err := csvw.Write(record); err != nil {
				return err
			}
			rowNumber++
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if !wrote {
			// A header-only CSV is a valid empty accreditation roster.
		}
		csvw.Flush()
		return csvw.Error()
	})
	return snapshot, err
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
