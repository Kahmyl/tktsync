package venue

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/outbox"
)

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
