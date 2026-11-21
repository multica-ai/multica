package infoflow

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TestEventContent reads the deliverable out of both payload shapes: the typed
// struct the bus carries in-process and the map a Redis round trip produces.
func TestEventContent(t *testing.T) {
	tests := []struct {
		name  string
		event events.Event
		want  string
	}{
		{
			name: "typed chat done carries the reply",
			event: events.Event{
				Type: protocol.EventChatDone,
				Payload: protocol.ChatDonePayload{
					ChatSessionID: "11111111-1111-1111-1111-111111111111",
					Content:       "here you go",
				},
			},
			want: "here you go",
		},
		{
			name: "map chat done carries the reply",
			event: events.Event{
				Type:    protocol.EventChatDone,
				Payload: map[string]any{"content": "here you go"},
			},
			want: "here you go",
		},
		{
			name: "task failed carries its error sentence",
			event: events.Event{
				Type:    protocol.EventTaskFailed,
				Payload: map[string]any{"error": "the model rate-limited us"},
			},
			want: "⚠️ the model rate-limited us",
		},
		{
			// A failure the engine will retry is not news for the user: the
			// reply arrives when the task finally settles.
			name: "task failed with a retry queued stays silent",
			event: events.Event{
				Type:    protocol.EventTaskFailed,
				Payload: map[string]any{"error": "transient", "retry_pending": true},
			},
			want: "",
		},
		{
			name:  "task failed without an error sentence stays silent",
			event: events.Event{Type: protocol.EventTaskFailed, Payload: map[string]any{}},
			want:  "",
		},
		{
			name:  "an unrelated event stays silent",
			event: events.Event{Type: protocol.EventIssueCreated},
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := eventContent(tt.event); got != tt.want {
				t.Fatalf("eventContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestTaskAndSessionFromEvent covers both payload shapes. The typed payload is
// the in-process case; the map is what the Redis stream fanout hands a second
// replica.
func TestTaskAndSessionFromEvent(t *testing.T) {
	const (
		taskID    = "11111111-1111-1111-1111-111111111111"
		sessionID = "22222222-2222-2222-2222-222222222222"
	)
	t.Run("typed", func(t *testing.T) {
		gotTask, gotSession, ok := taskAndSessionFromEvent(events.Event{
			Type:    protocol.EventChatDone,
			Payload: protocol.ChatDonePayload{TaskID: taskID, ChatSessionID: sessionID},
		})
		if !ok || gotTask.String() != taskID || gotSession.String() != sessionID {
			t.Fatalf("got (%v, %v, %v), want (%s, %s, true)", gotTask, gotSession, ok, taskID, sessionID)
		}
	})
	t.Run("map", func(t *testing.T) {
		gotTask, gotSession, ok := taskAndSessionFromEvent(events.Event{
			Type:    protocol.EventChatDone,
			Payload: map[string]any{"task_id": taskID, "chat_session_id": sessionID},
		})
		if !ok || gotTask.String() != taskID || gotSession.String() != sessionID {
			t.Fatalf("got (%v, %v, %v), want (%s, %s, true)", gotTask, gotSession, ok, taskID, sessionID)
		}
	})
	t.Run("no task id means not deliverable", func(t *testing.T) {
		if _, _, ok := taskAndSessionFromEvent(events.Event{Type: protocol.EventChatDone}); ok {
			t.Fatal("ok = true, want false")
		}
	})
}

// TestProcessEvent_NeverQueriesWhenThereIsNothingToSend pins the cheap exit:
// issue/autopilot events and silent payloads return before any DB work, so a
// busy bus never pays a query per unrelated event. q is nil on purpose — a
// query would panic and fail the test.
func TestProcessEvent_NeverQueriesWhenThereIsNothingToSend(t *testing.T) {
	o := NewOutbound(nil, nil, nil)
	ctx := context.Background()

	if err := o.processEvent(ctx, events.Event{Type: protocol.EventIssueCreated}); err != nil {
		t.Fatalf("issue event: %v", err)
	}
	// A chat task with no text (retry pending) is silent too, even with a session.
	if err := o.processEvent(ctx, events.Event{
		Type:    protocol.EventTaskFailed,
		Payload: map[string]any{"task_id": "11111111-1111-1111-1111-111111111111", "chat_session_id": "22222222-2222-2222-2222-222222222222", "retry_pending": true},
	}); err != nil {
		t.Fatalf("retry-pending failure: %v", err)
	}
}
