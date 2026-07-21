package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestTaskWakeupURL(t *testing.T) {
	tests := []struct {
		name       string
		baseURL    string
		runtimeIDs []string
		want       string
	}{
		{
			name:       "http base",
			baseURL:    "http://localhost:8080",
			runtimeIDs: []string{"runtime-b", "runtime-a"},
			want:       "ws://localhost:8080/api/daemon/ws?runtime_ids=runtime-a%2Cruntime-b",
		},
		{
			name:       "https base",
			baseURL:    "https://api.example.com",
			runtimeIDs: []string{"runtime-1"},
			want:       "wss://api.example.com/api/daemon/ws?runtime_ids=runtime-1",
		},
		{
			name:       "base path",
			baseURL:    "https://api.example.com/multica",
			runtimeIDs: []string{"runtime-1"},
			want:       "wss://api.example.com/multica/api/daemon/ws?runtime_ids=runtime-1",
		},
		{
			name:       "account-only connection",
			baseURL:    "https://api.example.com",
			runtimeIDs: nil,
			want:       "wss://api.example.com/api/daemon/ws",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := taskWakeupURL(tt.baseURL, tt.runtimeIDs)
			if err != nil {
				t.Fatalf("taskWakeupURL: %v", err)
			}
			if got != tt.want {
				t.Fatalf("taskWakeupURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestWSHeartbeatFreshnessSuppressesHTTP pins the WS-vs-HTTP coordination:
// once a runtime acked over WS within the freshness window the HTTP
// heartbeat loop must skip it to avoid duplicate DB writes.
func TestWSHeartbeatFreshnessSuppressesHTTP(t *testing.T) {
	d := New(Config{HeartbeatInterval: 15 * time.Second}, slog.Default())

	if d.wsHeartbeatRecentlyAcked("runtime-1") {
		t.Fatalf("expected unrecorded runtime to be stale")
	}

	d.recordWSHeartbeatAck("runtime-1")
	if !d.wsHeartbeatRecentlyAcked("runtime-1") {
		t.Fatalf("expected just-acked runtime to be fresh")
	}

	// Force the entry past the freshness window.
	d.wsHBMu.Lock()
	d.wsHBLastAck["runtime-1"] = time.Now().Add(-d.wsHeartbeatFreshness() - time.Second)
	d.wsHBMu.Unlock()
	if d.wsHeartbeatRecentlyAcked("runtime-1") {
		t.Fatalf("expected aged runtime to be stale (HTTP heartbeat must resume)")
	}

	d.recordWSHeartbeatAck("runtime-2")
	d.clearWSHeartbeatAcks()
	if d.wsHeartbeatRecentlyAcked("runtime-2") {
		t.Fatalf("expected clearWSHeartbeatAcks to drop all entries")
	}
}

func TestReadTaskWakeupMessagesTimesOutWithoutPeerTraffic(t *testing.T) {
	overrideTaskWakeupTimings(t, 60*time.Millisecond, 20*time.Millisecond, taskWakeupBackoffResetAfter)

	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		time.Sleep(300 * time.Millisecond)
	}))
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(taskWakeupTestWSURL(srv.URL), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	d := New(Config{}, slog.Default())
	errCh := make(chan error, 1)
	go func() {
		errCh <- d.readTaskWakeupMessages(conn, make(chan taskWakeup, 1))
	}()

	select {
	case err := <-errCh:
		var netErr net.Error
		if !errors.As(err, &netErr) || !netErr.Timeout() {
			t.Fatalf("readTaskWakeupMessages error = %v, want timeout", err)
		}
	case <-time.After(time.Second):
		t.Fatal("readTaskWakeupMessages did not time out")
	}
}

func TestReadTaskWakeupMessagesExtendsDeadlineOnServerPing(t *testing.T) {
	overrideTaskWakeupTimings(t, 120*time.Millisecond, 50*time.Millisecond, taskWakeupBackoffResetAfter)

	clientReceived := make(chan struct{})
	taskFrame := mustProtocolFrame(t, protocol.Message{
		Type: protocol.EventDaemonTaskAvailable,
		Payload: marshalRaw(protocol.TaskAvailablePayload{
			RuntimeID: "runtime-1",
			TaskID:    "task-1",
		}),
	})

	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for i := 0; i < 3; i++ {
			time.Sleep(50 * time.Millisecond)
			conn.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
			if err := conn.WriteMessage(websocket.PingMessage, []byte("keepalive")); err != nil {
				return
			}
		}

		if !writeWSMessage(t, conn, websocket.TextMessage, taskFrame) {
			return
		}
		waitForClientWakeup(t, clientReceived)
	}))
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(taskWakeupTestWSURL(srv.URL), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	d := New(Config{}, slog.Default())
	taskWakeups := make(chan taskWakeup, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- d.readTaskWakeupMessages(conn, taskWakeups)
	}()

	select {
	case wakeup := <-taskWakeups:
		if wakeup.runtimeID != "runtime-1" {
			t.Fatalf("wakeup runtimeID = %q, want runtime-1", wakeup.runtimeID)
		}
		close(clientReceived)
	case err := <-errCh:
		t.Fatalf("readTaskWakeupMessages returned before task frame: %v", err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for task wakeup")
	}
}

func TestReadTaskWakeupMessagesExtendsDeadlineOnApplicationMessage(t *testing.T) {
	overrideTaskWakeupTimings(t, 120*time.Millisecond, 50*time.Millisecond, taskWakeupBackoffResetAfter)

	clientReceived := make(chan struct{})
	ackFrame := mustProtocolFrame(t, protocol.Message{
		Type: protocol.EventDaemonHeartbeatAck,
		Payload: marshalRaw(HeartbeatResponse{
			RuntimeID: "runtime-1",
		}),
	})
	taskFrame := mustProtocolFrame(t, protocol.Message{
		Type: protocol.EventDaemonTaskAvailable,
		Payload: marshalRaw(protocol.TaskAvailablePayload{
			RuntimeID: "runtime-1",
			TaskID:    "task-1",
		}),
	})

	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for i := 0; i < 3; i++ {
			time.Sleep(50 * time.Millisecond)
			if !writeWSMessage(t, conn, websocket.TextMessage, ackFrame) {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
		if !writeWSMessage(t, conn, websocket.TextMessage, taskFrame) {
			return
		}
		waitForClientWakeup(t, clientReceived)
	}))
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(taskWakeupTestWSURL(srv.URL), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	d := New(Config{}, slog.Default())
	taskWakeups := make(chan taskWakeup, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- d.readTaskWakeupMessages(conn, taskWakeups)
	}()

	select {
	case wakeup := <-taskWakeups:
		if wakeup.runtimeID != "runtime-1" {
			t.Fatalf("wakeup runtimeID = %q, want runtime-1", wakeup.runtimeID)
		}
		close(clientReceived)
	case err := <-errCh:
		t.Fatalf("readTaskWakeupMessages returned before task frame: %v", err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for task wakeup")
	}
}

// A WS tasks.claim response carries the full claimed Task payload. Agent
// instructions, project context, comments, and skill references can make one
// valid response much larger than the old 64 KiB control-socket ceiling. The
// reader must deliver that response to the pending RPC and remain connected
// for the next task-available hint.
func TestReadTaskWakeupMessagesAcceptsLargeRPCResponse(t *testing.T) {
	overrideTaskWakeupTimings(t, time.Second, 100*time.Millisecond, taskWakeupBackoffResetAfter)

	requestIDs := make(chan string, 1)
	clientReceived := make(chan struct{})
	largeInstructions := strings.Repeat("x", 128*1024)
	body, err := json.Marshal(map[string]any{
		"tasks": []any{map[string]any{
			"id": "task-large",
			"agent": map[string]any{
				"id":           "agent-1",
				"name":         "Large Context Agent",
				"instructions": largeInstructions,
			},
		}},
	})
	if err != nil {
		t.Fatalf("marshal claim response body: %v", err)
	}

	upgrader := websocket.Upgrader{WriteBufferSize: 1024}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		requestID := <-requestIDs
		claimFrame := mustProtocolFrame(t, protocol.Message{
			Type: protocol.EventDaemonRPCResponse,
			Payload: marshalRaw(protocol.RPCResponsePayload{
				RequestID: requestID,
				Status:    http.StatusOK,
				Body:      body,
			}),
		})
		if len(claimFrame) <= 64*1024 {
			t.Errorf("claim frame size = %d, want larger than old 64 KiB limit", len(claimFrame))
			return
		}

		// A small write buffer forces gorilla/websocket to fragment this one
		// logical message. ReadLimit applies to the assembled message, so this
		// reproduces both single- and multi-frame delivery behavior.
		conn.SetWriteDeadline(time.Now().Add(time.Second))
		writer, err := conn.NextWriter(websocket.TextMessage)
		if err != nil {
			t.Errorf("open fragmented websocket writer: %v", err)
			return
		}
		split := len(claimFrame) / 2
		if _, err := writer.Write(claimFrame[:split]); err != nil {
			t.Errorf("write first claim fragment: %v", err)
			return
		}
		if _, err := writer.Write(claimFrame[split:]); err != nil {
			t.Errorf("write second claim fragment: %v", err)
			return
		}
		if err := writer.Close(); err != nil {
			t.Errorf("close fragmented websocket writer: %v", err)
			return
		}

		taskFrame := mustProtocolFrame(t, protocol.Message{
			Type: protocol.EventDaemonTaskAvailable,
			Payload: marshalRaw(protocol.TaskAvailablePayload{
				RuntimeID: "runtime-1",
				TaskID:    "task-next",
			}),
		})
		if !writeWSMessage(t, conn, websocket.TextMessage, taskFrame) {
			return
		}
		waitForClientWakeup(t, clientReceived)
	}))
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(taskWakeupTestWSURL(srv.URL), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	d := New(Config{}, slog.Default())
	d.wsRPC = newWSRPCClient(100 * time.Millisecond)
	d.wsRPC.attach(func(frame []byte) (*wsOutbound, error) {
		var msg protocol.Message
		if err := json.Unmarshal(frame, &msg); err != nil {
			return nil, err
		}
		var req protocol.RPCRequestPayload
		if err := json.Unmarshal(msg.Payload, &req); err != nil {
			return nil, err
		}
		requestIDs <- req.RequestID
		item := &wsOutbound{data: frame}
		item.beginWrite()
		return item, nil
	})

	taskWakeups := make(chan taskWakeup, 1)
	readerErrors := make(chan error, 1)
	go func() {
		readerErrors <- d.readTaskWakeupMessages(conn, taskWakeups)
	}()

	type callResult struct {
		status int
		err    error
		tasks  []*Task
	}
	callDone := make(chan callResult, 1)
	go func() {
		var resp struct {
			Tasks []*Task `json:"tasks"`
		}
		status, err := d.wsRPC.Call(context.Background(), "tasks.claim", 200*time.Millisecond, nil, &resp)
		callDone <- callResult{status: status, err: err, tasks: resp.Tasks}
	}()

	select {
	case result := <-callDone:
		if result.err != nil || result.status != http.StatusOK {
			t.Fatalf("claim RPC: status=%d err=%v", result.status, result.err)
		}
		if len(result.tasks) != 1 || result.tasks[0].ID != "task-large" {
			t.Fatalf("claim tasks = %+v, want task-large", result.tasks)
		}
		if got := len(result.tasks[0].Agent.Instructions); got != len(largeInstructions) {
			t.Fatalf("decoded instructions length = %d, want %d", got, len(largeInstructions))
		}
	case err := <-readerErrors:
		t.Fatalf("reader closed before claim response: %v", err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for parsed claim response")
	}

	select {
	case wakeup := <-taskWakeups:
		if wakeup.runtimeID != "runtime-1" {
			t.Fatalf("wakeup runtimeID = %q, want runtime-1", wakeup.runtimeID)
		}
		close(clientReceived)
	case err := <-readerErrors:
		t.Fatalf("reader closed before follow-up task frame: %v", err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for follow-up task wakeup")
	}
}

func TestReadTaskWakeupMessagesExtendsDeadlineOnPong(t *testing.T) {
	overrideTaskWakeupTimings(t, 120*time.Millisecond, 50*time.Millisecond, taskWakeupBackoffResetAfter)

	clientReceived := make(chan struct{})
	taskFrame := mustProtocolFrame(t, protocol.Message{
		Type: protocol.EventDaemonTaskAvailable,
		Payload: marshalRaw(protocol.TaskAvailablePayload{
			RuntimeID: "runtime-1",
			TaskID:    "task-1",
		}),
	})

	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for i := 0; i < 3; i++ {
			time.Sleep(50 * time.Millisecond)
			if !writeWSMessage(t, conn, websocket.PongMessage, []byte("keepalive")) {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
		if !writeWSMessage(t, conn, websocket.TextMessage, taskFrame) {
			return
		}
		waitForClientWakeup(t, clientReceived)
	}))
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(taskWakeupTestWSURL(srv.URL), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	d := New(Config{}, slog.Default())
	taskWakeups := make(chan taskWakeup, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- d.readTaskWakeupMessages(conn, taskWakeups)
	}()

	select {
	case wakeup := <-taskWakeups:
		if wakeup.runtimeID != "runtime-1" {
			t.Fatalf("wakeup runtimeID = %q, want runtime-1", wakeup.runtimeID)
		}
		close(clientReceived)
	case err := <-errCh:
		t.Fatalf("readTaskWakeupMessages returned before task frame: %v", err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for task wakeup")
	}
}

func TestShouldResetTaskWakeupBackoffRequiresStableConnection(t *testing.T) {
	old := taskWakeupBackoffResetAfter
	taskWakeupBackoffResetAfter = 10 * time.Second
	t.Cleanup(func() {
		taskWakeupBackoffResetAfter = old
	})

	if shouldResetTaskWakeupBackoff(0) {
		t.Fatal("zero connection uptime reset backoff")
	}
	if shouldResetTaskWakeupBackoff(9 * time.Second) {
		t.Fatal("short connection uptime reset backoff")
	}
	if !shouldResetTaskWakeupBackoff(10 * time.Second) {
		t.Fatal("stable connection uptime did not reset backoff")
	}
}

func TestRuntimeHeartbeatClosesIdleConnectionsAfterRepeatedTransientFailures(t *testing.T) {
	transport := &closeCountingTransport{}
	client := NewClient("http://daemon.test")
	client.client = &http.Client{
		Timeout:   time.Second,
		Transport: transport,
	}
	d := New(Config{HeartbeatInterval: 10 * time.Millisecond}, slog.Default())
	d.client = client

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.runRuntimeHeartbeat(ctx, "runtime-1")
	}()

	deadline := time.After(time.Second)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for transport.closeCount.Load() == 0 {
		select {
		case <-ticker.C:
		case <-deadline:
			cancel()
			t.Fatal("CloseIdleConnections was not called")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runRuntimeHeartbeat did not stop after context cancellation")
	}
	if got := transport.roundTrips.Load(); got < 2 {
		t.Fatalf("RoundTrip count = %d, want at least 2", got)
	}
}

type closeCountingTransport struct {
	roundTrips atomic.Int32
	closeCount atomic.Int32
}

func (t *closeCountingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.roundTrips.Add(1)
	return nil, errors.New("dial failed")
}

func (t *closeCountingTransport) CloseIdleConnections() {
	t.closeCount.Add(1)
}

func overrideTaskWakeupTimings(t *testing.T, pongWait, writeWait, backoffResetAfter time.Duration) {
	t.Helper()
	oldPongWait := taskWakeupPongWait
	oldWriteWait := taskWakeupWriteWait
	oldBackoffResetAfter := taskWakeupBackoffResetAfter
	taskWakeupPongWait = pongWait
	taskWakeupWriteWait = writeWait
	taskWakeupBackoffResetAfter = backoffResetAfter
	t.Cleanup(func() {
		taskWakeupPongWait = oldPongWait
		taskWakeupWriteWait = oldWriteWait
		taskWakeupBackoffResetAfter = oldBackoffResetAfter
	})
}

func taskWakeupTestWSURL(httpURL string) string {
	return strings.Replace(httpURL, "http", "ws", 1)
}

func mustProtocolFrame(t *testing.T, msg protocol.Message) []byte {
	t.Helper()
	frame, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal websocket frame: %v", err)
	}
	return frame
}

func writeWSMessage(t *testing.T, conn *websocket.Conn, messageType int, frame []byte) bool {
	t.Helper()
	conn.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
	if err := conn.WriteMessage(messageType, frame); err != nil {
		t.Errorf("write websocket frame: %v", err)
		return false
	}
	return true
}

func waitForClientWakeup(t *testing.T, clientReceived <-chan struct{}) {
	t.Helper()
	select {
	case <-clientReceived:
	case <-time.After(time.Second):
		t.Errorf("server timed out waiting for client wakeup")
	}
}
