package daemonws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestNotifyTaskAvailable(t *testing.T) {
	M.Reset()
	defer M.Reset()

	hub := NewHub()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.HandleWebSocket(w, r, ClientIdentity{RuntimeIDs: []string{"runtime-1"}})
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	deadline := time.Now().Add(time.Second)
	for hub.RuntimeConnectionCount("runtime-1") == 0 {
		if time.Now().After(deadline) {
			t.Fatal("runtime connection was not registered")
		}
		time.Sleep(10 * time.Millisecond)
	}

	hub.NotifyTaskAvailable("runtime-1", "task-1")

	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	var msg protocol.Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}
	if msg.Type != protocol.EventDaemonTaskAvailable {
		t.Fatalf("message type = %q, want %q", msg.Type, protocol.EventDaemonTaskAvailable)
	}

	var payload protocol.TaskAvailablePayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.RuntimeID != "runtime-1" || payload.TaskID != "task-1" {
		t.Fatalf("payload = %+v, want runtime/task IDs", payload)
	}
}

func TestRelayNotifierPublishesDaemonRuntimeScope(t *testing.T) {
	M.Reset()
	defer M.Reset()

	relay := &recordingRelayPublisher{}
	notifier := NewRelayNotifier(nil, relay)

	notifier.NotifyTaskAvailable("runtime-1", "task-1")

	if relay.scopeType != realtime.ScopeDaemonRuntime {
		t.Fatalf("scopeType = %q, want %q", relay.scopeType, realtime.ScopeDaemonRuntime)
	}
	if relay.scopeID != "task-1" {
		t.Fatalf("scopeID = %q, want task_id shard key", relay.scopeID)
	}
	if relay.eventID == "" {
		t.Fatal("expected event id")
	}
	if M.WakeupPublishedTotal.Load() != 1 {
		t.Fatalf("published metric = %d, want 1", M.WakeupPublishedTotal.Load())
	}

	var msg protocol.Message
	if err := json.Unmarshal(relay.frame, &msg); err != nil {
		t.Fatalf("unmarshal frame: %v", err)
	}
	if msg.Type != protocol.EventDaemonTaskAvailable {
		t.Fatalf("message type = %q, want %q", msg.Type, protocol.EventDaemonTaskAvailable)
	}
	var payload protocol.TaskAvailablePayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.RuntimeID != "runtime-1" || payload.TaskID != "task-1" {
		t.Fatalf("payload = %+v, want runtime/task IDs", payload)
	}
}

func TestRelayNotifierDedupsLocalRedisLoopback(t *testing.T) {
	M.Reset()
	defer M.Reset()

	hub := NewHub()
	client := attachDaemonTestClient(hub, "runtime-1")
	relay := &localFirstDaemonRelayPublisher{t: t, client: client}
	notifier := NewRelayNotifier(hub, relay)

	notifier.NotifyTaskAvailable("runtime-1", "task-1")

	if !relay.called {
		t.Fatal("expected relay publish to be invoked")
	}
	if relay.eventID == "" {
		t.Fatal("expected event id")
	}
	if M.WakeupDeliveredHit.Load() != 1 {
		t.Fatalf("delivered hit metric = %d, want 1", M.WakeupDeliveredHit.Load())
	}

	hub.DeliverDaemonRuntime(relay.scopeID, relay.frame, relay.eventID)

	select {
	case duplicate := <-client.send:
		t.Fatalf("expected redis loopback to be deduped, got duplicate %s", duplicate)
	case <-time.After(20 * time.Millisecond):
	}
	if M.WakeupDeliveredHit.Load() != 1 {
		t.Fatalf("delivered hit metric after loopback = %d, want 1", M.WakeupDeliveredHit.Load())
	}
	if M.WakeupDeliveredMiss.Load() != 0 {
		t.Fatalf("delivered miss metric after dedup = %d, want 0", M.WakeupDeliveredMiss.Load())
	}
}

// TestHeartbeatRoundTrip pins the WS heartbeat contract: a daemon:heartbeat
// frame invokes the registered HeartbeatHandler with the runtime ID, and the
// hub serializes the returned ack as a daemon:heartbeat_ack on the wire.
func TestHeartbeatRoundTrip(t *testing.T) {
	M.Reset()
	defer M.Reset()

	hub := NewHub()
	var calls atomic.Int32
	hub.SetHeartbeatHandler(func(_ context.Context, identity ClientIdentity, runtimeID string) (*protocol.DaemonHeartbeatAckPayload, error) {
		calls.Add(1)
		if identity.WorkspaceID != "ws-1" {
			t.Errorf("identity workspace = %q, want ws-1", identity.WorkspaceID)
		}
		return &protocol.DaemonHeartbeatAckPayload{
			RuntimeID: runtimeID,
			Status:    "ok",
			PendingUpdate: &protocol.DaemonHeartbeatPendingUpdate{
				ID:            "update-1",
				TargetVersion: "0.1.99",
			},
		}, nil
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.HandleWebSocket(w, r, ClientIdentity{
			WorkspaceID: "ws-1",
			RuntimeIDs:  []string{"runtime-1"},
		})
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	hbFrame, err := json.Marshal(protocol.Message{
		Type:    protocol.EventDaemonHeartbeat,
		Payload: mustMarshalRaw(protocol.DaemonHeartbeatRequestPayload{RuntimeID: "runtime-1"}),
	})
	if err != nil {
		t.Fatalf("marshal heartbeat: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, hbFrame); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	var msg protocol.Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("unmarshal ack envelope: %v", err)
	}
	if msg.Type != protocol.EventDaemonHeartbeatAck {
		t.Fatalf("ack type = %q, want %q", msg.Type, protocol.EventDaemonHeartbeatAck)
	}
	var ack protocol.DaemonHeartbeatAckPayload
	if err := json.Unmarshal(msg.Payload, &ack); err != nil {
		t.Fatalf("unmarshal ack payload: %v", err)
	}
	if ack.RuntimeID != "runtime-1" {
		t.Fatalf("ack runtime_id = %q, want runtime-1", ack.RuntimeID)
	}
	if ack.PendingUpdate == nil || ack.PendingUpdate.ID != "update-1" {
		t.Fatalf("ack pending_update = %+v, want update-1", ack.PendingUpdate)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("HeartbeatHandler invocations = %d, want 1", got)
	}
}

// TestHeartbeatRejectsUnauthorizedRuntime verifies that a heartbeat for a
// runtime outside the connection's authenticated set is dropped silently —
// no handler call, no ack frame.
func TestHeartbeatRejectsUnauthorizedRuntime(t *testing.T) {
	M.Reset()
	defer M.Reset()

	hub := NewHub()
	var called atomic.Bool
	hub.SetHeartbeatHandler(func(context.Context, ClientIdentity, string) (*protocol.DaemonHeartbeatAckPayload, error) {
		called.Store(true)
		return &protocol.DaemonHeartbeatAckPayload{Status: "ok"}, nil
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.HandleWebSocket(w, r, ClientIdentity{RuntimeIDs: []string{"runtime-1"}})
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	hbFrame, err := json.Marshal(protocol.Message{
		Type:    protocol.EventDaemonHeartbeat,
		Payload: mustMarshalRaw(protocol.DaemonHeartbeatRequestPayload{RuntimeID: "runtime-other"}),
	})
	if err != nil {
		t.Fatalf("marshal heartbeat: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, hbFrame); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatalf("expected no ack for unauthorized runtime, got message")
	}
	if called.Load() {
		t.Fatalf("HeartbeatHandler invoked for unauthorized runtime")
	}
}

func attachDaemonTestClient(hub *Hub, runtimeID string) *client {
	c := &client{
		send:     make(chan []byte, 2),
		runtimes: map[string]struct{}{runtimeID: {}},
	}

	hub.mu.Lock()
	hub.clients[c] = true
	hub.byRuntime[runtimeID] = map[*client]bool{c: true}
	hub.mu.Unlock()

	return c
}

type recordingRelayPublisher struct {
	scopeType string
	scopeID   string
	exclude   string
	frame     []byte
	eventID   string
}

func (r *recordingRelayPublisher) PublishWithID(scopeType, scopeID, exclude string, frame []byte, id string) error {
	r.scopeType = scopeType
	r.scopeID = scopeID
	r.exclude = exclude
	r.frame = append([]byte(nil), frame...)
	r.eventID = id
	return nil
}

type localFirstDaemonRelayPublisher struct {
	t      *testing.T
	client *client

	called     bool
	scopeType  string
	scopeID    string
	exclude    string
	frame      []byte
	eventID    string
	localFrame []byte
}

func (p *localFirstDaemonRelayPublisher) PublishWithID(scopeType, scopeID, exclude string, frame []byte, id string) error {
	p.called = true
	p.scopeType = scopeType
	p.scopeID = scopeID
	p.exclude = exclude
	p.frame = append([]byte(nil), frame...)
	p.eventID = id

	select {
	case p.localFrame = <-p.client.send:
	default:
		p.t.Fatal("expected local fanout to happen before relay publish")
	}
	return nil
}
