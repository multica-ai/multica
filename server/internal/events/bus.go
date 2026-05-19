package events

import (
	"log/slog"
	"sync"
)

// Event represents a domain event published by handlers or services.
type Event struct {
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

type listener struct {
	id      uint64
	handler Handler
}

// Bus is an in-process synchronous pub/sub event bus.
type Bus struct {
	mu             sync.RWMutex
	nextID         uint64
	listeners      map[string][]listener
	globalHandlers []listener
}

// New creates a new event bus.
func New() *Bus {
	return &Bus{
		listeners: make(map[string][]listener),
	}
}

// Subscribe registers a handler for a given event type.
// Handlers are called synchronously in registration order.
func (b *Bus) Subscribe(eventType string, h Handler) func() {
	b.mu.Lock()
	b.nextID++
	id := b.nextID
	b.listeners[eventType] = append(b.listeners[eventType], listener{id: id, handler: h})
	b.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.listeners[eventType] = removeListener(b.listeners[eventType], id)
		})
	}
}

// SubscribeAll registers a handler that receives ALL events regardless of type.
// Global handlers are called after type-specific handlers.
func (b *Bus) SubscribeAll(h Handler) func() {
	b.mu.Lock()
	b.nextID++
	id := b.nextID
	b.globalHandlers = append(b.globalHandlers, listener{id: id, handler: h})
	b.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.globalHandlers = removeListener(b.globalHandlers, id)
		})
	}
}

func removeListener(list []listener, id uint64) []listener {
	for i, candidate := range list {
		if candidate.id == id {
			copy(list[i:], list[i+1:])
			list[len(list)-1] = listener{}
			return list[:len(list)-1]
		}
	}
	return list
}

func handlersSnapshot(list []listener) []Handler {
	handlers := make([]Handler, len(list))
	for i, candidate := range list {
		handlers[i] = candidate.handler
	}
	return handlers
}

// Publish dispatches an event to all registered handlers for that event type.
// Type-specific handlers run first, then global (SubscribeAll) handlers.
// Each handler is called synchronously. Panics in individual handlers are
// recovered so one failing handler does not prevent others from executing.
func (b *Bus) Publish(e Event) {
	b.mu.RLock()
	handlers := handlersSnapshot(b.listeners[e.Type])
	globals := handlersSnapshot(b.globalHandlers)
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
