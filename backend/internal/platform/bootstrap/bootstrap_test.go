package bootstrap

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/tktsync/tktsync/backend/internal/platform/config"
)

func TestOptionalSecurityResourcesMayBeUnset(t *testing.T) {
	human, err := optionalHumanVerifier(config.Supabase{})
	if err != nil {
		t.Fatalf("human verifier: %v", err)
	}

	if human != nil {
		t.Fatal("expected unset human verifier to be nil")
	}

	ring, err := optionalHMACKeyring(
		"selection",
		config.HMACKeyring{},
	)
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}

	if ring != nil {
		t.Fatal("expected unset keyring to be nil")
	}
}

func TestOptionalHumanVerifierRejectsPartialConfiguration(t *testing.T) {
	_, err := optionalHumanVerifier(config.Supabase{
		JWTIssuer: "https://issuer.example",
	})

	if err == nil {
		t.Fatal("expected partial human auth configuration to fail")
	}
}

func TestOptionalKeyringRejectsPartialConfiguration(t *testing.T) {
	_, err := optionalHMACKeyring(
		"selection",
		config.HMACKeyring{
			ActiveVersion: 1,
		},
	)

	if err == nil {
		t.Fatal("expected partial HMAC keyring configuration to fail")
	}
}

func TestOptionalKeyringParsesConfiguredVersion(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}

	ring, err := optionalHMACKeyring(
		"selection",
		config.HMACKeyring{
			ActiveVersion: 2,
			Keys: "1:" +
				base64.RawURLEncoding.EncodeToString(key) +
				",2:" +
				base64.RawURLEncoding.EncodeToString(key),
		},
	)
	if err != nil {
		t.Fatalf("configure keyring: %v", err)
	}

	if ring == nil || ring.ActiveVersion() != 2 {
		t.Fatalf("unexpected keyring: %#v", ring)
	}
}

func TestDifferentSecurityDomainsUseSeparateConfiguration(t *testing.T) {
	if strings.EqualFold(
		"SELECTION_KEYRING_KEYS",
		"RESERVATION_KEYRING_KEYS",
	) {
		t.Fatal("security domains must remain distinct")
	}
}
