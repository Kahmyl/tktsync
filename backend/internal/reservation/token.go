package reservation

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

type tokenParts struct {
	Version       int
	ReservationID uuid.UUID
	MAC           []byte
}

func (s *Service) buildToken(
	reservationID uuid.UUID,
	partnerID uuid.UUID,
	eventID uuid.UUID,
	version int,
) (string, error) {
	if s.keys == nil {
		return "", errors.New(
			"reservation HMAC keyring is not configured",
		)
	}

	message := reservationTokenMessage(
		version,
		reservationID,
		partnerID,
		eventID,
	)

	mac, err := s.keys.MAC(
		version,
		message,
	)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(
		"rsv1.%d.%s.%s",
		version,
		reservationID.String(),
		base64.RawURLEncoding.EncodeToString(mac),
	), nil
}

func reservationTokenMessage(
	version int,
	reservationID uuid.UUID,
	partnerID uuid.UUID,
	eventID uuid.UUID,
) []byte {
	return auth.Canonical(
		"rsv1",
		strconv.Itoa(version),
		reservationID.String(),
		partnerID.String(),
		eventID.String(),
	)
}

func parseReservationToken(
	raw string,
) (tokenParts, error) {
	parts := strings.Split(
		strings.TrimSpace(raw),
		".",
	)

	if len(parts) != 4 ||
		parts[0] != "rsv1" {
		return tokenParts{},
			apierror.New(
				apierror.CodeHoldNotOwned,
				"Reservation continuation token is invalid",
			)
	}

	version, err := strconv.Atoi(parts[1])
	if err != nil || version <= 0 {
		return tokenParts{},
			apierror.New(
				apierror.CodeHoldNotOwned,
				"Reservation continuation token is invalid",
			)
	}

	reservationID, err := uuid.Parse(
		parts[2],
	)
	if err != nil {
		return tokenParts{},
			apierror.New(
				apierror.CodeHoldNotOwned,
				"Reservation continuation token is invalid",
			)
	}

	mac, err := base64.RawURLEncoding.DecodeString(
		parts[3],
	)
	if err != nil || len(mac) != 32 {
		return tokenParts{},
			apierror.New(
				apierror.CodeHoldNotOwned,
				"Reservation continuation token is invalid",
			)
	}

	return tokenParts{
		Version:       version,
		ReservationID: reservationID,
		MAC:           mac,
	}, nil
}

func (s *Service) verifyLockedToken(
	raw string,
	parts tokenParts,
	record reservationRecord,
	partnerID uuid.UUID,
) error {
	if s.keys == nil ||
		partnerID == uuid.Nil ||
		record.PartnerID != partnerID ||
		record.ID != parts.ReservationID ||
		record.TokenKeyVersion != parts.Version {
		return apierror.New(
			apierror.CodeHoldNotOwned,
			"Reservation is not owned by this caller",
		)
	}

	hash := auth.TokenHash(raw)

	if len(record.TokenHash) != len(hash) ||
		subtle.ConstantTimeCompare(
			record.TokenHash,
			hash[:],
		) != 1 {
		return apierror.New(
			apierror.CodeHoldNotOwned,
			"Reservation is not owned by this caller",
		)
	}

	if !s.keys.Verify(
		parts.Version,
		reservationTokenMessage(
			parts.Version,
			record.ID,
			record.PartnerID,
			record.EventID,
		),
		parts.MAC,
	) {
		return apierror.New(
			apierror.CodeHoldNotOwned,
			"Reservation is not owned by this caller",
		)
	}

	return nil
}

func (s *Service) RecoverToken(
	ctx context.Context,
	reservationID uuid.UUID,
	partnerID uuid.UUID,
) (string, error) {
	if reservationID == uuid.Nil ||
		partnerID == uuid.Nil {
		return "", apierror.New(
			apierror.CodeValidation,
			"Reservation and Partner are required",
		)
	}

	var token string

	err := s.transactions.Run(
		ctx,
		func(tx pgx.Tx) error {
			var (
				eventID         uuid.UUID
				storedPartnerID uuid.UUID
				version         int
			)

			err := tx.QueryRow(
				ctx,
				`
					SELECT
						event_id,
						partner_id,
						continuation_token_key_version
					FROM reservations
					WHERE id = $1
				`,
				reservationID,
			).Scan(
				&eventID,
				&storedPartnerID,
				&version,
			)
			if err != nil {
				if errors.Is(
					err,
					pgx.ErrNoRows,
				) {
					return apierror.New(
						apierror.CodeResourceNotFound,
						"Reservation not found",
					)
				}

				return err
			}

			if storedPartnerID != partnerID {
				return apierror.New(
					apierror.CodeHoldNotOwned,
					"Reservation is not owned by this caller",
				)
			}

			value, err := s.buildToken(
				reservationID,
				partnerID,
				eventID,
				version,
			)
			if err != nil {
				return err
			}

			token = value

			return nil
		},
	)
	if err != nil {
		return "", err
	}

	return token, nil
}
