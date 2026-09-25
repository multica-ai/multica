package daemonws

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TestNotifyPendingWork pins the hint that removes the up-to-one-heartbeat
// wait before a queued model-list request is picked up (MUL-5444): daemons
// watching the runtime must receive a runtime-scoped daemon:pending_work frame.
func TestNotifyPendingWork(t *testing.T) {
	M.Reset()
	defer M.Reset()

	hub := NewHub()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.HandleWebSocket(w, r, ClientIdentity{RuntimeIDs: []string{"runtime-1"}})
	}))
	defer server.Close()

	conn := dialPendingWorkClient(t, hub, server.URL, "runtime-1")
	defer conn.Close()

	hub.NotifyPendingWork("runtime-1", protocol.PendingWorkKindModelList)

	payload := readPendingWorkFrame(t, conn)
	if payload.RuntimeID != "runtime-1" {
		t.Fatalf("payload.RuntimeID = %q, want runtime-1", payload.RuntimeID)
	}
	if payload.Kind != protocol.PendingWorkKindModelList {
		t.Fatalf("payload.Kind = %q, want %q", payload.Kind, protocol.PendingWorkKindModelList)
	}
}

// TestDeliverDaemonRuntime_PendingWork covers the multi-node path: the node
// that holds the daemon's socket receives the frame off the Redis relay and has
// to route it by the payload's runtime_id, not by the relay shard key.
func TestDeliverDaemonRuntime_PendingWork(t *testing.T) {
	M.Reset()
	defer M.Reset()

	hub := NewHub()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.HandleWebSocket(w, r, ClientIdentity{RuntimeIDs: []string{"runtime-1"}})
	}))
	defer server.Close()

	conn := dialPendingWorkClient(t, hub, server.URL, "runtime-1")
	defer conn.Close()

	frame, err := pendingWorkFrame("runtime-1", protocol.PendingWorkKindModelList)
	if err != nil {
		t.Fatalf("pendingWorkFrame: %v", err)
	}
	hub.DeliverDaemonRuntime("runtime-1", frame, "event-1")

	payload := readPendingWorkFrame(t, conn)
	if payload.RuntimeID != "runtime-1" {
		t.Fatalf("payload.RuntimeID = %q, want runtime-1", payload.RuntimeID)
	}

	// Same event id must not be delivered twice — a mirrored relay can publish
	// the same hint through two paths.
	hub.DeliverDaemonRuntime("runtime-1", frame, "event-1")
	if err := conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("expected no second delivery for a duplicate event id")
	}
}

// TestNotifyPendingWork_IgnoresEmptyRuntime guards against a hint with no
// runtime scope fanning out to every connected daemon.
func TestNotifyPendingWork_IgnoresEmptyRuntime(t *testing.T) {
	M.Reset()
	defer M.Reset()

	hub := NewHub()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.HandleWebSocket(w, r, ClientIdentity{RuntimeIDs: []string{"runtime-1"}})
	}))
	defer server.Close()

	conn := dialPendingWorkClient(t, hub, server.URL, "runtime-1")
	defer conn.Close()

	hub.NotifyPendingWork("", protocol.PendingWorkKindModelList)

	if err := conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("expected no frame for an empty runtime id")
	}
}

func dialPendingWorkClient(t *testing.T, hub *Hub, serverURL, runtimeID string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(serverURL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for hub.RuntimeConnectionCount(runtimeID) == 0 {
		if time.Now().After(deadline) {
			conn.Close()
			t.Fatalf("runtime connection %s was not registered", runtimeID)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return conn
}

func readPendingWorkFrame(t *testing.T, conn *websocket.Conn) protocol.PendingWorkPayload {
	t.Helper()
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
	if msg.Type != protocol.EventDaemonPendingWork {
		t.Fatalf("message type = %q, want %q", msg.Type, protocol.EventDaemonPendingWork)
	}
	var payload protocol.PendingWorkPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return payload
}
