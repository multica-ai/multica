package wecom

// stream_bubble_test.go — the whole point of the feature, end to end: a
// question opens a bubble the user can see immediately, and the answer
// replaces that same bubble in place instead of arriving as a new message.

import (
	"context"
	"encoding/json"
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

	// failClosingWrite makes the socket itself refuse a closing frame — the
	// write returns this error, the way a half-closed connection reports a
	// broken pipe. No ack is ever produced for it: nothing may have left the
	// process, and nothing says whether it did.
	failClosingWrite error

	// disownAfterFrames makes the connection behave like a stream another
	// replica or a reconnect has taken over: the first n stream frames land,
	// and every one after that is refused with 846608 — a refresh, a closing
	// frame, the answer, all of them. Refusing only some of them would hide
	// the whole cost of a refresh loop, which is that every attempt is a write
	// counted against the bot's shared rate limit.
	disownAfterFrames int
	streamWrites      int

	// refusePushesFrom is the 1-based aibot_send_msg this server starts
	// refusing; every push from there on is answered 45009 and none of them
	// reaches the chat. Zero accepts them all.
	//
	// It is how a long answer breaks in the MIDDLE. An answer past the body
	// cap goes out as several messages, and a failure on the second leaves the
	// first one already in the chat — WeCom has no unsend, so the person is
	// looking at the opening of an answer whose remainder exists nowhere. That
	// is a different outcome from both "it arrived" and "it did not", and the
	// only double here that can produce it.
	refusePushesFrom int
	pushWrites       int

	// loseClosingAcks is how many closing frames, counted from the first one
	// written, are entered and never answered — the ack is swallowed the way
	// a socket that dropped right after the write swallows it. The frames are
	// still recorded, so a test can see what was retried. Zero answers every
	// closing frame.
	loseClosingAcks int
	closingWrites   int

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
	if _, isStream := streamOf(env); isStream {
		c.streamWrites++
		if c.disownAfterFrames > 0 && c.streamWrites > c.disownAfterFrames {
			code = errcodeStreamExpired
		}
	}
	if env.Cmd == cmdSendMsg {
		c.pushWrites++
		if c.refusePushesFrom > 0 && c.pushWrites >= c.refusePushesFrom {
			code = 45009 // api freq out of limit
		}
	}
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
		if f.Cmd != cmdRespondMsg {
			continue
		}
		var body map[string]any
		if err := json.Unmarshal(f.Body, &body); err != nil {
			t.Fatalf("decode frame body: %v", err)
		}
		stream, _ := body["stream"].(map[string]any)
		if stream != nil {
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

// readablePushes is how many plain messages the server ACCEPTED — what the
// person can actually read, which is not the same as what was written at the
// socket once refusePushesFrom is in play.
func (c *bubbleConn) readablePushes() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.refusePushesFrom <= 0 || c.pushWrites < c.refusePushesFrom {
		return c.pushWrites
	}
	return c.refusePushesFrom - 1
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
	// boundRuns is batch -> task id, mirroring what the debounced flush told
	// the store. It is the test's own copy of the binding, so a helper can name
	// the run behind a batch without reaching into the store.
	boundRuns map[engine.RunBatchID]string

	// logs is what the manager wrote while the test ran. Set only by the rigs
	// whose subject is a decision NOT to send: a refusal is invisible in the
	// frames, so the log line is the only thing that separates it from a
	// handler that returned for some unrelated reason. See failure_origin_test.
	logs *logRecorder

	// installer is whose bot this is. Left invalid by default, which is what
	// the bubble tests want — no principal means no round shows its steps, so
	// nothing here writes a refresh frame. progress_rounds_test.go sets it.
	installer pgtype.UUID
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
		conn:      conn,
		streams:   streams,
		senders:   reg,
		instID:    instID,
		now:       time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC),
		boundRuns: map[engine.RunBatchID]string{},
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
		// No guard: these tests drive the endings themselves.
		GuardAfter: -1,
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
// Router does after a successful ingest. batch is the engine debouncer's
// verdict on which run the message was collected into — the Router reads it
// off pendingBatcher.Schedule and hands it straight through, so a test says
// "the batcher merged these" by passing one id twice and "the batcher split
// them" by passing two, without touching a clock.
func (r *bubbleRig) ask(t *testing.T, reqID string, batch engine.RunBatchID) {
	t.Helper()
	r.askFrom(t, reqID, batch, channel.ChatTypeP2P)
}

// askFrom is ask with the kind of chat the question came from, which is half of
// what decides whether that round's bubble may show the run's steps.
func (r *bubbleRig) askFrom(t *testing.T, reqID string, batch engine.RunBatchID, chatType channel.ChatType) {
	t.Helper()
	wire := "single"
	if chatType == channel.ChatTypeGroup {
		wire = "group"
	}
	raw, err := json.Marshal(InboundMessage{
		BotID:        "BOT",
		ChatID:       "CHAT_1",
		ChatType:     wire,
		SenderUserID: "USER_1",
		ReqID:        reqID,
	})
	if err != nil {
		t.Fatalf("marshal raw: %v", err)
	}
	r.typing.OnIngested(context.Background(),
		engine.ResolvedInstallation{ID: r.instID, InstallerUserID: r.installer},
		channel.InboundMessage{
			Text:   "a question",
			Source: channel.Source{ChannelType: TypeWecom, ChatID: "CHAT_1", ChatType: chatType, SenderID: "USER_1"},
			Raw:    raw,
		},
		bubbleSessionID(t), batch)
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

// runStarted is the debounced flush reporting the task it created for a batch,
// the way Router.flushChatRun does after EnqueueChatTask returns.
//
// That flush only ever runs for messages this adapter ingested, so the run it
// creates was asked in the room by construction — which is the answer both
// origin gates want, and the reason it is stated here rather than in every
// test. A test modelling a question typed in Multica does not come through
// here; it says so itself with askedInTheBrowser.
func (r *bubbleRig) runStarted(t *testing.T, batch engine.RunBatchID, taskName string) {
	t.Helper()
	r.q.fileTask(t, taskUUID(t, taskName))
	r.typing.OnRunStarted(context.Background(), bubbleSessionID(t), batch, mustParseTestUUID(t, taskName))
	r.boundRuns[batch] = taskUUID(t, taskName)
	r.q.fileTask(t, taskUUID(t, taskName))
	r.q.channelIngested = askedOverWecom()
}

// ran is the common case: a message arrives and the flush 3s later creates its
// run.
func (r *bubbleRig) ran(t *testing.T, reqID string, batch engine.RunBatchID, taskName string) {
	t.Helper()
	r.ask(t, reqID, batch)
	r.runStarted(t, batch, taskName)
}

func (r *bubbleRig) answer(t *testing.T, content, taskName string) {
	t.Helper()
	if err := r.out.processEvent(context.Background(), events.Event{
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

// streamIDOf reads the stream id a batch's bubble is on right now, out of the
// store — which changes when the guard rotates the round onto a fresh one.
func (r *bubbleRig) streamIDOf(t *testing.T, batch engine.RunBatchID) string {
	t.Helper()
	r.streams.mu.Lock()
	defer r.streams.mu.Unlock()
	e := r.streams.entryLocked(bubbleSession, batch)
	if e == nil || !e.painted {
		t.Fatalf("batch %d has no bubble on file", batch)
	}
	return e.handle.StreamID
}

// rotated is the nine-minute guard firing on one round: it seals the bubble
// with "处理时间较长，接下一条" and opens a fresh stream on the same req_id for
// the run to carry on in. It runs the manager's own guard body, so a test gets
// the real thing without waiting out the window. The round stays on file, on
// the new stream, which is what the two ids returned say.
func (r *bubbleRig) rotated(t *testing.T, batch engine.RunBatchID) (old, next string) {
	t.Helper()
	old = r.streamIDOf(t, batch)
	// A rotation is for a run that is alive: one step since the stream opened.
	r.stepped(t, batch)
	r.typing.fireGuard(context.Background(), bubbleSessionID(t), batch)
	next = r.streamIDOf(t, batch)
	if next == old {
		t.Fatalf("could not rotate round %d: its bubble is still on stream %s", batch, old)
	}
	return old, next
}

// stepped records one sign of life for the run bound to batch, the way a
// task:message reaching recordStep does.
func (r *bubbleRig) stepped(t *testing.T, batch engine.RunBatchID) {
	t.Helper()
	if _, _, ok := r.streams.feedFor(bubbleSessionID(t), r.taskOfBatch(t, batch)); !ok {
		t.Fatalf("round %d has no bubble to step into", batch)
	}
}

// taskOfBatch is the run the flush bound to a batch, read back out of the rig's
// own bookkeeping so guardClosed can assert the promise it just made.
func (r *bubbleRig) taskOfBatch(t *testing.T, batch engine.RunBatchID) string {
	t.Helper()
	id, ok := r.boundRuns[batch]
	if !ok {
		t.Fatalf("batch %d has no run bound to it", batch)
	}
	return id
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
}

// taskUUID is the string form the event payloads carry.
func taskUUID(t *testing.T, name string) string {
	t.Helper()
	return util.UUIDToString(mustParseTestUUID(t, name))
}

// WeCom has no typing indicator, no reaction and no read receipt. The opening
// stream frame IS the receipt — a think tag renders as the client's own
// animated dots — and without it a slow agent looks like a dead bot.
func TestAQuestionPaintsALoadingBubbleImmediately(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ask(t, "REQ-A", 1)

	frames := rig.conn.streamFrames(t)
	if len(frames) != 1 {
		t.Fatalf("an ingested message wrote %d stream frames, want 1 — the user sees nothing at all until the agent finishes", len(frames))
	}
	if frames[0]["finish"] != false {
		t.Error("the opening frame sealed the bubble; nothing can fill it in later")
	}
	if frames[0]["content"] != streamThinkingPlaceholder {
		t.Errorf("opening frame content = %q, want the think tag %q that renders as the loading dots",
			frames[0]["content"], streamThinkingPlaceholder)
	}
}

// The answer replaces the bubble the question opened, in place — same stream
// id, finish=true — rather than arriving underneath it as a new message.
func TestTheAnswerReplacesTheBubbleInPlace(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-B", 1, "task-1")
	rig.answer(t, "the agent reply", "task-1")

	frames := rig.conn.streamFrames(t)
	if len(frames) != 2 {
		t.Fatalf("got %d stream frames, want 2 (open + seal)", len(frames))
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
	rig.ran(t, "REQ-RECONNECT", 1, "task-1")

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

// A blank closing frame is DISCARDED by WeCom, and the bubble it was meant to
// seal spins for good. An empty completion is a legitimate outcome — the agent
// had nothing to add — so the copy stands in for the silence.
func TestAnEmptyAnswerStillClosesTheBubbleWithWords(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-C", 1, "task-1")
	rig.answer(t, "   \n ", "task-1")

	frames := rig.conn.streamFrames(t)
	if len(frames) != 2 {
		t.Fatalf("got %d stream frames, want 2 — an empty answer left the bubble open", len(frames))
	}
	if frames[1]["finish"] != true {
		t.Fatal("an empty answer did not seal the bubble; it spins forever")
	}
	content, _ := frames[1]["content"].(string)
	if !hasVisibleChar(content) {
		t.Fatalf("the closing frame carries nothing visible (%q); WeCom discards it and the bubble spins forever", content)
	}
	if content != copyFor(DefaultLocale).StreamNoReply {
		t.Errorf("closing copy = %q, want %q", content, copyFor(DefaultLocale).StreamNoReply)
	}
}

// A message the batcher gave a run of its own is a round of its own, queued
// behind the run in flight — and it gets its own bubble immediately, because a
// wait with nothing on screen reads as a message that was lost.
//
// The two messages arrive at the SAME instant on this store's clock. Only the
// batcher's verdict separates them, which is the point: the gap between two
// messages is not this side's to measure, and a store that measured it would
// fold these two into one round and leave the second question with no receipt.
func TestAQueuedQuestionGetsItsOwnBubble(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ask(t, "REQ-D1", 1)
	rig.ask(t, "REQ-D2", 2)

	frames := rig.conn.streamFrames(t)
	if len(frames) != 2 {
		t.Fatalf("two questions a window apart wrote %d bubbles, want 2 — the second looks lost", len(frames))
	}
	if frames[0]["id"] == frames[1]["id"] {
		t.Fatal("the second question reused the first bubble; one of the two answers has nowhere to land")
	}
	if rig.streams.depth() != 2 {
		t.Fatalf("store holds %d open rounds, want 2", rig.streams.depth())
	}
}

// Two messages the batcher collected into ONE run share one bubble. A second
// bubble here is one nobody would ever close: the run produces one answer, it
// seals one bubble, and the other spins until the guard promises a separate
// reply for a question that has already been answered.
//
// The clock is moved a full window and a half between them, further apart than
// any local rule would call one round. The batcher says otherwise — it re-arms
// on every message, so a burst is one run however long it runs — and the
// batcher is the one that decides.
func TestMessagesInsideTheDebounceWindowShareOneBubble(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ask(t, "REQ-E1", 1)
	rig.now = rig.now.Add(engine.DefaultChatRunBatchWindow * 3 / 2)
	rig.ask(t, "REQ-E2", 1)

	if n := len(rig.conn.streamFrames(t)); n != 1 {
		t.Fatalf("two messages in one debounce window opened %d bubbles, want 1 — the extra one is never closed", n)
	}
	if rig.streams.depth() != 1 {
		t.Fatalf("store holds %d open rounds, want 1", rig.streams.depth())
	}
}

// A queued round whose run finished with nothing of its own to say has a
// better explanation than plain silence: the reply ahead of it already covered
// the message.
func TestAQueuedRoundWithNothingToSaySaysItWasMerged(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-F1", 1, "task-1")
	rig.ran(t, "REQ-F2", 2, "task-2")

	rig.answer(t, "the first reply", "task-1") // seals the head
	rig.answer(t, "", "task-2")                // seals the queued one

	frames := rig.conn.streamFrames(t)
	if len(frames) != 4 {
		t.Fatalf("got %d stream frames, want 4 (two opens, two seals)", len(frames))
	}
	if frames[3]["content"] != copyFor(DefaultLocale).StreamMerged {
		t.Errorf("a queued round's empty answer closed with %q, want %q",
			frames[3]["content"], copyFor(DefaultLocale).StreamMerged)
	}
}

// When the server refuses the closing frame the bubble cannot be sealed, but
// the ANSWER still has to reach the user — as an ordinary message.
func TestARefusedClosingFrameStillDeliversTheAnswer(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.conn.refuseClosingCode = errcodeStreamExpired
	rig.ran(t, "REQ-G", 1, "task-1")
	rig.answer(t, "the agent reply", "task-1")

	pushes := rig.conn.pushes(t)
	if len(pushes) != 1 {
		t.Fatalf("a refused closing frame produced %d plain messages, want 1 — the answer went nowhere", len(pushes))
	}
	md, _ := pushes[0]["markdown"].(map[string]any)
	if md == nil || md["content"] != "the agent reply" {
		t.Fatalf("the fallback message did not carry the answer: %v", pushes[0])
	}
}

// A run that fails publishes no chat:done, so the failure subscriber is the
// only thing that ever stops that spinner.
func TestAFailedRunClosesTheBubble(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-H", 1, "task-1")

	rig.failed(t, "task-1", false)

	frames := rig.conn.streamFrames(t)
	if len(frames) != 2 {
		t.Fatalf("a failed run wrote %d stream frames, want 2 — the bubble spins with no news at all", len(frames))
	}
	if frames[1]["finish"] != true {
		t.Fatal("the failure did not seal the bubble")
	}
	if frames[1]["content"] != copyFor(DefaultLocale).StreamFailed {
		t.Errorf("failure copy = %q, want %q", frames[1]["content"], copyFor(DefaultLocale).StreamFailed)
	}
}

// The guard is what keeps a run longer than the protocol's window from leaving
// a spinner nobody can touch. It does not end the round: it seals the stream
// that is about to expire and opens a fresh one on the same req_id, so the
// user keeps a live bubble and the answer still lands in place — in the newest
// one. This drives the real timer; the rotation itself, step by step, is
// stream_rotation_test.go.
//
// It waits for the guard to fire TWICE, which is what pins the re-arm: a
// rotation that did not arm the next guard would leave the second stream to
// run into its own window.
//
// REVERSE VERIFICATION: make armGuard return without arming (take its
// guardAfter <= 0 branch unconditionally) and this fails waiting for the third
// frame; drop the trailing armGuard call in fireGuard instead and it fails
// waiting for the fifth.
func TestTheGuardRotatesABubbleTheWindowIsAboutToStrand(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.typing.guardAfter = 100 * time.Millisecond
	rig.ask(t, "REQ-I", 1)
	rig.runStarted(t, 1, "task-1")
	opened := rig.conn.streamFrames(t)[0]["id"]

	// The run keeps stepping the whole time: a rotation is only for a run
	// that is alive (TestTheGuardLeavesAQuietRoundAlone).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && len(rig.conn.streamFrames(t)) < 5 {
		rig.stepped(t, 1)
		time.Sleep(time.Millisecond)
	}
	frames := rig.conn.streamFrames(t)
	if len(frames) < 5 {
		t.Fatalf("the guard wrote %d stream frames, want at least 5 (open, seal, reopen, seal, reopen) — the bubble runs into the window and strands", len(frames))
	}
	if frames[1]["id"] != opened || frames[1]["finish"] != true || frames[1]["content"] != streamCopyContinued {
		t.Fatalf("the guard's hand-over frame is %v, want %s sealed with %q", frames[1], opened, streamCopyContinued)
	}
	if frames[2]["id"] == opened || frames[2]["finish"] != false {
		t.Fatalf("the guard did not open a fresh stream after sealing the old one: %v", frames[2])
	}
	// The round is NOT over: the answer lands in whichever stream the round is
	// on by then — a second rotation may have fired under load, so it is read
	// off the store rather than assumed.
	rig.answer(t, "the answer to a long question", "task-1")
	frames = rig.conn.streamFrames(t)
	last := frames[len(frames)-1]
	var lastOpened any
	for _, f := range frames[:len(frames)-1] {
		if f["finish"] == false && f["content"] == streamThinkingPlaceholder {
			lastOpened = f["id"]
		}
	}
	if last["id"] != lastOpened || last["finish"] != true || last["content"] != "the answer to a long question" {
		t.Fatalf("the answer did not seal the newest bubble %v: %v", lastOpened, last)
	}
	if pushes := rig.conn.pushes(t); len(pushes) != 0 {
		t.Errorf("the answer went out as %d plain message(s) instead of into the rotated bubble", len(pushes))
	}
}

// A run whose bubble the guard has rotated must not seize the NEXT question's
// bubble as its own ending: that question's asker would read the previous
// answer, and its own run would find no bubble left. Its answer belongs in
// the stream the rotation opened for it.
func TestARotatedRunDoesNotSealTheNextQuestionsBubble(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)

	// The first round's run is known, and its bubble is rotated mid-run.
	rig.ran(t, "REQ-J1", 1, "task-1")
	_, first := rig.rotated(t, 1)

	// The next question opens a bubble of its own, and its own run.
	rig.ran(t, "REQ-J2", 2, "task-2")
	second := rig.streamIDOf(t, 2)

	rig.answer(t, "the first run's answer", "task-1")

	frames := rig.conn.streamFrames(t)
	sealed := frames[len(frames)-1]
	if sealed["content"] != "the first run's answer" || sealed["finish"] != true {
		t.Fatalf("the first run's answer did not seal a bubble: %v", sealed)
	}
	if sealed["id"] == second {
		t.Fatal("the first run seized the second question's bubble; that question's asker reads the wrong answer and its own run has nowhere to land")
	}
	if sealed["id"] != first {
		t.Fatalf("the first run's answer sealed stream %v, want the one its rotation opened (%s)", sealed["id"], first)
	}
	if rig.streams.depth() != 1 {
		t.Fatalf("store holds %d open rounds, want 1 — the second question kept its bubble", rig.streams.depth())
	}
	// And the second round's own answer still lands where it belongs.
	rig.answer(t, "the second run's answer", "task-2")
	frames = rig.conn.streamFrames(t)
	last := frames[len(frames)-1]
	if last["id"] != second || last["content"] != "the second run's answer" || last["finish"] != true {
		t.Fatalf("the second question's own answer did not seal its bubble: %v", last)
	}
}

// ---- the protocol window these two constants stand for ----

// TestALongRunStillAnswersInItsBubbleInsideTheMeasuredWindow is what pins
// streamMaxAge to what was measured rather than to what was guessed.
//
// Eight minutes is the interesting number: inside the ten the server actually
// allows, outside the six this adapter used to assume. Every other test here
// either disables the guard and drives the clock in relative steps, or moves
// time by streamMaxAge itself — so all of them follow the constant wherever it
// goes and none of them notices it being wrong. Set the window back to six and
// this run's answer stops landing in the bubble the asker has been watching for
// eight minutes and arrives underneath it as a separate message instead, with
// the spinner above it never sealed.
func TestALongRunStillAnswersInItsBubbleInsideTheMeasuredWindow(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-LONG", 1, "task-1")

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

// TestTheGuardRotatesTheBubbleWhileTheServerStillAcceptsFrames pins the other
// constant, as the relationship that gives it its meaning.
//
// The guard's whole job is to hand the run over to a fresh stream before the
// server stops accepting frames on the old one. Its hand-over frame is written
// at streamGuardAfter and has to be accepted, so it needs real headroom under
// the measured ceiling — not merely a value below streamMaxAge. Without this, a
// guard moved up to the window's edge still passes every test in this file and
// fails only in production, where the hand-over is refused and the user is
// left watching the spinner it was supposed to replace.
func TestTheGuardRotatesTheBubbleWhileTheServerStillAcceptsFrames(t *testing.T) {
	t.Parallel()
	const headroom = time.Minute
	if streamGuardAfter >= streamMaxAge {
		t.Fatalf("the guard fires at %v and the window closes at %v — the guard's own frame is "+
			"refused, so the bubble it exists to hand over spins for good", streamGuardAfter, streamMaxAge)
	}
	if got := streamMaxAge - streamGuardAfter; got < headroom {
		t.Fatalf("the guard fires %v before the window closes, want at least %v — one slow frame "+
			"and the hand-over is refused, leaving the asker the spinner the guard was there to "+
			"replace", got, headroom)
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
