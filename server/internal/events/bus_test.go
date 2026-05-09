package events

import (
	"sync/atomic"
	"testing"
)

func TestPublishDeliversToSubscribers(t *testing.T) {
	bus := New()
	var count int32

	bus.Subscribe("test:event", func(e Event) {
		atomic.AddInt32(&count, 1)
	})
	bus.Subscribe("test:event", func(e Event) {
		atomic.AddInt32(&count, 1)
	})

	bus.Publish(Event{Type: "test:event", Payload: "hello"})

	if count != 2 {
		t.Errorf("expected 2 handlers called, got %d", count)
	}
}

func TestPublishOnlyMatchingType(t *testing.T) {
	bus := New()
	var called bool

	bus.Subscribe("type:a", func(e Event) {
		called = true
	})

	bus.Publish(Event{Type: "type:b"})

	if called {
		t.Error("handler for type:a should not be called for type:b event")
	}
}

func TestPublishNoSubscribersIsNoop(t *testing.T) {
	bus := New()
	// Should not panic
	bus.Publish(Event{Type: "no:listeners"})
}

func TestPanicInHandlerDoesNotBreakOthers(t *testing.T) {
	bus := New()
	var secondCalled bool

	bus.Subscribe("test:panic", func(e Event) {
		panic("handler panic")
	})
	bus.Subscribe("test:panic", func(e Event) {
		secondCalled = true
	})

	bus.Publish(Event{Type: "test:panic"})

	if !secondCalled {
		t.Error("second handler should still be called after first panics")
	}
}

func TestSubscribeAllReceivesAllEventTypes(t *testing.T) {
	bus := New()

	var received []string
	bus.SubscribeAll(func(e Event) {
		received = append(received, e.Type)
	})

	bus.Publish(Event{Type: "issue:created"})
	bus.Publish(Event{Type: "comment:deleted"})
	bus.Publish(Event{Type: "skill:updated"})

	if len(received) != 3 {
		t.Fatalf("expected 3 events, got %d", len(received))
	}
	if received[0] != "issue:created" || received[1] != "comment:deleted" || received[2] != "skill:updated" {
		t.Fatalf("unexpected events: %v", received)
	}
}

func TestSubscribeAllCalledAfterTypeSpecific(t *testing.T) {
	bus := New()

	var order []string
	bus.Subscribe("issue:created", func(e Event) {
		order = append(order, "specific")
	})
	bus.SubscribeAll(func(e Event) {
		order = append(order, "global")
	})

	bus.Publish(Event{Type: "issue:created"})

	if len(order) != 2 || order[0] != "specific" || order[1] != "global" {
		t.Fatalf("expected [specific, global], got %v", order)
	}
}

func TestSubscribeAllPanicRecovery(t *testing.T) {
	bus := New()

	var secondCalled bool
	bus.SubscribeAll(func(e Event) {
		panic("test panic")
	})
	bus.SubscribeAll(func(e Event) {
		secondCalled = true
	})

	bus.Publish(Event{Type: "test"})

	if !secondCalled {
		t.Fatal("second global handler was not called after first panicked")
	}
}

func TestEventFieldsPassedThrough(t *testing.T) {
	bus := New()
	var received Event

	bus.Subscribe("test:fields", func(e Event) {
		received = e
	})

	bus.Publish(Event{
		Type:        "test:fields",
		WorkspaceID: "ws-123",
		ActorType:   "member",
		ActorID:     "user-456",
		Payload:     map[string]string{"key": "value"},
	})

	if received.WorkspaceID != "ws-123" {
		t.Errorf("expected WorkspaceID ws-123, got %s", received.WorkspaceID)
	}
	if received.ActorType != "member" {
		t.Errorf("expected ActorType member, got %s", received.ActorType)
	}
	if received.ActorID != "user-456" {
		t.Errorf("expected ActorID user-456, got %s", received.ActorID)
	}
}
