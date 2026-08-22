package partner

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/outbox"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
)

func mustJSON(value any) []byte { raw, _ := json.Marshal(value); return raw }

type Service struct {
	transactions *database.Runner
	audit        audit.Store
	outbox       outbox.Store
}

func (s *Service) SetAllowedReturnURLs(ctx context.Context, actorID, partnerID uuid.UUID, values []string) ([]string, error) {
	if actorID == uuid.Nil || partnerID == uuid.Nil {
		return nil, validation("actor and Partner are required")
	}
	unique := map[string]struct{}{}
	for _, raw := range values {
		parsed, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
			return nil, validation("allowed return URLs must be absolute HTTPS URLs without credentials or fragments")
		}
		unique[parsed.String()] = struct{}{}
	}
	normalized := make([]string, 0, len(unique))
	for value := range unique {
		normalized = append(normalized, value)
	}
	sort.Strings(normalized)
	err := s.transactions.Run(ctx, func(tx pgx.Tx) error {
		var state string
		if err := tx.QueryRow(ctx, `SELECT state FROM partners WHERE id=$1 FOR UPDATE`, partnerID).Scan(&state); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notFound("Partner")
			}
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE partners SET metadata=jsonb_set(metadata,'{allowed_return_urls}',$2::jsonb,true) WHERE id=$1`, partnerID, mustJSON(normalized)); err != nil {
			return err
		}
		if _, err := s.audit.Append(ctx, tx, audit.Event{PartnerID: &partnerID, ActorKind: audit.ActorUser, ActorUserID: &actorID, Operation: "PARTNER_RETURN_URLS_UPDATED", EntityType: "PARTNER", EntityID: &partnerID, NewState: map[string]any{"allowed_return_urls": normalized}}); err != nil {
			return err
		}
		_, err := s.outbox.Append(ctx, tx, outbox.Fact{FactType: "partner.return_urls_updated", AggregateType: "PARTNER", AggregateID: &partnerID, Payload: map[string]any{"allowed_return_urls": normalized}})
		return err
	})
	return normalized, err
}

func NewService(transactions *database.Runner) *Service {
	return &Service{
		transactions: transactions,
	}
}

func (s *Service) Create(
	ctx context.Context,
	actorID uuid.UUID,
	name string,
) (uuid.UUID, error) {
	name = strings.TrimSpace(name)

	if actorID == uuid.Nil || name == "" {
		return uuid.Nil, validation("actor and Partner name are required")
	}

	id := uuid.New()

	err := s.transactions.Run(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(
			ctx,
			`
				INSERT INTO partners (
					id,
					name,
					state,
					metadata,
					created_at
				)
				VALUES (
					$1,$2,'ACTIVE','{}'::jsonb,clock_timestamp()
				)
			`,
			id,
			name,
		)
		if err != nil {
			return err
		}

		if _, err := s.audit.Append(
			ctx,
			tx,
			audit.Event{
				PartnerID:   &id,
				ActorKind:   audit.ActorUser,
				ActorUserID: &actorID,
				Operation:   "PARTNER_CREATED",
				EntityType:  "PARTNER",
				EntityID:    &id,
				NewState: map[string]any{
					"state": "ACTIVE",
				},
			},
		); err != nil {
			return err
		}

		_, err = s.outbox.Append(
			ctx,
			tx,
			outbox.Fact{
				FactType:      "partner.created",
				AggregateType: "PARTNER",
				AggregateID:   &id,
				Payload: map[string]any{
					"partner_id": id.String(),
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

func (s *Service) CreateCredential(
	ctx context.Context,
	actorID uuid.UUID,
	partnerID uuid.UUID,
) (uuid.UUID, string, error) {
	if actorID == uuid.Nil || partnerID == uuid.Nil {
		return uuid.Nil, "", validation("actor and Partner are required")
	}

	generated, err := auth.GeneratePartnerCredential()
	if err != nil {
		return uuid.Nil, "", err
	}

	credentialID := uuid.New()

	err = s.transactions.Run(ctx, func(tx pgx.Tx) error {
		var state string

		if err := tx.QueryRow(
			ctx,
			`SELECT state FROM partners WHERE id = $1 FOR UPDATE`,
			partnerID,
		).Scan(&state); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notFound("Partner")
			}
			return err
		}

		_, err := tx.Exec(
			ctx,
			`
				INSERT INTO partner_credentials (
					id,
					partner_id,
					key_id,
					secret_hash,
					state,
					created_at
				)
				VALUES (
					$1,$2,$3,$4,'ACTIVE',clock_timestamp()
				)
			`,
			credentialID,
			partnerID,
			generated.KeyID,
			generated.SecretHash,
		)
		if err != nil {
			return err
		}

		if _, err := s.audit.Append(
			ctx,
			tx,
			audit.Event{
				PartnerID:   &partnerID,
				ActorKind:   audit.ActorUser,
				ActorUserID: &actorID,
				Operation:   "PARTNER_CREDENTIAL_CREATED",
				EntityType:  "PARTNER_CREDENTIAL",
				EntityID:    &credentialID,
				Metadata: map[string]any{
					"key_id": generated.KeyID,
				},
			},
		); err != nil {
			return err
		}

		_, err = s.outbox.Append(
			ctx,
			tx,
			outbox.Fact{
				FactType:      "partner.credential_created",
				AggregateType: "PARTNER",
				AggregateID:   &partnerID,
				Payload: map[string]any{
					"credential_id": credentialID.String(),
					"key_id":        generated.KeyID,
				},
			},
		)

		return err
	})

	if err != nil {
		return uuid.Nil, "", err
	}

	return credentialID, generated.Encoded, nil
}

func (s *Service) RevokeCredential(
	ctx context.Context,
	actorID uuid.UUID,
	credentialID uuid.UUID,
) error {
	return s.transactions.Run(ctx, func(tx pgx.Tx) error {
		var (
			partnerID uuid.UUID
			state     string
		)

		if err := tx.QueryRow(
			ctx,
			`
				SELECT partner_id, state
				FROM partner_credentials
				WHERE id = $1
				FOR UPDATE
			`,
			credentialID,
		).Scan(&partnerID, &state); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notFound("Partner credential")
			}
			return err
		}

		if state == "REVOKED" {
			return nil
		}

		if _, err := tx.Exec(
			ctx,
			`
				UPDATE partner_credentials
				SET
					state = 'REVOKED',
					revoked_at = clock_timestamp()
				WHERE id = $1
			`,
			credentialID,
		); err != nil {
			return err
		}

		if _, err := s.audit.Append(
			ctx,
			tx,
			audit.Event{
				PartnerID:   &partnerID,
				ActorKind:   audit.ActorUser,
				ActorUserID: &actorID,
				Operation:   "PARTNER_CREDENTIAL_REVOKED",
				EntityType:  "PARTNER_CREDENTIAL",
				EntityID:    &credentialID,
			},
		); err != nil {
			return err
		}

		_, err := s.outbox.Append(
			ctx,
			tx,
			outbox.Fact{
				FactType:      "partner.credential_revoked",
				AggregateType: "PARTNER",
				AggregateID:   &partnerID,
			},
		)

		return err
	})
}

func (s *Service) SetEnabled(
	ctx context.Context,
	actorID uuid.UUID,
	partnerID uuid.UUID,
	enabled bool,
) error {
	return s.transactions.Run(ctx, func(tx pgx.Tx) error {
		var previous string

		if err := tx.QueryRow(
			ctx,
			`SELECT state FROM partners WHERE id = $1 FOR UPDATE`,
			partnerID,
		).Scan(&previous); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notFound("Partner")
			}
			return err
		}

		next := "DISABLED"
		fact := "partner.disabled"

		if enabled {
			next = "ACTIVE"
			fact = "partner.enabled"
		}

		if previous == next {
			return nil
		}

		if enabled {
			if _, err := tx.Exec(
				ctx,
				`
					UPDATE partners
					SET
						state = 'ACTIVE',
						disabled_at = NULL
					WHERE id = $1
				`,
				partnerID,
			); err != nil {
				return err
			}
		} else {
			if _, err := tx.Exec(
				ctx,
				`
					UPDATE partners
					SET
						state = 'DISABLED',
						disabled_at = clock_timestamp()
					WHERE id = $1
				`,
				partnerID,
			); err != nil {
				return err
			}
		}

		if _, err := s.audit.Append(
			ctx,
			tx,
			audit.Event{
				PartnerID:   &partnerID,
				ActorKind:   audit.ActorUser,
				ActorUserID: &actorID,
				Operation:   "PARTNER_STATE_CHANGED",
				EntityType:  "PARTNER",
				EntityID:    &partnerID,
				PreviousState: map[string]any{
					"state": previous,
				},
				NewState: map[string]any{
					"state": next,
				},
			},
		); err != nil {
			return err
		}

		_, err := s.outbox.Append(
			ctx,
			tx,
			outbox.Fact{
				FactType:      fact,
				AggregateType: "PARTNER",
				AggregateID:   &partnerID,
			},
		)

		return err
	})
}

func (s *Service) GrantEventAccess(
	ctx context.Context,
	actorID uuid.UUID,
	eventID uuid.UUID,
	partnerID uuid.UUID,
) error {
	return s.transactions.Run(ctx, func(tx pgx.Tx) error {
		var lockedEvent uuid.UUID

		if err := tx.QueryRow(
			ctx,
			`SELECT id FROM events WHERE id = $1 FOR KEY SHARE`,
			eventID,
		).Scan(&lockedEvent); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notFound("Event")
			}
			return err
		}

		var partnerState string
		if err := tx.QueryRow(
			ctx,
			`SELECT state FROM partners WHERE id = $1 FOR UPDATE`,
			partnerID,
		).Scan(&partnerState); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notFound("Partner")
			}
			return err
		}

		_, err := tx.Exec(
			ctx,
			`
				INSERT INTO partner_event_access (
					id,
					partner_id,
					event_id,
					state,
					created_at,
					disabled_at
				)
				VALUES (
					gen_random_uuid(),
					$1,
					$2,
					'ACTIVE',
					clock_timestamp(),
					NULL
				)
				ON CONFLICT (partner_id, event_id)
				DO UPDATE SET
					state = 'ACTIVE',
					disabled_at = NULL
			`,
			partnerID,
			eventID,
		)
		if err != nil {
			return err
		}

		if _, err := s.audit.Append(
			ctx,
			tx,
			audit.Event{
				EventID:     &eventID,
				PartnerID:   &partnerID,
				ActorKind:   audit.ActorUser,
				ActorUserID: &actorID,
				Operation:   "PARTNER_EVENT_ACCESS_ENABLED",
				EntityType:  "PARTNER",
				EntityID:    &partnerID,
			},
		); err != nil {
			return err
		}

		_, err = s.outbox.Append(
			ctx,
			tx,
			outbox.Fact{
				EventID:       &eventID,
				FactType:      "partner.event_access_enabled",
				AggregateType: "PARTNER",
				AggregateID:   &partnerID,
			},
		)

		return err
	})
}

func (s *Service) DisableEventAccess(
	ctx context.Context,
	actorID uuid.UUID,
	eventID uuid.UUID,
	partnerID uuid.UUID,
) error {
	return s.transactions.Run(ctx, func(tx pgx.Tx) error {
		var lockedEvent uuid.UUID

		if err := tx.QueryRow(
			ctx,
			`SELECT id FROM events WHERE id = $1 FOR KEY SHARE`,
			eventID,
		).Scan(&lockedEvent); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notFound("Event")
			}
			return err
		}

		var lockedPartner uuid.UUID

		if err := tx.QueryRow(
			ctx,
			`SELECT id FROM partners WHERE id = $1 FOR UPDATE`,
			partnerID,
		).Scan(&lockedPartner); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notFound("Partner")
			}
			return err
		}

		result, err := tx.Exec(
			ctx,
			`
				UPDATE partner_event_access
				SET
					state = 'DISABLED',
					disabled_at = clock_timestamp()
				WHERE partner_id = $1
				  AND event_id = $2
			`,
			partnerID,
			eventID,
		)
		if err != nil {
			return err
		}

		if result.RowsAffected() != 1 {
			return notFound("Partner Event access")
		}

		if _, err := s.audit.Append(
			ctx,
			tx,
			audit.Event{
				EventID:     &eventID,
				PartnerID:   &partnerID,
				ActorKind:   audit.ActorUser,
				ActorUserID: &actorID,
				Operation:   "PARTNER_EVENT_ACCESS_DISABLED",
				EntityType:  "PARTNER",
				EntityID:    &partnerID,
			},
		); err != nil {
			return err
		}

		_, err = s.outbox.Append(
			ctx,
			tx,
			outbox.Fact{
				EventID:       &eventID,
				FactType:      "partner.event_access_disabled",
				AggregateType: "PARTNER",
				AggregateID:   &partnerID,
			},
		)

		return err
	})
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
