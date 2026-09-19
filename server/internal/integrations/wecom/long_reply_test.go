package wecom

// long_reply_test.go — what happens to an answer longer than one WeCom
// message.
//
// aibot caps a single body at 20480 utf8 bytes and refuses anything past it
// WHOLE: it does not clip, it answers 45002 and writes nothing. So a long
// answer used to reach the person in one of two ways, both of them bad. Down
// the plain path it never arrived at all — the frame was refused and the only
// record was a log line. Into a streaming bubble it arrived clipped, ending in
// an ellipsis, with no way to read the rest of it anywhere: WeCom has no edit
// and no unsend, and the tail of a code review or a pasted log is not filler.
//
// The invariant these tests hold the code to is the person's, not the wire's:
// whatever the agent wrote, the person can read all of it in the chat. How
// many messages that takes is an implementation detail; losing any of it is
// the defect.

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// capEnforcingConn is the server's own rule: a markdown body past the cap is
// refused with 45002 and never written into the chat. Modelling the refusal
// rather than just recording the write is the point — delivered() returns what
// the person can actually read, so a test cannot pass by writing a frame
// nobody ever saw.
type capEnforcingConn struct {
	mu     sync.Mutex
	frames []frameEnvelope
	seen   []string // contents of the frames the server accepted
	sender *wsSender
}

func (c *capEnforcingConn) WriteMessage(_ int, data []byte) error {
	var env frameEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}
	content := markdownContentOf(env)
	code, msg := 0, ""
	if len(content) > sendMsgContentLimit {
		code, msg = 45002, "content exceed max length"
	}
	c.mu.Lock()
	c.frames = append(c.frames, env)
	if code == 0 && env.Cmd == cmdSendMsg {
		c.seen = append(c.seen, content)
	}
	s := c.sender
	c.mu.Unlock()
	if s != nil {
		s.routeResponse(frameEnvelope{Headers: frameHeaders{ReqID: env.Headers.ReqID}, ErrCode: code, ErrMsg: msg})
	}
	return nil
}

func (c *capEnforcingConn) ReadMessage() (int, []byte, error) { return 0, nil, nil }
func (c *capEnforcingConn) SetReadDeadline(time.Time) error   { return nil }
func (c *capEnforcingConn) SetWriteDeadline(time.Time) error  { return nil }
func (c *capEnforcingConn) Close() error                      { return nil }

// delivered is everything the person can read, in the order it arrived.
func (c *capEnforcingConn) delivered() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string{}, c.seen...)
}

// markdownContentOf pulls the body text out of an aibot_send_msg frame.
func markdownContentOf(env frameEnvelope) string {
	if env.Cmd != cmdSendMsg {
		return ""
	}
	var body map[string]any
	if json.Unmarshal(env.Body, &body) != nil {
		return ""
	}
	md, _ := body["markdown"].(map[string]any)
	if md == nil {
		return ""
	}
	s, _ := md["content"].(string)
	return s
}

// sendMsgLabel is what one aibot_send_msg put in front of the reader: the
// markdown body for text, and "<msgtype> <media_id>" for a file. Both go out
// on the same push, so a test of the ORDER of what the reader sees has to hold
// both — a wire record that keeps only the text cannot tell whether a picture
// landed in the middle of an answer.
func sendMsgLabel(env frameEnvelope) string {
	if env.Cmd != cmdSendMsg {
		return ""
	}
	if md := markdownContentOf(env); md != "" {
		return md
	}
	var body map[string]any
	if json.Unmarshal(env.Body, &body) != nil {
		return ""
	}
	kind, _ := body["msgtype"].(string)
	nested, _ := body[kind].(map[string]any)
	if nested == nil {
		return kind
	}
	id, _ := nested["media_id"].(string)
	return kind + " " + id
}

// pieceMarker is the continuation counter splitForWire appends. It is stripped
// before reassembly because it is the adapter's word, not the agent's.
var pieceMarker = regexp.MustCompile(`\n\n\(\d+/\d+\)$`)

func reassemble(pieces []string) string {
	var b strings.Builder
	for _, p := range pieces {
		b.WriteString(pieceMarker.ReplaceAllString(p, ""))
	}
	return b.String()
}

// aLongAnswer is a body over two frames' worth with no line breaks in it, so
// the split falls on a rune boundary and reassembly is byte-exact. Multi-byte
// runes on purpose: a cut through one would corrupt the text either side of it.
func aLongAnswer() string {
	return strings.Repeat("答案很长，这是第一段。", sendMsgContentLimit/10)
}

// TestALongAnswerReachesTheChatWhole is the plain path — no bubble, the way
// every reply arrived before streaming and the way one still arrives when the
// bubble is gone. WeCom refuses the oversized frame outright, so without a
// split the person asks a question and gets nothing back at all.

// TestALongAnswerReachesTheChatWhole is the plain path — no bubble, the way
// every reply arrived before streaming and the way one still arrives when the
// bubble is gone. WeCom refuses the oversized frame outright, so without a
// split the person asks a question and gets nothing back at all.
func TestALongAnswerReachesTheChatWhole(t *testing.T) {
	t.Parallel()
	conn := &capEnforcingConn{}
	sender := newWSSender(conn, nil)
	conn.sender = sender

	answer := aLongAnswer()
	if err := sender.sendTextCtx(context.Background(), "CHAT_1", chatTypeSingleInt, answer); err != nil {
		t.Fatalf("sending a %d-byte answer failed: %v", len(answer), err)
	}

	got := conn.delivered()
	if len(got) == 0 {
		t.Fatalf("a %d-byte answer produced nothing the person can read: WeCom refuses a body past %d bytes "+
			"whole, so the question went unanswered and the only record is a log line",
			len(answer), sendMsgContentLimit)
	}
	for i, piece := range got {
		if len(piece) > sendMsgContentLimit {
			t.Fatalf("piece %d is %d bytes, past the %d-byte cap the server refuses — including its own continuation marker",
				i+1, len(piece), sendMsgContentLimit)
		}
	}
	if whole := reassemble(got); whole != answer {
		t.Fatalf("the person can read %d bytes of a %d-byte answer; %d bytes of what the agent wrote never reached the chat",
			len(whole), len(answer), len(answer)-len(whole))
	}
}

// TestAShortAnswerIsUntouched: splitting must cost the ordinary reply nothing —
// no extra frame, and above all no counter appended to an answer that is one
// message long.
func TestAShortAnswerIsUntouched(t *testing.T) {
	t.Parallel()
	conn := &capEnforcingConn{}
	sender := newWSSender(conn, nil)
	conn.sender = sender

	const answer = "答案是 42"
	if err := sender.sendTextCtx(context.Background(), "CHAT_1", chatTypeSingleInt, answer); err != nil {
		t.Fatalf("sendTextCtx: %v", err)
	}
	got := conn.delivered()
	if len(got) != 1 || got[0] != answer {
		t.Fatalf("a short answer went out as %q, want exactly [%q]", got, answer)
	}
}

// TestEachPieceSaysTheAnswerContinues: the reader has to know a message is
// part of something longer, or they read the first piece as the whole answer
// and act on half of it. The counter belongs to no language on purpose — it
// needs no translation, and it cannot contradict an answer written in one.
func TestEachPieceSaysTheAnswerContinues(t *testing.T) {
	t.Parallel()
	pieces := splitForWire(aLongAnswer())
	if len(pieces) < 2 {
		t.Fatalf("the fixture did not split: %d piece(s)", len(pieces))
	}
	for i, p := range pieces[:len(pieces)-1] {
		want := "\n\n(" + strconv.Itoa(i+1) + "/" + strconv.Itoa(len(pieces)) + ")"
		if !strings.HasSuffix(p, want) {
			t.Fatalf("piece %d does not end in %q, so the reader has nothing telling them the answer continues; it ends %q",
				i+1, want, tail(p, 12))
		}
	}
	if last := pieces[len(pieces)-1]; pieceMarker.MatchString(last) {
		t.Fatalf("the final piece carries a continuation marker (%q), which promises more that never comes", tail(last, 12))
	}
}

// TestAPieceNeverEndsMidCharacter: a cut through a multi-byte rune shows up in
// the chat as a replacement glyph on both sides of the seam.
func TestAPieceNeverEndsMidCharacter(t *testing.T) {
	t.Parallel()
	for i, p := range splitForWire(aLongAnswer()) {
		if !utf8.ValidString(p) {
			t.Fatalf("piece %d is not valid utf8 — the cut went through a character, "+
				"and the reader sees a replacement glyph on both sides of the seam", i+1)
		}
	}
}

// TestTheCutPrefersALineBoundary: a long answer is usually a log or a code
// block, and a piece that ends mid-line reads far worse than one that ends
// where the text already ended. The preference is bounded — a break near the
// start of the budget would waste most of a message.
func TestTheCutPrefersALineBoundary(t *testing.T) {
	t.Parallel()
	line := strings.Repeat("x", 79) + "\n"
	pieces := splitForWire(strings.Repeat(line, sendMsgContentLimit/len(line)*2+40))
	if len(pieces) < 2 {
		t.Fatalf("the fixture did not split: %d piece(s)", len(pieces))
	}
	for i, p := range pieces[:len(pieces)-1] {
		body := pieceMarker.ReplaceAllString(p, "")
		// The break that ended the last line stays with it, so a piece cut at
		// a line boundary ends in one. Splitting on it would leave an empty
		// final element that is not a short line.
		if !strings.HasSuffix(body, "\n") {
			t.Fatalf("piece %d ends %q, want the line break that ended its last line", i+1, tail(body, 12))
		}
		for j, got := range strings.Split(strings.TrimSuffix(body, "\n"), "\n") {
			if len(got) != len(line)-1 {
				t.Fatalf("piece %d line %d is %d characters of a %d-character line — the cut fell mid-line",
					i+1, j+1, len(got), len(line)-1)
			}
		}
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// aLongAnswerWithLineBreaks is the fixture the line-boundary branch needs: the
// cut lands on a '\n' rather than a rune boundary, and the text carries the
// three things a seam can eat — a single newline, a run of them, and multi-byte
// runes either side.
//
// Built so the breaks fall at irregular distances: a fixture whose lines are
// all the same length can put every cut in the same relative position and miss
// the case where the chosen break is the last byte of the budget.
func aLongAnswerWithLineBreaks() string {
	var b strings.Builder
	for i := 0; b.Len() < sendMsgContentLimit*2+500; i++ {
		switch i % 7 {
		case 0:
			b.WriteString("第一行，带一个换行\n")
		case 3:
			// A blank line: two breaks in a row, which is what a paragraph
			// boundary in a pasted log looks like.
			b.WriteString("一段结束\n\n")
		case 5:
			// No break at all, so some cuts still fall on a rune boundary.
			b.WriteString(strings.Repeat("连续文字没有换行", 40))
		default:
			b.WriteString("普通的一行，长度不一样一点点\n")
		}
	}
	return b.String()
}

// Every byte the agent wrote comes back, including the line breaks at the
// seams. This is the invariant this file's header states, and it is the one
// the first version of the split broke: wireCutPoint returned the index OF the
// newline and splitForWire then trimmed leading newlines from the same index,
// so one break vanished at every cut — worst on pasted logs and code, which is
// the content the split exists for.
//
// REVERSE VERIFICATION: return nl instead of nl+1 from wireCutPoint, or put
// strings.TrimLeft(remaining[cut:], "\n") back, and this fails with the
// reassembled answer shorter than the original by one byte per seam.
func TestReassemblingThePiecesGivesBackEveryByte(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body string
	}{
		{"line breaks, blank lines and multi-byte runes", aLongAnswerWithLineBreaks()},
		{"no line breaks at all", aLongAnswer()},
		{"a single break right at the budget", strings.Repeat("字", sendMsgContentLimit/3) + "\n" + strings.Repeat("字", sendMsgContentLimit)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pieces := splitForWire(tc.body)
			if len(pieces) < 2 {
				t.Fatalf("fixture produced %d piece(s); it is not exercising the split", len(pieces))
			}
			got := reassemble(pieces)
			if got == tc.body {
				return
			}
			// Report where they part company rather than dumping 40KB.
			n := 0
			for n < len(got) && n < len(tc.body) && got[n] == tc.body[n] {
				n++
			}
			t.Fatalf("reassembled %d bytes from %d pieces, want the original %d — they part at byte %d: original has %q, reassembly has %q",
				len(got), len(pieces), len(tc.body), n,
				safeWindow(tc.body, n), safeWindow(got, n))
		})
	}
}

// safeWindow is a short excerpt around i, for a failure message that has to be
// readable when the fixture is tens of kilobytes.
func safeWindow(s string, i int) string {
	lo, hi := i-12, i+12
	if lo < 0 {
		lo = 0
	}
	if hi > len(s) {
		hi = len(s)
	}
	return s[lo:hi]
}

// Each piece still has to fit the wire. The reassembly test above would pass
// against a split that emitted the whole answer as one oversized piece, which
// is the failure the platform refuses with 45002.
func TestEveryPieceFitsTheWireLimit(t *testing.T) {
	t.Parallel()
	for _, body := range []string{aLongAnswerWithLineBreaks(), aLongAnswer()} {
		for i, p := range splitForWire(body) {
			if len(p) > sendMsgContentLimit {
				t.Fatalf("piece %d is %d bytes, over the %d the server takes", i+1, len(p), sendMsgContentLimit)
			}
		}
	}
}

// Keeping every byte says nothing about WHICH side of the seam a line break
// lands on, and the two are not equally readable: a piece that opens with the
// break that ended the previous piece's last line renders as a blank first
// line, which looks like the message lost something. The break belongs to the
// line it terminated, so the cut goes after it.
//
// REVERSE VERIFICATION: return nl instead of nl+1 from wireCutPoint and this
// fails with piece 2 starting "\n…".
func TestNoPieceOpensWithTheBreakThatEndedTheLastOne(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, body string }{
		{"line breaks, blank lines and multi-byte runes", aLongAnswerWithLineBreaks()},
		{"a single break right at the budget", strings.Repeat("字", sendMsgContentLimit/3) + "\n" + strings.Repeat("字", sendMsgContentLimit)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pieces := splitForWire(tc.body)
			for i, p := range pieces[1:] {
				if strings.HasPrefix(p, "\n") {
					t.Fatalf("piece %d opens with %q — the break that ended piece %d fell into the gap", i+2, safeWindow(p, 0), i+1)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// a partial send counts the same on both paths
// ---------------------------------------------------------------------------

// One long answer, piece one accepted and piece two refused, is one
// user-visible event: most of the answer is in the chat and some of it is not.
// It has to move the same counter wherever the send happened, or a partial
// delivery pages an operator on a multi-replica deployment and passes
// unnoticed on a single-replica one — the same reply, two verdicts, decided by
// which replica held the socket.
//
// The pair below is the direct path (the replica that produced the
// completion); the relay pair further down is the lease holder.
//
// REVERSE VERIFICATION: drop the errPartiallySent arm from recordSend and this
// reports outbound_delivered = 0 with outbound_dropped = 1.
func TestDirectPath_ARefusedSecondPieceCountsDelivered(t *testing.T) {
	t.Parallel()
	o, conn, mx := newPartialSendRig(t)
	conn.refuseFromSend = 2

	if err := o.processEvent(context.Background(), aLongAnswerEvent()); err != nil {
		t.Fatalf("processEvent: %v", err)
	}

	if got := len(conn.sendFrames()); got < 2 {
		t.Fatalf("the socket saw %d send frame(s); the fixture did not split", got)
	}
	assertPartialCounted(t, mx, "a refused second piece")
}

// The other half: the second piece's verdict never comes back. The user's
// screen is in the same state — piece one is on it — and the same counter has
// to move, for the same reason: the operator action an unconfirmed reply
// invites is "resend it", and resending prints piece one twice.
//
// REVERSE VERIFICATION: drop the errPartiallySent arm from recordSend and this
// reports outbound_unconfirmed = 1 with outbound_delivered = 0.
func TestDirectPath_ALostAckOnTheSecondPieceCountsDelivered(t *testing.T) {
	t.Parallel()
	o, conn, mx := newPartialSendRig(t)
	conn.swallowAckFromSend = 2

	// The frame goes out and no verdict ever comes back. ackTimeout is five
	// seconds and a test that stands still for it is a test nobody runs, so
	// the caller's own deadline ends the wait instead — which is the same
	// branch of request(), and the same shape of failure the outbound
	// subscriber sees when a delivery runs out its budget mid-answer.
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if err := o.processEvent(ctx, aLongAnswerEvent()); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if got := len(conn.sendFrames()); got < 2 {
		t.Fatalf("the socket saw %d send frame(s); the second piece never reached the wire, so this is not a lost verdict", got)
	}
	assertPartialCounted(t, mx, "a lost ack on the second piece")
}

// newPartialSendRig is one replica that holds the socket, with counters.
func newPartialSendRig(t *testing.T) (*Outbound, *recordingConn, *countingMetrics) {
	t.Helper()
	q := &fakeOutboundQueries{
		sessionBinding:  db.ChannelChatSessionBinding{ChannelChatID: "CHAT_1", ChatType: "group"},
		installation:    db.ChannelInstallation{Status: string(InstallationActive)},
		channelIngested: askedOverWecom(),
	}
	q.fileTask(t, "33333333-3333-3333-3333-333333333333")
	reg := newSendersRegistry()
	instID := mustTestUUID(t)
	conn := &recordingConn{}
	reg.set(instID, conn.autoAck(newWSSender(conn, testLogger())))
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID
	mx := newCountingMetrics()
	return NewOutbound(q, reg, testLogger(), WithOutboundMetrics(mx)), conn, mx
}

func aLongAnswerEvent() events.Event {
	return events.Event{
		ChatSessionID: "22222222-2222-2222-2222-222222222222",
		Payload: protocol.ChatDonePayload{
			Content: aLongAnswer(),
			TaskID:  "33333333-3333-3333-3333-333333333333",
		},
	}
}

func assertPartialCounted(t *testing.T, mx *countingMetrics, what string) {
	t.Helper()
	if got := mx.get("outbound_delivered"); got != 1 {
		t.Errorf("outbound_delivered = %d, want 1 — %s still left most of the answer on the person's screen", got, what)
	}
	if got := mx.get("outbound_dropped"); got != 0 {
		t.Errorf("outbound_dropped = %d, want 0 — a drop tells an operator to resend, and a resend prints piece one twice", got)
	}
	if got := mx.get("outbound_unconfirmed"); got != 0 {
		t.Errorf("outbound_unconfirmed = %d, want 0 — the delivery is not unknown; part of it is on screen", got)
	}
}

// ---------------------------------------------------------------------------
// the pieces of one answer are contiguous on the wire
// ---------------------------------------------------------------------------

// slowAckConn answers every frame, after a delay, from its own goroutine —
// which is what the real server does and what the sender's mutex does NOT
// cover: mu orders one frame write and is released before the ack wait.
type slowAckConn struct {
	mu     sync.Mutex
	texts  []string
	sender *wsSender
	delay  time.Duration

	// swallowFrom, when non-zero, is the 1-based aibot_send_msg from which no
	// verdict ever comes back — a peer that took the bytes and went quiet.
	// The frame is still recorded: it reached the wire, which is the half this
	// models.
	swallowFrom int
	sends       int
}

func (c *slowAckConn) WriteMessage(_ int, data []byte) error {
	var env frameEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}
	c.mu.Lock()
	swallow := false
	if env.Cmd == cmdSendMsg {
		c.texts = append(c.texts, sendMsgLabel(env))
		c.sends++
		swallow = c.swallowFrom > 0 && c.sends >= c.swallowFrom
	}
	s, d := c.sender, c.delay
	c.mu.Unlock()
	if s != nil && !swallow {
		go func() {
			time.Sleep(d)
			s.routeResponse(frameEnvelope{Headers: frameHeaders{ReqID: env.Headers.ReqID}})
		}()
	}
	return nil
}

func (c *slowAckConn) ReadMessage() (int, []byte, error) { return 0, nil, nil }
func (c *slowAckConn) SetReadDeadline(time.Time) error   { return nil }
func (c *slowAckConn) SetWriteDeadline(time.Time) error  { return nil }
func (c *slowAckConn) Close() error                      { return nil }

func (c *slowAckConn) wire() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.texts...)
}

// One long answer and one unrelated push to the same chat, started together.
// The answer's pieces have to reach the wire as one run: a reader who finds
// somebody else's message between "(1/3)" and "(2/3)" has no way to tell which
// text the counters belong to, and with two long answers in flight the
// counters cannot be matched back at all — which is worse than the status quo,
// where a long answer simply never arrived.
//
// The ack delay is what makes this reproduce: the sender's mutex is released
// before the wait, so without a per-chat lock the interloper wins the mutex
// while piece one is still unacknowledged.
//
// REVERSE VERIFICATION: remove the s.chats.acquire/release pair from
// sendTextCtx and this fails with the interloper between two pieces.
func TestThePiecesOfOneAnswerAreContiguousOnTheWire(t *testing.T) {
	t.Parallel()
	conn := &slowAckConn{delay: 30 * time.Millisecond}
	sender := newWSSender(conn, testLogger())
	conn.sender = sender

	answer := aLongAnswer()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := sender.sendTextCtx(context.Background(), "CHAT_1", chatTypeSingleInt, answer); err != nil {
			t.Errorf("sending the long answer: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		// Started a beat later so the answer is already mid-flight; without
		// the lock this lands between two of its pieces.
		time.Sleep(10 * time.Millisecond)
		if err := sender.sendTextCtx(context.Background(), "CHAT_1", chatTypeSingleInt, "INTERLOPER"); err != nil {
			t.Errorf("sending the unrelated push: %v", err)
		}
	}()
	wg.Wait()

	assertAnswerIsUninterrupted(t, conn.wire(), "INTERLOPER")
}

// assertAnswerIsUninterrupted reads the wire as the person does: every push
// that is not the interloper is a piece of the one answer, and the interloper
// must sit before all of them or after all of them, never inside the run.
func assertAnswerIsUninterrupted(t *testing.T, wire []string, interloper string) {
	t.Helper()
	first, last := -1, -1
	for i, f := range wire {
		if f == interloper {
			continue
		}
		if first < 0 {
			first = i
		}
		last = i
	}
	if first < 0 || last <= first {
		t.Fatalf("the answer produced %d piece(s) on the wire: %v", last-first+1, summarize(wire))
	}
	for i := first; i <= last; i++ {
		if wire[i] == interloper {
			t.Fatalf("%q landed at position %d, between pieces of one answer: %v", interloper, i+1, summarize(wire))
		}
	}
}

// The same rule, for the push the lock never covered. sendTextCtx takes the
// per-chat lock and sendMedia went straight to request(), so a file delivered
// while an answer was still being written landed between two of its pieces —
// and the comment above sendTextCtx says in as many words that the lock is
// held for every send and names media as the thing it keeps out.
//
// Attachment delivery is asynchronous (deliverAttachmentsByID spawns it), so
// this is not a rare interleaving: the answer's pieces and the file it came
// with are in flight at the same time by construction.
//
// The ack delay is what reproduces it. The sender's write mutex is released
// before the wait, so the media push wins that mutex while piece one is still
// unacknowledged.
//
// REVERSE VERIFICATION: drop the s.chats.acquire/release pair from sendMedia
// and this fails with the file between two pieces.
func TestAFileDoesNotLandBetweenThePiecesOfOneAnswer(t *testing.T) {
	t.Parallel()
	conn := &slowAckConn{delay: 30 * time.Millisecond}
	sender := newWSSender(conn, testLogger())
	conn.sender = sender

	answer := aLongAnswer()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := sender.sendTextCtx(context.Background(), "CHAT_1", chatTypeSingleInt, answer); err != nil {
			t.Errorf("sending the long answer: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		// A beat later, so the answer is already mid-flight — the shape of a
		// file the same turn produced, delivered by its own goroutine.
		time.Sleep(10 * time.Millisecond)
		if err := sender.sendMedia(context.Background(), "CHAT_1", chatTypeSingleInt, mediaSend{
			Kind:    mediaTypeImage,
			MediaID: "MEDIA_1",
		}); err != nil {
			t.Errorf("sending the file: %v", err)
		}
	}()
	wg.Wait()

	assertAnswerIsUninterrupted(t, conn.wire(), "image MEDIA_1")
}

// The other side of the same rule: the lock is per chat, so an answer to one
// room must not hold up a push to another. Serializing the whole socket would
// pass the test above and be a head-of-line block on every other conversation.
//
// REVERSE VERIFICATION: key the lock on a constant instead of chatID and this
// times out — the second chat's push waits out the first answer's acks.
func TestAnAnswerToOneChatDoesNotHoldUpAnother(t *testing.T) {
	t.Parallel()
	conn := &slowAckConn{delay: 40 * time.Millisecond}
	sender := newWSSender(conn, testLogger())
	conn.sender = sender

	// Three pieces at 40ms an ack is ~120ms of answer; the other chat's single
	// frame is ~40ms and starts 10ms in. So "did it wait" is not a stopwatch
	// reading — the other push has to come back while the answer is still
	// going, and a socket-wide lock cannot produce that.
	slow := aLongAnswerWithLineBreaks()
	if n := len(splitForWire(slow)); n < 3 {
		t.Fatalf("the slow fixture is %d piece(s); it does not hold the lock long enough to tell the two designs apart", n)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = sender.sendTextCtx(context.Background(), "CHAT_SLOW", chatTypeSingleInt, slow)
	}()

	other := make(chan error, 1)
	go func() {
		time.Sleep(10 * time.Millisecond)
		other <- sender.sendTextCtx(context.Background(), "CHAT_OTHER", chatTypeSingleInt, "a short reply")
	}()

	select {
	case err := <-other:
		if err != nil {
			t.Fatalf("the other chat's push failed: %v", err)
		}
	case <-done:
		t.Fatal("the answer to one chat finished before a push to another got through — the lock is not per chat, it is the whole socket")
	case <-time.After(5 * time.Second):
		t.Fatal("a push to a different chat never came back")
	}
	<-done
}

// The lock has a second outcome, and before this nothing had a name for it:
// the WAIT runs out. sendTextCtx takes the chat's turn first and builds a
// frame second, so a caller whose context ends while queued has put not one
// byte anywhere — and that is the most retryable failure this package has.
//
// Both classifiers read it as the opposite. acquire returned a bare ctx.Err(),
// so unconfirmedReason filed it under "interrupted" — an outcome nobody may
// resend, because the message might be on the person's screen — and
// provablyNotSent said false, which tells the relay to settle the claim and
// stop offering the frame. The user gets nothing and the party whose job is to
// try again is told not to.
//
// REVERSE VERIFICATION: return ctx.Err() bare from chatLocks.acquire again and
// this reports provablyNotSent = false and unconfirmedReason = "interrupted",
// with the same zero frames on the wire.
func TestGivingUpOnTheChatsTurnIsAProvableNonDelivery(t *testing.T) {
	t.Parallel()
	conn := &slowAckConn{delay: time.Millisecond}
	sender := newWSSender(conn, testLogger())
	conn.sender = sender

	// Somebody else is mid-answer to this chat; the lock is theirs.
	release, err := sender.chats.acquire(context.Background(), "CHAT_1")
	if err != nil {
		t.Fatalf("taking the chat's turn: %v", err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	sendErr := sender.sendTextCtx(ctx, "CHAT_1", chatTypeSingleInt, "答案")

	if sendErr == nil {
		t.Fatal("the send reported success while another caller held the chat's turn")
	}
	if got := conn.wire(); len(got) != 0 {
		t.Fatalf("%d frame(s) reached the wire: %v — nothing here is a send that never started", len(got), summarize(got))
	}
	if !provablyNotSent(sendErr) {
		t.Errorf("provablyNotSent(%v) = false, want true — the relay settles the claim on that answer and stops re-offering a message that never reached the socket", sendErr)
	}
	if got := unconfirmedReason(sendErr); got != "" {
		t.Errorf("unconfirmedReason(%v) = %q, want \"\" — %q says the message may be on the person's screen, and nothing was written", sendErr, got, got)
	}

	// And the direct path's own verdict, through the one mapping it uses.
	mx := newCountingMetrics()
	o := NewOutbound(&fakeOutboundQueries{}, newSendersRegistry(), testLogger(), WithOutboundMetrics(mx))
	o.recordSend(context.Background(), testSessionID, "chat:done", sendErr)
	if got := mx.get("outbound_dropped"); got != 1 {
		t.Errorf("outbound_dropped = %d, want 1 — the reply is definitely not delivered", got)
	}
	if got := mx.get("outbound_unconfirmed"); got != 0 {
		t.Errorf("outbound_unconfirmed = %d, want 0 — \"result unknown\" is what stops an operator resending a message nobody ever sent", got)
	}
}

// summarize keeps a failure message readable when the pieces are 20KB each.
func summarize(wire []string) []string {
	out := make([]string, 0, len(wire))
	for _, f := range wire {
		if len(f) > 24 {
			out = append(out, strconv.Itoa(len(f))+" bytes ending "+tail(f, 8))
			continue
		}
		out = append(out, f)
	}
	return out
}

// A long answer whose tail is a run of blank lines puts that run in a piece of
// its own, and the last piece carries no marker — so it would reach the chat as
// an empty bubble, which is what hasVisibleChar stops at the call sites. The
// split has to stop it too, because by then the call site has already seen a
// body with visible characters in it.
//
// REVERSE VERIFICATION: remove the filter in splitForWire and this fails with
// the last piece carrying no visible character.
func TestAPieceThatRendersAsNothingIsNotSent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, body string }{
		{"a tail of blank lines", "答案的正文在这里。\n" + strings.Repeat("\n", sendMsgContentLimit*2)},
		{"a blank run in the middle", strings.Repeat("字", sendMsgContentLimit/2) + strings.Repeat("\n", sendMsgContentLimit) + "结尾还有字"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !hasVisibleChar(tc.body) {
				t.Fatalf("the fixture has nothing visible in it; the call sites would never reach the split")
			}
			if len(tc.body) <= sendMsgContentLimit {
				t.Fatalf("the fixture is %d bytes and never reaches the split", len(tc.body))
			}
			pieces := splitForWire(tc.body)
			for i, p := range pieces {
				if !hasVisibleChar(p) {
					t.Errorf("piece %d/%d is %d bytes with nothing visible in it — it reaches the chat as an empty bubble",
						i+1, len(pieces), len(p))
				}
			}
			// Nothing a reader can see is lost: every visible rune of the
			// original is still there, in order.
			if got, want := visibleOnly(reassemble(pieces)), visibleOnly(tc.body); got != want {
				t.Fatalf("the visible text changed: %d runes reached the chat, want %d", len([]rune(got)), len([]rune(want)))
			}
		})
	}
}

// visibleOnly is the original with everything the client renders as nothing
// taken out, which is the part the split promises to preserve.
func visibleOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if !unicode.IsSpace(r) && !unicode.IsControl(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
