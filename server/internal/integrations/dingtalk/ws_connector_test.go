package dingtalk

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeWSConn is a programmable in-memory wsConn for connector tests.
type fakeWSConn struct {
	frames chan []byte
	closed chan struct{}

	mu        sync.Mutex
	writes    [][]byte
	closeOnce sync.Once
}

func newFakeWSConn() *fakeWSConn {
	return &fakeWSConn{
		frames: make(chan []byte, 16),
		closed: make(chan struct{}),
	}
}

func (f *fakeWSConn) ReadMessage() (int, []byte, error) {
	select {
	case b := <-f.frames:
		return websocket.TextMessage, b, nil
	case <-f.closed:
		return 0, nil, websocket.ErrCloseSent
	}
}

func (f *fakeWSConn) WriteMessage(_ int, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = append(f.writes, append([]byte(nil), data...))
	return nil
}

func (f *fakeWSConn) WriteControl(int, []byte, time.Time) error { return nil }
func (f *fakeWSConn) SetReadDeadline(time.Time) error           { return nil }
func (f *fakeWSConn) SetWriteDeadline(time.Time) error          { return nil }
func (f *fakeWSConn) SetPongHandler(func(string) error)         {}
func (f *fakeWSConn) Close() error {
	f.closeOnce.Do(func() { close(f.closed) })
	return nil
}

func (f *fakeWSConn) responses(t *testing.T) []dataFrameResponse {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []dataFrameResponse
	for _, w := range f.writes {
		var r dataFrameResponse
		if err := json.Unmarshal(w, &r); err != nil {
			t.Fatalf("write not a dataFrameResponse: %v", err)
		}
		out = append(out, r)
	}
	return out
}

type fakeWSDialer struct{ conn wsConn }

func (d fakeWSDialer) dial(context.Context, string) (wsConn, error) { return d.conn, nil }

func newTestConnector(conn wsConn, onMessage func(context.Context, *botCallbackData) error) *wsConnector {
	return &wsConnector{
		dialer:       fakeWSDialer{conn: conn},
		open:         func(context.Context) (string, error) { return "wss://test/ws?ticket=t", nil },
		onMessage:    onMessage,
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		pingInterval: time.Hour, // don't fire during tests
	}
}

func mustFrame(t *testing.T, f dataFrame) []byte {
	t.Helper()
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	return b
}

const testCallbackData = `{"msgId":"m-1","msgtype":"text","senderStaffId":"s","conversationId":"c","conversationType":"1","text":{"content":"hi"}}`

func TestConnector_CallbackInvokesHandlerAndAcks(t *testing.T) {
	conn := newFakeWSConn()
	var got *botCallbackData
	gotCh := make(chan struct{})
	c := newTestConnector(conn, func(_ context.Context, d *botCallbackData) error {
		got = d
		close(gotCh)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- c.run(ctx) }()

	conn.frames <- mustFrame(t, dataFrame{
		Type:    frameTypeCallback,
		Headers: map[string]string{"topic": botMessageTopic, "messageId": "mid-1"},
		Data:    testCallbackData,
	})

	select {
	case <-gotCh:
	case <-time.After(2 * time.Second):
		t.Fatal("handler not invoked")
	}
	if got.MsgId != "m-1" || got.Text.Content != "hi" {
		t.Errorf("callback decoded wrong: %+v", got)
	}

	cancel()
	if err := <-runErr; err != nil {
		t.Errorf("run returned error on cancel: %v", err)
	}
	resps := conn.responses(t)
	if len(resps) == 0 || resps[0].Headers["messageId"] != "mid-1" || resps[0].Code != 200 {
		t.Errorf("expected 200 ack echoing mid-1, got %+v", resps)
	}
}

func TestConnector_RespondsToSystemPing(t *testing.T) {
	conn := newFakeWSConn()
	c := newTestConnector(conn, func(context.Context, *botCallbackData) error { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- c.run(ctx) }()

	conn.frames <- mustFrame(t, dataFrame{
		Type:    frameTypeSystem,
		Headers: map[string]string{"topic": systemTopicPing, "messageId": "ping-1"},
		Data:    `{"k":"v"}`,
	})

	deadline := time.After(2 * time.Second)
	for {
		if resps := conn.responses(t); len(resps) > 0 {
			if resps[0].Message != "ok" || resps[0].Headers["messageId"] != "ping-1" || resps[0].Data != `{"k":"v"}` {
				t.Errorf("bad pong: %+v", resps[0])
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("no pong written")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	<-runErr
}

func TestConnector_DisconnectFrameReturnsNil(t *testing.T) {
	conn := newFakeWSConn()
	c := newTestConnector(conn, func(context.Context, *botCallbackData) error { return nil })
	runErr := make(chan error, 1)
	go func() { runErr <- c.run(context.Background()) }()

	conn.frames <- mustFrame(t, dataFrame{
		Type:    frameTypeSystem,
		Headers: map[string]string{"topic": systemTopicDisconnect, "messageId": "d-1"},
	})

	select {
	case err := <-runErr:
		if err != nil {
			t.Errorf("disconnect must return nil, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return on disconnect frame")
	}
}

func TestConnector_CancelUnblocksRead(t *testing.T) {
	conn := newFakeWSConn()
	c := newTestConnector(conn, func(context.Context, *botCallbackData) error { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- c.run(ctx) }()

	time.Sleep(50 * time.Millisecond) // read loop is now blocked in ReadMessage
	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Errorf("cancel must return nil, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not unblock the read loop")
	}
}

func TestConnector_SkipsMalformedFrame(t *testing.T) {
	conn := newFakeWSConn()
	gotCh := make(chan struct{})
	c := newTestConnector(conn, func(context.Context, *botCallbackData) error { close(gotCh); return nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- c.run(ctx) }()

	conn.frames <- []byte(`{not json`)
	conn.frames <- mustFrame(t, dataFrame{
		Type:    frameTypeCallback,
		Headers: map[string]string{"topic": botMessageTopic, "messageId": "m"},
		Data:    testCallbackData,
	})

	select {
	case <-gotCh:
	case <-time.After(2 * time.Second):
		t.Fatal("connector did not survive a malformed frame")
	}
	cancel()
	<-runErr
}
