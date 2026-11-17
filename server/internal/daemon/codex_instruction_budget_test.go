package daemon

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunTaskChecksCodexManagedInstructionBudget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("synthetic app-server fixture uses a POSIX shell")
	}
	for _, tc := range []struct {
		name    string
		config  string
		blocked bool
	}{
		{"default_budget", `{}`, true},
		{"explicit_larger_budget", `{"project_doc_max_bytes":100000}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("CODEX_HOME", t.TempDir())
			t.Setenv("SYNTHETIC_CODEX_CONFIG", tc.config)
			trace := filepath.Join(t.TempDir(), "methods")
			t.Setenv("SYNTHETIC_CODEX_TRACE", trace)
			fake := filepath.Join(t.TempDir(), "codex-fixture")
			writeTestExecutable(t, fake, []byte(`#!/bin/sh
case "$*" in *--version*) echo 'codex-cli 0.160.0'; exit 0;; esac
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
  [ -n "$id" ] || continue
  case "$line" in
    *'"method":"config/read"'*)
      echo config/read >> "$SYNTHETIC_CODEX_TRACE"
      printf '{"id":%s,"result":{"config":%s,"layers":[]}}\n' "$id" "$SYNTHETIC_CODEX_CONFIG";;
    *'"method":"thread/start"'*)
      echo thread/start >> "$SYNTHETIC_CODEX_TRACE"
      printf '{"id":%s,"result":{"thread":{"id":"thread-synthetic"}}}\n' "$id";;
    *'"method":"turn/start"'*)
      echo turn/start >> "$SYNTHETIC_CODEX_TRACE"
      printf '{"id":%s,"result":{"turn":{"id":"turn-synthetic"}}}\n' "$id"
      printf '%s\n' '{"method":"item/completed","params":{"item":{"type":"agentMessage","id":"message-synthetic","text":"done","phase":"final_answer"}}}'
      printf '%s\n' '{"method":"turn/completed","params":{"turn":{"id":"turn-synthetic","status":"completed"}}}';;
    *) printf '{"id":%s,"result":{}}\n' "$id";;
  esac
done
`))
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
			defer srv.Close()
			d := &Daemon{
				client: NewClient(srv.URL), logger: logger,
				workspaces: make(map[string]*workspaceState), activeEnvRoots: make(map[string]int), activeStores: make(map[string]int),
				runtimeIndex: map[string]Runtime{"test-rt": {ID: "test-rt", Provider: "codex"}},
				cfg: Config{WorkspacesRoot: t.TempDir(), AgentTimeout: 10 * time.Second, CodexHandshakeTimeout: 2 * time.Second, CodexThreadHandshakeTimeout: 2 * time.Second,
					ServerBaseURL: srv.URL, Agents: map[string]AgentEntry{"codex": {Path: fake}}},
			}
			task := Task{ID: "budget-task", WorkspaceID: "budget-workspace", RuntimeID: "test-rt", AgentID: "test-agent", AuthToken: "mat_synthetic_budget",
				ChatSessionID: "budget-chat", ChatMessage: "SYNTHETIC_ONLY", Agent: &AgentData{ID: "test-agent", Name: "test", Model: "synthetic", Instructions: strings.Repeat("x", 50000)}}
			result, err := d.runTask(t.Context(), task, "codex", 0, logger)
			if err != nil {
				t.Fatal(err)
			}
			methods, readErr := os.ReadFile(trace)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if tc.blocked {
				if !strings.Contains(result.Comment, "project_doc_max_bytes=32768") && !strings.Contains(result.FailureReason, "project_doc_max_bytes=32768") {
					t.Fatalf("missing budget diagnostic: status=%q comment=%q reason=%q", result.Status, result.Comment, result.FailureReason)
				}
				if result.Status == "completed" || strings.Contains(string(methods), "thread/start") || strings.Contains(string(methods), "turn/start") {
					t.Fatalf("unsafe run: status=%s methods=%s", result.Status, methods)
				}
			} else if result.Status != "completed" || !strings.Contains(string(methods), "turn/start") {
				t.Fatalf("explicit native budget not honored: status=%s comment=%s methods=%s", result.Status, result.Comment, methods)
			}
		})
	}
}
