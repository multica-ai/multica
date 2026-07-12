package chatstream

import (
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func recvEvent(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for broker event")
		return Event{}
	}
}

func TestBrokerDeliversChatDoneToSessionSubscriber(t *testing.T) {
	bus := events.New()
	b := NewBroker(bus)

	sub, cancel := b.Subscribe("session-1")
	defer cancel()

	bus.Publish(events.Event{
		Type:          protocol.EventChatDone,
		WorkspaceID:   "ws-1",
		ChatSessionID: "session-1",
		Payload: protocol.ChatDonePayload{
			ChatSessionID: "session-1",
			TaskID:        "task-1",
			MessageID:     "msg-1",
			Content:       "the answer",
			ElapsedMs:     900,
		},
	})

	ev := recvEvent(t, sub)
	if ev.Type != EventDone {
		t.Errorf("Type = %q, want %q", ev.Type, EventDone)
	}
	if ev.TaskID != "task-1" || ev.MessageID != "msg-1" || ev.Content != "the answer" || ev.ElapsedMs != 900 {
		t.Errorf("unexpected event: %+v", ev)
	}
}

func TestBrokerIgnoresOtherSessions(t *testing.T) {
	bus := events.New()
	b := NewBroker(bus)

	sub, cancel := b.Subscribe("session-1")
	defer cancel()

	bus.Publish(events.Event{
		Type:          protocol.EventChatDone,
		ChatSessionID: "session-OTHER",
		Payload:       protocol.ChatDonePayload{ChatSessionID: "session-OTHER", TaskID: "t"},
	})

	select {
	case ev := <-sub:
		t.Fatalf("received event for foreign session: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestBrokerMapsTaskFailedPayload(t *testing.T) {
	bus := events.New()
	b := NewBroker(bus)

	sub, cancel := b.Subscribe("session-1")
	defer cancel()

	// broadcastTaskEvent publishes task:* events with a map payload and no
	// Event.ChatSessionID hint — the broker must read chat_session_id from
	// the payload map.
	bus.Publish(events.Event{
		Type: protocol.EventTaskFailed,
		Payload: map[string]any{
			"task_id":         "task-9",
			"status":          "failed",
			"chat_session_id": "session-1",
		},
	})

	ev := recvEvent(t, sub)
	if ev.Type != EventFailed {
		t.Errorf("Type = %q, want %q", ev.Type, EventFailed)
	}
	if ev.TaskID != "task-9" {
		t.Errorf("TaskID = %q, want task-9", ev.TaskID)
	}
}

func TestBrokerMapsTaskCancelledPayload(t *testing.T) {
	bus := events.New()
	b := NewBroker(bus)

	sub, cancel := b.Subscribe("session-1")
	defer cancel()

	bus.Publish(events.Event{
		Type: protocol.EventTaskCancelled,
		Payload: map[string]any{
			"task_id":         "task-3",
			"status":          "cancelled",
			"chat_session_id": "session-1",
		},
	})

	ev := recvEvent(t, sub)
	if ev.Type != EventCancelled {
		t.Errorf("Type = %q, want %q", ev.Type, EventCancelled)
	}
}

func TestBrokerCancelStopsDelivery(t *testing.T) {
	bus := events.New()
	b := NewBroker(bus)

	sub, cancel := b.Subscribe("session-1")
	cancel()

	bus.Publish(events.Event{
		Type:          protocol.EventChatDone,
		ChatSessionID: "session-1",
		Payload:       protocol.ChatDonePayload{ChatSessionID: "session-1", TaskID: "t"},
	})

	select {
	case ev, ok := <-sub:
		if ok {
			t.Fatalf("received event after cancel: %+v", ev)
		}
	case <-time.After(50 * time.Millisecond):
	}
}

// TestBrokerDoesNotBlockBusOnSlowSubscriber fills a subscriber's buffer and
// verifies Publish still returns — the bus is synchronous, so a blocking
// send here would freeze every handler that publishes chat events.
func TestBrokerDoesNotBlockBusOnSlowSubscriber(t *testing.T) {
	bus := events.New()
	b := NewBroker(bus)

	_, cancel := b.Subscribe("session-1")
	defer cancel()

	done := make(chan struct{})
	go func() {
		for i := 0; i < subscriberBuffer+8; i++ {
			bus.Publish(events.Event{
				Type:          protocol.EventChatDone,
				ChatSessionID: "session-1",
				Payload:       protocol.ChatDonePayload{ChatSessionID: "session-1", TaskID: "t"},
			})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a full subscriber buffer")
	}
}
