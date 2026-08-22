package venue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/outbox"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
)

type Service struct {
	transactions *database.Runner
	audit        audit.Store
	outbox       outbox.Store
}

func NewService(transactions *database.Runner) *Service {
	return &Service{
		transactions: transactions,
	}
}

type CreateVenueInput struct {
	Name        string
	AddressText string
	Metadata    json.RawMessage
}

type SectionInput struct {
	ObjectKey string
	Name      string
	Kind      string
	SortOrder int
	Metadata  json.RawMessage
}

type RowInput struct {
	ObjectKey  string
	SectionKey string
	Label      string
	SortOrder  int
	Metadata   json.RawMessage
}

type TableInput struct {
	ObjectKey  string
	SectionKey string
	Label      string
	Metadata   json.RawMessage
}

type SeatInput struct {
	ObjectKey  string
	SectionKey string
	RowKey     string
	TableKey   string
	SeatLabel  string
	SortOrder  int
	Metadata   json.RawMessage
}

type GAZoneInput struct {
	ObjectKey       string
	SectionKey      string
	Name            string
	DefaultCapacity *int
	Metadata        json.RawMessage
}

type ReplaceLayoutInput struct {
	Geometry json.RawMessage
	Sections []SectionInput
	Rows     []RowInput
	Tables   []TableInput
	Seats    []SeatInput
	GAZones  []GAZoneInput
}

func (s *Service) CreateVenue(
	ctx context.Context,
	actorID uuid.UUID,
	input CreateVenueInput,
) (uuid.UUID, error) {
	if actorID == uuid.Nil {
		return uuid.Nil, validation("actor is required")
	}

	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return uuid.Nil, validation("venue name is required")
	}

	metadata, err := normalizeJSON(input.Metadata)
	if err != nil {
		return uuid.Nil, err
	}

	id := uuid.New()

	err = s.transactions.Run(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(
			ctx,
			`
				INSERT INTO venues (
					id,
					name,
					address_text,
					metadata,
					created_at,
					updated_at
				)
				VALUES (
					$1,
					$2,
					NULLIF($3, ''),
					$4::jsonb,
					clock_timestamp(),
					clock_timestamp()
				)
			`,
			id,
			input.Name,
			strings.TrimSpace(input.AddressText),
			metadata,
		)
		if err != nil {
			return err
		}

		if _, err := s.audit.Append(
			ctx,
			tx,
			audit.Event{
				ActorKind:   audit.ActorUser,
				ActorUserID: &actorID,
				Operation:   "VENUE_CREATED",
				EntityType:  "VENUE",
				EntityID:    &id,
				NewState: map[string]any{
					"name": input.Name,
				},
			},
		); err != nil {
			return err
		}

		_, err = s.outbox.Append(
			ctx,
			tx,
			outbox.Fact{
				FactType:      "venue.created",
				AggregateType: "VENUE",
				AggregateID:   &id,
				Payload: map[string]any{
					"venue_id": id.String(),
				},
			},
		)

		return err
	})

	if err != nil {
		return uuid.Nil, err
	}

	return id, nil
}

func (s *Service) CreateLayoutVersion(
	ctx context.Context,
	actorID uuid.UUID,
	venueID uuid.UUID,
) (uuid.UUID, int, error) {
	if actorID == uuid.Nil || venueID == uuid.Nil {
		return uuid.Nil, 0, validation("actor and venue are required")
	}

	layoutID := uuid.New()
	version := 0

	err := s.transactions.Run(ctx, func(tx pgx.Tx) error {
		var locked uuid.UUID

		if err := tx.QueryRow(
			ctx,
			`SELECT id FROM venues WHERE id = $1 FOR UPDATE`,
			venueID,
		).Scan(&locked); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notFound("venue")
			}
			return err
		}

		if err := tx.QueryRow(
			ctx,
			`
				SELECT COALESCE(MAX(version_number), 0) + 1
				FROM venue_layout_versions
				WHERE venue_id = $1
			`,
			venueID,
		).Scan(&version); err != nil {
			return err
		}

		_, err := tx.Exec(
			ctx,
			`
				INSERT INTO venue_layout_versions (
					id,
					venue_id,
					version_number,
					state,
					geometry_json,
					created_at
				)
				VALUES (
					$1,
					$2,
					$3,
					'DRAFT',
					'{}'::jsonb,
					clock_timestamp()
				)
			`,
			layoutID,
			venueID,
			version,
		)
		if err != nil {
			return err
		}

		if _, err := s.audit.Append(
			ctx,
			tx,
			audit.Event{
				ActorKind:   audit.ActorUser,
				ActorUserID: &actorID,
				Operation:   "VENUE_LAYOUT_VERSION_CREATED",
				EntityType:  "VENUE_LAYOUT_VERSION",
				EntityID:    &layoutID,
				NewState: map[string]any{
					"venue_id":       venueID.String(),
					"version_number": version,
					"state":          "DRAFT",
				},
			},
		); err != nil {
			return err
		}

		_, err = s.outbox.Append(
			ctx,
			tx,
			outbox.Fact{
				FactType:      "venue.layout_version_created",
				AggregateType: "VENUE",
				AggregateID:   &venueID,
				Payload: map[string]any{
					"layout_version_id": layoutID.String(),
					"version_number":    version,
				},
			},
		)

		return err
	})

	if err != nil {
		return uuid.Nil, 0, err
	}

	return layoutID, version, nil
}

func (s *Service) ReplaceDraftLayout(
	ctx context.Context,
	actorID uuid.UUID,
	layoutID uuid.UUID,
	input ReplaceLayoutInput,
) error {
	if actorID == uuid.Nil || layoutID == uuid.Nil {
		return validation("actor and layout are required")
	}

	geometry, err := normalizeJSON(input.Geometry)
	if err != nil {
		return fmt.Errorf("geometry: %w", err)
	}

	if err := validateLayout(input); err != nil {
		return err
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
			return validation("only DRAFT layout versions are editable")
		}

		for _, statement := range []string{
			`DELETE FROM venue_layout_seats WHERE layout_version_id = $1`,
			`DELETE FROM venue_layout_ga_zones WHERE layout_version_id = $1`,
			`DELETE FROM venue_layout_rows WHERE layout_version_id = $1`,
			`DELETE FROM venue_layout_tables WHERE layout_version_id = $1`,
			`DELETE FROM venue_layout_sections WHERE layout_version_id = $1`,
		} {
			if _, err := tx.Exec(ctx, statement, layoutID); err != nil {
				return err
			}
		}

		if _, err := tx.Exec(
			ctx,
			`
				UPDATE venue_layout_versions
				SET
					geometry_json = $2::jsonb,
					content_hash = NULL
				WHERE id = $1
			`,
			layoutID,
			geometry,
		); err != nil {
			return err
		}

		sectionIDs := make(map[string]uuid.UUID, len(input.Sections))
		for _, section := range input.Sections {
			metadata, err := normalizeJSON(section.Metadata)
			if err != nil {
				return err
			}

			id := uuid.New()
			sectionIDs[section.ObjectKey] = id

			if _, err := tx.Exec(
				ctx,
				`
					INSERT INTO venue_layout_sections (
						id,
						layout_version_id,
						object_key,
						name,
						section_kind,
						sort_order,
						metadata
					)
					VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb)
				`,
				id,
				layoutID,
				section.ObjectKey,
				section.Name,
				section.Kind,
				section.SortOrder,
				metadata,
			); err != nil {
				return err
			}
		}

		rowIDs := make(map[string]uuid.UUID, len(input.Rows))
		rowSections := make(map[string]string, len(input.Rows))

		for _, row := range input.Rows {
			metadata, err := normalizeJSON(row.Metadata)
			if err != nil {
				return err
			}

			id := uuid.New()
			rowIDs[row.ObjectKey] = id
			rowSections[row.ObjectKey] = row.SectionKey

			if _, err := tx.Exec(
				ctx,
				`
					INSERT INTO venue_layout_rows (
						id,
						layout_version_id,
						section_id,
						object_key,
						label,
						sort_order,
						metadata
					)
					VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb)
				`,
				id,
				layoutID,
				sectionIDs[row.SectionKey],
				row.ObjectKey,
				row.Label,
				row.SortOrder,
				metadata,
			); err != nil {
				return err
			}
		}

		tableIDs := make(map[string]uuid.UUID, len(input.Tables))
		tableSections := make(map[string]string, len(input.Tables))

		for _, table := range input.Tables {
			metadata, err := normalizeJSON(table.Metadata)
			if err != nil {
				return err
			}

			id := uuid.New()
			tableIDs[table.ObjectKey] = id
			tableSections[table.ObjectKey] = table.SectionKey

			if _, err := tx.Exec(
				ctx,
				`
					INSERT INTO venue_layout_tables (
						id,
						layout_version_id,
						section_id,
						object_key,
						label,
						metadata
					)
					VALUES ($1,$2,$3,$4,$5,$6::jsonb)
				`,
				id,
				layoutID,
				sectionIDs[table.SectionKey],
				table.ObjectKey,
				table.Label,
				metadata,
			); err != nil {
				return err
			}
		}

		for _, seat := range input.Seats {
			metadata, err := normalizeJSON(seat.Metadata)
			if err != nil {
				return err
			}

			var rowID *uuid.UUID
			if seat.RowKey != "" {
				value := rowIDs[seat.RowKey]
				rowID = &value
			}

			var tableID *uuid.UUID
			if seat.TableKey != "" {
				value := tableIDs[seat.TableKey]
				tableID = &value
			}

			if _, err := tx.Exec(
				ctx,
				`
					INSERT INTO venue_layout_seats (
						id,
						layout_version_id,
						section_id,
						row_id,
						table_id,
						object_key,
						seat_label,
						sort_order,
						metadata
					)
					VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb)
				`,
				uuid.New(),
				layoutID,
				sectionIDs[seat.SectionKey],
				rowID,
				tableID,
				seat.ObjectKey,
				seat.SeatLabel,
				seat.SortOrder,
				metadata,
			); err != nil {
				return err
			}
		}

		for _, zone := range input.GAZones {
			metadata, err := normalizeJSON(zone.Metadata)
			if err != nil {
				return err
			}

			if _, err := tx.Exec(
				ctx,
				`
					INSERT INTO venue_layout_ga_zones (
						id,
						layout_version_id,
						section_id,
						object_key,
						name,
						default_capacity,
						metadata
					)
					VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb)
				`,
				uuid.New(),
				layoutID,
				sectionIDs[zone.SectionKey],
				zone.ObjectKey,
				zone.Name,
				zone.DefaultCapacity,
				metadata,
			); err != nil {
				return err
			}
		}

		if _, err := s.audit.Append(
			ctx,
			tx,
			audit.Event{
				ActorKind:   audit.ActorUser,
				ActorUserID: &actorID,
				Operation:   "VENUE_LAYOUT_DRAFT_REPLACED",
				EntityType:  "VENUE_LAYOUT_VERSION",
				EntityID:    &layoutID,
				NewState: map[string]any{
					"section_count": len(input.Sections),
					"seat_count":    len(input.Seats),
					"ga_zone_count": len(input.GAZones),
				},
			},
		); err != nil {
			return err
		}

		_, err := s.outbox.Append(
			ctx,
			tx,
			outbox.Fact{
				FactType:      "venue.layout_draft_replaced",
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
