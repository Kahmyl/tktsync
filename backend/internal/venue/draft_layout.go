package venue

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/outbox"
)

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
