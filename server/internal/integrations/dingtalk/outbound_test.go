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
	// inputBatch is what the task's chat_input_task_id owns. Empty is the
	// meaningful default: a task that reads as a web run through
	// channelIngested but owns no user messages is unclassifiable, not a web
	// run, and the two shapes must not behave alike.
	inputBatch []db.ChatMessage
	binding    db.ChannelChatSessionBinding
	bindingErr error
	inst       db.ChannelInstallation
}

func (f *fakeOutboundQueries) GetAgentTask(context.Context, pgtype.UUID) (db.AgentTaskQueue, error) {
	return f.task, nil
}

func (f *fakeOutboundQueries) TaskHasChannelIngestedMessages(context.Context, pgtype.UUID) (bool, error) {
	return f.channelIngested, nil
}

func (f *fakeOutboundQueries) ListChatInputMessages(context.Context, pgtype.UUID) ([]db.ChatMessage, error) {
	return f.inputBatch, nil
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
	// The shape a cancel is published with: ids on the envelope
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

func chatDoneEvent(taskID, content string) events.Event {
	return events.Event{
		Type:          protocol.EventChatDone,
		TaskID:        taskID,
		ChatSessionID: cancelTestSessionID,
		Payload: protocol.ChatDonePayload{
			TaskID:        taskID,
			ChatSessionID: cancelTestSessionID,
			Content:       content,
		},
	}
}

// asWebRun points the stub queries at a task that owns an input batch of
// messages the user typed in Multica — the one shape that proves a cancelled
// run is not the channel turn.
func asWebRun(q *fakeOutboundQueries) {
	q.channelIngested = false
	q.inputBatch = []db.ChatMessage{{Role: "user", Content: "typed into Multica"}}
}

// asChannelRun points them back at the turn the DingTalk message started.
func asChannelRun(q *fakeOutboundQueries) {
	q.channelIngested = true
	q.inputBatch = nil
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

	bus.Publish(chatDoneEvent(cancelTestTaskID, "here you go"))
	if n := atomic.LoadInt32(&d.sendCalls); n != 1 {
		t.Fatalf("setup: the reply should have been delivered once, sends = %d", n)
	}

	bus.Publish(cancelledEvent("44444444-4444-4444-4444-444444444444"))

	if n := atomic.LoadInt32(&d.sendCalls); n != 1 {
		t.Fatalf("the ack was already answered by the reply; a later cancel must not "+
			"withdraw it again (sends: %d)", n)
	}
}

// One chat session can be carrying two runs at once: the turn the DingTalk
// message started, and a turn the same user typed into Multica with the session
// open in the browser. The promise is recorded per session, so nothing in it
// distinguishes the two — and stopping the browser turn must not tell the room
// its answer is not coming, because it is, from the run still working.
func TestOutbound_WebRunCancelDoesNotWithdrawTheChannelTurnsAck(t *testing.T) {
	d := newDingtalkSendServer(t)
	o, ack, q := newCancelTestOutbound(t, d)
	postProcessingAck(t, ack)
	bus := events.New()
	o.Register(bus)

	asWebRun(q)
	bus.Publish(cancelledEvent("44444444-4444-4444-4444-444444444444"))

	if n := atomic.LoadInt32(&d.sendCalls); n != 0 {
		param, _ := d.lastBody["msgParam"].(string)
		t.Fatalf("stopping a run started in the browser posted %q into the DingTalk "+
			"room while the channel turn is still working (sends: %d)", param, n)
	}

	// The channel turn finishes and delivers the reply the ack promised. Had the
	// cancel above spoken, the room would have been told no reply was coming and
	// then handed one.
	asChannelRun(q)
	bus.Publish(chatDoneEvent(cancelTestTaskID, "here you go"))

	if n := atomic.LoadInt32(&d.sendCalls); n != 1 {
		t.Fatalf("the channel turn's reply did not land (sends: %d)", n)
	}
	if param, _ := d.lastBody["msgParam"].(string); !strings.Contains(param, "here you go") {
		t.Errorf("the last message in the room should be the reply; msgParam = %q", param)
	}
}

// The other half of the same rule: leaving the promise alone has to mean
// leaving it, not quietly consuming it. A gate that silences the web run's
// cancel but still takes the promise would leave the channel turn's own
// cancellation with nothing to withdraw — the exact indicator this PR is about,
// back again and harder to see.
func TestOutbound_ChannelTurnCancelStillSpeaksAfterAWebRunWasCancelled(t *testing.T) {
	d := newDingtalkSendServer(t)
	o, ack, q := newCancelTestOutbound(t, d)
	postProcessingAck(t, ack)
	bus := events.New()
	o.Register(bus)

	asWebRun(q)
	bus.Publish(cancelledEvent("44444444-4444-4444-4444-444444444444"))
	if n := atomic.LoadInt32(&d.sendCalls); n != 0 {
		t.Fatalf("setup: the web run's cancel must be silent, sends = %d", n)
	}

	asChannelRun(q)
	bus.Publish(cancelledEvent(cancelTestTaskID))

	if n := atomic.LoadInt32(&d.sendCalls); n != 1 {
		t.Fatalf("the channel turn was cancelled and the room still holds %q — an "+
			"earlier cancel on an unrelated run consumed its promise (sends: %d)",
			ackProcessingText, n)
	}
}

// Archiving a session removes its channel binding without cancelling what is
// running, so the run's ending arrives with nowhere to deliver it. That is still
// an ending, and the promise has to go with it — otherwise it waits on record
// for the next cancel in that conversation, which then posts "no reply is
// coming" about a run that finished.
func TestOutbound_AnEndingWithNoBindingLeftStillDischargesTheAck(t *testing.T) {
	d := newDingtalkSendServer(t)
	o, ack, q := newCancelTestOutbound(t, d)
	postProcessingAck(t, ack)
	bus := events.New()
	o.Register(bus)

	// The session was archived while the run was going: the binding is gone.
	q.bindingErr = pgx.ErrNoRows
	bus.Publish(chatDoneEvent(cancelTestTaskID, "the answer nobody can be sent"))
	if n := atomic.LoadInt32(&d.sendCalls); n != 0 {
		t.Fatalf("setup: there is no binding to deliver through, sends = %d", n)
	}

	// Any later cancel in this conversation must now find nothing owed.
	q.bindingErr = nil
	bus.Publish(cancelledEvent("44444444-4444-4444-4444-444444444444"))

	if n := atomic.LoadInt32(&d.sendCalls); n != 0 {
		t.Errorf("a later cancel posted %q for a run that had already ended — the "+
			"archived session's promise was never discharged (sends: %d)",
			ackProcessingText, n)
	}
}

// A run can end with nothing to post: an empty completion, an installation
// revoked mid-flight, a send that fails. The promise is discharged all the same,
// because the run it belongs to is over. Otherwise it sits on record until some
// later cancel in that conversation withdraws a promise nobody is waiting on.
func TestOutbound_EmptyCompletionStillDischargesTheAck(t *testing.T) {
	d := newDingtalkSendServer(t)
	o, ack, _ := newCancelTestOutbound(t, d)
	postProcessingAck(t, ack)
	bus := events.New()
	o.Register(bus)

	bus.Publish(chatDoneEvent(cancelTestTaskID, ""))
	if n := atomic.LoadInt32(&d.sendCalls); n != 0 {
		t.Fatalf("setup: an empty completion posts nothing, sends = %d", n)
	}

	bus.Publish(cancelledEvent("44444444-4444-4444-4444-444444444444"))

	if n := atomic.LoadInt32(&d.sendCalls); n != 0 {
		t.Fatalf("the run that made the ack already ended; a later cancel must not "+
			"withdraw its promise (sends: %d)", n)
	}
}

// A failure with a retry behind it is not an ending — the retry reports its own
// outcome — so it neither delivers nor discharges the promise. If it did, the
// retry's own cancellation would find nothing to withdraw.
func TestOutbound_RetryPendingFailureKeepsTheAckOutstanding(t *testing.T) {
	d := newDingtalkSendServer(t)
	o, ack, _ := newCancelTestOutbound(t, d)
	postProcessingAck(t, ack)
	bus := events.New()
	o.Register(bus)

	bus.Publish(events.Event{
		Type:          protocol.EventTaskFailed,
		TaskID:        cancelTestTaskID,
		ChatSessionID: cancelTestSessionID,
		Payload: map[string]any{
			"task_id":         cancelTestTaskID,
			"chat_session_id": cancelTestSessionID,
			"status":          "failed",
			"failure_reason":  "timeout",
			"retry_pending":   true,
		},
	})
	if n := atomic.LoadInt32(&d.sendCalls); n != 0 {
		t.Fatalf("setup: a retry-pending failure posts nothing, sends = %d", n)
	}

	bus.Publish(cancelledEvent(cancelTestTaskID))

	if n := atomic.LoadInt32(&d.sendCalls); n != 1 {
		t.Fatalf("the retry was cancelled and the room still holds %q — the failure "+
			"that preceded it discharged a promise the retry had not yet answered "+
			"(sends: %d)", ackProcessingText, n)
	}
}
