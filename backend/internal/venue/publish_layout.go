package venue

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/outbox"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

func (s *Service) PublishLayout(
	ctx context.Context,
	actorID uuid.UUID,
	layoutID uuid.UUID,
) error {
	if actorID == uuid.Nil || layoutID == uuid.Nil {
		return validation("actor and layout are required")
	}

	return s.transactions.Run(ctx, func(tx pgx.Tx) error {
		var venueID uuid.UUID

		if err := tx.QueryRow(
			ctx,
			`
				SELECT venue_id
				FROM venue_layout_versions
				WHERE id = $1
			`,
			layoutID,
		).Scan(&venueID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notFound("layout")
			}
			return err
		}

		var lockedVenue uuid.UUID
		if err := tx.QueryRow(
			ctx,
			`SELECT id FROM venues WHERE id = $1 FOR UPDATE`,
			venueID,
		).Scan(&lockedVenue); err != nil {
			return err
		}

		var state string
		if err := tx.QueryRow(
			ctx,
			`
				SELECT state
				FROM venue_layout_versions
				WHERE id = $1
				  AND venue_id = $2
				FOR UPDATE
			`,
			layoutID,
			venueID,
		).Scan(&state); err != nil {
			return err
		}

		if state != "DRAFT" {
			return validation("only DRAFT layout versions may be published")
		}

		var sectionCount, seatCount, gaCount int

		if err := tx.QueryRow(
			ctx,
			`
				SELECT
					(SELECT COUNT(*) FROM venue_layout_sections WHERE layout_version_id = $1),
					(SELECT COUNT(*) FROM venue_layout_seats WHERE layout_version_id = $1),
					(SELECT COUNT(*) FROM venue_layout_ga_zones WHERE layout_version_id = $1)
			`,
			layoutID,
		).Scan(&sectionCount, &seatCount, &gaCount); err != nil {
			return err
		}

		if sectionCount == 0 {
			return validation("published layout requires at least one section")
		}

		if seatCount+gaCount == 0 {
			return validation("published layout requires reserved or GA inventory structure")
		}

		if _, err := tx.Exec(
			ctx,
			`
				UPDATE venue_layout_versions
				SET
					state = 'PUBLISHED',
					published_at = clock_timestamp(),
					content_hash = digest(
						geometry_json::text
						|| ':' || id::text
						|| ':' || version_number::text,
						'sha256'
					)
				WHERE id = $1
			`,
			layoutID,
		); err != nil {
			return err
		}

		if _, err := s.audit.Append(
			ctx,
			tx,
			audit.Event{
				ActorKind:   audit.ActorUser,
				ActorUserID: &actorID,
				Operation:   "VENUE_LAYOUT_PUBLISHED",
				EntityType:  "VENUE_LAYOUT_VERSION",
				EntityID:    &layoutID,
				PreviousState: map[string]any{
					"state": "DRAFT",
				},
				NewState: map[string]any{
					"state": "PUBLISHED",
				},
			},
		); err != nil {
			return err
		}

		_, err := s.outbox.Append(
			ctx,
			tx,
			outbox.Fact{
				FactType:      "venue.layout_published",
				AggregateType: "VENUE",
				AggregateID:   &venueID,
				Payload: map[string]any{
					"layout_version_id": layoutID.String(),
				},
			},
		)

		return err
	})
}

func validateLayout(input ReplaceLayoutInput) error {
	if len(input.Sections) == 0 {
		return validation("layout requires at least one section")
	}

	sectionKeys := map[string]struct{}{}
	for _, section := range input.Sections {
		if strings.TrimSpace(section.ObjectKey) == "" ||
			strings.TrimSpace(section.Name) == "" {
			return validation("section object key and name are required")
		}

		switch section.Kind {
		case "RESERVED", "GA", "TABLE", "MIXED_VISUAL":
		default:
			return validation("invalid section kind")
		}

		if _, exists := sectionKeys[section.ObjectKey]; exists {
			return validation("duplicate section object key")
		}

		sectionKeys[section.ObjectKey] = struct{}{}
	}

	rowKeys := map[string]string{}
	for _, row := range input.Rows {
		if _, ok := sectionKeys[row.SectionKey]; !ok {
			return validation("row references unknown section")
		}
		if row.ObjectKey == "" || row.Label == "" {
			return validation("row object key and label are required")
		}
		if _, exists := rowKeys[row.ObjectKey]; exists {
			return validation("duplicate row object key")
		}
		rowKeys[row.ObjectKey] = row.SectionKey
	}

	tableKeys := map[string]string{}
	for _, table := range input.Tables {
		if _, ok := sectionKeys[table.SectionKey]; !ok {
			return validation("table references unknown section")
		}
		if table.ObjectKey == "" || table.Label == "" {
			return validation("table object key and label are required")
		}
		if _, exists := tableKeys[table.ObjectKey]; exists {
			return validation("duplicate table object key")
		}
		tableKeys[table.ObjectKey] = table.SectionKey
	}

	seatKeys := map[string]struct{}{}
	for _, seat := range input.Seats {
		if _, ok := sectionKeys[seat.SectionKey]; !ok {
			return validation("seat references unknown section")
		}

		if seat.ObjectKey == "" || seat.SeatLabel == "" {
			return validation("seat object key and label are required")
		}

		if _, exists := seatKeys[seat.ObjectKey]; exists {
			return validation("duplicate seat object key")
		}
		seatKeys[seat.ObjectKey] = struct{}{}

		if seat.RowKey != "" {
			section, ok := rowKeys[seat.RowKey]
			if !ok || section != seat.SectionKey {
				return validation("seat row must belong to the same section")
			}
		}

		if seat.TableKey != "" {
			section, ok := tableKeys[seat.TableKey]
			if !ok || section != seat.SectionKey {
				return validation("seat table must belong to the same section")
			}
		}
	}

	gaKeys := map[string]struct{}{}
	for _, zone := range input.GAZones {
		if _, ok := sectionKeys[zone.SectionKey]; !ok {
			return validation("GA zone references unknown section")
		}

		if zone.ObjectKey == "" || zone.Name == "" {
			return validation("GA zone object key and name are required")
		}

		if zone.DefaultCapacity != nil && *zone.DefaultCapacity < 0 {
			return validation("GA default capacity cannot be negative")
		}

		if _, exists := gaKeys[zone.ObjectKey]; exists {
			return validation("duplicate GA zone object key")
		}

		gaKeys[zone.ObjectKey] = struct{}{}
	}

	return nil
}

func normalizeJSON(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "{}", nil
	}

	if !json.Valid(raw) {
		return "", validation("invalid JSON metadata")
	}

	return string(raw), nil
}

func validation(message string) error {
	return apierror.New(apierror.CodeValidation, message)
}

func notFound(resource string) error {
	return apierror.New(
		apierror.CodeResourceNotFound,
		resource+" not found",
	)
}
