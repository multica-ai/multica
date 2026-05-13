package daemon

import (
	"log/slog"
	"testing"
	"time"
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
