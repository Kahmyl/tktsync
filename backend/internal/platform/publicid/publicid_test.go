package publicid

import (
	"testing"

	"github.com/google/uuid"
)

func TestRoundTrip(t *testing.T) {
	id := uuid.New()
	encoded := Encode(Event, id)

	decoded, err := Parse(encoded, Event)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if decoded != id {
		t.Fatalf("got %s, want %s", decoded, id)
	}
}

func TestRejectsWrongResourceNamespace(t *testing.T) {
	id := uuid.New()
	encoded := Encode(Event, id)

	if _, err := Parse(encoded, Ticket); err == nil {
		t.Fatal("expected wrong namespace to fail")
	}
}
