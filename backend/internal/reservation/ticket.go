package reservation

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/outbox"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

type VoidedTicket struct {
	TicketID   uuid.UUID
	State      string
	VoidedAt   time.Time
	VoidReason string
}

type ReissuedCredential struct {
	TicketID     uuid.UUID
	CredentialID uuid.UUID
	State        string
	IssuedAt     time.Time
}

type lockedTicket struct {
	ID      uuid.UUID
	EventID uuid.UUID
	State   string
}

type lockedCredential struct {
	ID         uuid.UUID
	State      string
	KeyVersion int
}

func (s *Service) VoidPartnerTicket(
	ctx context.Context,
	partnerID uuid.UUID,
	ticketID uuid.UUID,
	reason string,
) (VoidedTicket, error) {
	if partnerID == uuid.Nil ||
		ticketID == uuid.Nil {
		return VoidedTicket{},
			apierror.New(
				apierror.CodeValidation,
				"Partner and Ticket are required",
			)
	}

	reason = strings.TrimSpace(reason)

	var result VoidedTicket

	err := s.transactions.Run(
		ctx,
		func(tx pgx.Tx) error {
			eventID, err :=
				partnerCommercialTicketEventID(
					ctx,
					tx,
					partnerID,
					ticketID,
				)
			if err != nil {
				return err
			}

			if err := lockTicketEvent(
				ctx,
				tx,
				eventID,
			); err != nil {
				return err
			}

			ticket, err :=
				lockCommercialTicketForPartner(
					ctx,
					tx,
					partnerID,
					ticketID,
					eventID,
				)
			if err != nil {
				return err
			}

			if ticket.State == "VOIDED" {
				return apierror.New(
					apierror.CodeTicketVoid,
					"Ticket is already void",
				)
			}

			if ticket.State != "ACTIVE" {
				return apierror.New(
					apierror.CodeTicketInvalid,
					"Ticket is not active",
				)
			}

			credential, found, err :=
				lockActiveTicketCredential(
					ctx,
					tx,
					ticketID,
				)
			if err != nil {
				return err
			}

			now, err := ticketClock(
				ctx,
				tx,
			)
			if err != nil {
				return err
			}

			if found {
				commandTag, err :=
					tx.Exec(
						ctx,
						`
							UPDATE qr_credentials
							SET
								status = 'REVOKED',
								superseded_at = NULL,
								revoked_at = $2
							WHERE id = $1
							  AND status = 'ACTIVE'
						`,
						credential.ID,
						now,
					)
				if err != nil {
					return err
				}

				if commandTag.RowsAffected() != 1 {
					return apierror.New(
						apierror.CodeInternal,
						"Active QR credential changed during Ticket void",
					)
				}
			}

			commandTag, err :=
				tx.Exec(
					ctx,
					`
						UPDATE ticket_entitlements
						SET
							status = 'VOIDED',
							voided_at = $2,
							void_reason = NULLIF($3, '')
						WHERE id = $1
						  AND status = 'ACTIVE'
					`,
					ticketID,
					now,
					reason,
				)
			if err != nil {
				return err
			}

			if commandTag.RowsAffected() != 1 {
				return apierror.New(
					apierror.CodeTicketInvalid,
					"Ticket changed during void",
				)
			}

			if _, err := s.audit.Append(
				ctx,
				tx,
				audit.Event{
					EventID:             &eventID,
					PartnerID:           &partnerID,
					ActorKind:           audit.ActorPartner,
					ActorPartnerID:      &partnerID,
					Operation:           "TICKET_VOIDED",
					EntityType:          "TICKET",
					EntityID:            &ticketID,
					TicketEntitlementID: &ticketID,
					PreviousState: map[string]any{
						"state": "ACTIVE",
					},
					NewState: map[string]any{
						"state": "VOIDED",
					},
					Reason: reason,
				},
			); err != nil {
				return err
			}

			if _, err := s.outbox.Append(
				ctx,
				tx,
				outbox.Fact{
					EventID:       &eventID,
					FactType:      "ticket.voided",
					AggregateType: "TICKET",
					AggregateID:   &ticketID,
				},
			); err != nil {
				return err
			}

			result = VoidedTicket{
				TicketID:   ticketID,
				State:      "VOIDED",
				VoidedAt:   now,
				VoidReason: reason,
			}

			return nil
		},
	)
	if err != nil {
		return VoidedTicket{}, err
	}

	return result, nil
}

func (s *Service) ReissuePartnerCredential(
	ctx context.Context,
	partnerID uuid.UUID,
	ticketID uuid.UUID,
) (ReissuedCredential, error) {
	if partnerID == uuid.Nil ||
		ticketID == uuid.Nil {
		return ReissuedCredential{},
			apierror.New(
				apierror.CodeValidation,
				"Partner and Ticket are required",
			)
	}

	if s.qrKeys == nil {
		return ReissuedCredential{},
			apierror.New(
				apierror.CodeAuthorityTemporarilyUnavailable,
				"QR credential authority is not configured",
			)
	}

	var result ReissuedCredential

	err := s.transactions.Run(
		ctx,
		func(tx pgx.Tx) error {
			eventID, err :=
				partnerCommercialTicketEventID(
					ctx,
					tx,
					partnerID,
					ticketID,
				)
			if err != nil {
				return err
			}

			if err := lockTicketEvent(
				ctx,
				tx,
				eventID,
			); err != nil {
				return err
			}

			ticket, err :=
				lockCommercialTicketForPartner(
					ctx,
					tx,
					partnerID,
					ticketID,
					eventID,
				)
			if err != nil {
				return err
			}

			if ticket.State == "VOIDED" {
				return apierror.New(
					apierror.CodeTicketVoid,
					"Ticket is void",
				)
			}

			if ticket.State != "ACTIVE" {
				return apierror.New(
					apierror.CodeTicketInvalid,
					"Ticket is not active",
				)
			}

			current, found, err :=
				lockActiveTicketCredential(
					ctx,
					tx,
					ticketID,
				)
			if err != nil {
				return err
			}

			if !found {
				return ticketCredentialStateError(
					ctx,
					tx,
					ticketID,
				)
			}

			now, err := ticketClock(
				ctx,
				tx,
			)
			if err != nil {
				return err
			}

			commandTag, err :=
				tx.Exec(
					ctx,
					`
						UPDATE qr_credentials
						SET
							status = 'SUPERSEDED',
							superseded_at = $2,
							revoked_at = NULL
						WHERE id = $1
						  AND status = 'ACTIVE'
					`,
					current.ID,
					now,
				)
			if err != nil {
				return err
			}

			if commandTag.RowsAffected() != 1 {
				return apierror.New(
					apierror.CodeInternal,
					"Active QR credential changed during reissue",
				)
			}

			credentialID := uuid.New()
			version := s.qrKeys.ActiveVersion()

			payload, err :=
				s.buildQRPayload(
					credentialID,
					ticketID,
					eventID,
					version,
				)
			if err != nil {
				return apierror.New(
					apierror.CodeAuthorityTemporarilyUnavailable,
					"Replacement QR credential could not be created",
				)
			}

			hash := auth.TokenHash(payload)

			if _, err := tx.Exec(
				ctx,
				`
					INSERT INTO qr_credentials (
						id,
						ticket_entitlement_id,
						token_hash,
						token_key_version,
						status,
						issued_at,
						created_at
					)
					VALUES (
						$1,
						$2,
						$3,
						$4,
						'ACTIVE',
						$5,
						$5
					)
				`,
				credentialID,
				ticketID,
				hash[:],
				version,
				now,
			); err != nil {
				return err
			}

			if _, err := s.audit.Append(
				ctx,
				tx,
				audit.Event{
					EventID:             &eventID,
					PartnerID:           &partnerID,
					ActorKind:           audit.ActorPartner,
					ActorPartnerID:      &partnerID,
					Operation:           "TICKET_CREDENTIAL_SUPERSEDED",
					EntityType:          "QR_CREDENTIAL",
					EntityID:            &current.ID,
					TicketEntitlementID: &ticketID,
					PreviousState: map[string]any{
						"state": "ACTIVE",
					},
					NewState: map[string]any{
						"state": "SUPERSEDED",
					},
				},
			); err != nil {
				return err
			}

			if _, err := s.audit.Append(
				ctx,
				tx,
				audit.Event{
					EventID:             &eventID,
					PartnerID:           &partnerID,
					ActorKind:           audit.ActorPartner,
					ActorPartnerID:      &partnerID,
					Operation:           "TICKET_CREDENTIAL_ISSUED",
					EntityType:          "QR_CREDENTIAL",
					EntityID:            &credentialID,
					TicketEntitlementID: &ticketID,
					NewState: map[string]any{
						"state": "ACTIVE",
					},
					Metadata: map[string]any{
						"replaces_credential_id": current.ID,
					},
				},
			); err != nil {
				return err
			}

			if _, err := s.outbox.Append(
				ctx,
				tx,
				outbox.Fact{
					EventID:       &eventID,
					FactType:      "ticket.credential_reissued",
					AggregateType: "TICKET",
					AggregateID:   &ticketID,
					Payload: map[string]any{
						"credential_id": credentialID,
					},
				},
			); err != nil {
				return err
			}

			result = ReissuedCredential{
				TicketID:     ticketID,
				CredentialID: credentialID,
				State:        "ACTIVE",
				IssuedAt:     now,
			}

			return nil
		},
	)
	if err != nil {
		return ReissuedCredential{}, err
	}

	return result, nil
}

func partnerCommercialTicketEventID(
	ctx context.Context,
	tx pgx.Tx,
	partnerID uuid.UUID,
	ticketID uuid.UUID,
) (uuid.UUID, error) {
	var eventID uuid.UUID

	err := tx.QueryRow(
		ctx,
		`
			SELECT t.event_id
			FROM ticket_entitlements t
			JOIN sale_items si
			  ON si.id = t.origin_sale_item_id
			JOIN sales s
			  ON s.id = si.sale_id
			WHERE t.id = $1
			  AND s.partner_id = $2
		`,
		ticketID,
		partnerID,
	).Scan(
		&eventID,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil,
			apierror.New(
				apierror.CodeResourceNotFound,
				"Ticket not found",
			)
	}

	if err != nil {
		return uuid.Nil, err
	}

	return eventID, nil
}

func lockTicketEvent(
	ctx context.Context,
	tx pgx.Tx,
	eventID uuid.UUID,
) error {
	var id uuid.UUID

	err := tx.QueryRow(
		ctx,
		`
			SELECT id
			FROM events
			WHERE id = $1
			FOR KEY SHARE
		`,
		eventID,
	).Scan(
		&id,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return apierror.New(
			apierror.CodeResourceNotFound,
			"Event not found",
		)
	}

	return err
}

func lockCommercialTicketForPartner(
	ctx context.Context,
	tx pgx.Tx,
	partnerID uuid.UUID,
	ticketID uuid.UUID,
	eventID uuid.UUID,
) (lockedTicket, error) {
	var result lockedTicket

	err := tx.QueryRow(
		ctx,
		`
			SELECT
				t.id,
				t.event_id,
				t.status
			FROM ticket_entitlements t
			JOIN sale_items si
			  ON si.id = t.origin_sale_item_id
			JOIN sales s
			  ON s.id = si.sale_id
			WHERE t.id = $1
			  AND t.event_id = $2
			  AND s.partner_id = $3
			FOR UPDATE OF t
		`,
		ticketID,
		eventID,
		partnerID,
	).Scan(
		&result.ID,
		&result.EventID,
		&result.State,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return lockedTicket{},
			apierror.New(
				apierror.CodeResourceNotFound,
				"Ticket not found",
			)
	}

	if err != nil {
		return lockedTicket{}, err
	}

	return result, nil
}

func lockActiveTicketCredential(
	ctx context.Context,
	tx pgx.Tx,
	ticketID uuid.UUID,
) (lockedCredential, bool, error) {
	var result lockedCredential

	err := tx.QueryRow(
		ctx,
		`
			SELECT
				id,
				status,
				token_key_version
			FROM qr_credentials
			WHERE ticket_entitlement_id = $1
			  AND status = 'ACTIVE'
			ORDER BY issued_at DESC, id DESC
			LIMIT 1
			FOR UPDATE
		`,
		ticketID,
	).Scan(
		&result.ID,
		&result.State,
		&result.KeyVersion,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return lockedCredential{},
			false,
			nil
	}

	if err != nil {
		return lockedCredential{},
			false,
			err
	}

	return result, true, nil
}

func ticketCredentialStateError(
	ctx context.Context,
	tx pgx.Tx,
	ticketID uuid.UUID,
) error {
	var state string

	err := tx.QueryRow(
		ctx,
		`
			SELECT status
			FROM qr_credentials
			WHERE ticket_entitlement_id = $1
			ORDER BY issued_at DESC, id DESC
			LIMIT 1
		`,
		ticketID,
	).Scan(
		&state,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return apierror.New(
			apierror.CodeTicketInvalid,
			"Ticket has no QR credential",
		)
	}

	if err != nil {
		return err
	}

	switch state {
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
	}

	return apierror.New(
		apierror.CodeTicketInvalid,
		"Ticket has no active QR credential",
	)
}

func ticketClock(
	ctx context.Context,
	tx pgx.Tx,
) (time.Time, error) {
	var now time.Time

	err := tx.QueryRow(
		ctx,
		`SELECT clock_timestamp()`,
	).Scan(
		&now,
	)

	return now, err
}
