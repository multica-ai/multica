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
	if c.refuseClosingCode != 0 && isClosingFrame(env) {
		code = c.refuseClosingCode
	}
	c.mu.Unlock()
	if s != nil {
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

// bubbleRig is one installation with a live socket, a store, the typing
// indicator that opens bubbles and the outbound subscriber that closes them —
// the two halves of the feature, wired to the one store the way boot wires
// them.
type bubbleRig struct {
	conn    *bubbleConn
	streams *streamStore
	typing  *TypingIndicatorManager
	out     *Outbound
	instID  pgtype.UUID
	now     time.Time
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
		instID:  instID,
		now:     time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC),
	}
	streams.now = func() time.Time { return rig.now }

	rig.typing = NewTypingIndicator(TypingIndicatorConfig{
		Senders: reg,
		Streams: streams,
		// No guard: these tests drive the endings themselves.
		GuardAfter: -1,
	})
	q := &fakeOutboundQueries{
		sessionBinding: db.ChannelChatSessionBinding{InstallationID: instID, ChannelChatID: "CHAT_1", ChatType: "p2p"},
		installation:   db.ChannelInstallation{ID: instID, Status: string(InstallationActive)},
	}
	rig.out = NewOutbound(q, reg, streams, nil)
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
// Router does after a successful ingest.
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

func (r *bubbleRig) answer(t *testing.T, content, taskID string) {
	t.Helper()
	if err := r.out.processEvent(context.Background(), events.Event{
		ChatSessionID: bubbleSession,
		TaskID:        taskID,
		Payload:       protocol.ChatDonePayload{Content: content},
	}); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
}

// WeCom has no typing indicator, no reaction and no read receipt. The opening
// stream frame IS the receipt — a think tag renders as the client's own
// animated dots — and without it a slow agent looks like a dead bot.
func TestAQuestionPaintsALoadingBubbleImmediately(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ask(t, "REQ-A")

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
	rig.ask(t, "REQ-B")
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

// A blank closing frame is DISCARDED by WeCom, and the bubble it was meant to
// seal spins for good. An empty completion is a legitimate outcome — the agent
// had nothing to add — so the copy stands in for the silence.
func TestAnEmptyAnswerStillClosesTheBubbleWithWords(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ask(t, "REQ-C")
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
	if content != streamCopyNoReply {
		t.Errorf("closing copy = %q, want %q", content, streamCopyNoReply)
	}
}

// A message that arrives past the debounce window is a round of its own,
// queued behind the run in flight — and it gets its own bubble immediately,
// because a wait with nothing on screen reads as a message that was lost.
func TestAQueuedQuestionGetsItsOwnBubble(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ask(t, "REQ-D1")
	rig.now = rig.now.Add(sameRoundWindow + time.Second)
	rig.ask(t, "REQ-D2")

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

// Two messages inside the debounce window are ONE run, so they share one
// bubble. A second bubble here is one nobody would ever close.
func TestMessagesInsideTheDebounceWindowShareOneBubble(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ask(t, "REQ-E1")
	rig.now = rig.now.Add(sameRoundWindow / 3)
	rig.ask(t, "REQ-E2")

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
	rig.ask(t, "REQ-F1")
	rig.now = rig.now.Add(sameRoundWindow + time.Second)
	rig.ask(t, "REQ-F2")

	rig.answer(t, "the first reply", "task-1") // seals the head
	rig.answer(t, "", "task-2")                // seals the queued one

	frames := rig.conn.streamFrames(t)
	if len(frames) != 4 {
		t.Fatalf("got %d stream frames, want 4 (two opens, two seals)", len(frames))
	}
	if frames[3]["content"] != streamCopyMerged {
		t.Errorf("a queued round's empty answer closed with %q, want %q",
			frames[3]["content"], streamCopyMerged)
	}
}

// When the server refuses the closing frame the bubble cannot be sealed, but
// the ANSWER still has to reach the user — as an ordinary message.
func TestARefusedClosingFrameStillDeliversTheAnswer(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.conn.refuseClosingCode = errcodeStreamExpired
	rig.ask(t, "REQ-G")
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
	rig.ask(t, "REQ-H")

	rig.typing.handleTaskFailed(events.Event{
		Type:          protocol.EventTaskFailed,
		ChatSessionID: bubbleSession,
		TaskID:        "task-1",
	})

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

// The guard is what keeps a run longer than the protocol's window from leaving
// a spinner nobody can touch. It ends the BUBBLE without ending the round: the
// reply is still owed, and the note it leaves is what lets a later failure be
// reported at all.
func TestTheGuardClosesABubbleTheWindowIsAboutToStrand(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.typing.guardAfter = time.Millisecond
	rig.ask(t, "REQ-I")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(rig.conn.streamFrames(t)) < 2 {
		time.Sleep(time.Millisecond)
	}
	frames := rig.conn.streamFrames(t)
	if len(frames) != 2 {
		t.Fatalf("the guard wrote %d stream frames, want 2 — the bubble runs into the window and strands", len(frames))
	}
	if frames[1]["content"] != streamCopyStillWorking {
		t.Errorf("guard copy = %q, want %q", frames[1]["content"], streamCopyStillWorking)
	}
	// The round is NOT over: the guard promised a separate reply.
	if _, verdict := rig.streams.claimEnding(bubbleSessionID(t)); verdict != roundOwesAnEnding {
		t.Fatalf("after a guard close the store says %v, want roundOwesAnEnding — the promised reply would never be sent", verdict)
	}
}

// A run whose bubble the guard already closed must not seize the NEXT
// question's bubble as its own ending: that question's asker would read the
// previous answer, and its own run would find no bubble left.
func TestAGuardClosedRunDoesNotSealTheNextQuestionsBubble(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	sessionID := bubbleSessionID(t)

	rig.ask(t, "REQ-J1")
	// The first round's run is known, and its bubble is guard-closed mid-run.
	if _, ok := rig.streams.takeTask(sessionID, "task-1", roundOver); !ok {
		t.Fatal("could not match the first round to its run")
	}
	rig.streams.remember(sessionID, roundAddress{InstallationID: rig.instID, ChatID: "CHAT_1", ChatType: chatTypeSingleInt}, roundContinues, "task-1")

	// The next question opens a bubble of its own.
	rig.now = rig.now.Add(sameRoundWindow + time.Second)
	rig.ask(t, "REQ-J2")

	if _, ok := rig.streams.takeTask(sessionID, "task-1", roundOver); ok {
		t.Fatal("the first run seized the second question's bubble; that question's asker reads the wrong answer and its own run has nowhere to land")
	}
	if rig.streams.depth() != 1 {
		t.Fatalf("store holds %d open rounds, want 1 — the second question kept its bubble", rig.streams.depth())
	}
}
