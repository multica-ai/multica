package events

import (
	"log/slog"
	"sync"

	"github.com/google/uuid"
)

// Event represents a domain event published by handlers or services.
type Event struct {
	// ID is a UUIDv7 minted at Publish time. Stable across all listeners
	// (realtime, webhooks, audit). Sortable. Subscribers can dedup on it
	// across retries since every retry of the same delivery preserves the
	// event_id while a separate delivery_id changes per attempt batch.
	// See RFC #1964 (issue/1964) for the rationale.
	ID string

	Type        string // e.g. "issue:created", "inbox:new"
	WorkspaceID string // routes to correct Hub room
	ActorType   string // "member", "agent", or "system"
	ActorID     string
	Payload     any // JSON-serializable, same shape as current WS payloads

	// Optional scope hints used by the realtime fanout layer to route the
	// event to a more specific scope than `workspace:{WorkspaceID}`. When set
	// these tell the listener which Redis stream / Hub room to publish on
	// without re-deserializing Payload. See MUL-1138 phase 1.
	TaskID        string
	ChatSessionID string
}

// Handler is a function that processes an event.
type Handler func(Event)

// Bus is an in-process synchronous pub/sub event bus.
type Bus struct {
	mu             sync.RWMutex
	listeners      map[string][]Handler
	globalHandlers []Handler
}

// New creates a new event bus.
func New() *Bus {
	return &Bus{
		listeners: make(map[string][]Handler),
	}
}

// Subscribe registers a handler for a given event type.
// Handlers are called synchronously in registration order.
func (b *Bus) Subscribe(eventType string, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.listeners[eventType] = append(b.listeners[eventType], h)
}

// SubscribeAll registers a handler that receives ALL events regardless of type.
// Global handlers are called after type-specific handlers.
func (b *Bus) SubscribeAll(h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.globalHandlers = append(b.globalHandlers, h)
}

// Publish dispatches an event to all registered handlers for that event type.
// Type-specific handlers run first, then global (SubscribeAll) handlers.
// Each handler is called synchronously. Panics in individual handlers are
// recovered so one failing handler does not prevent others from executing.
//
// If the caller did not set Event.ID, Publish mints a UUIDv7 here so every
// listener (realtime hub, webhook dispatcher, future audit log) sees the
// same stable identifier. Callers that need to set a specific ID (e.g. tests
// reproducing a known sequence) can populate it before calling Publish.
func (b *Bus) Publish(e Event) {
	if e.ID == "" {
		if id, err := uuid.NewV7(); err == nil {
			e.ID = id.String()
		} else {
			// Fallback to v4 if v7 fails (vanishingly unlikely on a host
			// with a working entropy source). Still better than the empty
			// string which would force every listener to mint its own.
			e.ID = uuid.NewString()
		}
	}

	b.mu.RLock()
	handlers := b.listeners[e.Type]
	globals := b.globalHandlers
	b.mu.RUnlock()

	for _, h := range handlers {
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("panic in event listener", "event_type", e.Type, "recovered", r)
				}
			}()
			h(e)
		}()
	}

	for _, h := range globals {
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("panic in global event listener", "event_type", e.Type, "recovered", r)
				}
			}()
			h(e)
		}()
	}
}
