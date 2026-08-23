package event

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/outbox"
)

func (s *Service) MaterializeLayout(
	ctx context.Context,
	actorID uuid.UUID,
	eventID uuid.UUID,
	layoutID uuid.UUID,
) error {
	if actorID == uuid.Nil || eventID == uuid.Nil || layoutID == uuid.Nil {
		return validation("actor, event and layout are required")
	}

	return s.transactions.Run(ctx, func(tx pgx.Tx) error {
		var (
			state   string
			venueID uuid.UUID
		)

		if err := tx.QueryRow(
			ctx,
			`
				SELECT state, venue_id
				FROM events
				WHERE id = $1
				FOR UPDATE
			`,
			eventID,
		).Scan(&state, &venueID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notFound("event")
			}
			return err
		}

		if state != "DRAFT" {
			return validation("Event layout may only be materialized while DRAFT")
		}

		var (
			layoutState   string
			layoutVenueID uuid.UUID
		)

		if err := tx.QueryRow(
			ctx,
			`
				SELECT state, venue_id
				FROM venue_layout_versions
				WHERE id = $1
				FOR KEY SHARE
			`,
			layoutID,
		).Scan(&layoutState, &layoutVenueID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notFound("layout")
			}
			return err
		}

		if layoutState != "PUBLISHED" {
			return validation("Event materialization requires a PUBLISHED layout")
		}

		if layoutVenueID != venueID {
			return validation("layout does not belong to the Event venue")
		}

		var missingCapacity int
		if err := tx.QueryRow(
			ctx,
			`
				SELECT COUNT(*)
				FROM venue_layout_ga_zones
				WHERE layout_version_id = $1
				  AND default_capacity IS NULL
			`,
			layoutID,
		).Scan(&missingCapacity); err != nil {
			return err
		}

		if missingCapacity != 0 {
			return validation(
				"all materialized GA zones require an Event-resolvable capacity",
			)
		}

		for _, statement := range []string{
			`
				DELETE FROM ga_shared_inventory
				WHERE ga_pool_id IN (
					SELECT id
					FROM ga_inventory_pools
					WHERE event_id = $1
				)
			`,
			`DELETE FROM ga_inventory_pools WHERE event_id = $1`,
			`DELETE FROM reserved_inventory_units WHERE event_id = $1`,
			`DELETE FROM event_sections WHERE event_id = $1`,
			`DELETE FROM event_layout_snapshots WHERE event_id = $1`,
		} {
			if _, err := tx.Exec(ctx, statement, eventID); err != nil {
				return err
			}
		}

		var snapshot []byte

		if err := tx.QueryRow(
			ctx,
			`
				SELECT jsonb_build_object(
					'layout_version_id', vlv.id,
					'version_number', vlv.version_number,
					'geometry', vlv.geometry_json,
					'sections', COALESCE((
						SELECT jsonb_agg(
							jsonb_build_object(
								'object_key', s.object_key,
								'name', s.name,
								'kind', s.section_kind,
								'sort_order', s.sort_order,
								'metadata', s.metadata
							)
							ORDER BY s.sort_order, s.object_key
						)
						FROM venue_layout_sections s
						WHERE s.layout_version_id = vlv.id
					), '[]'::jsonb),
					'rows', COALESCE((
						SELECT jsonb_agg(
							jsonb_build_object(
								'object_key', r.object_key,
								'section_id', r.section_id,
								'label', r.label,
								'sort_order', r.sort_order,
								'metadata', r.metadata
							)
							ORDER BY r.sort_order, r.object_key
						)
						FROM venue_layout_rows r
						WHERE r.layout_version_id = vlv.id
					), '[]'::jsonb),
					'tables', COALESCE((
						SELECT jsonb_agg(
							jsonb_build_object(
								'object_key', t.object_key,
								'section_id', t.section_id,
								'label', t.label,
								'metadata', t.metadata
							)
							ORDER BY t.object_key
						)
						FROM venue_layout_tables t
						WHERE t.layout_version_id = vlv.id
					), '[]'::jsonb),
					'seats', COALESCE((
						SELECT jsonb_agg(
							jsonb_build_object(
								'id', seat.id,
								'object_key', seat.object_key,
								'section_id', seat.section_id,
								'row_id', seat.row_id,
								'table_id', seat.table_id,
								'seat_label', seat.seat_label,
								'sort_order', seat.sort_order,
								'metadata', seat.metadata
							)
							ORDER BY seat.sort_order, seat.object_key
						)
						FROM venue_layout_seats seat
						WHERE seat.layout_version_id = vlv.id
					), '[]'::jsonb),
					'ga_zones', COALESCE((
						SELECT jsonb_agg(
							jsonb_build_object(
								'id', z.id,
								'object_key', z.object_key,
								'section_id', z.section_id,
								'name', z.name,
								'default_capacity', z.default_capacity,
								'metadata', z.metadata
							)
							ORDER BY z.object_key
						)
						FROM venue_layout_ga_zones z
						WHERE z.layout_version_id = vlv.id
					), '[]'::jsonb)
				)
				FROM venue_layout_versions vlv
				WHERE vlv.id = $1
			`,
			layoutID,
		).Scan(&snapshot); err != nil {
			return err
		}

		if _, err := tx.Exec(
			ctx,
			`
				INSERT INTO event_layout_snapshots (
					event_id,
					source_layout_version_id,
					snapshot_json,
					content_hash,
					finalized_at,
					created_at,
					updated_at
				)
				VALUES (
					$1,
					$2,
					$3::jsonb,
					digest(($3::jsonb)::text, 'sha256'),
					clock_timestamp(),
					clock_timestamp(),
					clock_timestamp()
				)
			`,
			eventID,
			layoutID,
			string(snapshot),
		); err != nil {
			return err
		}

		if _, err := tx.Exec(
			ctx,
			`
				INSERT INTO event_sections (
					id,
					event_id,
					source_layout_section_id,
					snapshot_object_key,
					name,
					default_price_tier_id,
					sort_order,
					metadata
				)
				SELECT
					gen_random_uuid(),
					$1,
					s.id,
					s.object_key,
					s.name,
					NULL,
					s.sort_order,
					s.metadata
				FROM venue_layout_sections s
				WHERE s.layout_version_id = $2
				ORDER BY s.sort_order, s.object_key
			`,
			eventID,
			layoutID,
		); err != nil {
			return err
		}

		if _, err := tx.Exec(
			ctx,
			`
				INSERT INTO reserved_inventory_units (
					id,
					event_id,
					event_section_id,
					source_venue_seat_id,
					snapshot_object_key,
					row_label,
					seat_label,
					table_label,
					display_label,
					price_tier_override_id,
					metadata,
					created_at
				)
				SELECT
					gen_random_uuid(),
					$1,
					es.id,
					seat.id,
					seat.object_key,
					r.label,
					seat.seat_label,
					t.label,
					concat_ws(
						' ',
						es.name,
						NULLIF(r.label, ''),
						NULLIF(t.label, ''),
						seat.seat_label
					),
					NULL,
					seat.metadata,
					clock_timestamp()
				FROM venue_layout_seats seat
				JOIN event_sections es
				  ON es.event_id = $1
				 AND es.source_layout_section_id = seat.section_id
				LEFT JOIN venue_layout_rows r
				  ON r.id = seat.row_id
				LEFT JOIN venue_layout_tables t
				  ON t.id = seat.table_id
				WHERE seat.layout_version_id = $2
			`,
			eventID,
			layoutID,
		); err != nil {
			return err
		}

		if _, err := tx.Exec(
			ctx,
			`
				INSERT INTO ga_inventory_pools (
					id,
					event_id,
					event_section_id,
					source_ga_zone_id,
					snapshot_object_key,
					name,
					capacity,
					price_tier_id,
					metadata,
					created_at
				)
				SELECT
					gen_random_uuid(),
					$1,
					es.id,
					z.id,
					z.object_key,
					z.name,
					z.default_capacity,
					NULL,
					z.metadata,
					clock_timestamp()
				FROM venue_layout_ga_zones z
				JOIN event_sections es
				  ON es.event_id = $1
				 AND es.source_layout_section_id = z.section_id
				WHERE z.layout_version_id = $2
			`,
			eventID,
			layoutID,
		); err != nil {
			return err
		}

		if _, err := tx.Exec(
			ctx,
			`
				INSERT INTO ga_shared_inventory (
					ga_pool_id,
					available_quantity,
					active_reserved_quantity,
					sold_current_quantity,
					updated_at
				)
				SELECT
					id,
					capacity,
					0,
					0,
					clock_timestamp()
				FROM ga_inventory_pools
				WHERE event_id = $1
			`,
			eventID,
		); err != nil {
			return err
		}

		var (
			sourceSeats int
			eventSeats  int
			sourceGA    int
			eventGA     int
		)

		if err := tx.QueryRow(
			ctx,
			`
				SELECT
					(SELECT COUNT(*) FROM venue_layout_seats WHERE layout_version_id = $2),
					(SELECT COUNT(*) FROM reserved_inventory_units WHERE event_id = $1),
					(SELECT COUNT(*) FROM venue_layout_ga_zones WHERE layout_version_id = $2),
					(SELECT COUNT(*) FROM ga_inventory_pools WHERE event_id = $1)
			`,
			eventID,
			layoutID,
		).Scan(
			&sourceSeats,
			&eventSeats,
			&sourceGA,
			&eventGA,
		); err != nil {
			return err
		}

		if sourceSeats != eventSeats || sourceGA != eventGA {
			return validation("materialized inventory does not match layout snapshot")
		}

		if _, err := s.audit.Append(
			ctx,
			tx,
			audit.Event{
				EventID:     &eventID,
				ActorKind:   audit.ActorUser,
				ActorUserID: &actorID,
				Operation:   "EVENT_LAYOUT_MATERIALIZED",
				EntityType:  "EVENT",
				EntityID:    &eventID,
				NewState: map[string]any{
					"layout_version_id": layoutID.String(),
					"reserved_units":    eventSeats,
					"ga_pools":          eventGA,
				},
			},
		); err != nil {
			return err
		}

		_, err := s.outbox.Append(
			ctx,
			tx,
			outbox.Fact{
				EventID:       &eventID,
				FactType:      "event.layout_materialized",
				AggregateType: "EVENT",
				AggregateID:   &eventID,
				Payload: map[string]any{
					"layout_version_id": layoutID.String(),
				},
			},
		)

		return err
	})
}
