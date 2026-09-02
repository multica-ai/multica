package wecom

// receipt_loop_test.go — the receipt through the real connection loop: the
// server's aibot_msg_callback comes in over the socket, the handler (standing
// in for the engine) asks for the receipt, the frame goes out on the same
// socket with the callback's req_id, and the server's anonymous ack — which
// carries nothing but that req_id — is routed back to the frame that waits
// for it. That last hop is the one an isolated sender test cannot prove.
//
// REVERSE VERIFICATION: with routeResponse reverted to `return
// s.deliverReply(env)` the receipt never gets its verdict, respondStream
// times out and the test fails on the "not sent" log line.

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
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

// receiptLoopConn plays the server: acks the subscribe, hands over one
// message callback, acks the stream frame it then sees, and parks.
type receiptLoopConn struct {
	mu       sync.Mutex
	queue    [][]byte
	wake     chan struct{}
	closed   chan struct{}
	once     sync.Once
	respond  []frameEnvelope
	callback []byte
}

func newReceiptLoopConn(callback []byte) *receiptLoopConn {
	return &receiptLoopConn{wake: make(chan struct{}, 8), closed: make(chan struct{}), callback: callback}
}

func (c *receiptLoopConn) push(b []byte) {
	c.mu.Lock()
	c.queue = append(c.queue, b)
	c.mu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *receiptLoopConn) WriteMessage(_ int, data []byte) error {
	var env frameEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}
	switch env.Cmd {
	case cmdSubscribe:
		ack, _ := json.Marshal(frameEnvelope{Headers: frameHeaders{ReqID: env.Headers.ReqID}})
		c.push(ack)
		c.push(c.callback)
	case cmdRespondMsg:
		c.mu.Lock()
		c.respond = append(c.respond, env)
		c.mu.Unlock()
		ack, _ := json.Marshal(frameEnvelope{Headers: frameHeaders{ReqID: env.Headers.ReqID}})
		c.push(ack)
	}
	return nil
}

func (c *receiptLoopConn) ReadMessage() (int, []byte, error) {
	for {
		c.mu.Lock()
		if len(c.queue) > 0 {
			b := c.queue[0]
			c.queue = c.queue[1:]
			c.mu.Unlock()
			return websocket.TextMessage, b, nil
		}
		c.mu.Unlock()
		select {
		case <-c.wake:
		case <-c.closed:
			return 0, nil, errors.New("closed")
		}
	}
}

func (c *receiptLoopConn) SetReadDeadline(time.Time) error  { return nil }
func (c *receiptLoopConn) SetWriteDeadline(time.Time) error { return nil }
func (c *receiptLoopConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func (c *receiptLoopConn) respondFrames() []frameEnvelope {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]frameEnvelope(nil), c.respond...)
}

func TestReceipt_ThroughTheRealConnectionLoop(t *testing.T) {
	t.Parallel()
	const reqID = "45vKtfP3R5SNuv97kKWZwgAA"
	body, _ := json.Marshal(map[string]any{
		"msgid": "m1", "aibotid": "bot-1", "chatid": "CHAT_1", "chattype": "single",
		"from": map[string]any{"userid": "u1"}, "msgtype": "text",
		"text": map[string]any{"content": "S270 的价格"},
	})
	callback, _ := json.Marshal(frameEnvelope{Cmd: cmdMsgCallback, Headers: frameHeaders{ReqID: reqID}, Body: body})
	conn := newReceiptLoopConn(callback)

	logs := &strings.Builder{}
	log := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	reg := newSendersRegistry()
	instID := mustTestUUID(t)
	receipt := NewReceiptNotifier(reg, log)
	sessionID := mustTestUUID(t)

	done := make(chan struct{})
	c := &wecomChannel{
		installationID: instID,
		botID:          "bot-1",
		secret:         "secret-1",
		dialer:         scriptedDialer{conn: conn},
		wsURL:          "wss://example.test/ws",
		senders:        reg,
		logger:         log,
		handler: func(ctx context.Context, msg channel.InboundMessage) error {
			// The engine calls OnIngested on a detached goroutine once the
			// turn is persisted; the handler stands in for that.
			go func() {
				defer close(done)
				receipt.OnIngested(ctx, engine.ResolvedInstallation{ID: instID, Active: true}, msg, sessionID)
			}()
			return nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- c.Connect(ctx) }()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the receipt never returned")
	}
	cancel()
	conn.Close()
	<-errCh

	frames := conn.respondFrames()
	if len(frames) != 1 {
		t.Fatalf("%d aibot_respond_msg frames, want 1", len(frames))
	}
	if frames[0].Headers.ReqID != reqID {
		t.Errorf("frame req_id = %q, want the callback's %q", frames[0].Headers.ReqID, reqID)
	}
	var sb map[string]any
	_ = json.Unmarshal(frames[0].Body, &sb)
	stream, _ := sb["stream"].(map[string]any)
	if stream["finish"] != true || stream["content"] != receiptText {
		t.Errorf("stream body = %v, want finish=true with the receipt text", sb)
	}
	if strings.Contains(logs.String(), "wecom receipt: not sent") {
		t.Fatalf("the receipt's verdict never reached it:\n%s", logs.String())
	}
	_ = pgtype.UUID{}
}
