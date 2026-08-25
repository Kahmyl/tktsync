package reservation

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

func (s *Service) buildQRPayload(
	credentialID uuid.UUID,
	ticketID uuid.UUID,
	eventID uuid.UUID,
	version int,
) (string, error) {
	if s.qrKeys == nil {
		return "", errors.New(
			"QR HMAC keyring is not configured",
		)
	}

	mac, err := s.qrKeys.MAC(
		version,
		qrPayloadMessage(
			credentialID,
			ticketID,
			eventID,
		),
	)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(
		"qr1.%d.%s.%s",
		version,
		credentialID.String(),
		base64.RawURLEncoding.EncodeToString(mac),
	), nil
}

func qrPayloadMessage(
	credentialID uuid.UUID,
	ticketID uuid.UUID,
	eventID uuid.UUID,
) []byte {
	return auth.Canonical(
		credentialID.String(),
		ticketID.String(),
		eventID.String(),
	)
}

// RecoverActiveCredentialForPartner reconstructs the active credential only
// when the Ticket belongs to the authenticated Partner.
func (s *Service) RecoverActiveCredentialForPartner(
	ctx context.Context,
	partnerID uuid.UUID,
	ticketID uuid.UUID,
) (ActiveCredential, error) {
	if partnerID == uuid.Nil ||
		ticketID == uuid.Nil {
		return ActiveCredential{},
			apierror.New(
				apierror.CodeValidation,
				"Partner and Ticket are required",
			)
	}

	return s.recoverActiveCredentialByTicket(ctx, ticketID, &partnerID, nil)
}

// RecoverActiveCredentialAdmin reconstructs the current credential for any
// Ticket without changing credential identity or state. Authorization remains
// the responsibility of the Admin HTTP command boundary.
func (s *Service) RecoverActiveCredentialAdmin(ctx context.Context, ticketID uuid.UUID) (ActiveCredential, error) {
	if ticketID == uuid.Nil {
		return ActiveCredential{}, apierror.New(apierror.CodeValidation, "Ticket is required")
	}
	return s.recoverActiveCredentialByTicket(ctx, ticketID, nil, nil)
}

// recoverActiveCredentialByTicket is the one authoritative credential-state
// recovery path. Public wrappers supply either an ownership constraint or a
// credential identity bound into a presentation capability.
func (s *Service) recoverActiveCredentialByTicket(
	ctx context.Context,
	ticketID uuid.UUID,
	partnerID *uuid.UUID,
	expectedCredentialID *uuid.UUID,
) (ActiveCredential, error) {
	if s.qrKeys == nil {
		return ActiveCredential{}, apierror.New(apierror.CodeAuthorityTemporarilyUnavailable, "QR credential authority is not configured")
	}
	var partnerFilter any
	if partnerID != nil {
		partnerFilter = *partnerID
	}
	var credentialFilter any
	if expectedCredentialID != nil {
		credentialFilter = *expectedCredentialID
	}
	var result ActiveCredential
	err := s.transactions.Run(ctx, func(tx pgx.Tx) error {
		var eventID uuid.UUID
		var eventState string
		var ticketState string
		if err := tx.QueryRow(ctx, `
			SELECT t.event_id, t.status, e.state
			FROM ticket_entitlements t
			JOIN events e ON e.id = t.event_id
			WHERE t.id = $1
			  AND (
				$2::uuid IS NULL
				OR EXISTS (
					SELECT 1
					FROM sale_items si
					JOIN sales s ON s.id = si.sale_id
					WHERE si.id = t.origin_sale_item_id
					  AND s.partner_id = $2
				)
			  )
		`, ticketID, partnerFilter).Scan(&eventID, &ticketState, &eventState); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apierror.New(apierror.CodeResourceNotFound, "Ticket not found")
			}
			return err
		}
		if ticketState == "VOIDED" {
			return apierror.New(apierror.CodeTicketVoid, "Ticket is void")
		}
		if ticketState != "ACTIVE" {
			return apierror.New(apierror.CodeTicketInvalid, "Ticket is not active")
		}
		if eventState == "CANCELLED" {
			return apierror.New(apierror.CodeEventCancelled, "Event is cancelled")
		}
		var credentialID uuid.UUID
		var credentialState string
		var version int
		var storedHash []byte
		if err := tx.QueryRow(ctx, `
			SELECT id, status, token_key_version, token_hash
			FROM qr_credentials
			WHERE ticket_entitlement_id = $1
			  AND status = 'ACTIVE'
			  AND ($2::uuid IS NULL OR id = $2)
			ORDER BY issued_at DESC, id DESC
			LIMIT 1
		`, ticketID, credentialFilter).Scan(&credentialID, &credentialState, &version, &storedHash); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				if expectedCredentialID != nil {
					return ticketQRPresentationCredentialStateError(
						ctx,
						tx,
						ticketID,
						*expectedCredentialID,
					)
				}
				return ticketCredentialStateError(ctx, tx, ticketID)
			}
			return err
		}
		payload, err := s.buildQRPayload(credentialID, ticketID, eventID, version)
		if err != nil {
			return apierror.New(apierror.CodeAuthorityTemporarilyUnavailable, "QR credential could not be recovered")
		}
		recoveredHash := auth.TokenHash(payload)
		if len(storedHash) != len(recoveredHash) || subtle.ConstantTimeCompare(storedHash, recoveredHash[:]) != 1 {
			return apierror.New(apierror.CodeAuthorityTemporarilyUnavailable, "QR credential authority could not reproduce the active credential")
		}
		result = ActiveCredential{TicketID: ticketID, CredentialID: credentialID, State: credentialState, QRPayload: payload}
		return nil
	})
	if err != nil {
		return ActiveCredential{}, err
	}
	return result, nil
}

func ticketQRPresentationCredentialStateError(
	ctx context.Context,
	tx pgx.Tx,
	ticketID uuid.UUID,
	credentialID uuid.UUID,
) error {
	var state string
	err := tx.QueryRow(ctx, `
		SELECT status
		FROM qr_credentials
		WHERE ticket_entitlement_id = $1
		  AND id = $2
	`, ticketID, credentialID).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return ticketQRCapabilityNotFound()
	}
	if err != nil {
		return err
	}
	if state == "REVOKED" {
		return apierror.New(
			apierror.CodeCredentialRevoked,
			"Ticket credential is revoked",
		)
	}
	return ticketQRCapabilityNotFound()
}
