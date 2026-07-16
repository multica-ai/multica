package daemon

import (
	"context"
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
)

func TestRunTaskSquadLeaderReusesDaemonManagedWorkdir(t *testing.T) {
	t.Parallel()

	d, argsFile, cleanup := newLeaderReuseTestDaemon(t)
	defer cleanup()

	first := leaderReuseTestTask("task-first")
	firstResult, err := d.runTask(context.Background(), first, "claude", 0, d.logger)
	if err != nil {
		t.Fatalf("first runTask: %v", err)
	}
	if firstResult.SessionID == "" || firstResult.WorkDir == "" {
		t.Fatalf("first result missing resume state: %+v", firstResult)
	}
	if err := execenv.WriteGCMeta(firstResult.EnvRoot, execenv.GCMeta{
		Kind:        execenv.GCKindIssue,
		IssueID:     first.IssueID,
		WorkspaceID: first.WorkspaceID,
	}, d.logger); err != nil {
		t.Fatalf("write first-run GC metadata: %v", err)
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
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read claude args: %v", err)
	}
	if !strings.Contains(string(args), "--resume\nsession-leader-reuse\n") {
		t.Fatalf("second claude invocation did not resume prior session; args:\n%s", args)
	}
}

func TestRunTaskSquadLeaderDoesNotReuseExternalPriorWorkdir(t *testing.T) {
	t.Parallel()

	d, _, cleanup := newLeaderReuseTestDaemon(t)
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

func TestShouldReusePriorWorkdirSquadLeaderRejectsNonManagedPathUnderRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	userDir := filepath.Join(root, "ws-leader", "user-project")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatalf("mkdir user dir: %v", err)
	}

	task := leaderReuseTestTask("task-contained-user-dir")
	task.PriorWorkDir = userDir
	if shouldReusePriorWorkdir(task, nil, root) {
		t.Fatalf("leader reused non-managed path %q merely because it is under WorkspacesRoot", userDir)
	}
}

func TestShouldReusePriorWorkdirSquadLeaderRejectsUnmarkedManagedShape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "ws-leader", "not-a-task", "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	task := leaderReuseTestTask("task-unmarked-managed-shape")
	task.PriorWorkDir = workDir
	if shouldReusePriorWorkdir(task, nil, root) {
		t.Fatalf("leader reused unmarked lookalike workdir %q", workDir)
	}
}

func TestShouldReusePriorWorkdirSquadLeaderRejectsMismatchedTaskMarker(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "ws-leader", "12345678", "workdir")
	writeLeaderTaskMarker(t, workDir, "other-agent", "issue-leader")

	task := leaderReuseTestTask("task-mismatched-marker")
	task.PriorWorkDir = workDir
	if shouldReusePriorWorkdir(task, nil, root) {
		t.Fatalf("leader reused workdir %q with a marker for another agent", workDir)
	}
}

func TestShouldReusePriorWorkdirSquadLeaderRejectsMarkerWithoutManagedGCMeta(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "ws-leader", "12345678", "workdir")
	writeLeaderTaskMarker(t, workDir, "agent-leader", "issue-leader")

	task := leaderReuseTestTask("task-marker-without-gc-meta")
	task.PriorWorkDir = workDir
	if shouldReusePriorWorkdir(task, nil, root) {
		t.Fatalf("leader reused marked workdir %q without managed GC metadata", workDir)
	}
}

func TestShouldReusePriorWorkdirSquadLeaderRejectsLocalDirectoryGCMeta(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "ws-leader", "12345678", "workdir")
	writeLeaderTaskMarker(t, workDir, "agent-leader", "issue-leader")
	if err := execenv.WriteGCMeta(filepath.Dir(workDir), execenv.GCMeta{
		Kind:           execenv.GCKindIssue,
		IssueID:        "issue-leader",
		WorkspaceID:    "ws-leader",
		LocalDirectory: true,
	}, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("write local-directory GC metadata: %v", err)
	}

	task := leaderReuseTestTask("task-local-directory-gc-meta")
	task.PriorWorkDir = workDir
	if shouldReusePriorWorkdir(task, nil, root) {
		t.Fatalf("leader reused local-directory workdir %q without its path lock", workDir)
	}
}

func TestShouldReusePriorWorkdirSquadLeaderRejectsRegularFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "ws-leader", "12345678", "workdir")
	if err := os.MkdirAll(filepath.Dir(workDir), 0o755); err != nil {
		t.Fatalf("mkdir workdir parent: %v", err)
	}
	if err := os.WriteFile(workDir, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write workdir file: %v", err)
	}

	task := leaderReuseTestTask("task-file-workdir")
	task.PriorWorkDir = workDir
	if shouldReusePriorWorkdir(task, nil, root) {
		t.Fatalf("leader reused regular file %q as a workdir", workDir)
	}
}

func newLeaderReuseTestDaemon(t *testing.T) (*Daemon, string, func()) {
	t.Helper()

	testDir := t.TempDir()
	fakeBin := filepath.Join(testDir, "claude")
	argsFile := filepath.Join(testDir, "claude-args.txt")
	script := `#!/bin/sh
printf '%s\n' "$@" >> "` + argsFile + `"
printf '%s\n' '--invocation-end--' >> "` + argsFile + `"
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
	return d, argsFile, srv.Close
}

func writeLeaderTaskMarker(t *testing.T, workDir, agentID, issueID string) {
	t.Helper()

	markerPath := filepath.Join(workDir, execenv.TaskContextMarkerRelPath)
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		t.Fatalf("mkdir marker dir: %v", err)
	}
	marker := []byte(`{"managed_by":"` + execenv.TaskContextMarkerManagedBy + `","agent_id":"` + agentID + `","issue_id":"` + issueID + `"}`)
	if err := os.WriteFile(markerPath, marker, 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
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
