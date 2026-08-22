package auth

import (
	"encoding/base64"
	"testing"
)

func TestGeneratedPartnerCredentialFormat(t *testing.T) {
	generated, err := GeneratePartnerCredential()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	keyID, secret, err := ParsePartnerCredential(generated.Encoded)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if keyID != generated.KeyID {
		t.Fatalf("unexpected key ID %q", keyID)
	}

	if secret != generated.Secret {
		t.Fatal("unexpected secret")
	}

	decoded, err := base64.RawURLEncoding.DecodeString(secret)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}

	if len(decoded) != 32 {
		t.Fatalf("secret entropy length = %d, want 32 bytes", len(decoded))
	}

	hash, err := partnerSecretHash(secret)
	if err != nil {
		t.Fatalf("hash secret: %v", err)
	}

	if string(hash) != string(generated.SecretHash) {
		t.Fatal("secret hash mismatch")
	}
}

func TestPartnerCredentialRejectsMalformedMaterial(t *testing.T) {
	values := []string{
		"",
		"wrong_prefix",
		"tkp_bad_bad",
		"tkp_0011_not-base64",
	}

	for _, value := range values {
		if _, _, err := ParsePartnerCredential(value); err == nil {
			t.Fatalf("expected %q to fail", value)
		}
	}
}
