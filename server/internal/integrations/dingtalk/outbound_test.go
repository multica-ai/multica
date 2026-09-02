package dingtalk

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type noDeliveryOutboundQueries struct{}

func (noDeliveryOutboundQueries) GetChannelTaskDelivery(context.Context, pgtype.UUID) (db.ChannelTaskDelivery, error) {
	return db.ChannelTaskDelivery{}, pgx.ErrNoRows
}
func (noDeliveryOutboundQueries) GetAgentTask(context.Context, pgtype.UUID) (db.AgentTaskQueue, error) {
	panic("GetAgentTask must not run without a task delivery snapshot")
}
func (noDeliveryOutboundQueries) TaskHasChannelIngestedMessages(context.Context, pgtype.UUID) (bool, error) {
	panic("TaskHasChannelIngestedMessages must not run without a task delivery snapshot")
}
func (noDeliveryOutboundQueries) GetChannelInstallation(context.Context, db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	panic("GetChannelInstallation must not run without a task delivery snapshot")
}

func TestOutboundFailsClosedWithoutTaskDeliverySnapshot(t *testing.T) {
	o := NewOutbound(noDeliveryOutboundQueries{}, nil, nil, nil)
	event := events.Event{
		Type:          protocol.EventChatDone,
		TaskID:        "11111111-1111-1111-1111-111111111111",
		ChatSessionID: "22222222-2222-2222-2222-222222222222",
		Payload:       protocol.ChatDonePayload{Content: "must stay in Multica"},
	}
	if err := o.processEvent(context.Background(), event); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
}

func TestEventContent(t *testing.T) {
	cases := []struct {
		name  string
		event events.Event
		want  string
	}{
		{"chat done typed", events.Event{Type: protocol.EventChatDone, Payload: protocol.ChatDonePayload{Content: "reply"}}, "reply"},
		{"map round trip", events.Event{Type: protocol.EventChatDone, Payload: map[string]any{"content": "from map"}}, "from map"},
		{"empty map", events.Event{Type: protocol.EventChatDone, Payload: map[string]any{}}, ""},
		{"nil", events.Event{Type: protocol.EventChatDone}, ""},
		{
			"task failed with error",
			events.Event{Type: protocol.EventTaskFailed, Payload: map[string]any{"error": "task timed out", "retry_pending": false}},
			"⚠️ task timed out",
		},
		{
			// Retry-pending failures stay silent even if a mixed-version
			// publisher accidentally includes an error string.
			"task failed with retry pending",
			events.Event{Type: protocol.EventTaskFailed, Payload: map[string]any{"error": "task timed out", "failure_reason": "timeout", "retry_pending": true}},
			"",
		},
		{
			// Failure broadcasts without an error text have nothing safe to
			// deliver and stay silent.
			"task failed without error",
			events.Event{Type: protocol.EventTaskFailed, Payload: map[string]any{"failure_reason": "timeout", "retry_pending": false}},
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
