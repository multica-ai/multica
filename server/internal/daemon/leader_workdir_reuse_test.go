package daemon

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunTaskSquadLeaderReusesDaemonManagedWorkdir(t *testing.T) {
	t.Parallel()

	d, cleanup := newLeaderReuseTestDaemon(t)
	defer cleanup()

	first := leaderReuseTestTask("task-first")
	firstResult, err := d.runTask(context.Background(), first, "claude", 0, d.logger)
	if err != nil {
		t.Fatalf("first runTask: %v", err)
	}
	if firstResult.SessionID == "" || firstResult.WorkDir == "" {
		t.Fatalf("first result missing resume state: %+v", firstResult)
	}

	second := leaderReuseTestTask("task-second")
	second.PriorSessionID = firstResult.SessionID
	second.PriorWorkDir = firstResult.WorkDir
	secondResult, err := d.runTask(context.Background(), second, "claude", 0, d.logger)
	if err != nil {
		t.Fatalf("second runTask: %v", err)
	}
	if secondResult.WorkDir != firstResult.WorkDir {
		t.Fatalf("second WorkDir = %q, want reused leader workdir %q", secondResult.WorkDir, firstResult.WorkDir)
	}
}

func TestRunTaskSquadLeaderDoesNotReuseExternalPriorWorkdir(t *testing.T) {
	t.Parallel()

	d, cleanup := newLeaderReuseTestDaemon(t)
	defer cleanup()

	externalWorkDir := t.TempDir()
	task := leaderReuseTestTask("task-external")
	task.PriorSessionID = "session-leader-reuse"
	task.PriorWorkDir = externalWorkDir

	result, err := d.runTask(context.Background(), task, "claude", 0, d.logger)
	if err != nil {
		t.Fatalf("runTask: %v", err)
	}
	if result.WorkDir == externalWorkDir {
		t.Fatalf("leader reused external workdir %q without a local-directory lock", externalWorkDir)
	}
}

func newLeaderReuseTestDaemon(t *testing.T) (*Daemon, func()) {
	t.Helper()

	fakeBin := filepath.Join(t.TempDir(), "claude")
	script := `#!/bin/sh
IFS= read -r _
printf '%s\n' '{"type":"system","session_id":"session-leader-reuse"}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"session-leader-reuse","result":"done"}'
`
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake agent: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := &Daemon{
		client:         NewClient(srv.URL),
		logger:         logger,
		workspaces:     make(map[string]*workspaceState),
		runtimeIndex:   map[string]Runtime{"rt-leader": {ID: "rt-leader", Provider: "claude"}},
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
	return d, srv.Close
}

func leaderReuseTestTask(id string) Task {
	return Task{
		ID:           id,
		WorkspaceID:  "ws-leader",
		RuntimeID:    "rt-leader",
		IssueID:      "issue-leader",
		AgentID:      "agent-leader",
		AuthToken:    "mat_leader_reuse",
		IsLeaderTask: true,
		Agent: &AgentData{
			ID:   "agent-leader",
			Name: "leader-agent",
		},
	}
}
