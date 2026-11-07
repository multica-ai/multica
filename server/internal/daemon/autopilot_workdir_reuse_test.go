package daemon

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// GH #8532: consecutive runs of one autopilot must land in the SAME working
// directory so the provider's prompt cache survives across runs. The reuse
// path already exists for issue and chat follow-ups; these tests pin the
// autopilot scope onto it.

// writeAutopilotReuseEnvRoot stands up an env root that looks exactly like the
// output of a prior autopilot run's execenv.Prepare: provenance at the env
// root plus the task-context marker inside the workdir, both carrying the
// autopilot scope. The JSON is written by hand so the gate is exercised
// against the on-disk contract (old markers without autopilot_id must keep
// failing closed).
func writeAutopilotReuseEnvRoot(t *testing.T, workspacesRoot, workspaceID, agentID, autopilotID string, withAutopilotScope bool) string {
	t.Helper()

	workDir := filepath.Join(workspacesRoot, workspaceID, "taskroot-1", "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	envRoot := filepath.Dir(workDir)

	prov := fmt.Sprintf(`{"managed_by":%q,"workspace_id":%q,"agent_id":%q`, execenv.ManagedEnvProvenanceManagedBy, workspaceID, agentID)
	if withAutopilotScope {
		prov += fmt.Sprintf(`,"autopilot_id":%q`, autopilotID)
	}
	prov += "}"
	if err := os.WriteFile(filepath.Join(envRoot, ".managed_env.json"), []byte(prov), 0o644); err != nil {
		t.Fatalf("write provenance: %v", err)
	}

	marker := fmt.Sprintf(`{"managed_by":%q,"agent_id":%q`, execenv.TaskContextMarkerManagedBy, agentID)
	if withAutopilotScope {
		marker += fmt.Sprintf(`,"autopilot_id":%q`, autopilotID)
	}
	marker += "}"
	markerPath := filepath.Join(workDir, execenv.TaskContextMarkerRelPath)
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		t.Fatalf("mkdir marker dir: %v", err)
	}
	if err := os.WriteFile(markerPath, []byte(marker), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	return workDir
}

func autopilotReuseTask(priorWorkDir, autopilotID string) Task {
	return Task{
		ID:             "task-autopilot-2",
		WorkspaceID:    "ws-ap",
		RuntimeID:      "rt-ap",
		AgentID:        "agent-ap",
		AutopilotRunID: "run-2",
		AutopilotID:    autopilotID,
		PriorWorkDir:   priorWorkDir,
		AuthToken:      "mat_autopilot_reuse",
		Agent: &AgentData{
			ID:   "agent-ap",
			Name: "autopilot-agent",
		},
	}
}

func TestShouldReusePriorWorkdirAutopilotScope(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	// The gate canonicalizes both spellings (util.ResolveSymlinks), so build
	// the fixture from the canonical root to keep the string comparison exact.
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}

	t.Run("same autopilot id reuses", func(t *testing.T) {
		workDir := writeAutopilotReuseEnvRoot(t, root, "ws-ap", "agent-ap", "ap-1", true)
		got, ok := shouldReusePriorWorkdir(autopilotReuseTask(workDir, "ap-1"), nil, root)
		if !ok || got != workDir {
			t.Fatalf("shouldReusePriorWorkdir = %q, %v; want %q, true", got, ok, workDir)
		}
	})

	t.Run("different autopilot id declines", func(t *testing.T) {
		workDir := writeAutopilotReuseEnvRoot(t, root, "ws-ap", "agent-ap", "ap-1", true)
		if _, ok := shouldReusePriorWorkdir(autopilotReuseTask(workDir, "ap-other"), nil, root); ok {
			t.Fatal("reuse accepted a workdir owned by another autopilot")
		}
	})

	t.Run("legacy marker without autopilot scope declines", func(t *testing.T) {
		workDir := writeAutopilotReuseEnvRoot(t, root, "ws-ap", "agent-ap", "ap-1", false)
		if _, ok := shouldReusePriorWorkdir(autopilotReuseTask(workDir, "ap-1"), nil, root); ok {
			t.Fatal("reuse accepted a pre-autopilot-reuse marker; first run after upgrade must start fresh")
		}
	})

	t.Run("task without autopilot scope still declines (issue/chat unchanged)", func(t *testing.T) {
		workDir := writeAutopilotReuseEnvRoot(t, root, "ws-ap", "agent-ap", "ap-1", true)
		task := autopilotReuseTask(workDir, "ap-1")
		task.AutopilotID = ""
		task.IssueID = ""
		if _, ok := shouldReusePriorWorkdir(task, nil, root); ok {
			t.Fatal("reuse accepted a task with no scope at all")
		}
	})
}

// TestRunTaskAutopilotReusesWorkdir drives two real runTask calls, the second
// carrying the first run's workdir the way the server will offer it once the
// claim handler extends PriorWorkDir to autopilot tasks. The follow-up must
// run in the SAME directory: that path is what the provider's prompt cache
// keys on (GH #8532).
func TestRunTaskAutopilotReusesWorkdir(t *testing.T) {
	t.Parallel()

	d, _, cleanup := newAutopilotReuseTestDaemon(t)
	defer cleanup()

	first := autopilotReuseTask("", "ap-1")
	first.ID = "task-autopilot-1"
	firstResult, err := d.runTask(context.Background(), first, "claude", 0, d.logger)
	if err != nil {
		t.Fatalf("first runTask: %v", err)
	}
	if firstResult.WorkDir == "" {
		t.Fatalf("first result missing workdir: %+v", firstResult)
	}

	second := autopilotReuseTask(firstResult.WorkDir, "ap-1")
	secondResult, err := d.runTask(context.Background(), second, "claude", 0, d.logger)
	if err != nil {
		t.Fatalf("second runTask: %v", err)
	}
	// Compare identity, not spelling: the reuse gate reports the canonical
	// (symlink-resolved) path, which on macOS differs from the /tmp spelling
	// the fresh Prepare handed back.
	firstInfo, err := os.Stat(firstResult.WorkDir)
	if err != nil {
		t.Fatalf("stat first workdir: %v", err)
	}
	secondInfo, err := os.Stat(secondResult.WorkDir)
	if err != nil {
		t.Fatalf("stat second workdir: %v", err)
	}
	if !os.SameFile(firstInfo, secondInfo) {
		t.Fatalf("second WorkDir = %q, want reused autopilot workdir %q", secondResult.WorkDir, firstResult.WorkDir)
	}
}

func newAutopilotReuseTestDaemon(t *testing.T) (*Daemon, string, func()) {
	t.Helper()

	testDir := t.TempDir()
	fakeBin := filepath.Join(testDir, "claude")
	argsFile := filepath.Join(testDir, "claude-args.txt")
	script := `#!/bin/sh
printf '%s\n' "$@" >> "` + argsFile + `"
printf '%s\n' '--invocation-end--' >> "` + argsFile + `"
IFS= read -r _
printf '%s\n' '{"type":"system","session_id":"session-autopilot-reuse"}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"session-autopilot-reuse","result":"done"}'
`
	writeTestExecutable(t, fakeBin, []byte(script))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := &Daemon{
		client:         NewClient(srv.URL),
		logger:         logger,
		workspaces:     make(map[string]*workspaceState),
		runtimeIndex:   map[string]Runtime{"rt-ap": {ID: "rt-ap", Provider: "claude"}},
		activeEnvRoots: make(map[string]int),
		cfg: Config{
			WorkspacesRoot: t.TempDir(),
			AgentTimeout:   5 * time.Second,
			ServerBaseURL:  srv.URL,
			Agents: map[string]AgentEntry{
				"claude": {Path: fakeBin},
			},
		},
	}
	return d, argsFile, srv.Close
}
