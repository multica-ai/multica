package wecom

// stream_bubble_test.go — the whole point of the feature, end to end: a
// question opens a bubble the user can see immediately, and the answer
// replaces that same bubble in place instead of arriving as a new message.

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// bubbleConn answers every write like the server does, and can be told to
// refuse the CLOSING frame only — which is the case the plain-message fallback
// exists for, and the one a conn that refuses everything cannot produce.
type bubbleConn struct {
	mu     sync.Mutex
	frames []frameEnvelope
	sender *wsSender

	refuseClosingCode int

	// refuseOpeningCode makes the server state a verdict on the frame that
	// paints the bubble. 846605 and 846608 are the two that mean this stream
	// will never take a frame, so no bubble exists to close.
	refuseOpeningCode int

	// failClosingWrite makes the socket itself refuse a closing frame — the
	// write returns this error, the way a half-closed connection reports a
	// broken pipe. No ack is ever produced for it: nothing may have left the
	// process, and nothing says whether it did.
	failClosingWrite error

	// loseClosingAcks is how many closing frames, counted from the first one
	// written, are entered and never answered — the ack is swallowed the way
	// a socket that dropped right after the write swallows it. The frames are
	// still recorded, so a test can see what was retried. Zero answers every
	// closing frame.
	loseClosingAcks int
	closingWrites   int

	// failOpeningWrite makes the socket refuse the frame that paints the
	// bubble, with an error raised once WriteMessage had been entered — so the
	// placeholder may be on the asker's screen and nothing says whether it is.
	// Distinct from refuseOpeningCode, which is the server STATING that this
	// stream is dead and no bubble exists.
	failOpeningWrite error

	// onClosing runs after a closing frame has been recorded and before its
	// verdict is routed. It is how a test says "the socket dropped right after
	// this frame went out" — by swapping the installation's sender from
	// inside the write.
	onClosing func()
}

func (c *bubbleConn) WriteMessage(_ int, data []byte) error {
	var env frameEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}
	c.mu.Lock()
	c.frames = append(c.frames, env)
	s := c.sender
	code := 0
	lost := false
	var onClosing func()
	if isClosingFrame(env) {
		c.closingWrites++
		if c.failClosingWrite != nil {
			c.mu.Unlock()
			return c.failClosingWrite
		}
		if c.refuseClosingCode != 0 {
			code = c.refuseClosingCode
		}
		lost = c.closingWrites <= c.loseClosingAcks
		onClosing = c.onClosing
	} else if env.Cmd == cmdRespondMsg {
		if c.failOpeningWrite != nil {
			c.mu.Unlock()
			return c.failOpeningWrite
		}
		if c.refuseOpeningCode != 0 {
			code = c.refuseOpeningCode
		}
	}
	c.mu.Unlock()
	if onClosing != nil {
		onClosing()
	}
	if s != nil && !lost {
		s.routeResponse(frameEnvelope{
			Headers: frameHeaders{ReqID: env.Headers.ReqID},
			ErrCode: code,
			ErrMsg:  "refused",
		})
	}
	return nil
}
func (c *bubbleConn) ReadMessage() (int, []byte, error) { return 0, nil, nil }
func (c *bubbleConn) SetReadDeadline(time.Time) error   { return nil }
func (c *bubbleConn) SetWriteDeadline(time.Time) error  { return nil }
func (c *bubbleConn) Close() error                      { return nil }

func isClosingFrame(env frameEnvelope) bool {
	if env.Cmd != cmdRespondMsg {
		return false
	}
	var body map[string]any
	if json.Unmarshal(env.Body, &body) != nil {
		return false
	}
	stream, _ := body["stream"].(map[string]any)
	return stream != nil && stream["finish"] == true
}

// streamOf decodes one frame's stream body, and reports whether the frame was
// a stream frame at all.
func streamOf(env frameEnvelope) (map[string]any, bool) {
	if env.Cmd != cmdRespondMsg {
		return nil, false
	}
	var body map[string]any
	if json.Unmarshal(env.Body, &body) != nil {
		return nil, false
	}
	stream, _ := body["stream"].(map[string]any)
	return stream, stream != nil
}

// streamFrames returns the decoded stream bodies, in write order.
func (c *bubbleConn) streamFrames(t *testing.T) []map[string]any {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []map[string]any
	for _, f := range c.frames {
		if stream, ok := streamOf(f); ok {
			out = append(out, stream)
		}
	}
	return out
}

// streamReqIDs returns the callback req_id each stream frame echoed, in write
// order. It is what WeCom matches a frame to a turn by, and the only thing
// that says a frame written on one connection is addressed to a turn that
// arrived on another.
func (c *bubbleConn) streamReqIDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, f := range c.frames {
		if f.Cmd == cmdRespondMsg {
			out = append(out, f.Headers.ReqID)
		}
	}
	return out
}

// pushes returns the decoded aibot_send_msg bodies — the "as a new message"
// path, which a working bubble must NOT take.
func (c *bubbleConn) pushes(t *testing.T) []map[string]any {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []map[string]any
	for _, f := range c.frames {
		if f.Cmd != cmdSendMsg {
			continue
		}
		var body map[string]any
		if err := json.Unmarshal(f.Body, &body); err != nil {
			t.Fatalf("decode frame body: %v", err)
		}
		out = append(out, body)
	}
	return out
}

// pushText reads the words out of an aibot_send_msg body — the "as a new
// message" path, which ships as markdown (sendMsgTextBody).
func pushText(body map[string]any) string {
	md, _ := body["markdown"].(map[string]any)
	text, _ := md["content"].(string)
	return text
}

// bubbleRig is one installation with a live socket, a store, the typing
// indicator that opens bubbles and the outbound subscriber that closes them —
// the two halves of the feature, wired to the one store the way boot wires
// them.
type bubbleRig struct {
	conn    *bubbleConn
	streams *streamStore
	senders *sendersRegistry
	typing  *TypingIndicatorManager
	out     *Outbound
	q       *fakeOutboundQueries
	bus     *events.Bus
	instID  pgtype.UUID
	now     time.Time

	// logs is what the manager wrote while the test ran. Set only by the rigs
	// whose subject is a decision NOT to send: a refusal is invisible in the
	// frames, so the log line is the only thing that separates it from a
	// handler that returned for some unrelated reason. See failure_origin_test.
	logs *logRecorder
}

func newBubbleRig(t *testing.T) *bubbleRig {
	t.Helper()
	reg := newSendersRegistry()
	instID := mustTestUUID(t)
	conn := &bubbleConn{}
	sender := newWSSender(conn, nil)
	conn.sender = sender
	reg.set(instID, sender)

	streams := newStreamStore()
	rig := &bubbleRig{
		conn:    conn,
		streams: streams,
		senders: reg,
		instID:  instID,
		now:     time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC),
	}
	streams.now = func() time.Time { return rig.now }

	q := &fakeOutboundQueries{
		// The session is on this row in production and a queue row is fenced
		// on it, including the record a socket delivery leaves behind
		// (outbox_direct.go).
		sessionBinding: db.ChannelChatSessionBinding{
			ChatSessionID:  bubbleSessionID(t),
			InstallationID: instID,
			ChannelChatID:  "CHAT_1",
			ChatType:       "p2p",
		},
		installation: db.ChannelInstallation{ID: instID, Status: string(InstallationActive)},
		tasks:        map[string]db.AgentTaskQueue{},
		// Every round in this file was opened by a WeCom message, which is
		// what the origin gate in processEvent asks before it touches the
		// room — or the room's bubble.
		channelIngested: askedOverWecom(),
	}
	rig.q = q
	rig.typing = NewTypingIndicator(TypingIndicatorConfig{
		Senders: reg,
		Streams: streams,
		// Tasks is the retry-clone lookup, the same row *db.Queries answers in
		// production. Bindings stays nil: these tests are about the rounds this
		// process holds, not the restart path.
		Tasks: q,
		// Deliveries is what the origin gate reads, and it is wired in
		// production for every ending. It used to be left out here because a
		// round already on the open list skipped the gate — which stopped
		// being sound when binding moved to task:queued, since a browser run
		// publishes the same event. A rig without it is a rig where the gate
		// cannot run at all.
		Deliveries: q,
	})
	rig.out = NewOutbound(q, reg, streams, nil)
	// Both halves go on a real bus, subscribed the way boot subscribes them.
	// Driving the endings through Publish rather than by calling the handlers
	// keeps the tests honest about WHICH events this manager listens for: an
	// event nobody subscribed to leaves the bubble spinning, which is exactly
	// the bug a direct handler call cannot see.
	rig.bus = events.New()
	rig.typing.Register(rig.bus)
	return rig
}

const bubbleSession = "22222222-2222-2222-2222-222222222222"

func bubbleSessionID(t *testing.T) pgtype.UUID {
	t.Helper()
	id, err := util.ParseUUID(bubbleSession)
	if err != nil {
		t.Fatalf("parse session uuid: %v", err)
	}
	return id
}

// ask feeds one inbound message through the typing indicator, the way the
// Router does after a successful ingest. It says nothing about which run will
// answer it, because the Router says nothing either: a message that arrives
// while a round is still waiting for its run joins that round, and a test says
// "these two are one run" by not queueing a run between them and "these are two
// runs" by queueing one.
func (r *bubbleRig) ask(t *testing.T, reqID string) {
	t.Helper()
	raw, err := json.Marshal(InboundMessage{
		BotID:        "BOT",
		ChatID:       "CHAT_1",
		ChatType:     "single",
		SenderUserID: "USER_1",
		ReqID:        reqID,
	})
	if err != nil {
		t.Fatalf("marshal raw: %v", err)
	}
	r.typing.OnIngested(context.Background(),
		engine.ResolvedInstallation{ID: r.instID},
		channel.InboundMessage{
			Text:   "a question",
			Source: channel.Source{ChannelType: TypeWecom, ChatID: "CHAT_1", ChatType: channel.ChatTypeP2P, SenderID: "USER_1"},
			Raw:    raw,
		},
		bubbleSessionID(t))
}

// reconnect swaps the installation's live socket the way the Supervisor does
// after a drop: a new wsSender over a new conn, registered under the same
// installation id. Nothing touches the store — it is built at boot, not per
// connection, so the handles it holds are still there afterwards.
func (r *bubbleRig) reconnect() *bubbleConn {
	conn := &bubbleConn{}
	sender := newWSSender(conn, nil)
	conn.sender = sender
	r.senders.set(r.instID, sender)
	return conn
}

// queued publishes the task:queued that service.FinalizeChatTaskEnqueue
// broadcasts once the debounced flush has created the run — a chat turn, so
// chat_session_id is set and issue_id is the empty string a NULL column
// serializes to (CreateChatTask inserts issue_id as NULL).
//
// Driving it through the bus rather than by calling the handler is what keeps
// the test honest about the subscription itself: a manager that never
// subscribed to task:queued binds nothing, and every bubble in this file spins.
//
// The enqueue only ever happens for messages this adapter ingested, so the run
// was asked in the room by construction — which is the answer both origin gates
// want, and the reason it is stated here rather than in every test. A test
// modelling a question typed in Multica does not come through here; it says so
// itself with askedInTheBrowser.
func (r *bubbleRig) queued(t *testing.T, taskName string) {
	t.Helper()
	r.q.fileTask(t, taskUUID(t, taskName))
	r.queueTask(t, taskUUID(t, taskName), "")
	r.q.fileTask(t, taskUUID(t, taskName))
	r.q.channelIngested = askedOverWecom()
}

// queueTask publishes one task:queued with an explicit issue id, so a test can
// send the kind of run that must NOT take a bubble.
func (r *bubbleRig) queueTask(t *testing.T, taskID, issueID string) {
	t.Helper()
	r.bus.Publish(events.Event{
		Type:          protocol.EventTaskQueued,
		ChatSessionID: bubbleSession,
		TaskID:        taskID,
		Payload: map[string]any{
			"task_id":  taskID,
			"issue_id": issueID,
			"status":   "queued",
		},
	})
}

// ran is the common case: a message arrives and the flush 3s later creates its
// run.
func (r *bubbleRig) ran(t *testing.T, reqID, taskName string) {
	t.Helper()
	r.ask(t, reqID)
	r.queued(t, taskName)
}

func (r *bubbleRig) answer(t *testing.T, content, taskName string) {
	t.Helper()
	r.answerWithContext(t, context.Background(), content, taskName)
}

// answerWithContext is answer on a caller's own deadline, for the tests that
// care what the reply path does when the budget is already gone.
func (r *bubbleRig) answerWithContext(t *testing.T, ctx context.Context, content, taskName string) {
	t.Helper()
	if err := r.out.processEvent(ctx, events.Event{
		ChatSessionID: bubbleSession,
		TaskID:        taskUUID(t, taskName),
		Payload:       protocol.ChatDonePayload{Content: content},
	}); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	// An answer that did not land in a bubble is an ordinary message, and an
	// ordinary message is a push on this installation's socket. So a fall-back
	// is readable on the connection the moment this returns — there is no
	// durable hop in between to drain.
}

// failed publishes the task:failed FailTask broadcasts, retry_pending and all.
func (r *bubbleRig) failed(t *testing.T, taskName string, retryPending bool) {
	t.Helper()
	id := taskUUID(t, taskName)
	r.bus.Publish(events.Event{
		Type:          protocol.EventTaskFailed,
		ChatSessionID: bubbleSession,
		TaskID:        id,
		Payload: map[string]any{
			"task_id":        id,
			"failure_reason": "provider_network",
			"retry_pending":  retryPending,
		},
	})
}

// cancelled publishes the task:cancelled every cancel path broadcasts.
func (r *bubbleRig) cancelled(t *testing.T, taskName string) {
	t.Helper()
	id := taskUUID(t, taskName)
	r.bus.Publish(events.Event{
		Type:          protocol.EventTaskCancelled,
		ChatSessionID: bubbleSession,
		TaskID:        id,
		Payload: map[string]any{
			"task_id": id,
			"status":  "cancelled",
		},
	})
}

// mustParseTestUUID turns a readable test name into a stable UUID, so a test
// can say "task-1" and the store still sees the pgtype.UUID the seam carries.
func mustParseTestUUID(t *testing.T, name string) pgtype.UUID {
	t.Helper()
	raw, ok := testTaskUUIDs[name]
	if !ok {
		t.Fatalf("unknown test task %q", name)
	}
	id, err := util.ParseUUID(raw)
	if err != nil {
		t.Fatalf("parse test task %q: %v", name, err)
	}
	return id
}

// testTaskUUIDs maps the readable ids these tests use to real UUIDs.
var testTaskUUIDs = map[string]string{
	"task-1": "aaaaaaaa-0000-0000-0000-000000000001",
	"task-2": "aaaaaaaa-0000-0000-0000-000000000002",
	"task-3": "aaaaaaaa-0000-0000-0000-000000000003",
	"retry":  "aaaaaaaa-0000-0000-0000-0000000000ff",
	// An issue or autopilot run: the same events, no chat session at all.
	"issue-run": "aaaaaaaa-0000-0000-0000-0000000000e1",
	// A question typed in Multica on this same WeCom-bound session. Its
	// task:queued is indistinguishable from the room's on the bus.
	"web-1": "aaaaaaaa-0000-0000-0000-0000000000b1",
	"web-2": "aaaaaaaa-0000-0000-0000-0000000000b2",
	// The room's own run, queued behind a first-party one.
	"task-r": "aaaaaaaa-0000-0000-0000-0000000000c1",
}

// taskUUID is the string form the event payloads carry.
func taskUUID(t *testing.T, name string) string {
	t.Helper()
	return util.UUIDToString(mustParseTestUUID(t, name))
}

// has reports whether a session holds a round bound to this run. Tests use it
// to check that an ending kept or released the round it belongs to; no
// production path reads it.
func (s *streamStore) has(sessionID pgtype.UUID, taskID string) bool {
	if taskID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.sessions[util.UUIDToString(sessionID)] {
		if r.taskID == taskID {
			return true
		}
	}
	return false
}

// WeCom has no typing indicator, so the opening frame IS the receipt: an
// unsealed think tag the client renders as its own animated dots. The answer
// then replaces that bubble in place — same stream id, finish=true — rather
// than arriving underneath it as a new message.
func TestTheAnswerReplacesTheBubbleInPlace(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-B", "task-1")
	rig.answer(t, "the agent reply", "task-1")

	frames := rig.conn.streamFrames(t)
	if len(frames) != 2 {
		t.Fatalf("got %d stream frames, want 2 (open + seal)", len(frames))
	}
	if frames[0]["finish"] != false || frames[0]["content"] != streamThinkingPlaceholder {
		t.Fatalf("opening frame = %v, want an unsealed %q — without it a slow agent looks like a dead bot",
			frames[0], streamThinkingPlaceholder)
	}
	if frames[1]["id"] != frames[0]["id"] {
		t.Fatalf("the answer opened a SECOND bubble (%v) instead of replacing the first (%v); the loading one spins forever",
			frames[1]["id"], frames[0]["id"])
	}
	if frames[1]["finish"] != true {
		t.Error("the answer did not seal the bubble")
	}
	if frames[1]["content"] != "the agent reply" {
		t.Errorf("sealed content = %q, want the agent reply", frames[1]["content"])
	}
	if pushes := rig.conn.pushes(t); len(pushes) != 0 {
		t.Errorf("the answer also went out as %d plain message(s); the user reads it twice", len(pushes))
	}
}

// An agent run lasts minutes and a long connection does not always. The bubble
// has to survive a reconnect, because WeCom scopes a callback's req_id to the
// turn rather than to the socket it arrived on — measured 2026-08-09, see
// sendersRegistry.stream — and the answer is written to whichever sender the
// installation holds when it is ready, not to the one that opened the bubble.
//
// This pins our half of that: the closing frame goes out on the socket that
// exists now, still addressed to the turn that came in on the old one. The
// server's half is not something a test in this package can reach, which is
// why the measurement is written down where the behaviour depends on it.
func TestTheAnswerClosesTheBubbleOverTheNextConnection(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-RECONNECT", "task-1")

	opened := rig.conn.streamFrames(t)
	if len(opened) != 1 {
		t.Fatalf("got %d stream frames before the drop, want the opening one", len(opened))
	}
	dropped := rig.conn
	next := rig.reconnect()

	rig.answer(t, "the agent reply", "task-1")

	if after := dropped.streamFrames(t); len(after) != 1 {
		t.Fatalf("the closing frame was written to the connection that is gone (%d frames there now); it reaches nobody and the user keeps the spinner", len(after))
	}
	closing := next.streamFrames(t)
	if len(closing) != 1 {
		t.Fatalf("got %d stream frames on the new connection, want the closing one", len(closing))
	}
	if closing[0]["id"] != opened[0]["id"] {
		t.Fatalf("the answer opened a second bubble (%v) on the new connection instead of finishing the first (%v)",
			closing[0]["id"], opened[0]["id"])
	}
	if reqIDs := next.streamReqIDs(); len(reqIDs) != 1 || reqIDs[0] != "REQ-RECONNECT" {
		t.Fatalf("the closing frame echoed %v, want the req_id of the callback that opened the turn — WeCom refuses any other value", reqIDs)
	}
	if closing[0]["finish"] != true {
		t.Error("the answer did not seal the bubble")
	}
	if closing[0]["content"] != "the agent reply" {
		t.Errorf("sealed content = %q, want the agent reply", closing[0]["content"])
	}
	if pushes := next.pushes(t); len(pushes) != 0 {
		t.Errorf("the answer degraded to %d plain message(s) across the reconnect instead of landing in the bubble the question opened", len(pushes))
	}
}

// Two messages still inside one debounce window share one bubble. A second
// bubble here is one nobody would ever close: the run produces one answer, it
// seals one bubble, and the other spins until the guard promises a separate
// reply for a question that has already been answered.
//
// The clock is moved a full window and a half between them, further apart than
// any local rule would call one round. The batcher says otherwise — it re-arms
// on every message, so a burst is one run however long it runs — and the run,
// not the clock, is what this side reads: nothing has been queued yet.
func TestMessagesInsideTheDebounceWindowShareOneBubble(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ask(t, "REQ-E1")
	rig.now = rig.now.Add(engine.DefaultChatRunBatchWindow * 3 / 2)
	rig.ask(t, "REQ-E2")

	if n := len(rig.conn.streamFrames(t)); n != 1 {
		t.Fatalf("two messages in one debounce window opened %d bubbles, want 1 — the extra one is never closed", n)
	}
	if rig.streams.depth() != 1 {
		t.Fatalf("store holds %d open rounds, want 1", rig.streams.depth())
	}
}

// A run that fails publishes no chat:done, so the failure subscriber is the
// only thing that ever stops that spinner.
func TestAFailedRunClosesTheBubble(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-H", "task-1")

	rig.failed(t, "task-1", false)

	frames := rig.conn.streamFrames(t)
	if len(frames) != 2 {
		t.Fatalf("a failed run wrote %d stream frames, want 2 — the bubble spins with no news at all", len(frames))
	}
	if frames[1]["finish"] != true {
		t.Fatal("the failure did not seal the bubble")
	}
	if frames[1]["content"] != streamCopyFailed {
		t.Errorf("failure copy = %q, want %q", frames[1]["content"], streamCopyFailed)
	}
}

// ---- the protocol window this constant stands for ----

// TestALongRunStillAnswersInItsBubbleInsideTheMeasuredWindow is what pins
// streamMaxAge to what was measured rather than to what was guessed.
//
// Eight minutes is the interesting number: inside the ten the server actually
// allows, outside the six this adapter used to assume. Every other test here
// either drives the clock in relative steps or moves time by streamMaxAge
// itself — so all of them follow the constant wherever it goes and none of
// them notices it being wrong. Set the window back to six and
// this run's answer stops landing in the bubble the asker has been watching for
// eight minutes and arrives underneath it as a separate message instead, with
// the spinner above it never sealed.
func TestALongRunStillAnswersInItsBubbleInsideTheMeasuredWindow(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-LONG", "task-1")

	// A run that takes eight minutes. Long, and well within what WeCom took on
	// 2026-08-09: it accepted a frame at 600.0s and refused one at 630.0s.
	rig.now = rig.now.Add(8 * time.Minute)
	rig.answer(t, "the answer to a long question", "task-1")

	if pushes := rig.conn.pushes(t); len(pushes) != 0 {
		t.Fatalf("an eight-minute run's answer went out as %d plain message(s) — the window is "+
			"set shorter than the server's, so the handle was thrown away while it was still "+
			"usable and the asker's bubble is spinning above the answer", len(pushes))
	}
	frames := rig.conn.streamFrames(t)
	if len(frames) != 2 {
		t.Fatalf("got %d stream frames, want 2 (open + seal)", len(frames))
	}
	if frames[1]["id"] != frames[0]["id"] || frames[1]["finish"] != true ||
		frames[1]["content"] != "the answer to a long question" {
		t.Fatalf("the answer did not seal the bubble its question opened: %v", frames[1])
	}
}

// TestARunPastTheWindowAnswersAsExactlyOnePlainMessage is the other side of
// that constant: what the user gets when a run outlives it.
//
// Ten minutes is the whole budget a bubble has. Nothing written into it buys
// more — the server counts from the opening frame (streamMaxAge) — so a run
// that takes longer has no bubble left to answer in, and the answer arrives as
// an ordinary message underneath the spinner the asker has been watching.
// Degraded, and deliberately so: the words are all there, in one message.
//
// ONE message is the property worth pinning, and it is not automatic. The
// stream is expired, not merely old, so a closer that wrote the closing frame
// anyway would have it refused (846608) and then fall back — and the refused
// frame is not invisible: WeCom renders what it took before it refused, and
// there is no unsend. The store gives the handle up before writing instead,
// which is what makes the count one.
//
// REVERSE VERIFICATION: make expiredLocked always report false and this goes
// red with two messages — the closing frame the server refuses, and the plain
// one behind it.
func TestARunPastTheWindowAnswersAsExactlyOnePlainMessage(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	// The server's own stance on a stream this old, so the test does not rest
	// on our clock alone: every closing frame it is offered is refused with
	// 846608. Nothing here should ever offer it one.
	rig.conn.refuseClosingCode = errcodeStreamExpired
	rig.ran(t, "REQ-PAST-WINDOW", "task-1")

	// Eleven minutes: past the 600s the live tenant accepted on 2026-08-09 and
	// past the 630s at which it refused.
	const answer = "the first line of a long run's answer\nand the last line of it"
	rig.now = rig.now.Add(streamMaxAge + time.Minute)
	rig.answer(t, answer, "task-1")

	if got := said(t, rig.conn); len(got) != 1 || got[0] != answer {
		t.Fatalf("the asker read %d message(s) %q, want exactly one carrying the whole answer %q",
			len(got), got, answer)
	}
	frames := rig.conn.streamFrames(t)
	if len(frames) != 1 || frames[0]["finish"] != false {
		t.Fatalf("got %d stream frames (%v), want only the opening one — a closing frame on a "+
			"stream the server has already expired is a refusal charged against the whole bot's "+
			"rate limit, and whatever it renders before refusing the asker cannot unsee",
			len(frames), frames)
	}
}

// said is everything the person in the chat ended up reading on a connection:
// the text of every sealed bubble and every plain message, in write order.
//
// Both, deliberately. A closing frame and a push are the same thing to the
// reader, and they are how the same words reach them depending on whether the
// bubble survived — so a test watching only one of them would call a path
// silent while its words were on the screen, or count one ending twice. The
// opening frame is not in here: it carries no words, only the spinner.
func said(t *testing.T, c *bubbleConn) []string {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []string{}
	for _, f := range c.frames {
		var body map[string]any
		if err := json.Unmarshal(f.Body, &body); err != nil {
			t.Fatalf("decode frame body: %v", err)
		}
		switch f.Cmd {
		case cmdRespondMsg:
			stream, _ := body["stream"].(map[string]any)
			if stream == nil || stream["finish"] != true {
				continue
			}
			s, _ := stream["content"].(string)
			out = append(out, s)
		case cmdSendMsg:
			md, _ := body["markdown"].(map[string]any)
			if md == nil {
				continue
			}
			s, _ := md["content"].(string)
			out = append(out, s)
		}
	}
	return out
}

// A bubble is painted and the run never ends — the process that owns it goes
// away, or the answer is produced on a replica that does not hold the socket
// and arrives as its own message. Nothing seals the stream, so neither ending
// counter moves, and from those two alone a stranded bubble and a quiet hour
// are the same picture. stream_opened is what tells them apart.
//
// REVERSE VERIFICATION: move senders.recordOpened() inside the err == nil arm
// of OnIngested, or delete it, and this fails with stream_opened = 0 — the
// stranded bubble becomes invisible again, which is the whole point of it.
func TestABubbleNobodyEndsIsCountedAsOpenedWithNoEnding(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	mx := newCountingMetrics()
	rig.senders.WithMetrics(mx)

	rig.ran(t, "REQ-STRANDED", "task-1")
	// No answer, no failure, no cancellation. This is what the relay gap and a
	// restart mid-run both leave behind.

	if got := mx.get("stream_opened"); got != 1 {
		t.Fatalf("stream_opened = %d, want 1 — one bubble is on screen and owed an ending", got)
	}
	if got := mx.get("stream_finished"); got != 0 {
		t.Errorf("stream_finished = %d, want 0", got)
	}
	if got := mx.get("stream_fell_back"); got != 0 {
		t.Errorf("stream_fell_back = %d, want 0", got)
	}
	if opened, ended := mx.get("stream_opened"), mx.get("stream_finished")+mx.get("stream_fell_back"); opened-ended != 1 {
		t.Fatalf("opened - ended = %d, want 1 — this difference is the number an operator reads", opened-ended)
	}
}

// The same turn, ended properly: the difference goes back to zero. Without
// this the test above would pass against a counter that only ever counts up.
func TestAnAnsweredBubbleLeavesNothingOutstanding(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	mx := newCountingMetrics()
	rig.senders.WithMetrics(mx)

	rig.ran(t, "REQ-ANSWERED", "task-1")
	rig.answer(t, "the agent reply", "task-1")

	if opened, ended := mx.get("stream_opened"), mx.get("stream_finished")+mx.get("stream_fell_back"); opened != 1 || opened-ended != 0 {
		t.Fatalf("opened = %d, opened - ended = %d, want 1 and 0", opened, opened-ended)
	}
}

// The server states a verdict on the opening frame itself: this req_id will
// never carry a stream. No bubble was painted, so nothing is owed an ending
// and nothing is counted — the counter has to follow the handle, not the
// write. The answer still reaches the user, as the plain message main sends
// today.
//
// REVERSE VERIFICATION: count the open before the frame is written and this
// fails with stream_opened = 1, which would leave every refused opening frame
// sitting permanently in opened-minus-ended and make the number unreadable.
func TestARefusedOpeningFrameCountsNoBubble(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	mx := newCountingMetrics()
	rig.senders.WithMetrics(mx)
	rig.conn.refuseOpeningCode = errcodeStreamBadReqID

	rig.ran(t, "REQ-REFUSED", "task-1")
	rig.answer(t, "the agent reply", "task-1")

	if got := mx.get("stream_opened"); got != 0 {
		t.Fatalf("stream_opened = %d, want 0 — the server said this stream will never exist", got)
	}
	pushes := rig.conn.pushes(t)
	if len(pushes) != 1 || pushText(pushes[0]) != "the agent reply" {
		t.Fatalf("the asker read %d plain message(s) %v, want exactly the answer", len(pushes), pushes)
	}
}

// A write the socket may have taken is not a refusal, and the opening frame was
// the one site in this package still reading it as one.
//
// errWriteAttempted means WriteMessage was entered: the peer may have taken the
// bytes and painted the placeholder. Dropping the handle on that evidence
// leaves a bubble on the asker's screen with nothing left that could ever close
// it — the answer arrives as a plain message underneath a spinner that turns
// until the platform's window runs out.
//
// provablyNotSent returns false for this error and unconfirmedReason files it
// as write_attempted; this site now agrees with both.
func TestAnOpeningFrameTheSocketMayHaveTakenKeepsItsHandle(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.conn.failOpeningWrite = errors.New("broken pipe")

	rig.ask(t, "REQ-OPEN")

	if got := rig.streams.depth(); got != 1 {
		t.Fatalf("the store holds %d rounds after an opening frame the socket may have taken, "+
			"want 1 — if the placeholder is on screen, the handle is the only thing that could "+
			"ever close it", got)
	}
}
