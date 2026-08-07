package wecom

// trace_test.go — guards for the MULTICA_WECOM_TRACE operator switch. Three
// things have to hold or the switch is not shippable: it records nothing at
// all when off, it records enough to be worth turning on, and what it records
// is never a credential.
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
	"log/slog"
	"net/url"
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
// exercises every trace point: the subscribe write (traceOut), the subscribe
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

// TestTraceNeverLogsTheSmartBotSecret pins the reason traceOut reads named
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
