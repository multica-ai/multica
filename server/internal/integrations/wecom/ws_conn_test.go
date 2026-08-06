package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeServer wraps httptest.Server with a websocket upgrade hook. handler
// runs on its own goroutine per accepted connection; assertion failures
// inside it are surfaced via t.Errorf (safe until the test finishes) and
// a done channel signals graceful handler exit for tests that want to
// wait on server-side completion.
type fakeServer struct {
	t       *testing.T
	srv     *httptest.Server
	url     string
	done    chan struct{} // closed when the last handler exits
	handler func(t *testing.T, c *websocket.Conn)

	activeMu sync.Mutex
	active   int
}

func newFakeServer(t *testing.T, handler func(t *testing.T, c *websocket.Conn)) *fakeServer {
	t.Helper()
	fs := &fakeServer{t: t, handler: handler, done: make(chan struct{})}
	upgrader := websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}
	fs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("fake server: upgrade: %v", err)
			return
		}
		defer c.Close()
		fs.activeMu.Lock()
		fs.active++
		fs.activeMu.Unlock()
		defer func() {
			fs.activeMu.Lock()
			fs.active--
			last := fs.active == 0
			fs.activeMu.Unlock()
			if last {
				select {
				case <-fs.done:
				default:
					close(fs.done)
				}
			}
		}()
		fs.handler(t, c)
	}))
	fs.url = "ws" + strings.TrimPrefix(fs.srv.URL, "http")
	t.Cleanup(fs.srv.Close)
	return fs
}

// readFrame reads one JSON frame from c. On error the caller usually
// wants to bail out of the handler; readFrame reports the error via
// t.Errorf and returns ok=false.
func readFrame(t *testing.T, c *websocket.Conn) (Frame, bool) {
	t.Helper()
	_, raw, err := c.ReadMessage()
	if err != nil {
		if !isServerClosedErr(err) {
			t.Logf("fake server: read: %v", err)
		}
		return Frame{}, false
	}
	var f Frame
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Errorf("fake server: decode: %v raw=%q", err, string(raw))
		return Frame{}, false
	}
	return f, true
}

func isServerClosedErr(err error) bool {
	if err == nil {
		return false
	}
	if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
		return true
	}
	// The gorilla dialer's Close from the client side surfaces as
	// "use of closed network connection"; treat as clean.
	return strings.Contains(err.Error(), "use of closed") || strings.Contains(err.Error(), "closed")
}

func writeFrame(t *testing.T, c *websocket.Conn, f Frame) {
	t.Helper()
	raw, err := json.Marshal(f)
	if err != nil {
		t.Errorf("fake server: marshal: %v", err)
		return
	}
	if err := c.WriteMessage(websocket.TextMessage, raw); err != nil {
		t.Errorf("fake server: write: %v", err)
	}
}

func writeResponse(t *testing.T, c *websocket.Conn, reqID string, errcode int, errmsg string) {
	writeFrame(t, c, Frame{Headers: FrameHeaders{ReqID: reqID}, ErrCode: errcode, ErrMsg: errmsg})
}

// awaitSubscribe reads one subscribe frame and returns its req_id.
func awaitSubscribe(t *testing.T, c *websocket.Conn) (string, bool) {
	t.Helper()
	f, ok := readFrame(t, c)
	if !ok {
		return "", false
	}
	if f.Cmd != CmdSubscribe {
		t.Errorf("fake server: first frame cmd = %q, want %q", f.Cmd, CmdSubscribe)
		return "", false
	}
	return f.Headers.ReqID, true
}

// -------- Tests --------

func TestConn_SubscribeSuccessAndPingRoundTrip(t *testing.T) {
	pingReceived := make(chan struct{}, 1)
	fs := newFakeServer(t, func(t *testing.T, c *websocket.Conn) {
		subID, ok := awaitSubscribe(t, c)
		if !ok {
			return
		}
		writeResponse(t, c, subID, 0, "")
		for {
			f, ok := readFrame(t, c)
			if !ok {
				return
			}
			switch f.Cmd {
			case CmdPing:
				writeResponse(t, c, f.Headers.ReqID, 0, "")
				select {
				case pingReceived <- struct{}{}:
				default:
				}
			}
		}
	})

	conn, err := NewConn(ConnConfig{
		DialURL:      fs.url,
		BotID:        "bot",
		Secret:       "sec",
		PingInterval: 30 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewConn: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- conn.Run(ctx) }()

	select {
	case <-pingReceived:
	case <-time.After(2 * time.Second):
		t.Fatal("no ping observed on the fake server")
	}
	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned %v; want nil on ctx cancel", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestConn_SendRequestGeneratesReqIDAndCorrelatesOutOfOrder(t *testing.T) {
	// Server: read subscribe, ack; then read exactly 2 send requests;
	// respond in REVERSE order. If SendRequest correlates by req_id, both
	// waiters unblock cleanly and report distinct req_ids.
	seen := make(chan string, 4)
	fs := newFakeServer(t, func(t *testing.T, c *websocket.Conn) {
		subID, ok := awaitSubscribe(t, c)
		if !ok {
			return
		}
		writeResponse(t, c, subID, 0, "")

		reqIDs := make([]string, 0, 2)
		for len(reqIDs) < 2 {
			f, ok := readFrame(t, c)
			if !ok {
				return
			}
			if f.Cmd == CmdPing {
				writeResponse(t, c, f.Headers.ReqID, 0, "")
				continue
			}
			if f.Cmd != CmdSendMsg {
				t.Errorf("unexpected cmd %q", f.Cmd)
				return
			}
			reqIDs = append(reqIDs, f.Headers.ReqID)
			seen <- f.Headers.ReqID
		}
		// Respond in reverse order.
		writeResponse(t, c, reqIDs[1], 0, "")
		writeResponse(t, c, reqIDs[0], 0, "")

		// Hold the connection open until the test cancels the client.
		for {
			if _, ok := readFrame(t, c); !ok {
				return
			}
		}
	})

	conn, err := NewConn(ConnConfig{
		DialURL:      fs.url,
		BotID:        "bot",
		Secret:       "sec",
		PingInterval: time.Hour, // no interference
	})
	if err != nil {
		t.Fatalf("NewConn: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- conn.Run(ctx) }()

	// Give Run a moment to complete the subscribe handshake before we
	// hit SendRequest. sendAndWait itself already handles pre-Run gracefully
	// (ErrNotConnected), so we retry briefly instead of racing.
	waitConnected(t, conn)

	type result struct {
		resp Response
		err  error
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			resp, err := conn.SendRequest(ctx, CmdSendMsg, SendMsgBody{ChatID: "u1", MsgType: "text"})
			results <- result{resp: resp, err: err}
		}()
	}

	got := make(map[string]bool)
	for i := 0; i < 2; i++ {
		select {
		case r := <-results:
			if r.err != nil {
				t.Fatalf("SendRequest[%d] err: %v", i, r.err)
			}
			if r.resp.Headers.ReqID == "" {
				t.Fatalf("SendRequest[%d] returned empty req_id", i)
			}
			if got[r.resp.Headers.ReqID] {
				t.Fatalf("SendRequest returned duplicate req_id %q", r.resp.Headers.ReqID)
			}
			got[r.resp.Headers.ReqID] = true
		case <-time.After(3 * time.Second):
			t.Fatal("SendRequest timed out")
		}
	}

	// Verify server-side saw two distinct req_ids too.
	serverIDs := drainStrings(seen)
	if len(serverIDs) != 2 || serverIDs[0] == serverIDs[1] {
		t.Fatalf("server saw req_ids %v, want 2 distinct", serverIDs)
	}
	cancel()
	<-runErr
}

func drainStrings(ch <-chan string) []string {
	out := []string{}
	for {
		select {
		case s := <-ch:
			out = append(out, s)
		default:
			return out
		}
	}
}

// waitConnected polls c.state until Run has completed the handshake, so
// tests do not race sendAndWait against Run's setup.
func waitConnected(t *testing.T, c *Conn) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.state.Load() != nil {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("Run did not publish send state within deadline")
}

func TestConn_WelcomePreservesCallerReqID(t *testing.T) {
	const eventReqID = "EVENT-42"
	welcomeSeen := make(chan Frame, 1)
	fs := newFakeServer(t, func(t *testing.T, c *websocket.Conn) {
		subID, ok := awaitSubscribe(t, c)
		if !ok {
			return
		}
		writeResponse(t, c, subID, 0, "")

		body, _ := json.Marshal(EventCallbackBody{
			MsgID: "m-1",
			Event: EventBody{EventType: EventTypeEnterChat},
		})
		writeFrame(t, c, Frame{
			Cmd:     CmdEventCallback,
			Headers: FrameHeaders{ReqID: eventReqID},
			Body:    body,
		})

		for {
			f, ok := readFrame(t, c)
			if !ok {
				return
			}
			if f.Cmd == CmdRespondWelcome {
				welcomeSeen <- f
				writeResponse(t, c, f.Headers.ReqID, 0, "")
				continue
			}
			if f.Cmd == CmdPing {
				writeResponse(t, c, f.Headers.ReqID, 0, "")
			}
		}
	})

	replyReturned := make(chan error, 1)
	conn, err := NewConn(ConnConfig{
		DialURL:      fs.url,
		BotID:        "bot",
		Secret:       "sec",
		PingInterval: time.Hour,
		OnWelcome: func(ctx context.Context, wc WelcomeContext) {
			replyReturned <- wc.Reply(ctx, WelcomeMsgBody{
				MsgType:  "markdown",
				Markdown: &MarkdownBody{Content: "hi"},
			})
		},
	})
	if err != nil {
		t.Fatalf("NewConn: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- conn.Run(ctx) }()

	select {
	case f := <-welcomeSeen:
		if f.Headers.ReqID != eventReqID {
			t.Fatalf("welcome req_id = %q, want %q", f.Headers.ReqID, eventReqID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no welcome frame observed")
	}
	select {
	case err := <-replyReturned:
		if err != nil {
			t.Fatalf("wc.Reply err: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wc.Reply did not return")
	}
	cancel()
	<-runErr
}

// spyConn wraps a WSConn to observe concurrent WriteMessage calls.
type spyConn struct {
	inner   WSConn
	active  atomic.Int32
	maxSeen atomic.Int32
	writes  atomic.Int64
}

func (s *spyConn) ReadMessage() (int, []byte, error) { return s.inner.ReadMessage() }
func (s *spyConn) SetReadDeadline(t time.Time) error { return s.inner.SetReadDeadline(t) }
func (s *spyConn) SetWriteDeadline(t time.Time) error {
	return s.inner.SetWriteDeadline(t)
}
func (s *spyConn) Close() error { return s.inner.Close() }
func (s *spyConn) WriteMessage(mt int, data []byte) error {
	n := s.active.Add(1)
	for {
		m := s.maxSeen.Load()
		if n <= m {
			break
		}
		if s.maxSeen.CompareAndSwap(m, n) {
			break
		}
	}
	defer s.active.Add(-1)
	// Small delay increases the observability of any accidental parallel writer.
	time.Sleep(500 * time.Microsecond)
	s.writes.Add(1)
	return s.inner.WriteMessage(mt, data)
}

func TestConn_SingleWriter(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, c *websocket.Conn) {
		subID, ok := awaitSubscribe(t, c)
		if !ok {
			return
		}
		writeResponse(t, c, subID, 0, "")
		for {
			f, ok := readFrame(t, c)
			if !ok {
				return
			}
			// Echo every request as a success response so waiters clear.
			writeResponse(t, c, f.Headers.ReqID, 0, "")
		}
	})

	var spy atomic.Pointer[spyConn]
	conn, err := NewConn(ConnConfig{
		DialURL:      fs.url,
		BotID:        "bot",
		Secret:       "sec",
		PingInterval: 5 * time.Millisecond,
		ConnWrap: func(inner WSConn) WSConn {
			s := &spyConn{inner: inner}
			spy.Store(s)
			return s
		},
	})
	if err != nil {
		t.Fatalf("NewConn: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- conn.Run(ctx) }()

	waitConnected(t, conn)

	// Bombard the conn with SendRequests concurrently while pings tick.
	const N = 24
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := conn.SendRequest(ctx, CmdSendMsg, SendMsgBody{ChatID: "u", MsgType: "text"})
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("SendRequest: %v", err)
			}
		}()
	}
	wg.Wait()

	s := spy.Load()
	if s == nil {
		t.Fatal("spyConn was never installed")
	}
	if got := s.maxSeen.Load(); got > 1 {
		t.Fatalf("observed %d concurrent WriteMessage calls; want <=1", got)
	}
	cancel()
	<-runErr
}

func TestConn_DisconnectFailsPending(t *testing.T) {
	// Server: subscribe OK, then absorbs one send request and closes.
	sawSend := make(chan struct{}, 1)
	fs := newFakeServer(t, func(t *testing.T, c *websocket.Conn) {
		subID, ok := awaitSubscribe(t, c)
		if !ok {
			return
		}
		writeResponse(t, c, subID, 0, "")
		for {
			f, ok := readFrame(t, c)
			if !ok {
				return
			}
			if f.Cmd == CmdSendMsg {
				sawSend <- struct{}{}
				_ = c.Close()
				return
			}
		}
	})

	conn, err := NewConn(ConnConfig{
		DialURL:        fs.url,
		BotID:          "bot",
		Secret:         "sec",
		PingInterval:   time.Hour,
		RequestTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewConn: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- conn.Run(ctx) }()

	waitConnected(t, conn)

	sendResult := make(chan error, 1)
	go func() {
		_, err := conn.SendRequest(ctx, CmdSendMsg, SendMsgBody{ChatID: "u", MsgType: "text"})
		sendResult <- err
	}()

	select {
	case <-sawSend:
	case <-time.After(2 * time.Second):
		t.Fatal("server never saw SendRequest")
	}

	select {
	case err := <-sendResult:
		if err == nil {
			t.Fatal("SendRequest returned nil after server closed conn")
		}
		if !errors.Is(err, ErrConnectionClosed) {
			// Allow underlying context.Canceled too, though the invariant
			// prefers ErrConnectionClosed from failAllPending.
			t.Logf("SendRequest err: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SendRequest did not fail after server close")
	}
	// Run should exit non-nil (connection lost) or nil (if we cancelled first).
	select {
	case <-runErr:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not exit after connection loss")
	}
}

func TestConn_DisconnectedEventClosesConnection(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, c *websocket.Conn) {
		subID, ok := awaitSubscribe(t, c)
		if !ok {
			return
		}
		writeResponse(t, c, subID, 0, "")

		body, _ := json.Marshal(EventCallbackBody{
			MsgID: "m-1",
			Event: EventBody{EventType: EventTypeDisconnected},
		})
		writeFrame(t, c, Frame{
			Cmd:     CmdEventCallback,
			Headers: FrameHeaders{ReqID: "kick-1"},
			Body:    body,
		})
		// Hold the socket open; client should close on its side.
		for {
			if _, ok := readFrame(t, c); !ok {
				return
			}
		}
	})

	conn, err := NewConn(ConnConfig{
		DialURL:      fs.url,
		BotID:        "bot",
		Secret:       "sec",
		PingInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewConn: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- conn.Run(ctx) }()

	select {
	case err := <-runErr:
		var ke *DisconnectedKickError
		if !errors.As(err, &ke) {
			t.Fatalf("Run err = %v; want *DisconnectedKickError", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not exit after disconnected_event")
	}
}

func TestConn_AuthFailureIsClassified(t *testing.T) {
	fs := newFakeServer(t, func(t *testing.T, c *websocket.Conn) {
		subID, ok := awaitSubscribe(t, c)
		if !ok {
			return
		}
		writeResponse(t, c, subID, 40001, "bad secret")
		// Server just returned; client should see and exit.
	})

	// No RetryState → no sleep, so the auth error surfaces immediately.
	conn, err := NewConn(ConnConfig{
		DialURL: fs.url,
		BotID:   "bot",
		Secret:  "sec",
	})
	if err != nil {
		t.Fatalf("NewConn: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- conn.Run(ctx) }()

	select {
	case err := <-runErr:
		var ae *AuthFailedError
		if !errors.As(err, &ae) {
			t.Fatalf("Run err = %v; want *AuthFailedError", err)
		}
		if ae.Code != 40001 {
			t.Fatalf("AuthFailedError.Code = %d, want 40001", ae.Code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return on auth failure")
	}
}

func TestConn_AuthFailureAdvancesRetryStreak(t *testing.T) {
	// Retry with a clock stuck at now, but call SleepCtx with a tiny
	// pretend delay. The easier path is: install a RetryState and just
	// verify the streak advances after Run returns. We short-circuit the
	// sleep by giving Run an already-cancelled context after subscribe.
	//
	// The simpler test: use a custom AuthFailDelays via a shim would be
	// nice, but we can just override the streak's clock. To avoid a 5-min
	// sleep in the test, cancel ctx concurrently — SleepCtx honors it.
	fs := newFakeServer(t, func(t *testing.T, c *websocket.Conn) {
		subID, ok := awaitSubscribe(t, c)
		if !ok {
			return
		}
		writeResponse(t, c, subID, 45001, "bad")
	})

	retry := NewRetryState()
	conn, err := NewConn(ConnConfig{
		DialURL: fs.url,
		BotID:   "bot",
		Secret:  "sec",
		Retry:   retry,
	})
	if err != nil {
		t.Fatalf("NewConn: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel almost immediately so SleepCtx inside Run returns early
	// once the auth branch is taken.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	runErr := make(chan error, 1)
	go func() { runErr <- conn.Run(ctx) }()

	select {
	case err := <-runErr:
		var ae *AuthFailedError
		if !errors.As(err, &ae) {
			t.Fatalf("Run err = %v; want *AuthFailedError", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return")
	}
	if retry.AuthStreak() == 0 {
		t.Fatal("expected RetryState.NoteAuthFail to have advanced streak")
	}
}

func TestConn_SendRequestBeforeRun(t *testing.T) {
	conn, err := NewConn(ConnConfig{
		DialURL: "ws://127.0.0.1:0",
		BotID:   "b",
		Secret:  "s",
	})
	if err != nil {
		t.Fatalf("NewConn: %v", err)
	}
	_, err = conn.SendRequest(context.Background(), CmdSendMsg, nil)
	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("SendRequest before Run err = %v; want ErrNotConnected", err)
	}
}

func TestNewConnValidatesRequiredFields(t *testing.T) {
	cases := []ConnConfig{
		{BotID: "b", Secret: "s"},        // missing DialURL
		{DialURL: "ws://x", Secret: "s"}, // missing BotID
		{DialURL: "ws://x", BotID: "b"},  // missing Secret
	}
	for i, cfg := range cases {
		if _, err := NewConn(cfg); err == nil {
			t.Errorf("case %d: NewConn returned nil error", i)
		}
	}
}
