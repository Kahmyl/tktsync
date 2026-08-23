package admission

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/outbox"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

type Reversal struct {
	AdmissionID uuid.UUID
	TicketID    uuid.UUID
	ReversedAt  time.Time
	Reason      string
}

func (s *Service) Reverse(ctx context.Context, actorID, admissionID uuid.UUID, reason string) (Reversal, error) {
	reason = strings.TrimSpace(reason)
	if actorID == uuid.Nil || admissionID == uuid.Nil || reason == "" {
		return Reversal{}, apierror.New(apierror.CodeValidation, "Actor, Admission, and reason are required")
	}
	var result Reversal
	err := s.transactions.Run(ctx, func(tx pgx.Tx) error {
		var eventID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT event_id FROM admissions WHERE id=$1`, admissionID).Scan(&eventID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apierror.New(apierror.CodeResourceNotFound, "Admission not found")
			}
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT id FROM events WHERE id=$1 FOR KEY SHARE`, eventID); err != nil {
			return err
		}
		if err := requireAdmissionSupervisor(ctx, tx, actorID, eventID); err != nil {
			return err
		}
		var ticketID uuid.UUID
		var status string
		if err := tx.QueryRow(ctx, `SELECT ticket_entitlement_id,status FROM admissions WHERE id=$1 FOR UPDATE`, admissionID).Scan(&ticketID, &status); err != nil {
			return err
		}
		if status != "ACTIVE" {
			return apierror.New(apierror.CodeTicketInvalid, "Admission is not active")
		}
		var now time.Time
		if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE admissions SET status='REVERSED',reversed_at=$2,reversal_reason=$3,reversed_by_user_id=$4 WHERE id=$1 AND status='ACTIVE'`, admissionID, now, reason, actorID); err != nil {
			return err
		}
		if _, err := s.audit.Append(ctx, tx, audit.Event{EventID: &eventID, ActorKind: audit.ActorUser, ActorUserID: &actorID, Operation: "ADMISSION_REVERSED", EntityType: "ADMISSION", EntityID: &admissionID, TicketEntitlementID: &ticketID, PreviousState: map[string]any{"status": "ACTIVE"}, NewState: map[string]any{"status": "REVERSED"}, Reason: reason}); err != nil {
			return err
		}
		if _, err := s.outbox.Append(ctx, tx, outbox.Fact{EventID: &eventID, FactType: "admission.reversed", AggregateType: "ADMISSION", AggregateID: &admissionID, Payload: map[string]any{"ticket_id": ticketID}}); err != nil {
			return err
		}
		result = Reversal{AdmissionID: admissionID, TicketID: ticketID, ReversedAt: now, Reason: reason}
		return nil
	})
	if err != nil {
		return Reversal{}, err
	}
	return result, nil
}
