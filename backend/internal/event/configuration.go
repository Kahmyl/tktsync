package event

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/outbox"
)

type UpdateConfigurationInput struct {
	Name             *string
	StartsAt         *time.Time
	EndsAt           *time.Time
	SalesOpenAt      *time.Time
	SalesCloseAt     *time.Time
	AdmissionOpenAt  *time.Time
	AdmissionCloseAt *time.Time
	TimezoneName     *string
}

type UpdatePriceTierInput struct {
	Name        *string
	AmountMinor *int64
	State       *string
}

func (s *Service) UpdateConfiguration(
	ctx context.Context,
	actorID uuid.UUID,
	eventID uuid.UUID,
	input UpdateConfigurationInput,
) error {
	if actorID == uuid.Nil || eventID == uuid.Nil {
		return validation("actor and Event are required")
	}

	if input.Name == nil &&
		input.StartsAt == nil &&
		input.EndsAt == nil &&
		input.SalesOpenAt == nil &&
		input.SalesCloseAt == nil &&
		input.AdmissionOpenAt == nil &&
		input.AdmissionCloseAt == nil &&
		input.TimezoneName == nil {
		return validation("at least one Event configuration field is required")
	}

	if input.Name != nil {
		value := strings.TrimSpace(*input.Name)
		if value == "" {
			return validation("Event name cannot be empty")
		}
		input.Name = &value
	}

	if input.TimezoneName != nil {
		value := strings.TrimSpace(*input.TimezoneName)
		if value == "" {
			return validation("timezone name cannot be empty")
		}
		input.TimezoneName = &value
	}

	return s.transactions.Run(ctx, func(tx pgx.Tx) error {
		var state string

		if err := tx.QueryRow(
			ctx,
			`
				SELECT state
				FROM events
				WHERE id = $1
				FOR UPDATE
			`,
			eventID,
		).Scan(&state); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notFound("event")
			}
			return err
		}

		if state != "DRAFT" {
			return validation(
				"Event configuration fields are editable only while DRAFT",
			)
		}

		_, err := tx.Exec(
			ctx,
			`
				UPDATE events
				SET
					name = COALESCE($2, name),
					starts_at = COALESCE($3, starts_at),
					ends_at = COALESCE($4, ends_at),
					sales_open_at = COALESCE($5, sales_open_at),
					sales_close_at = COALESCE($6, sales_close_at),
					admission_open_at = COALESCE($7, admission_open_at),
					admission_close_at = COALESCE($8, admission_close_at),
					timezone_name = COALESCE($9, timezone_name),
					updated_at = clock_timestamp()
				WHERE id = $1
			`,
			eventID,
			input.Name,
			input.StartsAt,
			input.EndsAt,
			input.SalesOpenAt,
			input.SalesCloseAt,
			input.AdmissionOpenAt,
			input.AdmissionCloseAt,
			input.TimezoneName,
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
				Operation:   "EVENT_CONFIGURATION_UPDATED",
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
				FactType:      "event.configuration_updated",
				AggregateType: "EVENT",
				AggregateID:   &eventID,
			},
		)

		return err
	})
}

func (s *Service) UpdatePriceTier(
	ctx context.Context,
	actorID uuid.UUID,
	eventID uuid.UUID,
	priceTierID uuid.UUID,
	input UpdatePriceTierInput,
) error {
	if actorID == uuid.Nil ||
		eventID == uuid.Nil ||
		priceTierID == uuid.Nil {
		return validation(
			"actor, Event and price tier are required",
		)
	}

	if input.Name == nil &&
		input.AmountMinor == nil &&
		input.State == nil {
		return validation(
			"at least one price tier field is required",
		)
	}

	if input.Name != nil {
		value := strings.TrimSpace(*input.Name)
		if value == "" {
			return validation("price tier name cannot be empty")
		}
		input.Name = &value
	}

	if input.AmountMinor != nil && *input.AmountMinor < 0 {
		return validation("price cannot be negative")
	}

	if input.State != nil {
		value := strings.ToUpper(
			strings.TrimSpace(*input.State),
		)

		if value != "ACTIVE" && value != "RETIRED" {
			return validation(
				"price tier state must be ACTIVE or RETIRED",
			)
		}

		input.State = &value
	}

	return s.transactions.Run(ctx, func(tx pgx.Tx) error {
		var eventState string

		if err := tx.QueryRow(
			ctx,
			`
				SELECT state
				FROM events
				WHERE id = $1
				FOR UPDATE
			`,
			eventID,
		).Scan(&eventState); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notFound("event")
			}
			return err
		}

		switch eventState {
		case "DRAFT", "ON_SALE", "PAUSED":
		default:
			return validation(
				"Event state does not permit pricing mutation",
			)
		}

		var existingState string

		if err := tx.QueryRow(
			ctx,
			`
				SELECT state
				FROM event_price_tiers
				WHERE id = $1
				  AND event_id = $2
				FOR UPDATE
			`,
			priceTierID,
			eventID,
		).Scan(&existingState); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notFound("price tier")
			}
			return err
		}

		if input.State != nil &&
			*input.State == "RETIRED" &&
			existingState != "RETIRED" {
			var referenceCount int

			if err := tx.QueryRow(
				ctx,
				`
					SELECT
						(
							SELECT COUNT(*)
							FROM event_sections
							WHERE event_id = $1
							  AND default_price_tier_id = $2
						)
						+
						(
							SELECT COUNT(*)
							FROM reserved_inventory_units
							WHERE event_id = $1
							  AND price_tier_override_id = $2
						)
						+
						(
							SELECT COUNT(*)
							FROM ga_inventory_pools
							WHERE event_id = $1
							  AND price_tier_id = $2
						)
				`,
				eventID,
				priceTierID,
			).Scan(&referenceCount); err != nil {
				return err
			}

			if eventState != "DRAFT" &&
				referenceCount > 0 {
				return validation(
					"cannot retire a price tier still assigned to live inventory",
				)
			}
		}

		_, err := tx.Exec(
			ctx,
			`
				UPDATE event_price_tiers
				SET
					name = COALESCE($3, name),
					amount_minor = COALESCE($4, amount_minor),
					state = COALESCE($5, state),
					updated_at = clock_timestamp()
				WHERE id = $1
				  AND event_id = $2
			`,
			priceTierID,
			eventID,
			input.Name,
			input.AmountMinor,
			input.State,
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
				Operation:   "EVENT_PRICE_TIER_UPDATED",
				EntityType:  "EVENT_PRICE_TIER",
				EntityID:    &priceTierID,
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
}
