package realtimeapi

import (
	"testing"

	"github.com/google/uuid"
)

func TestHubScopesEventsAndSignalsBackpressure(t *testing.T) {
	hub := NewHub(1)
	eventA := uuid.New()
	eventB := uuid.New()

	a, cancelA := hub.Subscribe(eventA)
	defer cancelA()

	b, cancelB := hub.Subscribe(eventB)
	defer cancelB()

	first := Fact{FactID: uuid.New(), EventID: eventA, FactType: "event.updated"}
	hub.Publish(first)

	select {
	case got := <-a:
		if got.FactID != first.FactID {
			t.Fatal("wrong fact delivered")
		}
	default:
		t.Fatal("event-scoped subscriber did not receive fact")
	}

	select {
	case <-b:
		t.Fatal("cross-event realtime leak")
	default:
	}

	hub.Publish(Fact{FactID: uuid.New(), EventID: eventA, FactType: "inventory.changed"})
	hub.Publish(Fact{FactID: uuid.New(), EventID: eventA, FactType: "inventory.changed"})

	select {
	case got := <-a:
		if !got.Resync {
			t.Fatal("slow subscriber should receive resync signal")
		}
	default:
		t.Fatal("missing backpressure resync signal")
	}
}
