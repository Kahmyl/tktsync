package event

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/outbox"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
)

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

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

type CreateInput struct {
	VenueID          uuid.UUID
	Name             string
	StartsAt         *time.Time
	EndsAt           *time.Time
	SalesOpenAt      *time.Time
	SalesCloseAt     *time.Time
	AdmissionOpenAt  *time.Time
	AdmissionCloseAt *time.Time
	TimezoneName     string
}

type TransactionPolicyInput struct {
	HoldDurationSeconds                  int
	CheckoutProtectionSeconds            int
	PaymentRetrySeconds                  int
	ReconciliationSeconds                int
	MaxReservationLifetimeSeconds        int
	MaxHoldQuantity                      int
	MaxActiveReservationsPerPartner      int
	MaxActiveReservationsPerBuyerSession int
	AllowVoidedInventoryRerelease        bool
}

type PriceTierInput struct {
	Code        string
	Name        string
	AmountMinor int64
	Currency    string
}

type PricingAssignmentInput struct {
	PriceTierID        uuid.UUID
	SectionObjectKeys  []string
	ReservedObjectKeys []string
	GAPoolObjectKeys   []string
}

func (s *Service) Create(
	ctx context.Context,
	actorID uuid.UUID,
	input CreateInput,
) (uuid.UUID, error) {
	if actorID == uuid.Nil || input.VenueID == uuid.Nil {
		return uuid.Nil, validation("actor and venue are required")
	}

	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return uuid.Nil, validation("event name is required")
	}

	id := uuid.New()

	err := s.transactions.Run(ctx, func(tx pgx.Tx) error {
		var venueExists bool
		if err := tx.QueryRow(
			ctx,
			`SELECT EXISTS (SELECT 1 FROM venues WHERE id = $1)`,
			input.VenueID,
		).Scan(&venueExists); err != nil {
			return err
		}

		if !venueExists {
			return notFound("venue")
		}

		_, err := tx.Exec(
			ctx,
			`
				INSERT INTO events (
					id,
					venue_id,
					name,
					state,
					starts_at,
					ends_at,
					sales_open_at,
					sales_close_at,
					admission_open_at,
					admission_close_at,
					timezone_name,
					admission_policy,
					created_at,
					updated_at
				)
				VALUES (
					$1,$2,$3,'DRAFT',
					$4,$5,$6,$7,$8,$9,
					NULLIF($10,''),
					'SINGLE_ENTRY',
					clock_timestamp(),
					clock_timestamp()
				)
			`,
			id,
			input.VenueID,
			input.Name,
			input.StartsAt,
			input.EndsAt,
			input.SalesOpenAt,
			input.SalesCloseAt,
			input.AdmissionOpenAt,
			input.AdmissionCloseAt,
			strings.TrimSpace(input.TimezoneName),
		)
		if err != nil {
			return err
		}

		if _, err := s.audit.Append(
			ctx,
			tx,
			audit.Event{
				EventID:     &id,
				ActorKind:   audit.ActorUser,
				ActorUserID: &actorID,
				Operation:   "EVENT_CREATED",
				EntityType:  "EVENT",
				EntityID:    &id,
				NewState: map[string]any{
					"state": "DRAFT",
					"name":  input.Name,
				},
			},
		); err != nil {
			return err
		}

		_, err = s.outbox.Append(
			ctx,
			tx,
			outbox.Fact{
				EventID:       &id,
				FactType:      "event.created",
				AggregateType: "EVENT",
				AggregateID:   &id,
				Payload: map[string]any{
					"event_id": id.String(),
					"state":    "DRAFT",
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

func (s *Service) ConfigureTransactionPolicy(
	ctx context.Context,
	actorID uuid.UUID,
	eventID uuid.UUID,
	input TransactionPolicyInput,
) error {
	if err := validatePolicy(input); err != nil {
		return err
	}

	return s.transactions.Run(ctx, func(tx pgx.Tx) error {
		var state string

		if err := tx.QueryRow(
			ctx,
			`SELECT state FROM events WHERE id = $1 FOR UPDATE`,
			eventID,
		).Scan(&state); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notFound("event")
			}
			return err
		}

		if state == "COMPLETED" || state == "CANCELLED" {
			return validation("terminal Event policy cannot be modified")
		}

		_, err := tx.Exec(
			ctx,
			`
				INSERT INTO event_transaction_policies (
					event_id,
					hold_duration_seconds,
					checkout_protection_seconds,
					payment_retry_seconds,
					reconciliation_seconds,
					max_reservation_lifetime_seconds,
					max_hold_quantity,
					max_active_reservations_per_partner,
					max_active_reservations_per_buyer_session,
					allow_voided_inventory_rerelease,
					created_at,
					updated_at
				)
				VALUES (
					$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,
					clock_timestamp(),
					clock_timestamp()
				)
				ON CONFLICT (event_id)
				DO UPDATE SET
					hold_duration_seconds = EXCLUDED.hold_duration_seconds,
					checkout_protection_seconds = EXCLUDED.checkout_protection_seconds,
					payment_retry_seconds = EXCLUDED.payment_retry_seconds,
					reconciliation_seconds = EXCLUDED.reconciliation_seconds,
					max_reservation_lifetime_seconds = EXCLUDED.max_reservation_lifetime_seconds,
					max_hold_quantity = EXCLUDED.max_hold_quantity,
					max_active_reservations_per_partner = EXCLUDED.max_active_reservations_per_partner,
					max_active_reservations_per_buyer_session = EXCLUDED.max_active_reservations_per_buyer_session,
					allow_voided_inventory_rerelease = EXCLUDED.allow_voided_inventory_rerelease,
					updated_at = clock_timestamp()
			`,
			eventID,
			input.HoldDurationSeconds,
			input.CheckoutProtectionSeconds,
			input.PaymentRetrySeconds,
			input.ReconciliationSeconds,
			input.MaxReservationLifetimeSeconds,
			input.MaxHoldQuantity,
			input.MaxActiveReservationsPerPartner,
			input.MaxActiveReservationsPerBuyerSession,
			input.AllowVoidedInventoryRerelease,
		)
		if err != nil {
			return err
		}

		if _, err := s.audit.Append(
			ctx,
			tx,
			audit.Event{
				EventID:     &eventID,
				ActorKind:   audit.ActorUser,
				ActorUserID: &actorID,
				Operation:   "EVENT_TRANSACTION_POLICY_CONFIGURED",
				EntityType:  "EVENT",
				EntityID:    &eventID,
			},
		); err != nil {
			return err
		}

		_, err = s.outbox.Append(
			ctx,
			tx,
			outbox.Fact{
				EventID:       &eventID,
				FactType:      "event.transaction_policy_configured",
				AggregateType: "EVENT",
				AggregateID:   &eventID,
			},
		)

		return err
	})
}

func (s *Service) CreatePriceTier(
	ctx context.Context,
	actorID uuid.UUID,
	eventID uuid.UUID,
	input PriceTierInput,
) (uuid.UUID, error) {
	input.Code = strings.TrimSpace(input.Code)
	input.Name = strings.TrimSpace(input.Name)
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))

	if input.Code == "" || input.Name == "" {
		return uuid.Nil, validation("price tier code and name are required")
	}

	if input.AmountMinor < 0 {
		return uuid.Nil, validation("price cannot be negative")
	}

	if !currencyPattern.MatchString(input.Currency) {
		return uuid.Nil, validation("currency must be a three-letter uppercase code")
	}

	id := uuid.New()

	err := s.transactions.Run(ctx, func(tx pgx.Tx) error {
		var state string

		if err := tx.QueryRow(
			ctx,
			`SELECT state FROM events WHERE id = $1 FOR UPDATE`,
			eventID,
		).Scan(&state); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notFound("event")
			}
			return err
		}

		if state != "DRAFT" && state != "ON_SALE" && state != "PAUSED" {
			return validation("Event state does not permit pricing mutation")
		}

		_, err := tx.Exec(
			ctx,
			`
				INSERT INTO event_price_tiers (
					id,
					event_id,
					code,
					name,
					amount_minor,
					currency,
					state,
					created_at,
					updated_at
				)
				VALUES (
					$1,$2,$3,$4,$5,$6,'ACTIVE',
					clock_timestamp(),
					clock_timestamp()
				)
			`,
			id,
			eventID,
			input.Code,
			input.Name,
			input.AmountMinor,
			input.Currency,
		)
		if err != nil {
			return err
		}

		if _, err := s.audit.Append(
			ctx,
			tx,
			audit.Event{
				EventID:     &eventID,
				ActorKind:   audit.ActorUser,
				ActorUserID: &actorID,
				Operation:   "EVENT_PRICE_TIER_CREATED",
				EntityType:  "EVENT_PRICE_TIER",
				EntityID:    &id,
				NewState: map[string]any{
					"amount_minor": input.AmountMinor,
					"currency":     input.Currency,
				},
			},
		); err != nil {
			return err
		}

		_, err = s.outbox.Append(
			ctx,
			tx,
			outbox.Fact{
				EventID:       &eventID,
				FactType:      "event.pricing_changed",
				AggregateType: "EVENT",
				AggregateID:   &eventID,
			},
		)

		return err
	})

	if err != nil {
		return uuid.Nil, err
	}

	return id, nil
}

func (s *Service) AssignPricing(
	ctx context.Context,
	actorID uuid.UUID,
	eventID uuid.UUID,
	input PricingAssignmentInput,
) error {
	if input.PriceTierID == uuid.Nil {
		return validation("price tier is required")
	}

	sections := uniqueStrings(input.SectionObjectKeys)
	reserved := uniqueStrings(input.ReservedObjectKeys)
	gaPools := uniqueStrings(input.GAPoolObjectKeys)

	if len(sections)+len(reserved)+len(gaPools) == 0 {
		return validation("at least one pricing target is required")
	}

	return s.transactions.Run(ctx, func(tx pgx.Tx) error {
		var state string

		if err := tx.QueryRow(
			ctx,
			`SELECT state FROM events WHERE id = $1 FOR UPDATE`,
			eventID,
		).Scan(&state); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notFound("event")
			}
			return err
		}

		if state != "DRAFT" && state != "ON_SALE" && state != "PAUSED" {
			return validation("Event state does not permit pricing mutation")
		}

		var tierActive bool
		if err := tx.QueryRow(
			ctx,
			`
				SELECT EXISTS (
					SELECT 1
					FROM event_price_tiers
					WHERE id = $1
					  AND event_id = $2
					  AND state = 'ACTIVE'
				)
			`,
			input.PriceTierID,
			eventID,
		).Scan(&tierActive); err != nil {
			return err
		}

		if !tierActive {
			return validation("price tier is not active for this Event")
		}

		if len(sections) != 0 {
			result, err := tx.Exec(
				ctx,
				`
					UPDATE event_sections
					SET default_price_tier_id = $1
					WHERE event_id = $2
					  AND snapshot_object_key = ANY($3::text[])
				`,
				input.PriceTierID,
				eventID,
				sections,
			)
			if err != nil {
				return err
			}

			if result.RowsAffected() != int64(len(sections)) {
				return validation("one or more section pricing targets do not exist")
			}
		}

		if len(reserved) != 0 {
			result, err := tx.Exec(
				ctx,
				`
					UPDATE reserved_inventory_units
					SET price_tier_override_id = $1
					WHERE event_id = $2
					  AND snapshot_object_key = ANY($3::text[])
				`,
				input.PriceTierID,
				eventID,
				reserved,
			)
			if err != nil {
				return err
			}

			if result.RowsAffected() != int64(len(reserved)) {
				return validation("one or more reserved pricing targets do not exist")
			}
		}

		if len(gaPools) != 0 {
			result, err := tx.Exec(
				ctx,
				`
					UPDATE ga_inventory_pools
					SET price_tier_id = $1
					WHERE event_id = $2
					  AND snapshot_object_key = ANY($3::text[])
				`,
				input.PriceTierID,
				eventID,
				gaPools,
			)
			if err != nil {
				return err
			}

			if result.RowsAffected() != int64(len(gaPools)) {
				return validation("one or more GA pricing targets do not exist")
			}
		}

		if _, err := s.audit.Append(
			ctx,
			tx,
			audit.Event{
				EventID:     &eventID,
				ActorKind:   audit.ActorUser,
				ActorUserID: &actorID,
				Operation:   "EVENT_PRICING_ASSIGNED",
				EntityType:  "EVENT",
				EntityID:    &eventID,
				Metadata: map[string]any{
					"price_tier_id": input.PriceTierID.String(),
					"sections":      sections,
					"reserved":      reserved,
					"ga_pools":      gaPools,
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
				FactType:      "event.pricing_changed",
				AggregateType: "EVENT",
				AggregateID:   &eventID,
			},
		)

		return err
	})
}

func (s *Service) OpenSales(
	ctx context.Context,
	actorID uuid.UUID,
	eventID uuid.UUID,
) error {
	if actorID == uuid.Nil || eventID == uuid.Nil {
		return validation("actor and Event are required")
	}

	return s.transactions.Run(ctx, func(tx pgx.Tx) error {
		var state string

		if err := tx.QueryRow(
			ctx,
			`SELECT state FROM events WHERE id = $1 FOR UPDATE`,
			eventID,
		).Scan(&state); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notFound("event")
			}
			return err
		}

		if state != "DRAFT" {
			return validation("OpenSales requires Event state DRAFT")
		}

		var snapshotReady bool
		if err := tx.QueryRow(
			ctx,
			`
				SELECT EXISTS (
					SELECT 1
					FROM event_layout_snapshots
					WHERE event_id = $1
					  AND finalized_at IS NOT NULL
				)
			`,
			eventID,
		).Scan(&snapshotReady); err != nil {
			return err
		}

		if !snapshotReady {
			return validation("OpenSales requires a finalized Event layout snapshot")
		}

		var inventoryCount int
		if err := tx.QueryRow(
			ctx,
			`
				SELECT
					(SELECT COUNT(*) FROM reserved_inventory_units WHERE event_id = $1)
					+
					(SELECT COUNT(*) FROM ga_inventory_pools WHERE event_id = $1)
			`,
			eventID,
		).Scan(&inventoryCount); err != nil {
			return err
		}

		if inventoryCount == 0 {
			return validation("OpenSales requires materialized inventory")
		}

		var policyReady bool
		if err := tx.QueryRow(
			ctx,
			`
				SELECT EXISTS (
					SELECT 1
					FROM event_transaction_policies
					WHERE event_id = $1
				)
			`,
			eventID,
		).Scan(&policyReady); err != nil {
			return err
		}

		if !policyReady {
			return validation("OpenSales requires transaction policy configuration")
		}

		var unpricedReserved int
		if err := tx.QueryRow(
			ctx,
			`
				SELECT COUNT(*)
				FROM reserved_inventory_units riu
				JOIN event_sections es
				  ON es.id = riu.event_section_id
				LEFT JOIN event_price_tiers pt
				  ON pt.id = COALESCE(
				      riu.price_tier_override_id,
				      es.default_price_tier_id
				  )
				 AND pt.event_id = riu.event_id
				WHERE riu.event_id = $1
				  AND (
				      pt.id IS NULL
				      OR pt.state <> 'ACTIVE'
				  )
			`,
			eventID,
		).Scan(&unpricedReserved); err != nil {
			return err
		}

		if unpricedReserved != 0 {
			return validation("all reserved inventory requires active pricing")
		}

		var unpricedGA int
		if err := tx.QueryRow(
			ctx,
			`
				SELECT COUNT(*)
				FROM ga_inventory_pools gp
				LEFT JOIN event_price_tiers pt
				  ON pt.id = gp.price_tier_id
				 AND pt.event_id = gp.event_id
				WHERE gp.event_id = $1
				  AND (
				      pt.id IS NULL
				      OR pt.state <> 'ACTIVE'
				  )
			`,
			eventID,
		).Scan(&unpricedGA); err != nil {
			return err
		}

		if unpricedGA != 0 {
			return validation("all GA inventory requires active pricing")
		}

		if _, err := tx.Exec(
			ctx,
			`
				UPDATE events
				SET
					state = 'ON_SALE',
					updated_at = clock_timestamp()
				WHERE id = $1
			`,
			eventID,
		); err != nil {
			return err
		}

		if _, err := s.audit.Append(
			ctx,
			tx,
			audit.Event{
				EventID:     &eventID,
				ActorKind:   audit.ActorUser,
				ActorUserID: &actorID,
				Operation:   "EVENT_SALES_OPENED",
				EntityType:  "EVENT",
				EntityID:    &eventID,
				PreviousState: map[string]any{
					"state": "DRAFT",
				},
				NewState: map[string]any{
					"state": "ON_SALE",
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
				FactType:      "event.opened_for_sale",
				AggregateType: "EVENT",
				AggregateID:   &eventID,
				Payload: map[string]any{
					"event_id": eventID.String(),
				},
			},
		)

		return err
	})
}

func (s *Service) PauseSales(ctx context.Context, actorID, eventID uuid.UUID) error {
	return s.transitionLifecycle(ctx, actorID, eventID, []string{"ON_SALE"}, "PAUSED", "EVENT_SALES_PAUSED", "event.sales_paused", "")
}

func (s *Service) ResumeSales(ctx context.Context, actorID, eventID uuid.UUID) error {
	return s.transitionLifecycle(ctx, actorID, eventID, []string{"PAUSED"}, "ON_SALE", "EVENT_SALES_RESUMED", "event.sales_resumed", "")
}

func (s *Service) CloseSales(ctx context.Context, actorID, eventID uuid.UUID) error {
	return s.transitionLifecycle(ctx, actorID, eventID, []string{"ON_SALE", "PAUSED"}, "SALES_CLOSED", "EVENT_SALES_CLOSED", "event.sales_closed", "")
}

func (s *Service) CompleteEvent(ctx context.Context, actorID, eventID uuid.UUID) error {
	return s.transitionLifecycle(ctx, actorID, eventID, []string{"SALES_CLOSED"}, "COMPLETED", "EVENT_COMPLETED", "event.completed", "")
}

func (s *Service) CancelEvent(ctx context.Context, actorID, eventID uuid.UUID, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return validation("CancelEvent requires a reason")
	}
	return s.transitionLifecycle(ctx, actorID, eventID, []string{"DRAFT", "ON_SALE", "PAUSED", "SALES_CLOSED"}, "CANCELLED", "EVENT_CANCELLED", "event.cancelled", reason)
}

func (s *Service) transitionLifecycle(
	ctx context.Context,
	actorID uuid.UUID,
	eventID uuid.UUID,
	allowedStates []string,
	nextState string,
	operation string,
	factType string,
	reason string,
) error {
	if actorID == uuid.Nil || eventID == uuid.Nil {
		return validation("actor and Event are required")
	}

	return s.transactions.Run(ctx, func(tx pgx.Tx) error {
		var previousState string
		if err := tx.QueryRow(ctx, `SELECT state FROM events WHERE id = $1 FOR UPDATE`, eventID).Scan(&previousState); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notFound("event")
			}
			return err
		}

		allowed := false
		for _, state := range allowedStates {
			if previousState == state {
				allowed = true
				break
			}
		}
		if !allowed {
			return validation(fmt.Sprintf("cannot transition Event from %s to %s", previousState, nextState))
		}

		if _, err := tx.Exec(ctx, `
			UPDATE events
			SET state = $2,
				cancelled_at = CASE WHEN $2 = 'CANCELLED' THEN clock_timestamp() ELSE cancelled_at END,
				completed_at = CASE WHEN $2 = 'COMPLETED' THEN clock_timestamp() ELSE completed_at END,
				updated_at = clock_timestamp()
			WHERE id = $1
		`, eventID, nextState); err != nil {
			return err
		}

		metadata := map[string]any{}
		if reason != "" {
			metadata["reason"] = reason
		}
		if _, err := s.audit.Append(ctx, tx, audit.Event{
			EventID:       &eventID,
			ActorKind:     audit.ActorUser,
			ActorUserID:   &actorID,
			Operation:     operation,
			EntityType:    "EVENT",
			EntityID:      &eventID,
			PreviousState: map[string]any{"state": previousState},
			NewState:      map[string]any{"state": nextState},
			Reason:        reason,
			Metadata:      metadata,
		}); err != nil {
			return err
		}

		payload := map[string]any{"event_id": eventID.String(), "state": nextState}
		if reason != "" {
			payload["reason"] = reason
		}
		_, err := s.outbox.Append(ctx, tx, outbox.Fact{
			EventID:       &eventID,
			FactType:      factType,
			AggregateType: "EVENT",
			AggregateID:   &eventID,
			Payload:       payload,
		})
		return err
	})
}

func validatePolicy(input TransactionPolicyInput) error {
	if input.HoldDurationSeconds <= 0 {
		return validation("hold duration must be positive")
	}

	if input.CheckoutProtectionSeconds <= 0 {
		return validation("checkout protection duration must be positive")
	}

	if input.PaymentRetrySeconds < 0 {
		return validation("payment retry duration cannot be negative")
	}

	if input.ReconciliationSeconds <= 0 {
		return validation("reconciliation duration must be positive")
	}

	if input.MaxReservationLifetimeSeconds < input.HoldDurationSeconds {
		return validation("maximum Reservation lifetime cannot be shorter than hold duration")
	}

	if input.MaxHoldQuantity <= 0 ||
		input.MaxActiveReservationsPerPartner <= 0 ||
		input.MaxActiveReservationsPerBuyerSession <= 0 {
		return validation("Reservation limits must be positive")
	}

	return nil
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))

	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}

		if _, exists := seen[value]; exists {
			continue
		}

		seen[value] = struct{}{}
		out = append(out, value)
	}

	return out
}

func validation(message string) error {
	return apierror.New(apierror.CodeValidation, message)
}

func notFound(resource string) error {
	return apierror.New(
		apierror.CodeResourceNotFound,
		fmt.Sprintf("%s not found", resource),
	)
}
