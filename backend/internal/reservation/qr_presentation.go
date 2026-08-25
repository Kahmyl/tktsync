package reservation

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"strings"

	"github.com/google/uuid"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

const (
	ticketQRCapabilityPrefix  = "tqp1_"
	ticketQRCapabilitySize    = 4 + 12 + 32 + 16
	ticketQRCapabilityPurpose = "ticket-qr-presentation"
)

// TicketQRPresentationCapability builds an opaque, authenticated-encrypted
// capability bound to one Ticket credential. Credential reissue therefore
// revokes both the old QR payload and its hosted presentation URL.
func (s *Service) TicketQRPresentationCapability(
	ticketID uuid.UUID,
	credentialID uuid.UUID,
) (string, error) {
	if ticketID == uuid.Nil || credentialID == uuid.Nil {
		return "", apierror.New(
			apierror.CodeValidation,
			"Ticket and credential are required",
		)
	}

	if s.qrKeys == nil {
		return "", apierror.New(
			apierror.CodeAuthorityTemporarilyUnavailable,
			"Ticket QR presentation authority is not configured",
		)
	}

	version := s.qrKeys.ActiveVersion()
	identity := make([]byte, 32)
	copy(identity[:16], ticketID[:])
	copy(identity[16:], credentialID[:])
	sealed, err := s.qrKeys.SealDeterministic(
		version,
		ticketQRCapabilityPurpose,
		identity,
	)
	if err != nil {
		return "", apierror.New(
			apierror.CodeAuthorityTemporarilyUnavailable,
			"Ticket QR presentation capability could not be generated",
		)
	}

	raw := make([]byte, ticketQRCapabilitySize)
	binary.BigEndian.PutUint32(raw[:4], uint32(version))
	copy(raw[4:], sealed)

	return ticketQRCapabilityPrefix +
		base64.RawURLEncoding.EncodeToString(raw), nil
}

// RecoverActiveCredentialForPresentation treats possession of the verified
// capability as the narrow authorization to render this ticket's QR. It does
// not authorize any ticket mutation or disclose ticket metadata.
func (s *Service) RecoverActiveCredentialForPresentation(
	ctx context.Context,
	capability string,
) (ActiveCredential, error) {
	ticketID, credentialID, err := s.parseTicketQRPresentationCapability(capability)
	if err != nil {
		return ActiveCredential{}, err
	}

	return s.recoverActiveCredentialByTicket(ctx, ticketID, nil, &credentialID)
}

func (s *Service) parseTicketQRPresentationCapability(
	capability string,
) (uuid.UUID, uuid.UUID, error) {
	capability = strings.TrimSpace(capability)
	if s.qrKeys == nil ||
		!strings.HasPrefix(capability, ticketQRCapabilityPrefix) {
		return uuid.Nil, uuid.Nil, ticketQRCapabilityNotFound()
	}

	raw, err := base64.RawURLEncoding.DecodeString(
		strings.TrimPrefix(capability, ticketQRCapabilityPrefix),
	)
	if err != nil || len(raw) != ticketQRCapabilitySize {
		return uuid.Nil, uuid.Nil, ticketQRCapabilityNotFound()
	}

	versionValue := binary.BigEndian.Uint32(raw[:4])
	if versionValue == 0 {
		return uuid.Nil, uuid.Nil, ticketQRCapabilityNotFound()
	}
	version := int(versionValue)

	plaintext, err := s.qrKeys.OpenDeterministic(
		version,
		ticketQRCapabilityPurpose,
		raw[4:],
	)
	if err != nil || len(plaintext) != 32 {
		return uuid.Nil, uuid.Nil, ticketQRCapabilityNotFound()
	}
	ticketID, err := uuid.FromBytes(plaintext[:16])
	if err != nil || ticketID == uuid.Nil {
		return uuid.Nil, uuid.Nil, ticketQRCapabilityNotFound()
	}
	credentialID, err := uuid.FromBytes(plaintext[16:])
	if err != nil || credentialID == uuid.Nil {
		return uuid.Nil, uuid.Nil, ticketQRCapabilityNotFound()
	}

	return ticketID, credentialID, nil
}

func ticketQRCapabilityNotFound() error {
	return apierror.New(
		apierror.CodeResourceNotFound,
		"Hosted ticket QR not found",
	)
}
