package admission

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/idempotency"
	"github.com/tktsync/tktsync/backend/internal/outbox"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

type ManualOverrideInput struct {
	EventID       uuid.UUID
	TicketID      uuid.UUID
	GateReference string
	Reason        string
}

func (s *Service) ManualOverride(ctx context.Context, actorID uuid.UUID, input ManualOverrideInput) (ScanResult, error) {
	input.GateReference = strings.TrimSpace(input.GateReference)
	input.Reason = strings.TrimSpace(input.Reason)

	operationID, ok := idempotency.OperationIDFromContext(ctx)
	if actorID == uuid.Nil ||
		input.EventID == uuid.Nil ||
		input.TicketID == uuid.Nil ||
		input.Reason == "" ||
		!ok {
		return ScanResult{},
			apierror.New(
				apierror.CodeValidation,
				"Actor, Event, Ticket, reason, and idempotency operation are required",
			)
	}

	var result ScanResult

	err := s.transactions.Run(
		ctx,
		func(tx pgx.Tx) error {
			var (
				eventState     string
				admissionOpen  *time.Time
				admissionClose *time.Time
			)

			if err := tx.QueryRow(
				ctx,
				`
					SELECT
						state,
						admission_open_at,
						admission_close_at
					FROM events
					WHERE id = $1
					FOR KEY SHARE
				`,
				input.EventID,
			).Scan(
				&eventState,
				&admissionOpen,
				&admissionClose,
			); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return apierror.New(
						apierror.CodeResourceNotFound,
						"Event not found",
					)
				}

				return err
			}

			if err := requireAdmissionSupervisor(
				ctx,
				tx,
				actorID,
				input.EventID,
			); err != nil {
				return err
			}

			var ticketState string

			if err := tx.QueryRow(
				ctx,
				`
					SELECT status
					FROM ticket_entitlements
					WHERE id = $1
					  AND event_id = $2
					FOR UPDATE
				`,
				input.TicketID,
				input.EventID,
			).Scan(
				&ticketState,
			); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return apierror.New(
						apierror.CodeResourceNotFound,
						"Ticket not found",
					)
				}

				return err
			}

			if eventState == "CANCELLED" {
				return apierror.New(
					apierror.CodeEventCancelled,
					"Event is cancelled",
				)
			}

			if ticketState != "ACTIVE" {
				return apierror.New(
					apierror.CodeTicketVoid,
					"Ticket is void",
				)
			}

			credentialState, err :=
				lockManualOverrideCredentialState(
					ctx,
					tx,
					input.TicketID,
				)
			if err != nil {
				return err
			}

			if credentialState != "ACTIVE" {
				switch credentialState {
				case "REVOKED":
					return apierror.New(
						apierror.CodeCredentialRevoked,
						"Ticket credential is revoked",
					)

				case "SUPERSEDED":
					return apierror.New(
						apierror.CodeCredentialSuperseded,
						"Ticket credential is superseded",
					)

				default:
					return apierror.New(
						apierror.CodeTicketInvalid,
						"Ticket has no active credential",
					)
				}
			}

			var existing uuid.UUID

			if err := tx.QueryRow(
				ctx,
				`
					SELECT id
					FROM admissions
					WHERE ticket_entitlement_id = $1
					  AND status = 'ACTIVE'
					FOR UPDATE
				`,
				input.TicketID,
			).Scan(
				&existing,
			); err == nil {
				return apierror.New(
					apierror.CodeTicketAlreadyAdmitted,
					"Ticket is already admitted",
				)
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}

			var now time.Time

			if err := tx.QueryRow(
				ctx,
				`SELECT clock_timestamp()`,
			).Scan(
				&now,
			); err != nil {
				return err
			}

			admissionWindow := "OPEN"

			if eventState == "COMPLETED" ||
				(admissionOpen != nil &&
					now.Before(*admissionOpen)) ||
				(admissionClose != nil &&
					now.After(*admissionClose)) {
				admissionWindow =
					"ADMISSION_NOT_OPEN"
			}

			validationState :=
				map[string]any{
					"event_state":           eventState,
					"ticket_state":          ticketState,
					"credential_state":      credentialState,
					"admission_window":      admissionWindow,
					"credential_validation": "NOT_PERFORMED_MANUAL_LOOKUP",
				}

			scanID := uuid.New()
			admissionID := uuid.New()

			if _, err := tx.Exec(
				ctx,
				`
					INSERT INTO scan_attempts (
						id,
						event_id,
						scanner_user_id,
						ticket_entitlement_id,
						idempotency_operation_id,
						result,
						gate_reference,
						metadata,
						occurred_at
					)
					VALUES (
						$1,
						$2,
						$3,
						$4,
						$5,
						'MANUAL_OVERRIDE_ADMITTED',
						NULLIF($6, ''),
						jsonb_build_object(
							'reason',
							$7::text,
							'validation_state',
							jsonb_build_object(
								'event_state',
								$8::text,
								'ticket_state',
								$9::text,
								'credential_state',
								$10::text,
								'admission_window',
								$11::text,
								'credential_validation',
								'NOT_PERFORMED_MANUAL_LOOKUP'
							)
						),
						$12
					)
				`,
				scanID,
				input.EventID,
				actorID,
				input.TicketID,
				operationID,
				input.GateReference,
				input.Reason,
				eventState,
				ticketState,
				credentialState,
				admissionWindow,
				now,
			); err != nil {
				return err
			}

			if _, err := tx.Exec(
				ctx,
				`
					INSERT INTO admissions (
						id,
						event_id,
						ticket_entitlement_id,
						scan_attempt_id,
						status,
						admitted_at
					)
					VALUES (
						$1,
						$2,
						$3,
						$4,
						'ACTIVE',
						$5
					)
				`,
				admissionID,
				input.EventID,
				input.TicketID,
				scanID,
				now,
			); err != nil {
				return err
			}

			if _, err := s.audit.Append(
				ctx,
				tx,
				audit.Event{
					EventID:             &input.EventID,
					ActorKind:           audit.ActorUser,
					ActorUserID:         &actorID,
					Operation:           "ADMISSION_MANUAL_OVERRIDE",
					EntityType:          "ADMISSION",
					EntityID:            &admissionID,
					TicketEntitlementID: &input.TicketID,
					Reason:              input.Reason,
					NewState: map[string]any{
						"status": "ACTIVE",
					},
					Metadata: map[string]any{
						"validation_state": validationState,
					},
				},
			); err != nil {
				return err
			}

			if _, err := s.outbox.Append(
				ctx,
				tx,
				outbox.Fact{
					EventID:       &input.EventID,
					FactType:      "admission.manual_override",
					AggregateType: "ADMISSION",
					AggregateID:   &admissionID,
					Payload: map[string]any{
						"ticket_id":        input.TicketID,
						"validation_state": validationState,
					},
				},
			); err != nil {
				return err
			}

			display, err :=
				ticketDisplay(
					ctx,
					tx,
					input.TicketID,
				)
			if err != nil {
				return err
			}

			result = ScanResult{
				Result:        "MANUAL_OVERRIDE_ADMITTED",
				ScanAttemptID: scanID,
				AdmissionID:   &admissionID,
				TicketID:      &input.TicketID,
				TicketDisplay: display,
				AdmittedAt:    &now,
			}

			return nil
		},
	)
	if err != nil {
		return ScanResult{}, err
	}

	return result, nil
}

func lockManualOverrideCredentialState(
	ctx context.Context,
	tx pgx.Tx,
	ticketID uuid.UUID,
) (string, error) {
	var state string

	err := tx.QueryRow(
		ctx,
		`
			SELECT status
			FROM qr_credentials
			WHERE ticket_entitlement_id = $1
			  AND status = 'ACTIVE'
			ORDER BY issued_at DESC, id DESC
			LIMIT 1
			FOR UPDATE
		`,
		ticketID,
	).Scan(
		&state,
	)
	if err == nil {
		return state, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	err = tx.QueryRow(
		ctx,
		`
			SELECT status
			FROM qr_credentials
			WHERE ticket_entitlement_id = $1
			ORDER BY issued_at DESC, id DESC
			LIMIT 1
			FOR UPDATE
		`,
		ticketID,
	).Scan(
		&state,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return "",
			apierror.New(
				apierror.CodeTicketInvalid,
				"Ticket has no QR credential",
			)
	}

	if err != nil {
		return "", err
	}

	return state, nil
}

func requireAdmissionSupervisor(ctx context.Context, tx pgx.Tx, userID, eventID uuid.UUID) error {
	return auth.NewAuthorizer(tx).RequireHumanEventRole(ctx, userID, eventID, "GATE_SUPERVISOR", "EVENT_MANAGER")
}
