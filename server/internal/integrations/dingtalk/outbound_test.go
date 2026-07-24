package dingtalk

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestEventContent(t *testing.T) {
	cases := []struct {
		name  string
		event events.Event
		want  string
	}{
		{"chat done typed", events.Event{Type: protocol.EventChatDone, Payload: protocol.ChatDonePayload{Content: "reply"}}, "reply"},
		{
			"chat done prefers reply text",
			events.Event{Type: protocol.EventChatDone, Payload: protocol.ChatDonePayload{Content: "narration + reply", ReplyText: "reply"}},
			"reply",
		},
		{"quick create typed", events.Event{Type: protocol.EventQuickCreateDone, Payload: protocol.QuickCreateDonePayload{Content: "✅ MUL-1 — done"}}, "✅ MUL-1 — done"},
		{"map round trip", events.Event{Type: protocol.EventChatDone, Payload: map[string]any{"content": "from map"}}, "from map"},
		{
			"map round trip prefers reply text",
			events.Event{Type: protocol.EventChatDone, Payload: map[string]any{"content": "narration + reply", "reply_text": "reply"}},
			"reply",
		},
		{"empty map", events.Event{Type: protocol.EventChatDone, Payload: map[string]any{}}, ""},
		{"nil", events.Event{Type: protocol.EventChatDone}, ""},
		{
			"task failed with error",
			events.Event{Type: protocol.EventTaskFailed, Payload: map[string]any{"error": "task timed out", "retry_pending": false}},
			"⚠️ task timed out",
		},
		{
			// Failure broadcasts without an error text (a retry-pending
			// failure — the publisher omits `error` then — or the reaper
			// sweep) write no transcript message either — stay silent.
			"task failed without error",
			events.Event{Type: protocol.EventTaskFailed, Payload: map[string]any{"failure_reason": "timeout", "retry_pending": true}},
			"",
		},
		{
			// task:failed payloads never carry "content"; it must not leak
			// through the chat-done branch.
			"task failed ignores content key",
			events.Event{Type: protocol.EventTaskFailed, Payload: map[string]any{"content": "not for delivery"}},
			"",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eventContent(tc.event); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
