package admission

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/idempotency"
	"github.com/tktsync/tktsync/backend/internal/outbox"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
)

type Service struct {
	transactions *database.Runner
	qrKeys       *auth.HMACKeyring
	audit        audit.Store
	outbox       outbox.Store
}

func NewService(transactions *database.Runner, qrKeys *auth.HMACKeyring) *Service {
	return &Service{transactions: transactions, qrKeys: qrKeys, audit: audit.Store{}, outbox: outbox.Store{}}
}

type ScanInput struct {
	EventID                uuid.UUID
	Credential             string
	GateReference          string
	ScannerUserID          uuid.UUID
	IdempotencyOperationID uuid.UUID
}

type TicketDisplay struct {
	Section string `json:"section,omitempty"`
	Row     string `json:"row,omitempty"`
	Seat    string `json:"seat,omitempty"`
}

type ScanResult struct {
	Result             string
	ScanAttemptID      uuid.UUID
	AdmissionID        *uuid.UUID
	TicketID           *uuid.UUID
	TicketDisplay      TicketDisplay
	AdmittedAt         *time.Time
	PreviousAdmittedAt *time.Time
	PreviousGate       string
}

type parsedCredential struct {
	version int
	id      uuid.UUID
	mac     []byte
}

func parseCredential(raw string) (parsedCredential, error) {
	parts := strings.Split(strings.TrimSpace(raw), ".")
	if len(parts) != 4 || parts[0] != "qr1" {
		return parsedCredential{}, errors.New("malformed credential")
	}
	version, err := strconv.Atoi(parts[1])
	if err != nil || version <= 0 {
		return parsedCredential{}, errors.New("malformed credential version")
	}
	id, err := uuid.Parse(parts[2])
	if err != nil {
		return parsedCredential{}, errors.New("malformed credential identity")
	}
	mac, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(mac) != 32 {
		return parsedCredential{}, errors.New("malformed credential MAC")
	}
	return parsedCredential{version: version, id: id, mac: mac}, nil
}

func (s *Service) ValidateAndAdmit(ctx context.Context, input ScanInput) (ScanResult, error) {
	if input.EventID == uuid.Nil || input.ScannerUserID == uuid.Nil || input.IdempotencyOperationID == uuid.Nil {
		return ScanResult{}, apierror.New(apierror.CodeValidation, "Event, scanner, and idempotency operation are required")
	}
	input.Credential = strings.TrimSpace(input.Credential)
	input.GateReference = strings.TrimSpace(input.GateReference)
	parsed, parseErr := parseCredential(input.Credential)
	var result ScanResult
	err := s.transactions.Run(ctx, func(tx pgx.Tx) error {
		var eventState string
		var admissionOpen, admissionClose *time.Time
		if err := tx.QueryRow(ctx, `SELECT state,admission_open_at,admission_close_at FROM events WHERE id=$1 FOR KEY SHARE`, input.EventID).Scan(&eventState, &admissionOpen, &admissionClose); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apierror.New(apierror.CodeResourceNotFound, "Event not found")
			}
			return err
		}
		if parseErr != nil {
			return s.recordRejectedScan(ctx, tx, input, nil, nil, "INVALID_CREDENTIAL", "TICKET_INVALID", &result)
		}

		var ticketID, credentialID, actualEventID uuid.UUID
		var ticketState, credentialState string
		var storedVersion int
		var storedHash []byte
		err := tx.QueryRow(ctx, `
			SELECT t.id,t.event_id,t.status,q.id,q.status,q.token_key_version,q.token_hash
			FROM qr_credentials q JOIN ticket_entitlements t ON t.id=q.ticket_entitlement_id
			WHERE q.id=$1 FOR UPDATE OF t,q
		`, parsed.id).Scan(&ticketID, &actualEventID, &ticketState, &credentialID, &credentialState, &storedVersion, &storedHash)
		if errors.Is(err, pgx.ErrNoRows) {
			return s.recordRejectedScan(ctx, tx, input, nil, nil, "INVALID_CREDENTIAL", "TICKET_INVALID", &result)
		}
		if err != nil {
			return err
		}

		if s.qrKeys == nil || storedVersion != parsed.version {
			return apierror.New(apierror.CodeAuthorityTemporarilyUnavailable, "QR credential authority is unavailable")
		}
		expectedMAC, err := s.qrKeys.MAC(storedVersion, auth.Canonical(credentialID.String(), ticketID.String(), actualEventID.String()))
		if err != nil {
			return apierror.New(apierror.CodeAuthorityTemporarilyUnavailable, "QR credential key version is unavailable")
		}
		expectedPayload := "qr1." + strconv.Itoa(storedVersion) + "." + credentialID.String() + "." + base64.RawURLEncoding.EncodeToString(expectedMAC)
		expectedHash := auth.TokenHash(expectedPayload)
		if len(storedHash) != len(expectedHash) || subtle.ConstantTimeCompare(storedHash, expectedHash[:]) != 1 {
			return apierror.New(apierror.CodeAuthorityTemporarilyUnavailable, "QR credential integrity verification failed")
		}
		if subtle.ConstantTimeCompare(parsed.mac, expectedMAC) != 1 {
			return s.recordRejectedScan(ctx, tx, input, &ticketID, &credentialID, "INVALID_CREDENTIAL", "TICKET_INVALID", &result)
		}
		if actualEventID != input.EventID {
			return s.recordRejectedScan(ctx, tx, input, &ticketID, &credentialID, "WRONG_EVENT", "WRONG_EVENT", &result)
		}
		if eventState == "CANCELLED" {
			return s.recordRejectedScan(ctx, tx, input, &ticketID, &credentialID, "EVENT_CANCELLED", "EVENT_CANCELLED", &result)
		}
		if ticketState == "VOIDED" {
			return s.recordRejectedScan(ctx, tx, input, &ticketID, &credentialID, "TICKET_VOID", "TICKET_VOID", &result)
		}
		if credentialState == "REVOKED" {
			return s.recordRejectedScan(ctx, tx, input, &ticketID, &credentialID, "CREDENTIAL_REVOKED", "CREDENTIAL_REVOKED", &result)
		}
		if credentialState == "SUPERSEDED" {
			return s.recordRejectedScan(ctx, tx, input, &ticketID, &credentialID, "CREDENTIAL_SUPERSEDED", "CREDENTIAL_SUPERSEDED", &result)
		}
		var now time.Time
		if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
		if (admissionOpen != nil && now.Before(*admissionOpen)) || (admissionClose != nil && now.After(*admissionClose)) || eventState == "COMPLETED" {
			return s.recordRejectedScan(ctx, tx, input, &ticketID, &credentialID, "ADMISSION_NOT_OPEN", "ADMISSION_NOT_OPEN", &result)
		}
		var previousAt time.Time
		var previousGate *string
		err = tx.QueryRow(ctx, `SELECT admitted_at,gate_reference FROM admissions a JOIN scan_attempts sa ON sa.id=a.scan_attempt_id WHERE a.ticket_entitlement_id=$1 AND a.status='ACTIVE' FOR UPDATE OF a`, ticketID).Scan(&previousAt, &previousGate)
		if err == nil {
			if recordErr := s.recordRejectedScan(ctx, tx, input, &ticketID, &credentialID, "ALREADY_ADMITTED", "TICKET_ALREADY_ADMITTED", &result); recordErr != nil {
				return recordErr
			}
			result.PreviousAdmittedAt = &previousAt
			if previousGate != nil {
				result.PreviousGate = *previousGate
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		scanID := uuid.New()
		admissionID := uuid.New()
		if _, err = tx.Exec(ctx, `INSERT INTO scan_attempts (id,event_id,scanner_user_id,ticket_entitlement_id,qr_credential_id,idempotency_operation_id,result,gate_reference,occurred_at) VALUES ($1,$2,$3,$4,$5,$6,'ADMITTED',NULLIF($7,''),$8)`, scanID, input.EventID, input.ScannerUserID, ticketID, credentialID, input.IdempotencyOperationID, input.GateReference, now); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO admissions (id,event_id,ticket_entitlement_id,scan_attempt_id,status,admitted_at) VALUES ($1,$2,$3,$4,'ACTIVE',$5)`, admissionID, input.EventID, ticketID, scanID, now); err != nil {
			return err
		}
		if _, err = s.audit.Append(ctx, tx, audit.Event{EventID: &input.EventID, ActorKind: audit.ActorUser, ActorUserID: &input.ScannerUserID, Operation: "TICKET_ADMITTED", EntityType: "ADMISSION", EntityID: &admissionID, TicketEntitlementID: &ticketID, NewState: map[string]any{"status": "ACTIVE"}}); err != nil {
			return err
		}
		if _, err = s.outbox.Append(ctx, tx, outbox.Fact{EventID: &input.EventID, FactType: "admission.admitted", AggregateType: "ADMISSION", AggregateID: &admissionID, Payload: map[string]any{"ticket_id": ticketID}}); err != nil {
			return err
		}
		display, err := ticketDisplay(ctx, tx, ticketID)
		if err != nil {
			return err
		}
		result = ScanResult{Result: "ADMITTED", ScanAttemptID: scanID, AdmissionID: &admissionID, TicketID: &ticketID, TicketDisplay: display, AdmittedAt: &now}
		return nil
	})
	if err != nil {
		return ScanResult{}, err
	}
	return result, nil
}

func (s *Service) recordRejectedScan(ctx context.Context, tx pgx.Tx, input ScanInput, ticketID, credentialID *uuid.UUID, storedResult, responseResult string, output *ScanResult) error {
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	scanID := uuid.New()
	if _, err := tx.Exec(ctx, `INSERT INTO scan_attempts (id,event_id,scanner_user_id,ticket_entitlement_id,qr_credential_id,idempotency_operation_id,result,gate_reference,occurred_at) VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),$9)`, scanID, input.EventID, input.ScannerUserID, ticketID, credentialID, input.IdempotencyOperationID, storedResult, input.GateReference, now); err != nil {
		return err
	}
	*output = ScanResult{Result: responseResult, ScanAttemptID: scanID, TicketID: ticketID}
	if ticketID != nil {
		display, err := ticketDisplay(ctx, tx, *ticketID)
		if err != nil {
			return err
		}
		output.TicketDisplay = display
	}
	return nil
}

func ticketDisplay(ctx context.Context, tx pgx.Tx, ticketID uuid.UUID) (TicketDisplay, error) {
	var d TicketDisplay
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(es.name,''),COALESCE(riu.row_label,''),COALESCE(riu.seat_label,'')
		FROM ticket_entitlements t
		LEFT JOIN reserved_inventory_units riu ON riu.id=t.reserved_inventory_unit_id
		LEFT JOIN event_sections es ON es.id=riu.event_section_id
		WHERE t.id=$1
	`, ticketID).Scan(&d.Section, &d.Row, &d.Seat)
	return d, err
}

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
