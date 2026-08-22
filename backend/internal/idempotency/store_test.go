package idempotency

import (
	"testing"

	"github.com/google/uuid"
)

func TestFingerprintStable(t *testing.T) {
	a := Fingerprint([]byte(`{"a":1}`))
	b := Fingerprint([]byte(`{"a":1}`))
	c := Fingerprint([]byte(`{"a":2}`))

	if string(a) != string(b) {
		t.Fatal("same canonical request did not produce same fingerprint")
	}

	if string(a) == string(c) {
		t.Fatal("different request produced same test fingerprint")
	}
}

func TestClaimInsertRequiresValidScope(t *testing.T) {
	_, _, err := claimInsert(
		Scope{
			Kind: "UNKNOWN",
			ID:   uuid.New(),
		},
		"TEST",
		"key",
		[]byte{1},
	)

	if err == nil {
		t.Fatal("expected unsupported scope to fail")
	}
}
