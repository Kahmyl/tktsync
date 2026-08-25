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
	ticketQRCapabilitySize    = 4 + 12 + 16 + 16
	ticketQRCapabilityPurpose = "ticket-qr-presentation"
)

// TicketQRPresentationCapability builds an opaque, authenticated-encrypted
// ticket-level capability. It is independent of credential identity, so the
// same URL resolves to the current active credential after a normal reissue.
func (s *Service) TicketQRPresentationCapability(
	ticketID uuid.UUID,
) (string, error) {
	if ticketID == uuid.Nil {
		return "", apierror.New(
			apierror.CodeValidation,
			"Ticket is required",
		)
	}

	if s.qrKeys == nil {
		return "", apierror.New(
			apierror.CodeAuthorityTemporarilyUnavailable,
			"Ticket QR presentation authority is not configured",
		)
	}

	version := s.qrKeys.ActiveVersion()
	sealed, err := s.qrKeys.SealDeterministic(
		version,
		ticketQRCapabilityPurpose,
		ticketID[:],
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
	ticketID, err := s.parseTicketQRPresentationCapability(capability)
	if err != nil {
		return ActiveCredential{}, err
	}

	return s.RecoverActiveCredentialAdmin(ctx, ticketID)
}

func (s *Service) parseTicketQRPresentationCapability(
	capability string,
) (uuid.UUID, error) {
	capability = strings.TrimSpace(capability)
	if s.qrKeys == nil ||
		!strings.HasPrefix(capability, ticketQRCapabilityPrefix) {
		return uuid.Nil, ticketQRCapabilityNotFound()
	}

	raw, err := base64.RawURLEncoding.DecodeString(
		strings.TrimPrefix(capability, ticketQRCapabilityPrefix),
	)
	if err != nil || len(raw) != ticketQRCapabilitySize {
		return uuid.Nil, ticketQRCapabilityNotFound()
	}

	versionValue := binary.BigEndian.Uint32(raw[:4])
	if versionValue == 0 {
		return uuid.Nil, ticketQRCapabilityNotFound()
	}
	version := int(versionValue)

	plaintext, err := s.qrKeys.OpenDeterministic(
		version,
		ticketQRCapabilityPurpose,
		raw[4:],
	)
	if err != nil {
		return uuid.Nil, ticketQRCapabilityNotFound()
	}
	ticketID, err := uuid.FromBytes(plaintext)
	if err != nil || ticketID == uuid.Nil {
		return uuid.Nil, ticketQRCapabilityNotFound()
	}

	return ticketID, nil
}

func ticketQRCapabilityNotFound() error {
	return apierror.New(
		apierror.CodeResourceNotFound,
		"Hosted ticket QR not found",
	)
}
