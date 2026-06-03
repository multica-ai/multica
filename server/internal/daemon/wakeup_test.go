package daemon

import (
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestApplyCustomRuntimeAddPersistsAndUpdatesInMemoryConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	binDir := t.TempDir()
	fake := filepath.Join(binDir, "codewhale")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake codewhale: %v", err)
	}
	t.Setenv("PATH", binDir)

	d := New(Config{
		Agents:         map[string]AgentEntry{},
		WorkspacesRoot: t.TempDir(),
	}, slog.Default())

	err := d.applyCustomRuntimeAdd(protocol.CustomRuntimeAddPayload{
		RequestID:      "req-1",
		Provider:       "CodeWhale",
		Name:           "CodeWhale",
		Path:           "codewhale",
		Args:           []string{"exec", "--auto", "--output-format", "stream-json", "{{prompt}}"},
		ResumeArgs:     []string{"exec", "--resume", "{{session_id}}", "{{prompt}}"},
		SessionIDRegex: `"sessionId":"([^"]+)"`,
	})
	if err != nil {
		t.Fatalf("applyCustomRuntimeAdd: %v", err)
	}

	entry, ok := d.cfg.Agents["codewhale"]
	if !ok {
		t.Fatalf("expected codewhale in daemon cfg agents, got %v", d.cfg.Agents)
	}
	if entry.DisplayName != "CodeWhale" {
		t.Fatalf("DisplayName = %q", entry.DisplayName)
	}
	if !reflect.DeepEqual(entry.Custom.Args, []string{"exec", "--auto", "--output-format", "stream-json", "{{prompt}}"}) {
		t.Fatalf("Custom.Args = %v", entry.Custom.Args)
	}
	if !reflect.DeepEqual(entry.Custom.ResumeArgs, []string{"exec", "--resume", "{{session_id}}", "{{prompt}}"}) {
		t.Fatalf("Custom.ResumeArgs = %v", entry.Custom.ResumeArgs)
	}
	if entry.Custom.SessionIDRegex != `"sessionId":"([^"]+)"` {
		t.Fatalf("Custom.SessionIDRegex = %q", entry.Custom.SessionIDRegex)
	}

	raw, err := os.ReadFile(filepath.Join(home, ".multica", "custom-runtimes.json"))
	if err != nil {
		t.Fatalf("read managed custom runtime config: %v", err)
	}
	if !strings.Contains(string(raw), `"provider": "codewhale"`) {
		t.Fatalf("managed config missing normalized provider: %s", raw)
	}
	if !strings.Contains(string(raw), `"resume_args": [`) {
		t.Fatalf("managed config missing resume args: %s", raw)
	}
}
