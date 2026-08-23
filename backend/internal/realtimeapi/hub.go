package realtimeapi

import (
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
)

type Fact struct {
	FactID        uuid.UUID
	EventID       uuid.UUID
	FactType      string
	AggregateType string
	AggregateID   *uuid.UUID
	Resync        bool
}

type subscriber struct {
	eventID uuid.UUID
	events  chan Fact
}

type Hub struct {
	mu        sync.RWMutex
	next      atomic.Uint64
	queueSize int
	items     map[uint64]subscriber
}

func NewHub(queueSize int) *Hub {
	if queueSize < 1 {
		queueSize = 32
	}
	return &Hub{
		queueSize: queueSize,
		items:     make(map[uint64]subscriber),
	}
}

func (h *Hub) Subscribe(eventID uuid.UUID) (<-chan Fact, func()) {
	id := h.next.Add(1)
	ch := make(chan Fact, h.queueSize)

	h.mu.Lock()
	h.items[id] = subscriber{eventID: eventID, events: ch}
	h.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.items, id)
			h.mu.Unlock()
		})
	}

	return ch, cancel
}

func (h *Hub) Publish(fact Fact) {
	if h == nil || fact.EventID == uuid.Nil {
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, sub := range h.items {
		if sub.eventID != fact.EventID {
			continue
		}

		select {
		case sub.events <- fact:
		default:
			select {
			case <-sub.events:
			default:
			}

			select {
			case sub.events <- Fact{
				EventID: fact.EventID,
				Resync:  true,
			}:
			default:
			}
		}
	}
}

func (h *Hub) SubscriberCount() int {
	if h == nil {
		return 0
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.items)
}
