package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

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

func TestSendWSHeartbeatsAdvertisesNotificationDeliveryResult(t *testing.T) {
	d := New(Config{}, slog.Default())
	writes := make(chan []byte, 1)

	d.sendWSHeartbeats(context.Background(), []string{"runtime-1"}, writes)

	select {
	case raw := <-writes:
		var msg protocol.Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatalf("unmarshal heartbeat envelope: %v", err)
		}
		if msg.Type != protocol.EventDaemonHeartbeat {
			t.Fatalf("message type = %q, want %q", msg.Type, protocol.EventDaemonHeartbeat)
		}
		var payload protocol.DaemonHeartbeatRequestPayload
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("unmarshal heartbeat payload: %v", err)
		}
		if payload.RuntimeID != "runtime-1" {
			t.Fatalf("runtime_id = %q, want runtime-1", payload.RuntimeID)
		}
		if !payload.SupportsBatchImport {
			t.Fatal("expected heartbeat to advertise batch import support")
		}
		if !payload.SupportsNotificationDeliveryResult {
			t.Fatal("expected heartbeat to advertise notification delivery result support")
		}
	case <-time.After(time.Second):
		t.Fatal("expected heartbeat frame")
	}
}
