package wecom

// wecom_channel_test.go — Connect lifecycle guards. The one that matters most
// is the deadlock regression: a read error while the parent ctx is still live
// (exactly the transient drop the Supervisor exists to retry) must return to
// the caller, not park forever. The bug was a LIFO defer ordering — the ping
// goroutine's wait ran before its cancel — and it was invisible on the
// shutdown path, so it needs a guard that drives the live-ctx error path.
//
// Co-authored analysis and the original defect report: seacen (PR #5833 review).

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/util"
)

// scriptedConn is a wsConn that acks the subscribe frame it is written (echoing
// the caller's req_id, which is generated internally and not knowable ahead of
// time) and then, on the next read, returns a transient error — standing in for
// a dropped socket while the parent ctx is still live.
type scriptedConn struct {
	mu        sync.Mutex
	ackFrame  []byte // queued subscribe ack, set when the subscribe write is seen
	reads     int
	readErr   error
	writes    int
	closed    bool
	closeOnce sync.Once
}

func (c *scriptedConn) WriteMessage(_ int, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes++
	var env frameEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil
	}
	if env.Cmd == cmdSubscribe {
		ack, _ := json.Marshal(frameEnvelope{Headers: frameHeaders{ReqID: env.Headers.ReqID}, ErrCode: 0})
		c.ackFrame = ack
	}
	return nil
}

func (c *scriptedConn) ReadMessage() (int, []byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads++
	if c.ackFrame != nil {
		ack := c.ackFrame
		c.ackFrame = nil
		return websocket.TextMessage, ack, nil
	}
	// Subscribe already acked; the socket now "drops".
	return 0, nil, errors.New("simulated transient read error")
}

func (c *scriptedConn) SetReadDeadline(time.Time) error  { return nil }
func (c *scriptedConn) SetWriteDeadline(time.Time) error { return nil }
func (c *scriptedConn) Close() error {
	c.closeOnce.Do(func() { c.closed = true })
	return nil
}

type scriptedDialer struct{ conn wsConn }

func (d scriptedDialer) DialContext(context.Context, string, http.Header) (wsConn, *http.Response, error) {
	return d.conn, nil, nil
}

// TestConnectReturnsOnReadErrorWhileCtxLive drives the exact path the deadlock
// hid on: subscribe succeeds, the ping goroutine is running, then a read fails
// while ctx is NOT cancelled. Connect must return the error promptly. Against
// the pre-fix defer ordering this blocks forever on <-pingDone and the test
// times out.
func TestConnectReturnsOnReadErrorWhileCtxLive(t *testing.T) {
	t.Parallel()

	c := &wecomChannel{
		installationID: mustTestUUID(t),
		botID:          "bot-1",
		secret:         "secret-1",
		handler:        func(context.Context, channel.InboundMessage) error { return nil },
		dialer:         scriptedDialer{conn: &scriptedConn{}},
		wsURL:          "wss://example.test/ws",
		senders:        newSendersRegistry(),
	}

	errCh := make(chan error, 1)
	go func() { errCh <- c.Connect(context.Background()) }()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("Connect returned nil; expected the transient read error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Connect did not return within 3s on a read error with a live ctx — the ping defer deadlock has regressed")
	}
}

// TestConnectReturnsOnCtxCancel confirms the ordinary shutdown path still
// returns (the path that always worked, kept so a fix to the above can't break
// it).
func TestConnectReturnsOnCtxCancel(t *testing.T) {
	t.Parallel()

	// A conn that acks subscribe then blocks on read until Close, so only ctx
	// cancellation (which closes the socket via the watchdog) ends Connect.
	bc := &blockingConn{unblock: make(chan struct{})}
	c := &wecomChannel{
		installationID: mustTestUUID(t),
		botID:          "bot-1",
		secret:         "secret-1",
		handler:        func(context.Context, channel.InboundMessage) error { return nil },
		dialer:         scriptedDialer{conn: bc},
		wsURL:          "wss://example.test/ws",
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- c.Connect(ctx) }()

	time.Sleep(50 * time.Millisecond) // let it reach the read loop
	cancel()

	select {
	case <-errCh:
		// returned (nil on ctx path) — success
	case <-time.After(3 * time.Second):
		t.Fatal("Connect did not return within 3s after ctx cancel")
	}
}

// blockingConn acks subscribe, then blocks every subsequent read until Close.
type blockingConn struct {
	mu       sync.Mutex
	ack      []byte
	acked    bool
	unblock  chan struct{}
	closeOne sync.Once
}

func (c *blockingConn) WriteMessage(_ int, data []byte) error {
	var env frameEnvelope
	if err := json.Unmarshal(data, &env); err == nil && env.Cmd == cmdSubscribe {
		c.mu.Lock()
		c.ack, _ = json.Marshal(frameEnvelope{Headers: frameHeaders{ReqID: env.Headers.ReqID}})
		c.mu.Unlock()
	}
	return nil
}

func (c *blockingConn) ReadMessage() (int, []byte, error) {
	c.mu.Lock()
	if !c.acked && c.ack != nil {
		c.acked = true
		ack := c.ack
		c.mu.Unlock()
		return websocket.TextMessage, ack, nil
	}
	c.mu.Unlock()
	<-c.unblock // block until Close
	return 0, nil, errors.New("closed")
}

func (c *blockingConn) SetReadDeadline(time.Time) error  { return nil }
func (c *blockingConn) SetWriteDeadline(time.Time) error { return nil }
func (c *blockingConn) Close() error {
	c.closeOne.Do(func() { close(c.unblock) })
	return nil
}

func mustTestUUID(t *testing.T) pgtype.UUID {
	t.Helper()
	u, err := util.ParseUUID("11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatalf("parse uuid: %v", err)
	}
	return u
}

// floodConn acks subscribe, hands out a fixed run of msg_callback frames
// back-to-back, then blocks until Close. It reports (via delivered) the moment
// the last frame has left it, which is the moment the read loop is committed
// to a send the dead worker can no longer receive.
type floodConn struct {
	mu        sync.Mutex
	ack       []byte
	acked     bool
	frames    [][]byte
	sent      int
	delivered chan struct{}
	unblock   chan struct{}
	closeOne  sync.Once
}

func (c *floodConn) WriteMessage(_ int, data []byte) error {
	var env frameEnvelope
	if err := json.Unmarshal(data, &env); err == nil && env.Cmd == cmdSubscribe {
		c.mu.Lock()
		c.ack, _ = json.Marshal(frameEnvelope{Headers: frameHeaders{ReqID: env.Headers.ReqID}})
		c.mu.Unlock()
	}
	return nil
}

func (c *floodConn) ReadMessage() (int, []byte, error) {
	c.mu.Lock()
	if !c.acked && c.ack != nil {
		c.acked = true
		ack := c.ack
		c.mu.Unlock()
		return websocket.TextMessage, ack, nil
	}
	if c.sent < len(c.frames) {
		f := c.frames[c.sent]
		c.sent++
		last := c.sent == len(c.frames)
		c.mu.Unlock()
		if last {
			close(c.delivered)
		}
		return websocket.TextMessage, f, nil
	}
	c.mu.Unlock()
	<-c.unblock
	return 0, nil, errors.New("closed")
}

func (c *floodConn) SetReadDeadline(time.Time) error  { return nil }
func (c *floodConn) SetWriteDeadline(time.Time) error { return nil }
func (c *floodConn) Close() error {
	c.closeOne.Do(func() { close(c.unblock) })
	return nil
}

// TestConnectReturnsWhenCallbackWorkerDiesWithAFullQueue drives the one
// arrangement in which the callback hand-off has no receiver and no closer:
// the worker returns on its first dispatch error while the read loop is parked
// on a send into a full queue.
//
// The worker closes the socket on its way out, which wakes a read loop parked
// in ReadMessage — but a parked channel send is not a read, and closing a
// socket does not move it. Nothing else closes the queue either: that happens
// in a defer that cannot run until the read loop returns. Only ctx
// cancellation would break the cycle, and on this path ctx is live: the
// Supervisor holds the lease open and reports the installation connected, so
// the bot stops answering that installation until the process restarts.
//
// The frame count is deliberate: the worker holds one frame while its handler
// blocks, callbackQueueDepth more fill the buffer, and the next send is the
// one with nowhere to go.
func TestConnectReturnsWhenCallbackWorkerDiesWithAFullQueue(t *testing.T) {
	t.Parallel()

	frames := make([][]byte, 0, callbackQueueDepth+2)
	for i := 0; i < callbackQueueDepth+2; i++ {
		payload, err := json.Marshal(msgCallbackFrame(t, "text", "hello"))
		if err != nil {
			t.Fatalf("marshal frame: %v", err)
		}
		frames = append(frames, payload)
	}

	conn := &floodConn{
		frames:    frames,
		delivered: make(chan struct{}),
		unblock:   make(chan struct{}),
	}

	// The first callback's handler parks until the queue behind it is full,
	// then fails. Every later one would succeed — they never get the chance,
	// which is the point.
	var calls atomic.Int32
	firstIn := make(chan struct{})
	release := make(chan struct{})
	wantErr := errors.New("handler failed")

	c := &wecomChannel{
		installationID: mustTestUUID(t),
		botID:          "bot-1",
		secret:         "secret-1",
		handler: func(context.Context, channel.InboundMessage) error {
			if calls.Add(1) == 1 {
				close(firstIn)
				<-release
				return wantErr
			}
			return nil
		},
		dialer:  scriptedDialer{conn: conn},
		wsURL:   "wss://example.test/ws",
		senders: newSendersRegistry(),
	}

	// Cancelling at the end releases a Connect this test proved is stuck, so a
	// failure here does not also strand a goroutine for the rest of the run.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- c.Connect(ctx) }()

	select {
	case <-firstIn:
	case <-time.After(3 * time.Second):
		t.Fatal("the first callback never reached the handler; the worker is not running")
	}
	select {
	case <-conn.delivered:
	case <-time.After(3 * time.Second):
		t.Fatal("the read loop never took every frame, so the queue never filled and this test proves nothing")
	}

	close(release) // the worker's dispatch now fails and it returns

	select {
	case err := <-errCh:
		if !errors.Is(err, wantErr) {
			t.Fatalf("Connect returned %v, want the dispatch error %v", err, wantErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Connect did not return within 3s after the callback worker died with a full queue — " +
			"the read loop is parked on a send with no receiver and no close, and only ctx cancellation can free it")
	}
}
