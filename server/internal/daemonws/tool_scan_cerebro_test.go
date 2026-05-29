// CEREBRO-PATCH(daemonws-tool-scan-now-test): FIR-2230 — tests for the server→daemon tool-scan push (tool_scan_cerebro.go). Cerebro-only feature file living in the upstream daemonws package.
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

// TestRequestToolScan proves the FIR-2230 admin "Scan now" push reaches the
// daemon connection for the runtime as a daemon:tool_scan_requested frame
// carrying the runtime ID — the same delivery path as a task-available wakeup.
func TestRequestToolScan(t *testing.T) {
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

	hub.RequestToolScan("runtime-1")

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
	if msg.Type != protocol.EventDaemonToolScanRequested {
		t.Fatalf("message type = %q, want %q", msg.Type, protocol.EventDaemonToolScanRequested)
	}

	var payload protocol.ToolScanRequestedPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.RuntimeID != "runtime-1" {
		t.Fatalf("payload = %+v, want runtime-1", payload)
	}
}

// TestRequestToolScanNoConnectionIsNoop confirms a scan request for a runtime
// with no connected daemon simply drops (best-effort) rather than panicking —
// the admin handler is responsible for the offline 502, not the hub.
func TestRequestToolScanNoConnectionIsNoop(t *testing.T) {
	M.Reset()
	defer M.Reset()
	hub := NewHub()
	hub.RequestToolScan("runtime-absent") // must not panic
}
