package dingtalk

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

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

// fakeOutboundQueries is the DB surface Outbound reads, stubbed.
type fakeOutboundQueries struct {
	task            db.AgentTaskQueue
	channelIngested bool
	binding         db.ChannelChatSessionBinding
	inst            db.ChannelInstallation
}

func (f *fakeOutboundQueries) GetAgentTask(context.Context, pgtype.UUID) (db.AgentTaskQueue, error) {
	return f.task, nil
}

func (f *fakeOutboundQueries) TaskHasChannelIngestedMessages(context.Context, pgtype.UUID) (bool, error) {
	return f.channelIngested, nil
}

func (f *fakeOutboundQueries) GetChannelChatSessionBindingBySession(context.Context, db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error) {
	return f.binding, nil
}

func (f *fakeOutboundQueries) GetChannelInstallation(context.Context, db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	return f.inst, nil
}

func testUUID(b byte) pgtype.UUID {
	u := pgtype.UUID{Valid: true}
	for i := range u.Bytes {
		u.Bytes[i] = b
	}
	return u
}

// newCancelTestOutbound wires an Outbound over stub queries and the send server,
// with a DingTalk-ingested chat task bound to a group conversation.
func newCancelTestOutbound(t *testing.T, d *dingtalkSendServer) (*Outbound, *fakeOutboundQueries) {
	t.Helper()
	box := testBox(t)
	sealed, err := box.Seal([]byte("the-app-secret"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	cfg, err := json.Marshal(installConfig{
		AppID:              "appkey-1",
		RobotCode:          "appkey-1",
		AppSecretEncrypted: base64.StdEncoding.EncodeToString(sealed),
	})
	if err != nil {
		t.Fatalf("marshal install config: %v", err)
	}
	q := &fakeOutboundQueries{
		task:            db.AgentTaskQueue{ChatInputTaskID: testUUID(0x33)},
		channelIngested: true,
		binding: db.ChannelChatSessionBinding{
			InstallationID: testUUID(0x11),
			ChannelChatID:  "cid-1",
			Config:         json.RawMessage(`{"conversation_type":"2","conversation_id":"cid-1"}`),
		},
		inst: db.ChannelInstallation{ID: testUUID(0x11), Status: "active", Config: cfg},
	}
	o := NewOutbound(q, box.Open, NewClient(nil, d.srv.URL), slog.New(slog.NewTextHandler(io.Discard, nil)))
	return o, q
}

func cancelledEvent() events.Event {
	// The shape broadcastTaskEvent publishes for a cancel: ids on the envelope
	// and in the payload map, status "cancelled", and no content of any kind.
	return events.Event{
		Type:          protocol.EventTaskCancelled,
		TaskID:        "33333333-3333-3333-3333-333333333333",
		ChatSessionID: "22222222-2222-2222-2222-222222222222",
		Payload: map[string]any{
			"task_id":         "33333333-3333-3333-3333-333333333333",
			"chat_session_id": "22222222-2222-2222-2222-222222222222",
			"status":          "cancelled",
		},
	}
}

// DingTalk's processing indicator is not a reaction. The classic robot API this
// adapter sends through exposes none, so ack.go posts a real message promising
// a reply ("👀 On it — I'll reply here when it's ready"). A cancelled run
// publishes neither chat-done nor task-failed, so nothing follows that promise
// and it stands in the conversation for good. Closing the indicator here means
// withdrawing it.
//
// Published on a real bus rather than handed to handleEvent — the handler runs
// identically whether or not Register subscribed to task:cancelled, so a test
// calling it directly passes with the fix reverted.
func TestOutbound_TaskCancelledWithdrawsTheProcessingAck(t *testing.T) {
	d := newDingtalkSendServer(t)
	o, _ := newCancelTestOutbound(t, d)
	bus := events.New()
	o.Register(bus)

	bus.Publish(cancelledEvent())

	if n := atomic.LoadInt32(&d.sendCalls); n != 1 {
		t.Fatalf("the run was cancelled and DingTalk said nothing — the user is left "+
			"holding %q for a reply that is never coming (sends: %d)", ackProcessingText, n)
	}
	param, _ := d.lastBody["msgParam"].(string)
	if !strings.Contains(param, "cancelled") {
		t.Errorf("the notice must say the run was cancelled; msgParam = %q", param)
	}
}

// The counterweight to the test above: withdrawing the ack means posting a
// message, and a message must only go where the ack went. A run started in the
// browser against a session that also has a DingTalk binding never produced an
// ack in that room, so its cancellation must stay silent there — otherwise one
// "cancel all tasks" click announces itself in every DingTalk conversation the
// agent serves.
func TestOutbound_TaskCancelledStaysSilentForANonDingTalkRun(t *testing.T) {
	d := newDingtalkSendServer(t)
	o, q := newCancelTestOutbound(t, d)
	q.channelIngested = false
	bus := events.New()
	o.Register(bus)

	bus.Publish(cancelledEvent())

	if n := atomic.LoadInt32(&d.sendCalls); n != 0 {
		t.Fatalf("a web run's cancellation must not be announced in the DingTalk room; sends = %d", n)
	}
}
