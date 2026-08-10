package wecom

// trace_test.go — guards for the MULTICA_WECOM_TRACE operator switch. Six
// things have to hold or the switch is not shippable: it records nothing at
// all when off, it records enough to be worth turning on, what it records is
// never a credential, the order it records outbound frames in is the order
// they reached the socket, a frame that failed to go out does not read like
// one that went, and the one line whose value is its exact bytes — an
// attachment's Content-Disposition — is not cut down to the message preview's
// cap.
//
// These tests do NOT call t.Parallel: `tracing` is a package-level atomic and
// the assertions are about whether a log line exists. `go test` runs top-level
// non-parallel tests one at a time, so they do not interleave with each other;
// every one restores the switch on the way out.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

// ---- capture helpers ----

// syncBuf is an io.Writer safe for the Connect goroutine to write while the
// test body reads.
type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// capturingLogger returns a logger writing into a buffer the test can read.
func capturingLogger() (*slog.Logger, *syncBuf) {
	var b syncBuf
	return slog.New(slog.NewTextHandler(&b, &slog.HandlerOptions{Level: slog.LevelDebug})), &b
}

// withTrace sets the switch for the duration of one test and restores it.
func withTrace(t *testing.T, on bool) {
	t.Helper()
	prev := tracingOn()
	SetTrace(on)
	t.Cleanup(func() { SetTrace(prev) })
}

// productionShapedToken mints a token the same way BindingTokenService.Mint
// does (32 random bytes, base64url, no padding → 43 characters), so the
// redaction test is measured against the real credential shape rather than a
// short placeholder that would fit under any cap.
func productionShapedToken(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

// callbackConn acks the subscribe frame, then delivers one aibot_msg_callback,
// then drops the socket so Connect returns. It is the smallest script that
// exercises every trace point: the subscribe write (the dir=out pair), the subscribe
// ack (traceIn in the handshake loop), the callback frame (traceIn in the read
// loop), and the decoded message (traceInbound).
type callbackConn struct {
	mu       sync.Mutex
	queue    [][]byte
	acked    bool
	callback []byte
	closeOne sync.Once
}

func (c *callbackConn) WriteMessage(_ int, data []byte) error {
	var env frameEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil
	}
	if env.Cmd == cmdSubscribe {
		ack, _ := json.Marshal(frameEnvelope{Headers: frameHeaders{ReqID: env.Headers.ReqID}})
		c.mu.Lock()
		c.queue = append(c.queue, ack)
		c.mu.Unlock()
	}
	return nil
}

func (c *callbackConn) ReadMessage() (int, []byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.queue) > 0 {
		next := c.queue[0]
		c.queue = c.queue[1:]
		return websocket.TextMessage, next, nil
	}
	if !c.acked {
		c.acked = true
		return websocket.TextMessage, c.callback, nil
	}
	return 0, nil, errors.New("simulated drop")
}

func (c *callbackConn) SetReadDeadline(time.Time) error  { return nil }
func (c *callbackConn) SetWriteDeadline(time.Time) error { return nil }
func (c *callbackConn) Close() error                     { c.closeOne.Do(func() {}); return nil }

// runOneCallback drives a full Connect against a scripted socket carrying one
// text message, and returns everything the channel logged.
func runOneCallback(t *testing.T, text string) string {
	t.Helper()
	mc := aibotMsgCallback{MsgID: "MSG_1", ChatID: "GROUP_CHAT_ID", ChatType: "group", MsgType: "text"}
	mc.From.UserID = "SENDER_USERID"
	mc.Text.Content = text
	body, err := json.Marshal(mc)
	if err != nil {
		t.Fatalf("marshal callback: %v", err)
	}
	frame, err := json.Marshal(frameEnvelope{Cmd: cmdMsgCallback, Headers: frameHeaders{ReqID: "REQ_1"}, Body: body})
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}

	log, buf := capturingLogger()
	c := &wecomChannel{
		installationID: mustTestUUID(t),
		botID:          "bot-1",
		secret:         "THE_SMART_BOT_SECRET",
		handler:        func(context.Context, channel.InboundMessage) error { return nil },
		dialer:         scriptedDialer{conn: &callbackConn{callback: frame}},
		wsURL:          "wss://example.test/ws",
		logger:         log,
		senders:        newSendersRegistry(),
	}

	done := make(chan struct{})
	go func() { _ = c.Connect(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Connect did not return within 3s")
	}
	return buf.String()
}

// ---- the guards ----

// TestTraceOffRecordsNothing is the one that makes the switch safe to ship:
// with MULTICA_WECOM_TRACE unset, a real inbound message and the frames around
// it must leave no trace line at all. The logger is at LevelDebug — the
// package default (logger.parseLevel falls through to debug) — so a trace line
// that is merely "debug level" would still be written here.
func TestTraceOffRecordsNothing(t *testing.T) {
	withTrace(t, false)

	out := runOneCallback(t, "一条不该被记录的消息")

	if strings.Contains(out, "wecom trace") {
		t.Errorf("tracing is off but a trace line was written:\n%s", out)
	}
	if strings.Contains(out, "一条不该被记录的消息") {
		t.Errorf("tracing is off but the message body reached the log:\n%s", out)
	}
}

// TestTraceOnRecordsBothDirections is the other half: turned on, the switch has
// to actually answer the question it exists for — which frame went which way,
// addressed to which chat, from which person, and what the server said back.
func TestTraceOnRecordsBothDirections(t *testing.T) {
	withTrace(t, true)

	out := runOneCallback(t, "hello from the room")

	for _, want := range []string{
		`dir=out`,             // the subscribe frame we wrote
		`cmd=aibot_subscribe`, //
		`dir=in`,              // the server's ack for it
		`dir=in.msg`,          // the decoded user message
		`chatid=GROUP_CHAT_ID`,
		`sender=SENDER_USERID`,
		`chat_type=group`,
		`msg_id=MSG_1`,
		`text="hello from the room"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("trace output is missing %q; got:\n%s", want, out)
		}
	}
}

// TestTraceNeverLogsTheSmartBotSecret pins the reason traceOutFields reads named
// fields instead of dumping the frame: the aibot_subscribe body carries the
// installation's decrypted smart-bot secret. Replacing the field-by-field
// walk with a body dump fails here.
func TestTraceNeverLogsTheSmartBotSecret(t *testing.T) {
	withTrace(t, true)

	out := runOneCallback(t, "hi")

	if !strings.Contains(out, "cmd=aibot_subscribe") {
		t.Fatalf("the subscribe frame was not traced, so this test proves nothing:\n%s", out)
	}
	if strings.Contains(out, "THE_SMART_BOT_SECRET") {
		t.Errorf("the smart-bot secret reached the log:\n%s", out)
	}
}

// TestTraceNeverLogsABindingToken is the defect this PR had to close before
// the switch could ship. sendBindingPrompt mints a 43-character bearer token
// and puts it in a URL inside the message body; with a normal MULTICA_APP_URL
// that whole URL lands inside the 120-rune preview, so an unredacted preview
// prints a live credential. This drives the real replier path — mint, URL,
// sendText, write, traceOut — and asserts the raw token never appears.
//
// A binding token is a bearer credential: RedeemAndBind checks only that the
// redeemer belongs to the token's workspace, and the bind page redeems on load
// as whoever is signed in. replier.go:150 already refuses to post the link
// into a room for exactly this reason; a log the token is printed into is the
// same hijack by another route.
func TestTraceNeverLogsABindingToken(t *testing.T) {
	withTrace(t, true)

	rawToken := productionShapedToken(t)
	const senderID = "SENDER_USERID"

	log, buf := capturingLogger()
	reg := newSendersRegistry()
	inst := engine.ResolvedInstallation{ID: mustTestUUID(t)}
	conn := &recordingConn{}
	// autoAck, because sendBindingPrompt reads the server's verdict: a double
	// that never answers turns the send into a 5-second timeout and the trace
	// this test reads is never written.
	reg.set(inst.ID, conn.autoAck(newWSSender(conn, log)))
	r := NewOutboundReplier(OutboundReplierConfig{Senders: reg, AppURL: "https://multica.example"})
	r.binding = fakeBinder{raw: rawToken}

	msg := channel.InboundMessage{Source: channel.Source{
		ChatID:   "GROUP_CHAT_ID",
		ChatType: channel.ChatTypeGroup,
		SenderID: senderID,
	}}
	if err := r.sendBindingPrompt(context.Background(), inst, msg, engine.Result{Sender: senderID}); err != nil {
		t.Fatalf("sendBindingPrompt: %v", err)
	}

	out := buf.String()
	// Guard against a vacuous pass: the prompt must have been traced at all.
	if !strings.Contains(out, "dir=out") || !strings.Contains(out, "wecom/bind") {
		t.Fatalf("the binding prompt was not traced, so this test proves nothing:\n%s", out)
	}
	if strings.Contains(out, rawToken) {
		t.Errorf("the raw binding token reached the log:\n%s", out)
	}
	if !strings.Contains(out, "token=[redacted]") {
		t.Errorf("expected the token parameter to be redacted in place; got:\n%s", out)
	}
}

// TestBindingPromptFitsInsideThePreviewCap is the measurement behind the test
// above: it shows the leak is reachable rather than theoretical. It rebuilds
// the exact prompt sendBindingPrompt builds and asserts the token's LAST
// character still falls inside tracePreviewRunes — i.e. an unredacted preview
// would print the credential whole, not a useless fragment.
func TestBindingPromptFitsInsideThePreviewCap(t *testing.T) {
	rawToken := productionShapedToken(t)
	if len(rawToken) != 43 {
		t.Fatalf("token length = %d, want the 43 chars Mint produces", len(rawToken))
	}
	// Mirrors replier.go sendBindingPrompt.
	bindURL := "https://multica.example" + "/wecom/bind" + "?token=" + url.QueryEscape(rawToken)
	upToTokenEnd := "👋 请先绑定你的 Multica 账号，才能与我对话：\n" + bindURL

	if n := utf8.RuneCountInString(upToTokenEnd); n > tracePreviewRunes {
		t.Fatalf("the token ends at rune %d, past the %d-rune cap — the leak this "+
			"PR redacts would not be reachable and the redaction rationale needs revisiting",
			n, tracePreviewRunes)
	}
}

// TestTracePreviewCutsOnARuneBoundary pins the cap to runes, not bytes. A
// Chinese message is 3 bytes per character, so a byte-based cut both truncates
// far too early and can split a character into invalid UTF-8 — which is how a
// log line stops being readable in the deployment this switch exists for.
// The offset case matters on its own: with a body that is a whole number of
// 3-byte characters, a byte-based cut lands between characters by luck and
// only truncates early. One ASCII character in front of the same body is
// enough to make the same cut land mid-character and emit invalid UTF-8.
func TestTracePreviewCutsOnARuneBoundary(t *testing.T) {
	for _, body := range []string{
		strings.Repeat("测", 300),
		"x" + strings.Repeat("测", 300),
	} {
		got := tracePreview(body)

		if !utf8.ValidString(got) {
			t.Errorf("preview of %d-rune body is not valid UTF-8: %q", utf8.RuneCountInString(body), got)
		}
		trimmed := strings.TrimSuffix(got, "…")
		if trimmed == got {
			t.Errorf("a %d-rune body should have been marked as cut with …, got %q", utf8.RuneCountInString(body), got)
		}
		if n := utf8.RuneCountInString(trimmed); n != tracePreviewRunes {
			t.Errorf("preview kept %d runes, want exactly %d", n, tracePreviewRunes)
		}
		if strings.ContainsRune(trimmed, utf8.RuneError) {
			t.Errorf("preview contains a replacement character — a multi-byte rune was split: %q", trimmed)
		}
	}
}

// TestTracePreviewFlattensNewlines keeps one frame on one log line. The
// binding prompt is three lines; split across log records it is far harder to
// pair with the frame it belongs to.
func TestTracePreviewFlattensNewlines(t *testing.T) {
	got := tracePreview("first\nsecond\r\nthird")
	if strings.ContainsAny(got, "\n\r") {
		t.Errorf("preview still contains a line break: %q", got)
	}
	if !strings.Contains(got, "first") || !strings.Contains(got, "third") {
		t.Errorf("preview dropped content while flattening: %q", got)
	}
}

// TestRedactBearerTokensCoversTheTokenShapes covers the parameter names worth
// hiding wherever they appear — the function matches on the query parameter,
// not on the configured bind path, because BindingPath is configurable and
// this package-level function cannot see it.
func TestRedactBearerTokensCoversTheTokenShapes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"binding url", "go to https://x.test/wecom/bind?token=abc123DEF now", "go to https://x.test/wecom/bind?token=[redacted] now"},
		{"custom path", "https://x.test/custom/path?token=abc123DEF", "https://x.test/custom/path?token=[redacted]"},
		{"binding_token param", "?binding_token=abc123DEF", "?binding_token=[redacted]"},
		{"access_token param", "?access_token=abc123DEF", "?access_token=[redacted]"},
		{"oauth code", "?code=abc123DEF", "?code=[redacted]"},
		{"stops at ampersand", "?token=abc123DEF&next=/home", "?token=[redacted]&next=/home"},
		{"leaves ordinary text alone", "the token is not in a url here", "the token is not in a url here"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := redactBearerTokens(tc.in); got != tc.want {
				t.Errorf("redactBearerTokens(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// ---- outbound ordering + outcome ----
//
// wsSender.write is the single writer for one connection, and the ping loop,
// agent replies and inbox pushes all reach it concurrently. What follows pins
// the two properties that gives the trace: the recorded order of outbound
// frames is the order they reached WriteMessage, and every recorded attempt
// carries a recorded outcome saying whether the socket took it.

// traceLine is one "wecom trace" record with its attributes pulled apart, so a
// test can ask for a field by name instead of matching a formatted line —
// "dir=out" is a prefix of "dir=out.done" and substring checks confuse them.
type traceLine struct {
	fields map[string]string
	raw    string
}

// hookHandler collects trace records and can run a hook after each one, with
// its own lock already released.
//
// The released lock is the point of it. slog's stock handlers hold a mutex
// across the whole of Handle, including the write to the sink, so two
// goroutines emitting a trace line serialize inside the handler — which is
// the very ordering under test. A handler that imposes an order of its own
// would let these tests pass whether or not wsSender imposes one.
type hookHandler struct {
	mu    sync.Mutex
	lines []traceLine
	after func(traceLine) // set before any goroutine runs; never reassigned
}

func (h *hookHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *hookHandler) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *hookHandler) WithGroup(string) slog.Handler            { return h }

func (h *hookHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Message != "wecom trace" {
		return nil
	}
	l := traceLine{fields: make(map[string]string)}
	var b strings.Builder
	b.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		l.fields[a.Key] = a.Value.String()
		fmt.Fprintf(&b, " %s=%s", a.Key, a.Value.String())
		return true
	})
	l.raw = b.String()

	h.mu.Lock()
	h.lines = append(h.lines, l)
	h.mu.Unlock()

	if h.after != nil {
		h.after(l)
	}
	return nil
}

func (h *hookHandler) snapshot() []traceLine {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.lines)
}

// withDir returns the recorded lines for one direction, in the order they were
// recorded.
func withDir(lines []traceLine, dir string) []traceLine {
	var out []traceLine
	for _, l := range lines {
		if l.fields["dir"] == dir {
			out = append(out, l)
		}
	}
	return out
}

// reqIDsOf is the recorded order of one direction, named by req_id.
func reqIDsOf(lines []traceLine, dir string) []string {
	var out []string
	for _, l := range withDir(lines, dir) {
		out = append(out, l.fields["req_id"])
	}
	return out
}

// wireConn records the order frames actually reached WriteMessage — by the
// req_id each test frame carries — and can fail either half of the write.
// deadlineErr, writeErr and onWrite are set before any goroutine starts and
// never change afterwards.
type wireConn struct {
	mu      sync.Mutex
	written []string

	deadlineErr error
	writeErr    error
	onWrite     func()
}

func (c *wireConn) WriteMessage(_ int, data []byte) error {
	var env frameEnvelope
	_ = json.Unmarshal(data, &env)
	if c.onWrite != nil {
		c.onWrite()
	}
	c.mu.Lock()
	c.written = append(c.written, env.Headers.ReqID)
	c.mu.Unlock()
	return c.writeErr
}

func (c *wireConn) order() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.written)
}

func (c *wireConn) ReadMessage() (int, []byte, error) {
	return 0, nil, errors.New("wireConn is write-only")
}
func (c *wireConn) SetReadDeadline(time.Time) error  { return nil }
func (c *wireConn) SetWriteDeadline(time.Time) error { return c.deadlineErr }
func (c *wireConn) Close() error                     { return nil }

// pingFrame is the smallest frame wsSender.write accepts, tagged with a req_id
// the test can follow from the log onto the socket.
func pingFrame(reqID string) map[string]any {
	return map[string]any{"cmd": cmdPing, "headers": frameHeaders{ReqID: reqID}}
}

// traceParkBudget bounds how long the ordering test holds one writer inside
// its trace emission. It is only ever paid in full when the recording happens
// under the writer mutex — that is the case where the second writer is parked
// on the mutex and can never signal, so the budget is what releases the first.
// When the recording happens outside the mutex the second writer completes in
// microseconds and the budget is not reached.
const traceParkBudget = 750 * time.Millisecond

// TestTraceOutOrderIsTheWireOrder is the deterministic guard for the ordering
// property: the outbound trace has to name frames in the order the socket took
// them, or an operator reading it draws the wrong conclusion about which of
// two frames the server saw first — the question the switch exists to answer.
//
// The mechanism, rather than a race that may or may not show up: writer A is
// held inside its own trace emission until writer B has run a whole write(),
// and then both orders are compared.
//
//   - Recording under the writer mutex (correct): A holds the mutex while it
//     is parked, B blocks on the mutex before recording anything, and the park
//     ends on the budget. A is recorded first and reaches the socket first.
//
//   - Recording before the mutex (the reviewed defect): A holds nothing while
//     it is parked. B records, takes the mutex, writes, and finishes — so A is
//     recorded first but reaches the socket second, and the trace is a lie
//     about the wire.
func TestTraceOutOrderIsTheWireOrder(t *testing.T) {
	withTrace(t, true)

	startB := make(chan struct{})
	bDone := make(chan struct{})

	h := &hookHandler{}
	h.after = func(l traceLine) {
		if l.fields["dir"] != "out" || l.fields["req_id"] != "A" {
			return
		}
		// A has just recorded its send attempt and has not written yet.
		// Give B a whole write() and hold A here while it runs.
		close(startB)
		select {
		case <-bDone:
		case <-time.After(traceParkBudget):
		}
	}

	conn := &wireConn{}
	s := newWSSender(conn, slog.New(h))

	go func() {
		<-startB
		if err := s.write(pingFrame("B")); err != nil {
			t.Errorf("write B: %v", err)
		}
		close(bDone)
	}()

	if err := s.write(pingFrame("A")); err != nil {
		t.Fatalf("write A: %v", err)
	}
	select {
	case <-bDone:
	case <-time.After(5 * time.Second):
		t.Fatal("B's write never finished")
	}

	traced := reqIDsOf(h.snapshot(), "out")
	wire := conn.order()
	if len(wire) != 2 {
		t.Fatalf("wire order = %v, want both frames written", wire)
	}
	if !slices.Equal(traced, wire) {
		t.Errorf("trace order %v != wire order %v: the trace records a frame as "+
			"sent before one that reached WriteMessage earlier, so it cannot be "+
			"read as the order the server saw", traced, wire)
	}
}

// TestTraceOutOrderUnderConcurrentWriters is the same property under the shape
// it actually takes in production: many senders on one connection at once (the
// ping loop, agent replies, inbox pushes). It also pins the two things that
// make the record readable afterwards — seq counts the wire positions in
// order, and every attempt is paired with an outcome under the same seq.
func TestTraceOutOrderUnderConcurrentWriters(t *testing.T) {
	withTrace(t, true)

	const writers = 16

	h := &hookHandler{}
	// Hold the mutex for a moment on each write so the other writers pile up
	// behind it; without contention nothing is being tested.
	conn := &wireConn{onWrite: func() { time.Sleep(200 * time.Microsecond) }}
	s := newWSSender(conn, slog.New(h))

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			if err := s.write(pingFrame(fmt.Sprintf("W%02d", i))); err != nil {
				t.Errorf("write W%02d: %v", i, err)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	lines := h.snapshot()
	traced := reqIDsOf(lines, "out")
	wire := conn.order()

	if len(wire) != writers {
		t.Fatalf("%d frames reached the socket, want %d", len(wire), writers)
	}
	if !slices.Equal(traced, wire) {
		t.Errorf("trace order != wire order under %d concurrent writers\n trace: %v\n  wire: %v",
			writers, traced, wire)
	}

	// seq has to be the wire position, so a reader can recover the order from
	// the field rather than from how the lines happen to sit in the file.
	attempts := withDir(lines, "out")
	for i, l := range attempts {
		if want := fmt.Sprint(i + 1); l.fields["seq"] != want {
			t.Errorf("attempt %d has seq=%q, want %q", i, l.fields["seq"], want)
		}
	}

	// Every attempt is answered. An attempt with no outcome is a real signal
	// (a write that hung, a process killed mid-write) and must not be one the
	// tracing produces on its own.
	outcomes := map[string]traceLine{}
	for _, l := range withDir(lines, "out.done") {
		outcomes[l.fields["seq"]] = l
	}
	if len(outcomes) != writers {
		t.Fatalf("%d outcomes recorded for %d attempts", len(outcomes), writers)
	}
	for _, a := range attempts {
		o, ok := outcomes[a.fields["seq"]]
		if !ok {
			t.Errorf("attempt seq=%s has no outcome", a.fields["seq"])
			continue
		}
		if o.fields["req_id"] != a.fields["req_id"] {
			t.Errorf("outcome seq=%s is for req_id %q, attempt was %q",
				a.fields["seq"], o.fields["req_id"], a.fields["req_id"])
		}
		if o.fields["ok"] != "true" {
			t.Errorf("outcome seq=%s reports %q for a write that succeeded", a.fields["seq"], o.fields["ok"])
		}
	}
}

// onlyOutcome returns the single "dir=out.done" line, and fails the test if
// the attempt or the outcome is missing — an assertion about a line that was
// never written proves nothing.
func onlyOutcome(t *testing.T, lines []traceLine) traceLine {
	t.Helper()
	if n := len(withDir(lines, "out")); n != 1 {
		t.Fatalf("%d send attempts recorded, want exactly 1", n)
	}
	got := withDir(lines, "out.done")
	if len(got) != 1 {
		t.Fatalf("%d outcomes recorded, want exactly 1 — a send with no recorded "+
			"outcome is indistinguishable from a successful one", len(got))
	}
	return got[0]
}

// TestTraceOutRecordsAFailedWrite is the second half of the blocking review
// point. "Did this frame actually reach the wire?" is what an operator turns
// the switch on to ask, and a rejected WriteMessage used to leave a log
// identical to a delivered frame's.
func TestTraceOutRecordsAFailedWrite(t *testing.T) {
	withTrace(t, true)

	h := &hookHandler{}
	conn := &wireConn{writeErr: errors.New("websocket: close sent")}
	s := newWSSender(conn, slog.New(h))

	if err := s.write(pingFrame("R1")); err == nil {
		t.Fatal("write returned nil for a socket that rejected the frame")
	}

	o := onlyOutcome(t, h.snapshot())
	if o.fields["ok"] != "false" {
		t.Errorf("outcome reports ok=%q for a failed write; got %s", o.fields["ok"], o.raw)
	}
	if o.fields["req_id"] != "R1" {
		t.Errorf("outcome req_id = %q, want R1 so it pairs with the attempt", o.fields["req_id"])
	}
	if o.fields["stage"] != traceStageWrite {
		t.Errorf("outcome stage = %q, want %q", o.fields["stage"], traceStageWrite)
	}
	if !strings.Contains(o.fields["error"], "close sent") {
		t.Errorf("outcome does not carry the socket's error; got %s", o.raw)
	}
}

// TestTraceOutRecordsAFailedWriteDeadline covers the other early return. It
// matters on its own because nothing reaches the socket at all here: the trace
// must say so rather than show a send that never happened.
func TestTraceOutRecordsAFailedWriteDeadline(t *testing.T) {
	withTrace(t, true)

	h := &hookHandler{}
	conn := &wireConn{deadlineErr: errors.New("set tcp: use of closed network connection")}
	s := newWSSender(conn, slog.New(h))

	if err := s.write(pingFrame("R2")); err == nil {
		t.Fatal("write returned nil when the write deadline could not be set")
	}
	if n := len(conn.order()); n != 0 {
		t.Fatalf("%d frames reached WriteMessage, want 0 — the deadline failed first", n)
	}

	o := onlyOutcome(t, h.snapshot())
	if o.fields["ok"] != "false" {
		t.Errorf("outcome reports ok=%q though no frame was written; got %s", o.fields["ok"], o.raw)
	}
	if o.fields["stage"] != traceStageDeadline {
		t.Errorf("outcome stage = %q, want %q", o.fields["stage"], traceStageDeadline)
	}
}

// TestTraceOutRecordsASuccessfulWrite is the control for the two above: the
// outcome of a frame that did go out says so, and says nothing about a stage.
func TestTraceOutRecordsASuccessfulWrite(t *testing.T) {
	withTrace(t, true)

	h := &hookHandler{}
	conn := &wireConn{}
	s := newWSSender(conn, slog.New(h))

	if err := s.write(pingFrame("R3")); err != nil {
		t.Fatalf("write: %v", err)
	}

	o := onlyOutcome(t, h.snapshot())
	if o.fields["ok"] != "true" {
		t.Errorf("outcome reports ok=%q for a delivered frame; got %s", o.fields["ok"], o.raw)
	}
	if o.fields["stage"] != "" {
		t.Errorf("outcome names stage=%q on success; a stage is only meaningful on a failure", o.fields["stage"])
	}
	if o.fields["error"] != "" {
		t.Errorf("outcome carries error=%q on success", o.fields["error"])
	}
}

// TestTraceOffRecordsNoOutcomeEither keeps the outcome line on the same switch
// as everything else. It is a second line per outbound frame, so a deployment
// that never asked for tracing must not get it.
func TestTraceOffRecordsNoOutcomeEither(t *testing.T) {
	withTrace(t, false)

	h := &hookHandler{}
	conn := &wireConn{writeErr: errors.New("websocket: close sent")}
	s := newWSSender(conn, slog.New(h))

	_ = s.write(pingFrame("R4"))

	if lines := h.snapshot(); len(lines) != 0 {
		t.Errorf("tracing is off but %d trace lines were written: %v", len(lines), lines)
	}
}

// TestTheMediaHeaderIsNotCutToTheMessagePreviewCap is the case traceMediaHeaders
// was added for, and the one the message preview cap silently failed.
//
// A non-ASCII filename cannot sit in the plain filename= parameter, so it
// arrives as nothing but percent escapes: nine runes per Chinese character,
// after 23 runes of `attachment; filename=""` scaffolding. Under
// tracePreviewRunes (120) an eleven-character name is already over, and the
// cut lands inside an escape — so an operator who turned MULTICA_WECOM_TRACE
// on precisely to learn how the CDN encodes a Chinese name got a prefix
// ending in half an escape, which cannot even be decoded back into the
// character it stood for.
func TestTheMediaHeaderIsNotCutToTheMessagePreviewCap(t *testing.T) {
	withTrace(t, true)

	const name = "季度经营分析报告最终版本二零二六.docx"
	encoded := url.QueryEscape(name)
	// Both forms together, which is what a server sends when it offers a
	// legacy fallback beside the extended parameter — the longest shape this
	// line has to carry in practice.
	raw := `attachment; filename="` + encoded + `"; filename*=UTF-8''` + encoded

	if n := utf8.RuneCountInString(raw); n <= tracePreviewRunes {
		t.Fatalf("fixture proves nothing: the header is %d runes, under the preview cap of %d", n, tracePreviewRunes)
	}

	logger, buf := capturingLogger()
	traceMediaHeaders(logger, "MSGID-MEDIA", 0, mediaHeaders{Disposition: raw, Filename: name})
	logged := buf.String()

	// slog's TextHandler quotes the value and escapes the quotes inside it,
	// so the assertion is on the escaped name — which is the part that
	// matters and contains no character the handler rewrites. Both parameters
	// carry it, so it must appear twice.
	if n := strings.Count(logged, encoded); n != 2 {
		t.Fatalf("the escaped name appears %d times, want both parameters intact: %s", n, logged)
	}
	if strings.Contains(logged, "…") {
		t.Fatalf("the header was truncated, which is the whole failure: %s", logged)
	}
}

// TestTheMediaHeaderIsStillBounded keeps the larger cap a runaway guard rather
// than an open door. The value is a remote string on a log line; no real
// Content-Disposition approaches traceHeaderRunes, so anything that does is
// not a filename and must not be written whole.
func TestTheMediaHeaderIsStillBounded(t *testing.T) {
	withTrace(t, true)

	logger, buf := capturingLogger()
	runaway := `attachment; filename="` + strings.Repeat("A", traceHeaderRunes*2) + `"`
	traceMediaHeaders(logger, "MSGID-MEDIA", 0, mediaHeaders{Disposition: runaway, Filename: runaway})
	logged := buf.String()

	if !strings.Contains(logged, "…") {
		t.Fatalf("a %d-rune header was written without a cut: %d bytes logged",
			utf8.RuneCountInString(runaway), len(logged))
	}
	if n := utf8.RuneCountInString(logged); n > 4*traceHeaderRunes {
		t.Fatalf("the line grew to %d runes; both fields are meant to be capped at %d", n, traceHeaderRunes)
	}
}

// TestTheMediaHeaderStaysOnOneLine keeps a header carrying a newline from
// splitting one attachment across two log records. net/http rejects a
// response header with a bare LF in it today, so this guards the flattening
// rather than a reachable input — but the trace's whole contract is one line
// per event, and traceBound is shared with the message preview.
func TestTheMediaHeaderStaysOnOneLine(t *testing.T) {
	withTrace(t, true)

	logger, buf := capturingLogger()
	traceMediaHeaders(logger, "MSGID-MEDIA", 0, mediaHeaders{
		Disposition: "attachment;\r\n filename=\"a.docx\"",
	})
	if got := strings.TrimSuffix(buf.String(), "\n"); strings.ContainsAny(got, "\n\r") {
		t.Fatalf("the media line was split across records: %q", got)
	}
}
