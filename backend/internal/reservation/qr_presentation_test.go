package reservation

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
)

func TestTicketQRPresentationCapabilityIsOpaqueStableAndCredentialBound(t *testing.T) {
	service := NewService(nil, nil, testTicketQRKeyring(t))
	ticketID := uuid.New()
	credentialID := uuid.New()

	first, err := service.TicketQRPresentationCapability(ticketID, credentialID)
	if err != nil {
		t.Fatalf("build capability: %v", err)
	}
	second, err := service.TicketQRPresentationCapability(ticketID, credentialID)
	if err != nil {
		t.Fatalf("rebuild capability: %v", err)
	}

	if first != second {
		t.Fatalf("capability changed: first=%q second=%q", first, second)
	}
	raw, err := base64.RawURLEncoding.DecodeString(
		strings.TrimPrefix(first, ticketQRCapabilityPrefix),
	)
	if err != nil {
		t.Fatalf("decode capability: %v", err)
	}
	if bytes.Contains(raw, ticketID[:]) || bytes.Contains(raw, credentialID[:]) {
		t.Fatal("decoded capability exposes identity UUID bytes")
	}
	for _, forbidden := range []string{
		"qr1",
		ticketID.String(),
		publicid.Encode(publicid.Ticket, ticketID),
		credentialID.String(),
		publicid.Encode(publicid.Credential, credentialID),
		"Bearer",
		"?",
	} {
		if strings.Contains(first, forbidden) {
			t.Fatalf("capability %q contains forbidden value %q", first, forbidden)
		}
	}

	parsedTicket, parsedCredential, err := service.parseTicketQRPresentationCapability(first)
	if err != nil {
		t.Fatalf("parse capability: %v", err)
	}
	if parsedTicket != ticketID || parsedCredential != credentialID {
		t.Fatalf("parsed identities=%s/%s want=%s/%s", parsedTicket, parsedCredential, ticketID, credentialID)
	}

	other, err := service.TicketQRPresentationCapability(uuid.New(), credentialID)
	if err != nil {
		t.Fatalf("build other capability: %v", err)
	}
	if other == first {
		t.Fatal("different tickets received the same capability")
	}
	reissued, err := service.TicketQRPresentationCapability(ticketID, uuid.New())
	if err != nil {
		t.Fatalf("build reissued capability: %v", err)
	}
	if reissued == first {
		t.Fatal("different credentials received the same capability")
	}
}

func TestTicketQRPresentationCapabilityRejectsMalformedRandomAndTamperedValues(t *testing.T) {
	service := NewService(nil, nil, testTicketQRKeyring(t))
	capability, err := service.TicketQRPresentationCapability(uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("build capability: %v", err)
	}

	tampered := capability[:len(capability)-1] + "A"
	if tampered == capability {
		tampered = capability[:len(capability)-1] + "B"
	}

	for _, value := range []string{
		"",
		"random-capability",
		"tqp1_not-base64!",
		capability + "extra",
		tampered,
	} {
		_, _, parseErr := service.parseTicketQRPresentationCapability(value)
		apiErr, ok := apierror.As(parseErr)
		if !ok || apiErr.Code != apierror.CodeResourceNotFound {
			t.Fatalf("parse %q error=%v want safe not found", value, parseErr)
		}
	}
}

func testTicketQRKeyring(t *testing.T) *auth.HMACKeyring {
	t.Helper()
	key := make([]byte, 32)
	for index := range key {
		key[index] = byte(index + 1)
	}
	keyring, err := auth.ParseHMACKeyring(
		1,
		"1:"+base64.RawURLEncoding.EncodeToString(key),
	)
	if err != nil {
		t.Fatalf("parse keyring: %v", err)
	}
	return keyring
}
