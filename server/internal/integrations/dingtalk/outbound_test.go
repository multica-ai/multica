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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
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
	bindingErr      error
	inst            db.ChannelInstallation
}

func (f *fakeOutboundQueries) GetAgentTask(context.Context, pgtype.UUID) (db.AgentTaskQueue, error) {
	return f.task, nil
}

func (f *fakeOutboundQueries) TaskHasChannelIngestedMessages(context.Context, pgtype.UUID) (bool, error) {
	return f.channelIngested, nil
}

func (f *fakeOutboundQueries) GetChannelChatSessionBindingBySession(context.Context, db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error) {
	return f.binding, f.bindingErr
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

const (
	cancelTestSessionID = "22222222-2222-2222-2222-222222222222"
	cancelTestTaskID    = "33333333-3333-3333-3333-333333333333"
)

// newCancelTestOutbound wires an Outbound and the ack notifier it shares with
// the inbound side over stub queries and the send server. The task is a channel
// run bound to a group conversation.
//
// The notifier's own send is stubbed out, so every request the send server
// counts is a cancellation notice and nothing else.
func newCancelTestOutbound(t *testing.T, d *dingtalkSendServer) (*Outbound, *ackNotifier, *fakeOutboundQueries) {
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
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ack := NewAckNotifier(NewClient(nil, d.srv.URL), box.Open, logger)
	ack.sendText = func(context.Context, engine.ResolvedInstallation, channel.InboundMessage, string) error {
		return nil
	}
	o := NewOutbound(q, box.Open, NewClient(nil, d.srv.URL), ack, logger)
	return o, ack, q
}

// postProcessingAck drives the inbound half: a DingTalk message lands in the
// group and the notifier posts "👀 On it" into it, which is the promise a cancel
// then has to withdraw.
func postProcessingAck(t *testing.T, ack *ackNotifier) {
	t.Helper()
	var sessionID pgtype.UUID
	if err := sessionID.Scan(cancelTestSessionID); err != nil {
		t.Fatalf("scan session id: %v", err)
	}
	ack.OnIngested(context.Background(),
		engine.ResolvedInstallation{ID: testUUID(0x11)},
		channel.InboundMessage{
			MessageID: "msg-1",
			Source: channel.Source{
				ChatID:   "cid-1",
				ChatType: channel.ChatTypeGroup,
				SenderID: "staff-1",
			},
		},
		sessionID)
}

func cancelledEvent(taskID string) events.Event {
	// The shape broadcastTaskEvent publishes for a cancel: ids on the envelope
	// and in the payload map, status "cancelled", and no content of any kind.
	return events.Event{
		Type:          protocol.EventTaskCancelled,
		TaskID:        taskID,
		ChatSessionID: cancelTestSessionID,
		Payload: map[string]any{
			"task_id":         taskID,
			"chat_session_id": cancelTestSessionID,
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
// The task is given the shape the #6611 report had: it owns an input batch
// (ChatInputTaskID set) holding no channel-ingested message, so the provenance
// query calls it a web run. That is the production case, and a notice gated on
// that query is skipped on exactly the conversation still holding the ack.
//
// Published on a real bus rather than handed to handleEvent — the handler runs
// identically whether or not Register subscribed to task:cancelled, so a test
// calling it directly passes with the fix reverted.
func TestOutbound_TaskCancelledWithdrawsAckForAnEmptyInputBatch(t *testing.T) {
	d := newDingtalkSendServer(t)
	o, ack, q := newCancelTestOutbound(t, d)
	q.channelIngested = false
	postProcessingAck(t, ack)
	bus := events.New()
	o.Register(bus)

	bus.Publish(cancelledEvent(cancelTestTaskID))

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
func TestOutbound_TaskCancelledStaysSilentWhenNoAckIsOutstanding(t *testing.T) {
	d := newDingtalkSendServer(t)
	o, _, _ := newCancelTestOutbound(t, d)
	bus := events.New()
	o.Register(bus)

	bus.Publish(cancelledEvent(cancelTestTaskID))

	if n := atomic.LoadInt32(&d.sendCalls); n != 0 {
		t.Fatalf("a run that never acked in this room must not be announced there; sends = %d", n)
	}
}

// Cancel is broadcast once per task row, so a "cancel all tasks" click, or a
// session delete carrying several queued turns, delivers several events for one
// conversation. The user made one request and is owed one message about it, not
// a run of identical ones.
func TestOutbound_BulkCancelWithdrawsTheAckOnce(t *testing.T) {
	d := newDingtalkSendServer(t)
	o, ack, _ := newCancelTestOutbound(t, d)
	postProcessingAck(t, ack)
	bus := events.New()
	o.Register(bus)

	for _, taskID := range []string{
		"33333333-3333-3333-3333-333333333333",
		"44444444-4444-4444-4444-444444444444",
		"55555555-5555-5555-5555-555555555555",
	} {
		bus.Publish(cancelledEvent(taskID))
	}

	if n := atomic.LoadInt32(&d.sendCalls); n != 1 {
		t.Fatalf("three tasks cancelled in one call put %d copies of the same notice "+
			"into one conversation; the user is owed exactly one", n)
	}
}

// Deleting a chat session cancels its queued turns and deletes the DingTalk
// binding in one transaction, then broadcasts the cancels after that
// transaction commits. The binding is gone by the time they arrive, so the
// notice has to be addressed from what the ack itself recorded.
func TestOutbound_TaskCancelledWithdrawsAckAfterTheBindingIsGone(t *testing.T) {
	d := newDingtalkSendServer(t)
	o, ack, q := newCancelTestOutbound(t, d)
	postProcessingAck(t, ack)
	q.binding = db.ChannelChatSessionBinding{}
	q.bindingErr = pgx.ErrNoRows
	bus := events.New()
	o.Register(bus)

	bus.Publish(cancelledEvent(cancelTestTaskID))

	if n := atomic.LoadInt32(&d.sendCalls); n != 1 {
		t.Fatalf("the session was deleted and the room keeps %q with nothing after it "+
			"(sends: %d)", ackProcessingText, n)
	}
	if d.lastPath != pathSendGroup {
		t.Errorf("the notice must go back to the group the ack went into; path = %q", d.lastPath)
	}
}

// A reply that actually lands answers the promise, so a later cancel on the same
// session has nothing to withdraw. Without this the next cancelled turn — a web
// one, say — would post a withdrawal for a promise the room already saw kept.
func TestOutbound_ReplyDeliveredLeavesNothingToWithdraw(t *testing.T) {
	d := newDingtalkSendServer(t)
	o, ack, _ := newCancelTestOutbound(t, d)
	postProcessingAck(t, ack)
	bus := events.New()
	o.Register(bus)

	bus.Publish(events.Event{
		Type:          protocol.EventChatDone,
		TaskID:        cancelTestTaskID,
		ChatSessionID: cancelTestSessionID,
		Payload: protocol.ChatDonePayload{
			TaskID:        cancelTestTaskID,
			ChatSessionID: cancelTestSessionID,
			Content:       "here you go",
		},
	})
	if n := atomic.LoadInt32(&d.sendCalls); n != 1 {
		t.Fatalf("setup: the reply should have been delivered once, sends = %d", n)
	}

	bus.Publish(cancelledEvent("44444444-4444-4444-4444-444444444444"))

	if n := atomic.LoadInt32(&d.sendCalls); n != 1 {
		t.Fatalf("the ack was already answered by the reply; a later cancel must not "+
			"withdraw it again (sends: %d)", n)
	}
}
