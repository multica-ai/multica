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
	"sync"
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

// TestBuildTaskWakeupHeaders_LayersExtraAndIdentity pins the layering
// contract for buildTaskWakeupHeaders: Authorization, then extra headers
// (multi-value), then X-Client-* identity headers, then X-Client-Capabilities.
// An operator-configured X-Client-* override must NOT survive the identity
// Set pass — the same defence-in-depth the HTTP path enforces through
// Client.setIdentityHeaders (MUL-5707 etc.).
func TestBuildTaskWakeupHeaders_LayersExtraAndIdentity(t *testing.T) {
	c := NewClient("ws://localhost:8080")
	c.SetToken("tok")
	c.SetVersion("9.9.9")
	c.SetExtraHeaders(http.Header{
		"X-Operator":        []string{"yes"},
		"X-Multi":           []string{"a", "b", "c"},
		"X-Client-Platform": []string{"forged"}, // must lose
	})

	headers := buildTaskWakeupHeaders(c)

	if got := headers.Get("Authorization"); got != "Bearer tok" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer tok")
	}
	if got := headers.Get("X-Operator"); got != "yes" {
		t.Errorf("X-Operator = %q, want %q", got, "yes")
	}
	if got := headers.Values("X-Multi"); len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("X-Multi = %v, want [a b c] (multi-value must survive)", got)
	}
	if got := headers.Get("X-Client-Platform"); got != "daemon" {
		t.Errorf("X-Client-Platform = %q, want daemon (operator override must lose)", got)
	}
	if got := headers.Get("X-Client-Version"); got != "9.9.9" {
		t.Errorf("X-Client-Version = %q, want 9.9.9", got)
	}
	if got := headers.Get("X-Client-Capabilities"); got == "" {
		t.Errorf("X-Client-Capabilities missing")
	}
}

// TestBuildTaskWakeupHeaders_OmitsWhenUnset ensures the helper is a no-op
// when no token / version / extra headers are configured. The daemon always
// sends X-Client-Capabilities (it's how the server gates the WS-only claim
// features), but nothing else should be invented.
func TestBuildTaskWakeupHeaders_OmitsWhenUnset(t *testing.T) {
	c := NewClient("ws://localhost:8080")
	headers := buildTaskWakeupHeaders(c)

	if got := headers.Get("Authorization"); got != "" {
		t.Errorf("Authorization = %q, want empty when no token", got)
	}
	if got := headers.Get("X-Client-Version"); got != "" {
		t.Errorf("X-Client-Version = %q, want empty when SetVersion not called", got)
	}
	if got := headers.Get("X-Client-Capabilities"); got == "" {
		t.Errorf("X-Client-Capabilities missing (server-side capability gating relies on it)")
	}
}

// TestTaskWakeupHandshakeCarriesExtraHeaders is the end-to-end wire-level
// pin for TIM-142 PR 2: bring up an httptest websocket upgrader, dial with
// the headers buildTaskWakeupHeaders produces, and assert the server sees
// Authorization, the extra headers (multi-value), and the X-Client-* set on
// the upgrade request. The HTTP-path test in client_test.go covers the
// post-upgrade request layer; this covers the one the HTTP path cannot
// reach — gorilla/websocket.Dialer does not share http.Client's transport
// hook, so the upgrade headers have to be wired separately.
func TestTaskWakeupHandshakeCarriesExtraHeaders(t *testing.T) {
	type observed struct {
		auth    string
		extras  map[string][]string
		ident   map[string][]string
		upgrade string
	}
	var (
		mu    sync.Mutex
		notes []observed
	)
	record := func(r *http.Request) {
		extras := map[string][]string{}
		for _, name := range []string{"X-Test", "X-Multi"} {
			extras[name] = append([]string(nil), r.Header.Values(name)...)
		}
		ident := map[string][]string{}
		for _, name := range []string{"X-Client-Platform", "X-Client-Version", "X-Client-OS", "X-Client-Capabilities"} {
			ident[name] = append([]string(nil), r.Header.Values(name)...)
		}
		mu.Lock()
		notes = append(notes, observed{
			auth:    r.Header.Get("Authorization"),
			extras:  extras,
			ident:   ident,
			upgrade: r.Header.Get("Upgrade"),
		})
		mu.Unlock()
	}

	upgrader := websocket.Upgrader{}
	connCh := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		connCh <- conn
		// Hold the conn open until the test tears down.
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	c.SetToken("tok")
	c.SetVersion("9.9.9")
	c.SetExtraHeaders(http.Header{
		"X-Test":  []string{"alpha", "beta"},
		"X-Multi": []string{"one", "two"},
	})

	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	conn, resp, err := dialer.DialContext(context.Background(), taskWakeupTestWSURL(srv.URL), buildTaskWakeupHeaders(c))
	if err != nil {
		t.Fatalf("dial: %v (resp=%v)", err, resp)
	}
	defer conn.Close()

	// Wait for the server-side upgrader to record what it saw, then assert.
	select {
	case srvConn := <-connCh:
		_ = srvConn
	case <-time.After(2 * time.Second):
		t.Fatal("server never received the upgrade")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(notes) != 1 {
		t.Fatalf("observed %d upgrade requests, want 1: %+v", len(notes), notes)
	}
	got := notes[0]
	if got.upgrade != "websocket" {
		t.Errorf("Upgrade header = %q, want %q", got.upgrade, "websocket")
	}
	if got.auth != "Bearer tok" {
		t.Errorf("Authorization = %q, want %q", got.auth, "Bearer tok")
	}
	if vals := got.extras["X-Test"]; len(vals) != 2 || vals[0] != "alpha" || vals[1] != "beta" {
		t.Errorf("X-Test = %v, want [alpha beta]", vals)
	}
	if vals := got.extras["X-Multi"]; len(vals) != 2 || vals[0] != "one" || vals[1] != "two" {
		t.Errorf("X-Multi = %v, want [one two]", vals)
	}
	if len(got.ident["X-Client-Platform"]) == 0 || got.ident["X-Client-Platform"][0] != "daemon" {
		t.Errorf("X-Client-Platform = %v, want [daemon]", got.ident["X-Client-Platform"])
	}
	if len(got.ident["X-Client-Version"]) == 0 || got.ident["X-Client-Version"][0] != "9.9.9" {
		t.Errorf("X-Client-Version = %v, want [9.9.9]", got.ident["X-Client-Version"])
	}
	if len(got.ident["X-Client-Capabilities"]) == 0 {
		t.Errorf("X-Client-Capabilities missing on upgrade")
	}
}
