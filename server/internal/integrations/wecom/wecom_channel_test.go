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
