package wecom

// welcome_test.go — the first thing a person sees.
//
// Before this, opening the bot's chat produced nothing: an empty window, no
// statement of what the bot is for, and no hint that it needs an account link
// before it will answer — findable only by messaging it and being told. Slack
// posts an App Home and Lark sends a welcome card; WeCom was the one that said
// nothing.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---- doubles ----

// fakeWelcomeLookup stands in for the two reads the greeting makes. blockOnCtx
// makes the binding read hang until its context ends, which is how the
// queue-pressure test wedges the welcome worker.
type fakeWelcomeLookup struct {
	binding    db.ChannelUserBinding
	bindingErr error
	install    db.ChannelInstallation
	installErr error
	blockOnCtx bool
}

func (f *fakeWelcomeLookup) GetChannelUserBindingByUserID(ctx context.Context, _ db.GetChannelUserBindingByUserIDParams) (db.ChannelUserBinding, error) {
	if f.blockOnCtx {
		<-ctx.Done()
		return db.ChannelUserBinding{}, ctx.Err()
	}
	return f.binding, f.bindingErr
}

func (f *fakeWelcomeLookup) GetChannelInstallation(_ context.Context, _ db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	return f.install, f.installErr
}

// countingBinder mints a recognizable raw token and counts how often it was
// asked. The count is the assertion for "no token was minted for somebody who
// must not receive one".
type countingBinder struct {
	mu    sync.Mutex
	calls int
	raw   string
	err   error
}

func (b *countingBinder) Mint(context.Context, pgtype.UUID, pgtype.UUID, string) (BindingToken, error) {
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()
	if b.err != nil {
		return BindingToken{}, b.err
	}
	return BindingToken{Raw: b.raw, ExpiresAt: time.Now().Add(BindingTokenTTL)}, nil
}

func (b *countingBinder) mintCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

// enterChatBody is what WeCom pushes the moment somebody opens the chat.
func enterChatBody(t *testing.T, chatType, userID string) json.RawMessage {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"msgid":    "EV-1",
		"aibotid":  "bot-1",
		"chatid":   "T-alex",
		"chattype": chatType,
		"from":     map[string]any{"userid": userID},
		"event":    map[string]any{"eventtype": eventEnterChat},
	})
	if err != nil {
		t.Fatalf("marshal enter_chat: %v", err)
	}
	return body
}

func enterChatFrame(t *testing.T, reqID, chatType, userID string) frameEnvelope {
	t.Helper()
	return frameEnvelope{
		Cmd:     cmdEventCallback,
		Headers: frameHeaders{ReqID: reqID},
		Body:    enterChatBody(t, chatType, userID),
	}
}

// welcomeRig builds a channel with the binding surface wired and a socket that
// acks whatever it is handed, for the tests that call handleEnterChat directly.
func welcomeRig(t *testing.T, lookup *fakeWelcomeLookup, minter binder) (*wecomChannel, *recordingConn, *wsSender) {
	t.Helper()
	conn := &recordingConn{}
	sender := conn.autoAck(newWSSender(conn, nil))
	c := &wecomChannel{
		installationID: mustTestUUID(t),
		botID:          "bot-1",
		logger:         slog.Default(),
		welcome:        lookup,
		appURL:         "https://multica.example",
	}
	if minter != nil {
		c.binding = minter
	}
	return c, conn, sender
}

// welcomeSaid returns the single greeting written to conn, or "". It fails the
// test on a second greeting: one enter_chat is one welcome.
func welcomeSaid(t *testing.T, conn *recordingConn) string {
	t.Helper()
	var out []string
	conn.mu.Lock()
	frames := append([]frameEnvelope(nil), conn.frames...)
	conn.mu.Unlock()
	for _, f := range frames {
		if f.Cmd != cmdRespondWelcome {
			continue
		}
		var body struct {
			Markdown struct {
				Content string `json:"content"`
			} `json:"markdown"`
		}
		if err := json.Unmarshal(f.Body, &body); err != nil {
			t.Fatalf("decode welcome body: %v", err)
		}
		out = append(out, body.Markdown.Content)
	}
	if len(out) == 0 {
		return ""
	}
	if len(out) > 1 {
		t.Fatalf("the bot greeted %d times for one enter_chat: %v", len(out), out)
	}
	return out[0]
}

// ---- the greeting itself ----

// An unlinked person gets the link, in the only place a bearer token may go.
func TestOpeningTheChatUnboundHandsOverTheLink(t *testing.T) {
	t.Parallel()
	lookup := &fakeWelcomeLookup{
		bindingErr: pgx.ErrNoRows,
		install:    db.ChannelInstallation{ID: mustTestUUID(t), WorkspaceID: mustTestUUID(t)},
	}
	minter := &countingBinder{raw: "s3cret"}
	c, conn, sender := welcomeRig(t, lookup, minter)

	c.handleEnterChat(context.Background(), enterChatFrame(t, "req-1", "single", "T-alex"), sender, slog.Default())

	said := welcomeSaid(t, conn)
	if said == "" {
		t.Fatal("opening the chat produced nothing — an empty window with no way to find out what to do")
	}
	if !strings.Contains(said, "https://multica.example/wecom/bind?token=s3cret") {
		t.Fatalf("the greeting carries no bind link: %q", said)
	}
}

// Somebody already linked does not need a link, and offering one reads as
// though the bot has forgotten their account.
func TestOpeningTheChatBoundGreetsWithoutALink(t *testing.T) {
	t.Parallel()
	lookup := &fakeWelcomeLookup{binding: db.ChannelUserBinding{MulticaUserID: mustTestUUID(t)}}
	minter := &countingBinder{raw: "s3cret"}
	c, conn, sender := welcomeRig(t, lookup, minter)

	c.handleEnterChat(context.Background(), enterChatFrame(t, "req-2", "single", "T-alex"), sender, slog.Default())

	said := welcomeSaid(t, conn)
	if said == "" {
		t.Fatal("a linked user opening the chat was greeted with nothing")
	}
	if strings.Contains(said, "token=") {
		t.Fatalf("a bind link was offered to somebody already bound: %q", said)
	}
	if n := minter.mintCount(); n != 0 {
		t.Fatalf("minted %d tokens for a bound user", n)
	}
}

// A binding token is a bearer credential: whoever opens the link owns the
// account it was minted for. It must never be posted where an audience can
// read it — the same rule replier.go enforces for the needs_binding prompt.
// And a greeting naming whoever just walked in, to a room, is not the act
// opening a chat asked for.
func TestAGroupGetsNoGreetingAndNoToken(t *testing.T) {
	t.Parallel()
	lookup := &fakeWelcomeLookup{
		bindingErr: pgx.ErrNoRows,
		install:    db.ChannelInstallation{ID: mustTestUUID(t), WorkspaceID: mustTestUUID(t)},
	}
	minter := &countingBinder{raw: "s3cret"}
	c, conn, sender := welcomeRig(t, lookup, minter)

	c.handleEnterChat(context.Background(), enterChatFrame(t, "req-3", "group", "T-alex"), sender, slog.Default())

	if said := welcomeSaid(t, conn); said != "" {
		t.Fatalf("a room was greeted: %q", said)
	}
	if minter.mintCount() != 0 {
		t.Fatal("a binding token was minted for a group, where anyone in the room could redeem it first")
	}
}

// A deployment with no binding surface still greets. Degrading to silence
// would make the bot look broken over a feature it does not have.
func TestNoBindingSurfaceStillGreets(t *testing.T) {
	t.Parallel()
	conn := &recordingConn{}
	sender := conn.autoAck(newWSSender(conn, nil))
	c := &wecomChannel{installationID: mustTestUUID(t), logger: slog.Default()}

	c.handleEnterChat(context.Background(), enterChatFrame(t, "req-5", "single", "T-alex"), sender, slog.Default())

	if said := welcomeSaid(t, conn); said != welcomeBoundText {
		t.Fatalf("said %q, want the plain greeting", said)
	}
}

// A database that cannot answer must not turn into a bind link offered to
// somebody who is already linked — that reads as the bot having lost their
// account — nor into a token minted for a user we cannot prove needs one.
func TestAnUnreadableBindingDoesNotOfferALink(t *testing.T) {
	t.Parallel()
	lookup := &fakeWelcomeLookup{bindingErr: errors.New("connection refused")}
	minter := &countingBinder{raw: "s3cret"}
	c, conn, sender := welcomeRig(t, lookup, minter)

	c.handleEnterChat(context.Background(), enterChatFrame(t, "req-6", "single", "T-alex"), sender, slog.Default())

	if said := welcomeSaid(t, conn); strings.Contains(said, "token=") {
		t.Fatalf("a link was offered on a failed lookup: %q", said)
	}
	if n := minter.mintCount(); n != 0 {
		t.Fatalf("minted %d tokens without knowing whether the user was bound", n)
	}
}

// The reply has to echo the frame's req_id. WeCom addresses a welcome by that
// and nothing else, so a fresh id means the greeting is refused.
func TestTheGreetingEchoesTheFramesReqID(t *testing.T) {
	t.Parallel()
	lookup := &fakeWelcomeLookup{binding: db.ChannelUserBinding{MulticaUserID: mustTestUUID(t)}}
	c, conn, sender := welcomeRig(t, lookup, nil)

	c.handleEnterChat(context.Background(), enterChatFrame(t, "req-echo-me", "single", "T-alex"), sender, slog.Default())

	conn.mu.Lock()
	frames := append([]frameEnvelope(nil), conn.frames...)
	conn.mu.Unlock()
	for _, f := range frames {
		if f.Cmd != cmdRespondWelcome {
			continue
		}
		if f.Headers.ReqID != "req-echo-me" {
			t.Fatalf("req_id = %q, want the frame's own — WeCom refuses anything else", f.Headers.ReqID)
		}
		return
	}
	t.Fatal("no welcome frame was written")
}

// The read loop must recognise enter_chat and nothing else as a welcome, or it
// either greets on the wrong event or never greets at all.
func TestOnlyEnterChatRoutesToTheWelcomeWorker(t *testing.T) {
	t.Parallel()
	if !isWelcomeFrame(enterChatFrame(t, "r", "single", "T-alex")) {
		t.Fatal("enter_chat was not recognised; the greeting would never fire")
	}
	for _, ev := range []string{eventDisconnected, eventTemplateCard, eventFeedback} {
		body, err := json.Marshal(map[string]any{"event": map[string]any{"eventtype": ev}})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if isWelcomeFrame(frameEnvelope{Cmd: cmdEventCallback, Body: body}) {
			t.Fatalf("%s was routed to the welcome worker, which never escalates and never reaches the engine", ev)
		}
	}
	if isWelcomeFrame(frameEnvelope{Cmd: cmdMsgCallback, Body: json.RawMessage(`{}`)}) {
		t.Fatal("a message callback was routed to the welcome worker, which never calls the engine")
	}
}

// ---- the live connection ----

// welcomeConn is a socket double for the whole read loop. It acks the
// subscribe handshake, then delivers a scripted sequence of server frames, and
// answers every frame we write the way WeCom does — with a separate ack frame
// carrying the same req_id, delivered back over the socket. That last part is
// what makes this a real exercise of the path: the greeting waits for a server
// verdict, and the read loop is the only thing that delivers one.
type welcomeConn struct {
	mu     sync.Mutex
	frames []frameEnvelope

	script []json.RawMessage
	sent   bool

	reads  chan []byte
	closed chan struct{}
	once   sync.Once
}

func newWelcomeConn(script ...json.RawMessage) *welcomeConn {
	return &welcomeConn{
		script: script,
		reads:  make(chan []byte, 256),
		closed: make(chan struct{}),
	}
}

func (c *welcomeConn) WriteMessage(_ int, data []byte) error {
	var env frameEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}
	c.mu.Lock()
	c.frames = append(c.frames, env)
	c.mu.Unlock()
	if env.Headers.ReqID == "" {
		return nil
	}
	ack, err := json.Marshal(frameEnvelope{Headers: frameHeaders{ReqID: env.Headers.ReqID}})
	if err != nil {
		return err
	}
	select {
	case c.reads <- ack:
	case <-c.closed:
	}
	return nil
}

func (c *welcomeConn) ReadMessage() (int, []byte, error) {
	select {
	case b := <-c.reads:
		c.mu.Lock()
		first := !c.sent
		c.sent = true
		script := c.script
		c.mu.Unlock()
		if first {
			// That was the subscribe ack. The main read loop takes over from
			// here, so the scripted frames can go out now.
			go func() {
				for _, f := range script {
					select {
					case c.reads <- f:
					case <-c.closed:
						return
					}
				}
			}()
		}
		return websocket.TextMessage, b, nil
	case <-c.closed:
		return 0, nil, errors.New("wecom test: socket closed")
	}
}

func (c *welcomeConn) SetReadDeadline(time.Time) error  { return nil }
func (c *welcomeConn) SetWriteDeadline(time.Time) error { return nil }
func (c *welcomeConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

// waitForCmd polls the recorded writes for a frame with the given cmd.
func (c *welcomeConn) waitForCmd(cmd string, d time.Duration) (frameEnvelope, bool) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		for _, f := range c.frames {
			if f.Cmd == cmd {
				c.mu.Unlock()
				return f, true
			}
		}
		c.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	return frameEnvelope{}, false
}

// serverFrame renders one frame the way the server would push it.
func serverFrame(t *testing.T, cmd, reqID string, body json.RawMessage) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(frameEnvelope{Cmd: cmd, Headers: frameHeaders{ReqID: reqID}, Body: body})
	if err != nil {
		t.Fatalf("marshal server frame: %v", err)
	}
	return b
}

func textCallbackFrame(t *testing.T, reqID, text string) json.RawMessage {
	t.Helper()
	mc := aibotMsgCallback{MsgID: "m-1", ChatID: "T-alex", ChatType: "single", MsgType: "text"}
	mc.From.UserID = "T-alex"
	mc.Text.Content = text
	body, err := json.Marshal(mc)
	if err != nil {
		t.Fatalf("marshal msg callback: %v", err)
	}
	return serverFrame(t, cmdMsgCallback, reqID, body)
}

func connectedChannel(t *testing.T, conn wsConn, lookup *fakeWelcomeLookup, handler channel.InboundHandler) *wecomChannel {
	t.Helper()
	return &wecomChannel{
		installationID: mustTestUUID(t),
		botID:          "bot-1",
		secret:         "secret-1",
		handler:        handler,
		dialer:         scriptedDialer{conn: conn},
		wsURL:          "wss://example.test/ws",
		logger:         slog.Default(),
		welcome:        lookup,
		appURL:         "https://multica.example",
	}
}

// The whole point, driven through Connect: WeCom pushes enter_chat, and the
// bot answers on the live socket. Before this the frame reached dispatchFrame's
// default arm and became a debug log line, so a brand-new user opened the bot
// and got an empty window.
func TestOpeningTheChatIsAnsweredOverTheLiveConnection(t *testing.T) {
	t.Parallel()
	conn := newWelcomeConn(serverFrame(t, cmdEventCallback, "req-live", enterChatBody(t, "single", "T-alex")))
	c := connectedChannel(t, conn,
		&fakeWelcomeLookup{binding: db.ChannelUserBinding{MulticaUserID: mustTestUUID(t)}},
		func(context.Context, channel.InboundMessage) error { return nil })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Connect(ctx) }()

	f, ok := conn.waitForCmd(cmdRespondWelcome, 3*time.Second)
	cancel()
	<-done

	if !ok {
		t.Fatal("enter_chat arrived on a live connection and the bot said nothing — a first-time user is looking at an empty window")
	}
	if f.Headers.ReqID != "req-live" {
		t.Errorf("welcome req_id = %q, want the enter_chat frame's own req-live", f.Headers.ReqID)
	}
}

// The deliberate asymmetry, made observable. The welcome queue is shallow and
// DROPS; the callback queue is deep and BLOCKS. If the welcome queue blocked
// instead, a wedged greeting would stop the read loop, and every message that
// arrived behind the burst — including the ones the user typed — would go
// unread until the socket died.
func TestAFullWelcomeQueueDoesNotStallTheReadLoop(t *testing.T) {
	t.Parallel()
	// Far more enter_chat frames than the welcome queue can hold, then one
	// ordinary message. The lookup hangs, so the welcome worker takes exactly
	// one frame and stops.
	var script []json.RawMessage
	for i := 0; i < welcomeQueueDepth*3; i++ {
		script = append(script, serverFrame(t, cmdEventCallback, "req-burst", enterChatBody(t, "single", "T-alex")))
	}
	script = append(script, textCallbackFrame(t, "req-text", "are you there"))

	conn := newWelcomeConn(script...)
	delivered := make(chan string, 1)
	c := connectedChannel(t, conn, &fakeWelcomeLookup{blockOnCtx: true},
		func(_ context.Context, m channel.InboundMessage) error {
			select {
			case delivered <- m.Text:
			default:
			}
			return nil
		})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Connect(ctx) }()

	var got string
	var ok bool
	select {
	case got, ok = <-delivered:
	case <-time.After(3 * time.Second):
	}
	cancel()
	<-done

	if !ok {
		t.Fatal("a burst of enter_chat frames stalled the read loop: the message the user typed behind them was never read")
	}
	if got != "are you there" {
		t.Errorf("handler got %q, want the user's message", got)
	}
}
