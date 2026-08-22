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

func (s *Service) RecoverActiveCredential(
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

	if s.qrKeys == nil {
		return ActiveCredential{},
			apierror.New(
				apierror.CodeAuthorityTemporarilyUnavailable,
				"QR credential authority is not configured",
			)
	}

	var result ActiveCredential

	err := s.transactions.Run(
		ctx,
		func(tx pgx.Tx) error {
			var (
				eventID     uuid.UUID
				ticketState string
			)

			err := tx.QueryRow(
				ctx,
				`
					SELECT
						t.event_id,
						t.status
					FROM ticket_entitlements t
					JOIN sale_items si
					  ON si.id =
					     t.origin_sale_item_id
					JOIN sales s
					  ON s.id = si.sale_id
					WHERE t.id = $1
					  AND s.partner_id = $2
				`,
				ticketID,
				partnerID,
			).Scan(
				&eventID,
				&ticketState,
			)
			if err != nil {
				if errors.Is(
					err,
					pgx.ErrNoRows,
				) {
					return apierror.New(
						apierror.CodeResourceNotFound,
						"Ticket not found",
					)
				}

				return err
			}

			if ticketState == "VOIDED" {
				return apierror.New(
					apierror.CodeTicketVoid,
					"Ticket is void",
				)
			}

			if ticketState != "ACTIVE" {
				return apierror.New(
					apierror.CodeTicketInvalid,
					"Ticket is not active",
				)
			}

			var (
				credentialID    uuid.UUID
				credentialState string
				version         int
				storedHash      []byte
			)

			err = tx.QueryRow(
				ctx,
				`
					SELECT
						id,
						status,
						token_key_version,
						token_hash
					FROM qr_credentials
					WHERE ticket_entitlement_id =
					      $1
					  AND status = 'ACTIVE'
					ORDER BY
						issued_at DESC,
						id DESC
					LIMIT 1
				`,
				ticketID,
			).Scan(
				&credentialID,
				&credentialState,
				&version,
				&storedHash,
			)
			if err != nil {
				if !errors.Is(
					err,
					pgx.ErrNoRows,
				) {
					return err
				}

				var latestState string

				latestErr :=
					tx.QueryRow(
						ctx,
						`
							SELECT status
							FROM qr_credentials
							WHERE ticket_entitlement_id =
							      $1
							ORDER BY
								issued_at DESC,
								id DESC
							LIMIT 1
						`,
						ticketID,
					).Scan(
						&latestState,
					)

				if errors.Is(
					latestErr,
					pgx.ErrNoRows,
				) {
					return apierror.New(
						apierror.CodeTicketInvalid,
						"Ticket has no QR credential",
					)
				}

				if latestErr != nil {
					return latestErr
				}

				switch latestState {
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
						"Ticket has no active QR credential",
					)
				}
			}

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
					"QR credential could not be recovered",
				)
			}

			recoveredHash :=
				auth.TokenHash(
					payload,
				)

			if len(storedHash) !=
				len(recoveredHash) ||
				subtle.ConstantTimeCompare(
					storedHash,
					recoveredHash[:],
				) != 1 {
				return apierror.New(
					apierror.CodeAuthorityTemporarilyUnavailable,
					"QR credential authority could not reproduce the active credential",
				)
			}

			result = ActiveCredential{
				TicketID:     ticketID,
				CredentialID: credentialID,
				State:        credentialState,
				QRPayload:    payload,
			}

			return nil
		},
	)
	if err != nil {
		return ActiveCredential{},
			err
	}

	return result, nil
}
