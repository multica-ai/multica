//go:build unix

package daemon

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func TestRunTaskDroidResumeRecovery(t *testing.T) {
	for _, tc := range []struct {
		name, failure, event string
		freshFails           bool
		wantAttempts         int
	}{
		{name: "missing session retries fresh", failure: "Could not load session", wantAttempts: 2},
		{name: "fresh failure stops after one retry", failure: "Could not load session", freshFails: true, wantAttempts: 2},
		{name: "confirmed session prevents retry", failure: "Provider failed", event: `printf '%s\n' '{"type":"system","subtype":"init","session_id":"stale-session"}'`, wantAttempts: 1},
		{name: "tool use prevents retry", failure: "Could not load session", event: `printf '%s\n' '{"type":"tool_call","id":"call-1","toolName":"Read","parameters":{}}'`, wantAttempts: 1},
		{name: "authentication keeps existing session", failure: "Authentication failed: invalid API key", wantAttempts: 1},
		{name: "network failure keeps existing session", failure: "connection refused", wantAttempts: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			redirectTestHome(t, t.TempDir())
			t.Setenv("MULTICA_TASK_CONFIG_ROOT", "")
			dir := t.TempDir()
			attemptFile := filepath.Join(dir, "attempts")
			fakeBin := filepath.Join(dir, "droid")
			script := `#!/bin/sh
cat >/dev/null
printf '%s\n' "$*" >> "$DROID_ATTEMPTS"
case "$*" in
  *--session-id*)
` + tc.event + "\nprintf '%s\\n' '{\"type\":\"error\",\"message\":\"" + tc.failure + "\"}'\nexit 1\n;;\nesac\n"
			if tc.freshFails {
				script += "printf '%s\\n' '{\"type\":\"error\",\"message\":\"Fresh attempt failed\"}'\nexit 1\n"
			} else {
				script += "printf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"fresh-session\"}' '{\"type\":\"completion\",\"finalText\":\"done\"}'\n"
			}
			writeTestExecutable(t, fakeBin, []byte(script))
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
			defer srv.Close()
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			d := &Daemon{
				client: NewClient(srv.URL), logger: logger,
				workspaces: make(map[string]*workspaceState), activeEnvRoots: make(map[string]int),
				runtimeIndex: map[string]Runtime{"rt-droid": {ID: "rt-droid", Provider: "droid"}},
				cfg: Config{WorkspacesRoot: t.TempDir(), AgentTimeout: 10 * time.Second, ServerBaseURL: srv.URL,
					Agents: map[string]AgentEntry{"droid": {Path: fakeBin}}},
			}
			task := Task{
				ID: "task-droid", WorkspaceID: "ws-droid", RuntimeID: "rt-droid", IssueID: "issue-droid",
				AgentID: "agent-droid", AuthToken: "mat_droid_fixture", PriorSessionID: "stale-session",
				Agent: &AgentData{ID: "agent-droid", Name: "droid-agent", CustomEnv: map[string]string{"DROID_ATTEMPTS": attemptFile}},
			}
			prior, err := execenv.Prepare(execenv.PrepareParams{
				WorkspacesRoot: d.cfg.WorkspacesRoot, WorkspaceID: task.WorkspaceID,
				TaskID: "prior-droid", Provider: "droid",
				Task: execenv.TaskContextForEnv{AgentID: task.AgentID, IssueID: task.IssueID},
			}, logger)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = prior.Cleanup(true) })
			prior.ReleaseLock()
			task.PriorWorkDir = prior.WorkDir
			if _, ok := shouldReusePriorWorkdir(task, nil, d.cfg.WorkspacesRoot); !ok {
				t.Fatal("fixture did not prepare a reusable session workdir")
			}
			result, err := d.runTask(context.Background(), task, "droid", 0, logger)
			if err != nil {
				t.Fatalf("runTask: %v", err)
			}
			attempts, err := os.ReadFile(attemptFile)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(attempts)), "\n")
			if len(lines) != tc.wantAttempts || !strings.Contains(lines[0], "--session-id stale-session") {
				t.Fatalf("attempts = %q, want %d starting with resume; logs:\n%s", lines, tc.wantAttempts, logs.String())
			}
			if tc.wantAttempts == 2 {
				if strings.Contains(lines[1], "--session-id") || result.RetiredSessionID != "stale-session" {
					t.Fatalf("fresh attempt retained the stale session: attempts=%q result=%+v", lines, result)
				}
				if !tc.freshFails && (result.Status != "completed" || result.Comment != "done" || result.SessionID != "fresh-session") {
					t.Fatalf("fresh retry result = %+v", result)
				}
				if tc.freshFails && result.Status != "blocked" {
					t.Fatalf("failed fresh retry = %+v", result)
				}
			} else if result.Status != "blocked" || result.RetiredSessionID != "" {
				t.Fatalf("non-retryable failure lost its disposition: %+v", result)
			}
		})
	}
}
