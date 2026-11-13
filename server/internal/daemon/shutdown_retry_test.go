package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Exercise handleTask's real disposition and HTTP delivery without launching a
// provider or stopping a daemon. Cancellation happens while the runner is active.
func TestHandleTask_ShutdownDisposition(t *testing.T) {
	for _, tc := range []struct {
		name, status, reason, serverStatus, wantEndpoint, wantReason string
		shutdown                                                     bool
	}{
		{"shutdown", "cancelled", "", "running", "/fail", "runtime_recovery", true},
		{"explicit agent cancellation", "cancelled", "", "running", "/fail", "cancelled", false},
		{"server cancellation during shutdown", "cancelled", "", "cancelled", "/cancel-ack", "", true},
		{"timeout during shutdown", "blocked", "timeout", "running", "/fail", "timeout", true},
		{"completed before shutdown", "completed", "", "running", "/complete", "", true},
		{"cleanup uncertainty", "cancelled", "process_cleanup_unconfirmed", "running", "/fail", "process_cleanup_unconfirmed", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var mu sync.Mutex
			var endpoint string
			var terminal, usage map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				switch {
				case strings.HasSuffix(r.URL.Path, "/status"):
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]string{"status": tc.serverStatus})
				case strings.HasSuffix(r.URL.Path, "/usage"):
					if err := json.NewDecoder(r.Body).Decode(&usage); err != nil {
						t.Error(err)
					}
				case strings.HasSuffix(r.URL.Path, "/progress"):
				default:
					endpoint = r.URL.Path
					if err := json.NewDecoder(r.Body).Decode(&terminal); err != nil {
						t.Error(err)
					}
				}
			}))
			defer srv.Close()
			d := &Daemon{
				client: NewClient(srv.URL), logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
				runtimeIndex:       map[string]Runtime{"rt": {ID: "rt", Provider: "codex"}},
				cancelPollInterval: time.Hour,
			}
			d.runner = taskRunnerFunc(func(runCtx context.Context, _ Task, _ string, _ int, _ *slog.Logger) (TaskResult, error) {
				if runCtx.Err() != nil {
					t.Fatal("run started with cancelled context")
				}
				if tc.shutdown {
					cancel()
					<-runCtx.Done()
				}
				return TaskResult{
					Status: tc.status, FailureReason: tc.reason,
					SessionID: "session", WorkDir: "/workdir", BranchName: "agent/work",
					Usage: []TaskUsageEntry{{Provider: "openai", Model: "test", InputTokens: 100, OutputTokens: 50}},
				}, nil
			})
			d.handleTask(ctx, Task{ID: "task", RuntimeID: "rt"}, 0)
			mu.Lock()
			defer mu.Unlock()
			if !strings.HasSuffix(endpoint, tc.wantEndpoint) {
				t.Fatalf("endpoint = %q, want %q", endpoint, tc.wantEndpoint)
			}
			if tc.wantReason != "" && terminal["failure_reason"] != tc.wantReason {
				t.Errorf("reason = %v, want %s", terminal["failure_reason"], tc.wantReason)
			}
			if tc.wantReason == "runtime_recovery" && terminal["error"] != "daemon shut down while task was in flight" {
				t.Errorf("missing shutdown diagnostic: %v", terminal)
			}
			if tc.wantReason == "cancelled" && terminal["error"] != "" {
				t.Errorf("explicit cancellation has untranslated error: %v", terminal)
			}
			if tc.wantEndpoint != "/cancel-ack" {
				for k, want := range map[string]string{"session_id": "session", "work_dir": "/workdir", "branch_name": "agent/work"} {
					if terminal[k] != want {
						t.Errorf("%s = %v, want %s", k, terminal[k], want)
					}
				}
			}
			entries, ok := usage["usage"].([]any)
			if !ok || len(entries) != 1 {
				t.Fatalf("usage lost: %v", usage)
			}
			entry := entries[0].(map[string]any)
			if entry["input_tokens"] != float64(100) || entry["output_tokens"] != float64(50) {
				t.Errorf("usage changed: %v", entry)
			}
		})
	}
}
