package auth

import (
	"encoding/base64"
	"testing"
)

func TestCanonicalIsUnambiguous(t *testing.T) {
	a := Canonical("ab", "c")
	b := Canonical("a", "bc")

	if string(a) == string(b) {
		t.Fatal("canonical encoding is ambiguous")
	}
}

func TestHMACKeyringSignsAndVerifies(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}

	raw := "1:" + base64.RawURLEncoding.EncodeToString(key)

	ring, err := ParseHMACKeyring(1, raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	message := Canonical("one", "two")

	mac, err := ring.MAC(1, message)
	if err != nil {
		t.Fatalf("MAC: %v", err)
	}

	if !ring.Verify(1, message, mac) {
		t.Fatal("expected MAC verification to pass")
	}

	if ring.Verify(1, Canonical("one", "changed"), mac) {
		t.Fatal("expected changed message to fail")
	}
}
