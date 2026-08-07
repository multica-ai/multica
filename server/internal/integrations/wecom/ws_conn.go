package wecom

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// Package-scoped errors returned by Conn.
var (
	// ErrNotConnected is returned by SendRequest / SendWelcome when the
	// caller invokes them before Run has completed the subscribe
	// handshake or after Run has torn the socket down.
	ErrNotConnected = errors.New("wecom: not connected")

	// ErrConnectionClosed is returned to pending waiters when Run tears
	// the socket down (ctx cancel, read error, kick). It classifies as
	// "connection lost" for higher-level retry policy.
	ErrConnectionClosed = errors.New("wecom: connection closed")
)

// AuthFailedError is returned by Run when the aibot_subscribe handshake
// completes but the server returned a non-zero errcode. The caller
// (channel.Channel.Connect) should treat it as terminal for this attempt
// and hand back to the Supervisor; RetryState.NoteAuthFail has already
// been advanced by Run when a RetryState was wired.
type AuthFailedError struct {
	Code int
	Msg  string
}

func (e *AuthFailedError) Error() string {
	return fmt.Sprintf("wecom: subscribe failed errcode=%d errmsg=%q", e.Code, e.Msg)
}

// DisconnectedKickError is returned by Run when the server pushed an
// aibot_event_callback with eventtype=disconnected_event. It classifies
// as a "another replica took over" hint and RetryState.NoteKick has
// already been advanced by Run when a RetryState was wired.
type DisconnectedKickError struct {
	Reason string
}

func (e *DisconnectedKickError) Error() string {
	return fmt.Sprintf("wecom: disconnected_event kick: %s", e.Reason)
}

// minReadTimeout floors the derived ReadTimeout (see withDefaults).
const minReadTimeout = 30 * time.Second

// osDeadline builds an absolute deadline for SetReadDeadline / SetWriteDeadline
// from the REAL clock, never cfg.Now. The kernel compares these against wall
// time, so a fake or frozen clock injected for tests would otherwise produce a
// deadline permanently in the past and fail every I/O immediately.
func osDeadline(d time.Duration) time.Time { return time.Now().Add(d) }

// WSDialer is the subset of *websocket.Dialer Conn consumes; tests
// inject a fake pointing at an httptest server.
type WSDialer interface {
	DialContext(ctx context.Context, urlStr string, requestHeader http.Header) (WSConn, *http.Response, error)
}

// WSConn is the subset of *websocket.Conn Conn consumes.
type WSConn interface {
	ReadMessage() (messageType int, p []byte, err error)
	WriteMessage(messageType int, data []byte) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
	Close() error
}

// WelcomeContext is delivered to OnWelcome and preserves the caller
// req_id required by aibot_respond_welcome_msg. Reply is a shortcut for
// (Conn).SendWelcome(ctx, ReqID, body); it MUST be invoked from the
// welcome worker goroutine and MUST NOT be called from readPump.
type WelcomeContext struct {
	ReqID string
	Frame Frame
	Reply func(ctx context.Context, body any) error
}

// ConnConfig collects every knob and hook Conn needs. Zero values on
// the timing fields fall back to production defaults. Required fields
// are validated by NewConn.
type ConnConfig struct {
	// DialURL is the WebSocket endpoint (ws:// or wss://). Tests point
	// this at an httptest.Server URL rewritten to ws://.
	DialURL string

	// BotID / Secret are the credentials embedded in aibot_subscribe.
	BotID  string
	Secret string

	// InstallationID identifies this installation in log/metric fields.
	InstallationID string

	// Dialer is the injected transport; nil defaults to a gorilla dialer
	// with a 15s handshake timeout.
	Dialer WSDialer

	// Logger optional; defaults to slog.Default.
	Logger *slog.Logger

	// Metrics optional; defaults to NoopMetrics.
	Metrics WecomMetrics

	// Retry, when non-nil, receives NoteAuthFail / NoteAuthSuccess /
	// NoteKick calls from Run and Run sleeps the returned duration
	// before returning to Supervisor.
	Retry *RetryState

	// Now optional; defaults to time.Now.
	Now func() time.Time

	// OnMsgCallback / OnEventCallback receive unsolicited aibot_msg_callback
	// and aibot_event_callback frames (except enter_chat and
	// disconnected_event, which are routed separately). They run on
	// bounded callback workers off the readPump; a slow handler applies
	// back-pressure via a bounded queue and is never dropped silently.
	OnMsgCallback   func(context.Context, Frame)
	OnEventCallback func(context.Context, Frame)

	// OnWelcome receives enter_chat events on a dedicated welcome worker.
	// The handler is expected to invoke wc.Reply within its 5s protocol
	// deadline; wc.Reply preserves the caller req_id and goes through the
	// high-priority write queue.
	OnWelcome func(context.Context, WelcomeContext)

	// Timing knobs. All zero values fall back to production defaults.
	PingInterval time.Duration // default 20s
	// ReadTimeout bounds how long readPump waits for ANY inbound frame
	// before declaring the peer dead. It must exceed PingInterval so a
	// healthy idle link — where the only inbound traffic is the response to
	// our own heartbeat — is never torn down; default 3x PingInterval.
	ReadTimeout       time.Duration // default 3x PingInterval
	SubscribeTimeout  time.Duration // default 10s
	WriteTimeout      time.Duration // default 10s
	RequestTimeout    time.Duration // default 15s (SendRequest wait cap)
	WelcomeDeadline   time.Duration // default 4s (welcome worker deadline)
	CallbackQueueSize int           // default 64
	WriteQueueSize    int           // default 32

	// ConnWrap wraps the accepted WSConn before pumps run; tests use it
	// to instrument concurrency. Nil leaves the conn as-is.
	ConnWrap func(WSConn) WSConn
}

func (c ConnConfig) withDefaults() ConnConfig {
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.Metrics == nil {
		c.Metrics = NoopMetrics()
	}
	if c.PingInterval == 0 {
		c.PingInterval = 20 * time.Second
	}
	if c.ReadTimeout == 0 {
		// Three ping intervals tolerates two consecutive unanswered
		// heartbeats before declaring the peer dead. Floored, because a small
		// PingInterval is usually chosen to make pings frequent — not to make
		// liveness hair-trigger — and a sub-second window would drop healthy
		// connections on an ordinary GC pause. Tests that exercise liveness
		// set ReadTimeout explicitly.
		if derived := 3 * c.PingInterval; derived > minReadTimeout {
			c.ReadTimeout = derived
		} else {
			c.ReadTimeout = minReadTimeout
		}
	}
	if c.SubscribeTimeout == 0 {
		c.SubscribeTimeout = 10 * time.Second
	}
	if c.WriteTimeout == 0 {
		c.WriteTimeout = 10 * time.Second
	}
	if c.RequestTimeout == 0 {
		c.RequestTimeout = 15 * time.Second
	}
	if c.WelcomeDeadline == 0 {
		c.WelcomeDeadline = 4 * time.Second
	}
	if c.CallbackQueueSize == 0 {
		c.CallbackQueueSize = 64
	}
	if c.WriteQueueSize == 0 {
		c.WriteQueueSize = 32
	}
	if c.Dialer == nil {
		c.Dialer = NewGorillaDialer()
	}
	return c
}

// Conn owns a single WeCom long-connection lifecycle. It enforces the
// single-writer / single-reader gorilla/websocket invariant and drives
// subscribe, ping, callback dispatch, request/response correlation, and
// clean teardown on ctx cancel / read error / disconnected_event kick.
//
// A Conn value may be reused across Run invocations; pending state and
// send-state are cleared between runs.
type Conn struct {
	cfg ConnConfig

	pendingMu sync.Mutex
	pending   map[string]pendingEntry

	state atomic.Pointer[sendState]
}

// NewConn validates cfg and returns a Conn ready for Run.
func NewConn(cfg ConnConfig) (*Conn, error) {
	if cfg.DialURL == "" {
		return nil, errors.New("wecom conn: DialURL is required")
	}
	if cfg.BotID == "" {
		return nil, errors.New("wecom conn: BotID is required")
	}
	if cfg.Secret == "" {
		return nil, errors.New("wecom conn: Secret is required")
	}
	return &Conn{
		cfg:     cfg.withDefaults(),
		pending: make(map[string]pendingEntry),
	}, nil
}

// pendingEntry is the resp/err pair a request goroutine parks on.
type pendingEntry struct {
	respCh chan Frame
	errCh  chan error
}

// writeReq wraps a serialized frame handed to writePump.
type writeReq struct {
	data []byte
}

// sendState is the per-run publication SendRequest / SendWelcome read.
// It is stored atomically so callers reading before Run has spawned
// writePump — or after teardown — cleanly get ErrNotConnected.
type sendState struct {
	highPri chan writeReq
	normal  chan writeReq
	done    chan struct{}
}

// Run performs one full connection lifecycle: dial → subscribe → spawn
// pumps + workers → block until ctx cancel / read error / kick →
// teardown + join. See spec §5.1.1 for the concurrency invariants this
// method enforces.
//
// Return classification:
//   - nil on ctx cancel
//   - *AuthFailedError when subscribe returns non-zero errcode
//   - *DisconnectedKickError when the server pushed disconnected_event
//   - any other error from dial / read / write
//
// When a RetryState is wired on the config, Run sleeps the streak-derived
// delay *before* returning on auth-fail / kick so Supervisor exponential
// backoff never thrashes subscribe.
func (c *Conn) Run(ctx context.Context) error {
	log := c.cfg.Logger.With(
		"installation_id", c.cfg.InstallationID,
		"bot_id", c.cfg.BotID,
	)

	rawConn, _, err := c.cfg.Dialer.DialContext(ctx, c.cfg.DialURL, nil)
	if err != nil {
		c.cfg.Metrics.RecordConnectFailure()
		return fmt.Errorf("wecom: dial: %w", err)
	}
	conn := rawConn
	if c.cfg.ConnWrap != nil {
		conn = c.cfg.ConnWrap(rawConn)
	}

	// Sync subscribe handshake — we own reads/writes exclusively until
	// pumps are spawned, so no gorilla concurrency violation.
	if err := c.subscribe(ctx, conn); err != nil {
		_ = conn.Close()
		var ae *AuthFailedError
		if errors.As(err, &ae) {
			c.cfg.Metrics.RecordAuthFailure()
			if c.cfg.Retry != nil {
				_ = SleepCtx(ctx, c.cfg.Retry.NoteAuthFail())
			}
		} else {
			c.cfg.Metrics.RecordConnectFailure()
		}
		return err
	}
	if c.cfg.Retry != nil {
		c.cfg.Retry.NoteAuthSuccess()
	}
	log.Info("wecom: subscribed")

	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()

	// Watchdog: on runCtx cancel, close the socket so ReadMessage
	// unblocks. Also runs on any other exit path to avoid goroutine leak.
	watchDone := make(chan struct{})
	go func() {
		select {
		case <-runCtx.Done():
			_ = conn.Close()
		case <-watchDone:
		}
	}()

	highPri := make(chan writeReq, 1)
	normal := make(chan writeReq, c.cfg.WriteQueueSize)
	stateDone := make(chan struct{})
	c.state.Store(&sendState{highPri: highPri, normal: normal, done: stateDone})

	msgQ := make(chan Frame, c.cfg.CallbackQueueSize)
	eventQ := make(chan Frame, c.cfg.CallbackQueueSize)
	welcomeQ := make(chan Frame, 1)

	var wg sync.WaitGroup

	writeDone := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		writeDone <- c.writePump(runCtx, conn, highPri, normal, log)
	}()

	readDone := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		readDone <- c.readPump(runCtx, conn, msgQ, eventQ, welcomeQ, log)
	}()

	// Callback worker: single goroutine keeps in-order dispatch per
	// spec §5.1.1 point 5. Event and welcome are separate.
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.callbackWorker(runCtx, msgQ, eventQ)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		c.welcomeWorker(runCtx, welcomeQ)
	}()

	// Block until any exit signal. writePump / readPump each write exactly
	// once to their (buffered) done channel, so the pump not selected here
	// is still safe to receive from later during teardown; we do not.
	var exitErr error
	select {
	case <-ctx.Done():
		exitErr = nil
	case err := <-readDone:
		exitErr = err
	case err := <-writeDone:
		if err != nil {
			exitErr = err
		} else {
			exitErr = ErrConnectionClosed
		}
	}

	// Teardown. Cancel runCtx first so pumps/workers see it; publish
	// nil send state so late Send* calls see ErrNotConnected; fail all
	// pending waiters.
	runCancel()
	_ = conn.Close()
	close(watchDone)
	c.state.Store(nil)
	close(stateDone)
	c.failAllPending(ErrConnectionClosed)
	wg.Wait()

	// Classify + sleep-before-return so Supervisor backoff does not
	// thrash subscribe.
	if ke := new(DisconnectedKickError); errors.As(exitErr, &ke) {
		if c.cfg.Retry != nil {
			_ = SleepCtx(ctx, c.cfg.Retry.NoteKick())
		}
		return exitErr
	}
	if ctx.Err() != nil {
		return nil
	}
	return exitErr
}

// subscribe performs the aibot_subscribe handshake synchronously. It is
// the only place that reads/writes without going through the pumps.
func (c *Conn) subscribe(ctx context.Context, conn WSConn) error {
	reqID := NewReqID()
	body, err := json.Marshal(SubscribeBody{BotID: c.cfg.BotID, Secret: c.cfg.Secret})
	if err != nil {
		return fmt.Errorf("wecom: marshal subscribe body: %w", err)
	}
	raw, err := json.Marshal(Frame{
		Cmd:     CmdSubscribe,
		Headers: FrameHeaders{ReqID: reqID},
		Body:    body,
	})
	if err != nil {
		return fmt.Errorf("wecom: marshal subscribe frame: %w", err)
	}
	deadline := c.cfg.Now().Add(c.cfg.SubscribeTimeout)
	if err := conn.SetWriteDeadline(deadline); err != nil {
		return fmt.Errorf("wecom: set write deadline: %w", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, raw); err != nil {
		return fmt.Errorf("wecom: write subscribe: %w", err)
	}
	if err := conn.SetReadDeadline(deadline); err != nil {
		return fmt.Errorf("wecom: set read deadline: %w", err)
	}
	_, resp, err := conn.ReadMessage()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("wecom: read subscribe response: %w", err)
	}
	var f Frame
	if err := json.Unmarshal(resp, &f); err != nil {
		return fmt.Errorf("wecom: decode subscribe response: %w", err)
	}
	if f.ErrCode != 0 {
		return &AuthFailedError{Code: f.ErrCode, Msg: f.ErrMsg}
	}
	// Clear the handshake write deadline; writeOne sets its own per frame.
	// The READ deadline is deliberately NOT cleared to zero here — readPump
	// re-arms it before every ReadMessage, and a zero deadline in between
	// would leave a window with no liveness bound at all.
	_ = conn.SetWriteDeadline(time.Time{})
	return nil
}

// writePump is the sole caller of conn.WriteMessage. It prioritizes
// high-priority frames (welcome) over normal frames (SendRequest / ping).
// It returns nil on clean ctx-driven shutdown and a wrapped error when
// WriteMessage fails; Run treats a non-nil return as a connection loss.
func (c *Conn) writePump(ctx context.Context, conn WSConn, hi, lo <-chan writeReq, log *slog.Logger) error {
	ping := time.NewTicker(c.cfg.PingInterval)
	defer ping.Stop()
	for {
		// Prefer high-priority frames without blocking so a burst of
		// SendRequest / ping cannot starve a welcome frame.
		select {
		case <-ctx.Done():
			return nil
		case wr := <-hi:
			if err := c.writeOne(conn, wr); err != nil {
				log.Warn("wecom: write hi frame", "err", err)
				return fmt.Errorf("wecom: write: %w", err)
			}
			continue
		default:
		}

		select {
		case <-ctx.Done():
			return nil
		case wr := <-hi:
			if err := c.writeOne(conn, wr); err != nil {
				log.Warn("wecom: write hi frame", "err", err)
				return fmt.Errorf("wecom: write: %w", err)
			}
		case wr := <-lo:
			if err := c.writeOne(conn, wr); err != nil {
				log.Warn("wecom: write frame", "err", err)
				return fmt.Errorf("wecom: write: %w", err)
			}
		case <-ping.C:
			data, err := marshalFrame(CmdPing, NewReqID(), nil)
			if err != nil {
				log.Warn("wecom: marshal ping", "err", err)
				continue
			}
			if err := c.writeOne(conn, writeReq{data: data}); err != nil {
				log.Warn("wecom: write ping", "err", err)
				return fmt.Errorf("wecom: write ping: %w", err)
			}
		}
	}
}

func (c *Conn) writeOne(conn WSConn, wr writeReq) error {
	if err := conn.SetWriteDeadline(osDeadline(c.cfg.WriteTimeout)); err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, wr.data)
}

// readPump is the sole caller of conn.ReadMessage. It decodes each frame,
// routes response frames (keyed by req_id) to pending waiters, and hands
// unsolicited pushes to the callback / welcome queues. On enqueue it
// selects against ctx so it never blocks past teardown.
func (c *Conn) readPump(ctx context.Context, conn WSConn, msgQ, eventQ, welcomeQ chan Frame, log *slog.Logger) error {
	for {
		// Re-arm before every read. ANY inbound frame proves the peer is
		// alive, so the deadline is effectively refreshed by traffic — on an
		// otherwise idle connection the response to our own heartbeat is what
		// keeps it moving. Without this the read blocks forever on a
		// half-open TCP connection (writes keep succeeding into the kernel
		// send buffer, so writePump never errors either) and Run never
		// returns, so Supervisor never redials: spec §9 requires an
		// unanswered heartbeat to close the connection and reconnect.
		if err := conn.SetReadDeadline(osDeadline(c.cfg.ReadTimeout)); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("wecom: set read deadline: %w", err)
		}
		_, raw, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return nil
			}
			return fmt.Errorf("wecom: read: %w", err)
		}
		var f Frame
		if err := json.Unmarshal(raw, &f); err != nil {
			log.Warn("wecom: decode frame", "err", err, "raw_len", len(raw))
			continue
		}

		// Any frame carrying a known req_id belongs to a pending waiter.
		// This covers subscribe echoes (shouldn't happen post-handshake
		// but harmless), ping responses, aibot_send_msg responses, and
		// welcome responses.
		if f.Headers.ReqID != "" && c.deliverPending(f) {
			continue
		}

		switch f.Cmd {
		case CmdMsgCallback:
			if err := sendOrCtx(ctx, msgQ, f); err != nil {
				return nil
			}
		case CmdEventCallback:
			eventType := peekEventType(f.Body)
			switch eventType {
			case EventTypeDisconnected:
				return &DisconnectedKickError{Reason: "disconnected_event"}
			case EventTypeEnterChat:
				if err := sendOrCtx(ctx, welcomeQ, f); err != nil {
					return nil
				}
			default:
				if err := sendOrCtx(ctx, eventQ, f); err != nil {
					return nil
				}
			}
		default:
			log.Debug("wecom: unhandled frame", "cmd", f.Cmd, "req_id", f.Headers.ReqID)
		}
	}
}

func (c *Conn) callbackWorker(ctx context.Context, msgQ, eventQ <-chan Frame) {
	for {
		select {
		case <-ctx.Done():
			return
		case f := <-msgQ:
			if c.cfg.OnMsgCallback != nil {
				c.cfg.OnMsgCallback(ctx, f)
			}
		case f := <-eventQ:
			if c.cfg.OnEventCallback != nil {
				c.cfg.OnEventCallback(ctx, f)
			}
		}
	}
}

func (c *Conn) welcomeWorker(ctx context.Context, welcomeQ <-chan Frame) {
	for {
		select {
		case <-ctx.Done():
			return
		case f := <-welcomeQ:
			if c.cfg.OnWelcome == nil {
				continue
			}
			// Give the welcome handler its own deadline so the readPump
			// is never observably blocked by welcome IO.
			hCtx, cancel := context.WithTimeout(ctx, c.cfg.WelcomeDeadline)
			reqID := f.Headers.ReqID
			c.cfg.OnWelcome(hCtx, WelcomeContext{
				ReqID: reqID,
				Frame: f,
				Reply: func(rctx context.Context, body any) error {
					return c.SendWelcome(rctx, reqID, body)
				},
			})
			cancel()
		}
	}
}

// SendRequest generates a fresh req_id, registers the pending waiter
// BEFORE enqueueing the write (so an out-of-order response cannot arrive
// while the waiter is still un-registered), and blocks until either the
// response arrives, ctx expires, or the connection is torn down.
func (c *Conn) SendRequest(ctx context.Context, cmd string, body any) (Response, error) {
	reqID := NewReqID()
	frame, err := c.sendAndWait(ctx, cmd, reqID, body, false)
	if err != nil {
		return Response{}, err
	}
	return Response{Headers: frame.Headers, ErrCode: frame.ErrCode, ErrMsg: frame.ErrMsg}, nil
}

// SendWelcome writes a CmdRespondWelcome frame that preserves the caller
// req_id (required by the platform: welcomes correlate off the enter_chat
// event's req_id, not a freshly minted one). The frame goes through the
// high-priority write queue so the normal send / ping stream cannot
// starve the 5s welcome deadline.
func (c *Conn) SendWelcome(ctx context.Context, reqID string, body any) error {
	if reqID == "" {
		return errors.New("wecom: SendWelcome requires a non-empty req_id")
	}
	_, err := c.sendAndWait(ctx, CmdRespondWelcome, reqID, body, true)
	return err
}

func (c *Conn) sendAndWait(ctx context.Context, cmd, reqID string, body any, highPri bool) (Frame, error) {
	st := c.state.Load()
	if st == nil {
		return Frame{}, ErrNotConnected
	}

	data, err := marshalFrame(cmd, reqID, body)
	if err != nil {
		return Frame{}, err
	}

	respCh := make(chan Frame, 1)
	errCh := make(chan error, 1)
	c.registerPending(reqID, pendingEntry{respCh: respCh, errCh: errCh})
	defer c.unregisterPending(reqID)

	target := st.normal
	if highPri {
		target = st.highPri
	}
	select {
	case target <- writeReq{data: data}:
	case <-ctx.Done():
		return Frame{}, ctx.Err()
	case <-st.done:
		return Frame{}, ErrConnectionClosed
	}

	// Apply RequestTimeout as an upper bound on the wait so a lost
	// response cannot pin the caller goroutine forever.
	waitCtx, cancel := context.WithTimeout(ctx, c.cfg.RequestTimeout)
	defer cancel()

	select {
	case resp := <-respCh:
		return resp, nil
	case err := <-errCh:
		return Frame{}, err
	case <-waitCtx.Done():
		return Frame{}, waitCtx.Err()
	case <-st.done:
		return Frame{}, ErrConnectionClosed
	}
}

func (c *Conn) registerPending(reqID string, entry pendingEntry) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	c.pending[reqID] = entry
}

func (c *Conn) unregisterPending(reqID string) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	delete(c.pending, reqID)
}

func (c *Conn) deliverPending(f Frame) bool {
	c.pendingMu.Lock()
	entry, ok := c.pending[f.Headers.ReqID]
	if ok {
		delete(c.pending, f.Headers.ReqID)
	}
	c.pendingMu.Unlock()
	if !ok {
		return false
	}
	select {
	case entry.respCh <- f:
	default:
	}
	return true
}

func (c *Conn) failAllPending(err error) {
	c.pendingMu.Lock()
	entries := make([]pendingEntry, 0, len(c.pending))
	for k, e := range c.pending {
		entries = append(entries, e)
		delete(c.pending, k)
	}
	c.pendingMu.Unlock()
	for _, e := range entries {
		select {
		case e.errCh <- err:
		default:
		}
	}
}

// marshalFrame serializes a Frame with cmd + reqID + optional body.
func marshalFrame(cmd, reqID string, body any) ([]byte, error) {
	var raw json.RawMessage
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("wecom: marshal %s body: %w", cmd, err)
		}
		raw = b
	}
	return json.Marshal(Frame{
		Cmd:     cmd,
		Headers: FrameHeaders{ReqID: reqID},
		Body:    raw,
	})
}

// peekEventType decodes only the eventtype discriminator; malformed
// bodies fall through the switch and are logged as unhandled events.
func peekEventType(body json.RawMessage) string {
	if len(body) == 0 {
		return ""
	}
	var probe struct {
		Event EventBody `json:"event"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return ""
	}
	return probe.Event.EventType
}

// sendOrCtx enqueues f on q or returns ctx.Err on cancel. Never drops.
func sendOrCtx(ctx context.Context, q chan<- Frame, f Frame) error {
	select {
	case q <- f:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// NewReqID returns a 32-hex-character random request ID. Exported for
// tests that want to pre-register pending waiters.
func NewReqID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand.Read on a healthy runtime does not fail; fall back
		// to a timestamp-based id so we never panic mid-connect.
		return fmt.Sprintf("wecom-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// GorillaDialer is the production WSDialer, wrapping websocket.Dialer.
type GorillaDialer struct {
	Dialer *websocket.Dialer
}

// NewGorillaDialer builds a dialer with a bounded handshake timeout and
// a proxy resolver that honors HTTPS_PROXY / HTTP_PROXY / NO_PROXY.
func NewGorillaDialer() *GorillaDialer {
	return &GorillaDialer{
		Dialer: &websocket.Dialer{
			HandshakeTimeout: 15 * time.Second,
			Proxy:            http.ProxyFromEnvironment,
		},
	}
}

func (g *GorillaDialer) DialContext(ctx context.Context, urlStr string, requestHeader http.Header) (WSConn, *http.Response, error) {
	d := g.Dialer
	if d == nil {
		d = websocket.DefaultDialer
	}
	c, resp, err := d.DialContext(ctx, urlStr, requestHeader)
	if err != nil {
		return nil, resp, err
	}
	return c, resp, nil
}
