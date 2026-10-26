//go:build !windows

package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestRunTaskOpenCodeClockSkew(t *testing.T) {
	for _, failLaunch := range []bool{false, true} {
		name := "fresh session"
		if failLaunch {
			name = "launch failure still reports retirement"
		}
		t.Run(name, func(t *testing.T) {
			redirectTestHome(t, t.TempDir())
			dir := t.TempDir()
			bin := filepath.Join(dir, "opencode")
			argsPath, promptPath := filepath.Join(dir, "args"), filepath.Join(dir, "prompt")
			script := `#!/bin/sh
case "$1" in
export)
  printf '%s' '{"info":{"id":"ses-old","time":{"updated":9999999999999}},"messages":[]}'
  if [ "$CLOCK_FAIL_LAUNCH" = true ]; then chmod -x "$0"; fi
  exit 0;;
models) exit 0;;
run)
  printf '%s\n' "$@" > "$CLOCK_ARGS"
  cat > "$CLOCK_PROMPT"
  printf '%s\n' '{"type":"step_start","sessionID":"ses-new","part":{"type":"step-start"}}'
  printf '%s\n' '{"type":"text","sessionID":"ses-new","part":{"type":"text","text":"done"}}'
  printf '%s\n' '{"type":"step_finish","sessionID":"ses-new","part":{"type":"step-finish","reason":"stop"}}'
  exit 0;;
esac
exit 1
`
			if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			reports := make(chan map[string]any, 2)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/fail") || strings.HasSuffix(r.URL.Path, "/complete") {
					var report map[string]any
					if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
						t.Error(err)
					}
					reports <- report
				}
				_, _ = w.Write([]byte(`{"status":"running"}`))
			}))
			defer srv.Close()
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			d := &Daemon{
				client: NewClient(srv.URL), logger: logger,
				workspaces:     make(map[string]*workspaceState),
				runtimeIndex:   map[string]Runtime{"rt-clock": {ID: "rt-clock", Provider: "opencode"}},
				activeEnvRoots: make(map[string]int),
				agentVersions:  make(map[string]string),
				cfg:            Config{WorkspacesRoot: t.TempDir(), AgentTimeout: 5 * time.Second, ServerBaseURL: srv.URL, Agents: map[string]AgentEntry{"opencode": {Path: bin}}},
			}
			d.setAgentVersion("opencode", "1.18.31")
			d.runner = taskRunnerFunc(d.runTask)
			fail := "false"
			if failLaunch {
				fail = "true"
			}
			task := Task{ID: "task-clock", WorkspaceID: "ws-clock", RuntimeID: "rt-clock", IssueID: "issue-clock", AgentID: "agent-clock", AuthToken: "mat_clock", PriorSessionID: "ses-old", Agent: &AgentData{ID: "agent-clock", Name: "clock-agent", CustomEnv: map[string]string{"CLOCK_FAIL_LAUNCH": fail, "CLOCK_ARGS": argsPath, "CLOCK_PROMPT": promptPath}}}
			seed := task
			seed.ID, seed.PriorSessionID = "seed-clock", ""
			prepared, err := d.runTask(context.Background(), seed, "opencode", 0, logger)
			if err != nil || prepared.Status != "completed" {
				t.Fatalf("seed managed workdir: %+v, %v", prepared, err)
			}
			task.PriorWorkDir = prepared.WorkDir
			d.handleTask(context.Background(), task, 0)
			select {
			case report := <-reports:
				if report["retired_session_id"] != "ses-old" {
					t.Fatalf("retirement missing from terminal callback: %+v", report)
				}
				if report["work_dir"] == "" || report["work_dir"] == nil {
					t.Fatalf("workdir lost: %+v", report)
				}
				if !failLaunch && report["session_id"] != "ses-new" {
					t.Fatalf("fresh session not reported: %+v", report)
				}
				if failLaunch && report["error"] == nil {
					t.Fatalf("launch failure not reported: %+v", report)
				}
			default:
				t.Fatal("terminal callback not sent")
			}
			if !failLaunch {
				args, err := os.ReadFile(argsPath)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(args), "--session") {
					t.Fatalf("stale session forwarded: %s", args)
				}
				prompt, err := os.ReadFile(promptPath)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(prompt), "Session Continuity Notice") {
					t.Fatalf("cold-start continuity notice missing: %s", prompt)
				}
			}
		})
	}
}

func TestOpenCodeClockSkewClassification(t *testing.T) {
	reason, ok := classifyOpenCodeClockSkew("opencode", agent.ErrOpenCodeClockSkew.Error())
	if !ok || reason != "opencode_clock_skew" || !service.ResumeUnsafeFailure(reason, "") {
		t.Fatalf("unsafe reason not propagated: %q %v", reason, ok)
	}
	if _, ok := classifyOpenCodeClockSkew("codex", agent.ErrOpenCodeClockSkew.Error()); ok {
		t.Fatal("other providers must not match")
	}
}

func TestOpenCodeClockGateUnverifiedPreservesResume(t *testing.T) {
	var logs strings.Builder
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	task := Task{PriorSessionID: "ses-old"}
	carrier := execenv.TaskContextForEnv{PriorSessionResumed: true}
	retired := gateOpenCodeResumeToSaneClock(context.Background(), &task, &carrier, "opencode", agent.Config{ExecutablePath: filepath.Join(t.TempDir(), "missing"), BuiltinRuntime: true, CLIVersion: "opencode v2.0.10"}, "", nil, logger)
	if retired != "" || task.PriorSessionID != "ses-old" || !carrier.PriorSessionResumed || task.PriorSessionResumeUnavailable {
		t.Fatal("unverified runtime changed resume state")
	}
	if !strings.Contains(logs.String(), `"resume_clock_check":"unverified"`) {
		t.Fatal("missing structured degradation warning")
	}
}
