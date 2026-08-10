package dingtalk

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
	// pendingTurn is what agent_task_queue says about the session: whether a
	// chat turn is still queued, running or waiting. False is the meaningful
	// default — a cancel is broadcast after its own row is already terminal, so
	// a session whose only run was the cancelled one reads as idle here.
	pendingTurn    bool
	pendingTurnErr error
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

func (f *fakeOutboundQueries) HasPendingChatTurnForSession(context.Context, pgtype.UUID) (bool, error) {
	return f.pendingTurn, f.pendingTurnErr
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
		mustScanUUID(t, cancelTestSessionID))
}

func mustScanUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		t.Fatalf("scan uuid %q: %v", s, err)
	}
	return u
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

func TestOutbound_InterleavedRoundsLoseTheSecondPromise(t *testing.T) {
	const taskA = "33333333-3333-3333-3333-333333333333"
	const taskB = "44444444-4444-4444-4444-444444444444"

	d := newDingtalkSendServer(t)
	o, ack, q := newCancelTestOutbound(t, d)

	base := time.Now()
	now := base
	ack.now = func() time.Time { return now }

	postProcessingAck(t, ack) // round A promises a reply
	now = base.Add(10 * time.Second)
	postProcessingAck(t, ack) // round B, past the window, promises its own

	bus := events.New()
	o.Register(bus)

	asChannelRun(q)
	bus.Publish(chatDoneEvent(taskA, "here you go")) // A ends, reply delivered
	afterA := atomic.LoadInt32(&d.sendCalls)

	bus.Publish(cancelledEvent(taskB)) // B cancelled, ack still standing

	if n := atomic.LoadInt32(&d.sendCalls) - afterA; n != 1 {
		t.Fatalf("round B was cancelled while its own ack was still standing in the room; "+
			"cancellation notices posted = %d, want 1", n)
	}
}

// postTwoAckedRounds drives the inbound half twice with the second turn past the
// coalesce window, which is the shape the window is sized for: a burst yields one
// ack, a genuinely later turn acks again. The room ends up holding two "👀 On it"
// messages and is owed two endings.
func postTwoAckedRounds(t *testing.T, ack *ackNotifier) {
	t.Helper()
	base := time.Unix(1700000000, 0)
	now := base
	ack.now = func() time.Time { return now }
	postProcessingAck(t, ack)
	now = base.Add(2 * ackCoalesceWindow)
	postProcessingAck(t, ack)
}

// The dedupe that holds a bulk cancel to one message used to be a side effect of
// there being at most one promise to take. With two rounds acked there are two,
// and every cancelled row would find one — so a session delete carrying both
// turns would put two identical notices into one conversation. The user made one
// request to stop and is owed one answer to it.
//
// The third event is the same bulk cancel arriving for a row that never acked
// here; it must not add a message either.
func TestOutbound_BulkCancelOfTwoAckedRoundsSpeaksOnce(t *testing.T) {
	d := newDingtalkSendServer(t)
	o, ack, _ := newCancelTestOutbound(t, d)
	postTwoAckedRounds(t, ack)
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
		t.Fatalf("one session delete stopped two acked rounds and put %d copies of the "+
			"same notice into one conversation; the user is owed exactly one", n)
	}
	param, _ := d.lastBody["msgParam"].(string)
	if !strings.Contains(param, "cancelled") {
		t.Errorf("the one message the room gets must be the cancellation notice; msgParam = %q", param)
	}
}

// The counterweight: with one round stopped and another still running, a reply
// really is still coming to that room, and "no reply is coming for it" cannot say
// which round it means. So the notice waits, and the round still working delivers
// into a room that was not told to give up on it.
//
// What makes the room "still owed" is the task queue, not the promise count. Two
// turns inside the 5s coalesce window share one ack while running as two tasks,
// so a room with a single promise can still have a reply on the way — the state
// a count of promises reads as owing nothing.
func TestOutbound_OneRoundStoppedWhileAnotherIsStillRunningStaysSilent(t *testing.T) {
	d := newDingtalkSendServer(t)
	o, ack, q := newCancelTestOutbound(t, d)
	postProcessingAck(t, ack)
	bus := events.New()
	o.Register(bus)

	asChannelRun(q)
	q.pendingTurn = true
	bus.Publish(cancelledEvent("44444444-4444-4444-4444-444444444444"))
	if n := atomic.LoadInt32(&d.sendCalls); n != 0 {
		param, _ := d.lastBody["msgParam"].(string)
		t.Fatalf("one of two turns sharing an ack was stopped and the room was told %q "+
			"while the other was still working (sends: %d)", param, n)
	}

	q.pendingTurn = false
	bus.Publish(chatDoneEvent(cancelTestTaskID, "here you go"))

	if n := atomic.LoadInt32(&d.sendCalls); n != 1 {
		t.Fatalf("the round that was still working did not deliver (sends: %d)", n)
	}
	if param, _ := d.lastBody["msgParam"].(string); !strings.Contains(param, "here you go") {
		t.Errorf("the last message in the room should be the reply; msgParam = %q", param)
	}
}

// A turn whose flush enqueues nothing — the agent went offline or was archived
// between ingest and flush — settles without any task lifecycle event, so the
// engine closes that turn's promise directly. It must close that turn's and stop
// there: an earlier round acked in the same room belongs to a run that is still
// going, and taking its promise here would leave its own cancellation with
// nothing to withdraw, which is the stuck "👀 On it" this path exists to prevent.
func TestOutbound_SettleClosesOneRoundNotTheRoomsWholeQueue(t *testing.T) {
	d := newDingtalkSendServer(t)
	o, ack, _ := newCancelTestOutbound(t, d)
	postTwoAckedRounds(t, ack)
	bus := events.New()
	o.Register(bus)

	// The second turn's flush found no runtime to enqueue against.
	ack.OnSettled(context.Background(), mustScanUUID(t, cancelTestSessionID))

	bus.Publish(cancelledEvent(cancelTestTaskID))

	if n := atomic.LoadInt32(&d.sendCalls); n != 1 {
		t.Fatalf("the round that was actually running was cancelled and the room still "+
			"holds %q — settling the other turn took its promise too (sends: %d)",
			ackProcessingText, n)
	}
}

// countBodies reports how many of the server's sends carried the given text.
func countBodies(bodies []string, want string) int {
	n := 0
	for _, b := range bodies {
		if strings.Contains(b, want) {
			n++
		}
	}
	return n
}

// The router posts the ack on a detached goroutine, so a short run can report
// its ending before that send returns. The promise is recorded after the send,
// so that ending finds nothing to discharge — and the promise then lands in a
// room whose round is already over.
//
// One such promise used to sit at the head of the room's queue until the
// day-long sweep. Every later cancel there popped it, found the room still
// reading as owed a reply, and stayed silent: one lost ending, and the
// conversation never heard about a cancellation again.
func TestOutbound_ARunThatOutranItsOwnAckStillLetsLaterCancelsSpeak(t *testing.T) {
	d := newDingtalkSendServer(t)
	o, ack, q := newCancelTestOutbound(t, d)
	base := time.Unix(1700000000, 0)
	now := base
	ack.now = func() time.Time { return now }
	bus := events.New()
	o.Register(bus)

	entered := make(chan struct{})
	release := make(chan struct{})
	ack.sendText = func(context.Context, engine.ResolvedInstallation, channel.InboundMessage, string) error {
		close(entered)
		<-release
		return nil
	}
	sessionID := mustScanUUID(t, cancelTestSessionID)
	ingested := make(chan struct{})
	go func() {
		defer close(ingested)
		ack.OnIngested(context.Background(), engine.ResolvedInstallation{ID: testUUID(0x11)},
			channel.InboundMessage{MessageID: "msg-1", Source: channel.Source{
				ChatID: "cid-1", ChatType: channel.ChatTypeGroup, SenderID: "staff-1",
			}}, sessionID)
	}()

	<-entered
	bus.Publish(chatDoneEvent(cancelTestTaskID, "round one, answered"))
	close(release)
	<-ingested

	afterRoundOne := atomic.LoadInt32(&d.sendCalls)
	if afterRoundOne != 1 {
		t.Fatalf("setup: round one should have delivered its reply once, sends = %d", afterRoundOne)
	}

	// A later turn in the same conversation, past the coalesce window, acks and
	// is cancelled.
	ack.sendText = func(context.Context, engine.ResolvedInstallation, channel.InboundMessage, string) error {
		return nil
	}
	now = base.Add(2 * ackCoalesceWindow)
	postProcessingAck(t, ack)
	asChannelRun(q)
	bus.Publish(cancelledEvent("44444444-4444-4444-4444-444444444444"))

	if n := atomic.LoadInt32(&d.sendCalls) - afterRoundOne; n != 1 {
		t.Fatalf("a later round was cancelled and the room was told %d times, want 1: "+
			"round one's promise landed after its own reply and stood in front of it", n)
	}
	if got := countBodies(d.bodies(), "cancelled"); got != 1 {
		t.Errorf("the room got %d cancellation notices, want 1", got)
	}
}

// The same shape from the other direction, and the one the 24h sweep was the
// only answer to: a round whose ending never arrives at all. Archiving an agent
// cancels its tasks without broadcasting per row, and a daemon that goes away
// mid-run reports nothing either, so the promise simply stands.
//
// The next cancellation in that conversation has to speak anyway. Whether it
// does cannot turn on the count of promises, because that count is exactly what
// the lost ending corrupted.
func TestOutbound_APromiseNothingEverAnsweredDoesNotSilenceTheNextCancel(t *testing.T) {
	d := newDingtalkSendServer(t)
	o, ack, q := newCancelTestOutbound(t, d)
	postTwoAckedRounds(t, ack) // round one's ending never arrives
	bus := events.New()
	o.Register(bus)

	asChannelRun(q)
	bus.Publish(cancelledEvent("44444444-4444-4444-4444-444444444444"))

	if n := atomic.LoadInt32(&d.sendCalls); n != 1 {
		t.Fatalf("the round that was cancelled left the room holding %q; an earlier "+
			"round whose ending was lost made the room read as still owed a reply "+
			"(sends: %d)", ackProcessingText, n)
	}
	// And the room is clear afterwards, so its next cancel spends no queries and
	// posts nothing.
	if ack.hasOutstandingAck(mustScanUUID(t, cancelTestSessionID)) {
		t.Error("the room still reads as owed a reply, so every cancel in this session " +
			"keeps paying for queries that find a promise nothing will answer")
	}
}

// A reply landing and another round being cancelled are two facts, both already
// committed before either event is published. Which subscriber the bus runs
// first must not decide what the user sees.
//
// It used to. The cancel took the room's oldest promise — the delivered round's,
// if that ending had not been processed yet — found one still standing, and said
// nothing; run the other way round it found the room empty and spoke.
func TestOutbound_ReplyAndCancelSpeakTheSameInEitherOrder(t *testing.T) {
	const taskA = cancelTestTaskID
	const taskB = "44444444-4444-4444-4444-444444444444"

	for _, tc := range []struct {
		name  string
		first events.Event
		then  events.Event
	}{
		{"reply handled first", chatDoneEvent(taskA, "here you go"), cancelledEvent(taskB)},
		{"cancel handled first", cancelledEvent(taskB), chatDoneEvent(taskA, "here you go")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDingtalkSendServer(t)
			o, ack, q := newCancelTestOutbound(t, d)
			postTwoAckedRounds(t, ack)
			bus := events.New()
			o.Register(bus)

			asChannelRun(q)
			bus.Publish(tc.first)
			bus.Publish(tc.then)

			bodies := d.bodies()
			if got := countBodies(bodies, "here you go"); got != 1 {
				t.Errorf("the delivered round's reply reached the room %d times, want 1", got)
			}
			if got := countBodies(bodies, "cancelled"); got != 1 {
				t.Fatalf("the cancelled round was announced %d times, want 1: the same two "+
					"endings must read the same whichever the bus runs first", got)
			}
		})
	}
}

// Whether the room is owed a reply is read from agent_task_queue, so a read that
// fails leaves the question open. Nothing is posted and nothing is discharged:
// the notice cannot be unsent, and the next cancel asks again.
func TestOutbound_UnreadableTaskQueueLeavesThePromiseAloneAndSaysNothing(t *testing.T) {
	d := newDingtalkSendServer(t)
	o, ack, q := newCancelTestOutbound(t, d)
	postProcessingAck(t, ack)
	bus := events.New()
	o.Register(bus)

	q.pendingTurnErr = errors.New("connection reset")
	bus.Publish(cancelledEvent(cancelTestTaskID))
	if n := atomic.LoadInt32(&d.sendCalls); n != 0 {
		t.Fatalf("the task queue could not be read and the room was told a reply was "+
			"not coming anyway (sends: %d)", n)
	}

	q.pendingTurnErr = nil
	bus.Publish(cancelledEvent("44444444-4444-4444-4444-444444444444"))
	if n := atomic.LoadInt32(&d.sendCalls); n != 1 {
		t.Fatalf("the failed read consumed the promise, so the next cancel had nothing "+
			"to withdraw and the room keeps %q (sends: %d)", ackProcessingText, n)
	}
}
