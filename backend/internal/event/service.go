package event

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/outbox"
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
