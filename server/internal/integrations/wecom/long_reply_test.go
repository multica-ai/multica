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
	"unicode/utf8"
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
		for j, got := range strings.Split(body, "\n") {
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
