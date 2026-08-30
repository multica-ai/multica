package daemon

import (
	"context"
	"io"
	"io/fs"
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

// TestRunTaskSquadLeaderReusesWorkdirBeforeGCMetaWritten drives two real
// runTask calls and asserts the follow-up reuses the first workdir and provider
// session while NO .gc_meta.json exists. That absence is the whole point: the
// server marks the prior task completed, reconciles the follow-up, and wakes
// the runtime before the prior task's handler writes .gc_meta.json, so a
// successor can be claimed inside that window (MUL-4886). Reuse must therefore
// hinge on the Prepare-time .managed_env.json provenance, not the terminal GC
// file. runTask writes that provenance via execenv.Prepare; this test never
// writes .gc_meta.json, so it fails against the pre-fix GC-meta-keyed gate.
func TestRunTaskSquadLeaderReusesWorkdirBeforeGCMetaWritten(t *testing.T) {
	t.Parallel()

	d, argsFile, cleanup := newLeaderReuseTestDaemon(t)
	defer cleanup()

	first := leaderReuseTestTask("task-first")
	first.WorkspaceSlug = "original-workspace"
	first.IssueIdentifier = "MUL-6063"
	firstResult, err := d.runTask(context.Background(), first, "claude", 0, d.logger)
	if err != nil {
		t.Fatalf("first runTask: %v", err)
	}
	if firstResult.SessionID == "" || firstResult.WorkDir == "" {
		t.Fatalf("first result missing resume state: %+v", firstResult)
	}
	// Simulate the race window: the successor is claimed before the prior
	// task's handler writes .gc_meta.json. The Prepare-time provenance is the
	// only reuse signal available.
	if _, err := os.Stat(filepath.Join(firstResult.EnvRoot, ".gc_meta.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no .gc_meta.json before the completion handler runs; stat err = %v", err)
	}

	second := leaderReuseTestTask("task-second")
	// Labels are mutable display data. A renamed workspace or issue must keep
	// reusing the recorded path rather than deriving a new root from the names.
	second.WorkspaceSlug = "renamed-workspace"
	second.IssueIdentifier = "NEW-6063"
	second.PriorSessionID = firstResult.SessionID
	second.PriorWorkDir = firstResult.WorkDir
	secondResult, err := d.runTask(context.Background(), second, "claude", 0, d.logger)
	if err != nil {
		t.Fatalf("second runTask: %v", err)
	}
	// Compare resolved paths: reuse runs in the canonical directory it locked,
	// and on macOS the test root itself is a symlink (/tmp -> /private/tmp), so
	// the two spellings name one directory.
	if !sameDir(t, secondResult.WorkDir, firstResult.WorkDir) {
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

// TestShouldReusePriorWorkdirNonLeaderRequiresProvenance prevents an ordinary
// task from inheriting another agent's repository and Git HEAD merely because
// the server offered that path.
func TestShouldReusePriorWorkdirNonLeaderRequiresProvenance(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	task := leaderReuseTestTask("task-non-leader")
	task.IsLeaderTask = false
	task.PriorWorkDir = filepath.Join(root, "anything", "workdir")
	if _, ok := shouldReusePriorWorkdir(task, nil, root); ok {
		t.Fatal("non-leader task reused a prior workdir without any ownership provenance")
	}
}

func TestShouldReusePriorWorkdirChatAcceptsMatchingConversation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "ws-chat", "12345678", "workdir")
	writeChatTaskMarker(t, workDir, "agent-chat", "chat-session")
	writeChatManagedEnvProvenance(t, workDir, "ws-chat", "chat-session", "agent-chat")

	task := leaderReuseTestTask("task-chat")
	task.WorkspaceID = "ws-chat"
	task.IssueID = ""
	task.ChatSessionID = "chat-session"
	task.AgentID = "agent-chat"
	task.IsLeaderTask = false
	task.PriorWorkDir = workDir
	if _, ok := shouldReusePriorWorkdir(task, nil, root); !ok {
		t.Fatalf("chat task did not reuse its fully-provenanced conversation workdir %q", workDir)
	}

	task.ChatSessionID = "another-chat"
	if _, ok := shouldReusePriorWorkdir(task, nil, root); ok {
		t.Fatal("chat task reused a workdir belonging to another conversation")
	}
}

// TestShouldReusePriorWorkdirSquadLeaderAcceptsManagedProvenance is the unit
// positive: managed shape + matching Prepare-time provenance + matching marker.
func TestShouldReusePriorWorkdirSquadLeaderAcceptsManagedProvenance(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "ws-leader", "12345678", "workdir")
	writeLeaderTaskMarker(t, workDir, "agent-leader", "issue-leader")
	writeLeaderManagedEnvProvenance(t, workDir, "ws-leader", "issue-leader", "agent-leader")

	task := leaderReuseTestTask("task-accept")
	task.PriorWorkDir = workDir
	if _, ok := shouldReusePriorWorkdir(task, nil, root); !ok {
		t.Fatalf("leader did not reuse a fully-provenanced managed workdir %q", workDir)
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
	if _, ok := shouldReusePriorWorkdir(task, nil, root); ok {
		t.Fatalf("leader reused non-managed path %q merely because it is under WorkspacesRoot", userDir)
	}
}

// TestShouldReusePriorWorkdirSquadLeaderRejectsManagedShapeWithoutProvenance
// covers the race-critical case and the local_directory fail-closed guarantee:
// a workdir with the right shape and a valid marker but NO .managed_env.json is
// rejected. Local_directory envs never get provenance (Prepare skips it), and a
// follow-up claimed before any provenance exists must start fresh rather than
// risk reusing a user path.
func TestShouldReusePriorWorkdirSquadLeaderRejectsManagedShapeWithoutProvenance(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "ws-leader", "12345678", "workdir")
	writeLeaderTaskMarker(t, workDir, "agent-leader", "issue-leader")

	task := leaderReuseTestTask("task-without-provenance")
	task.PriorWorkDir = workDir
	if _, ok := shouldReusePriorWorkdir(task, nil, root); ok {
		t.Fatalf("leader reused marked workdir %q without managed-env provenance", workDir)
	}
}

// TestShouldReusePriorWorkdirSquadLeaderRejectsMismatchedProvenanceOwner
// rejects a provenance file whose workspace/issue/agent does not match the
// claiming task, even when the marker is otherwise well-formed.
func TestShouldReusePriorWorkdirSquadLeaderRejectsMismatchedProvenanceOwner(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "ws-leader", "12345678", "workdir")
	writeLeaderTaskMarker(t, workDir, "agent-leader", "issue-leader")
	writeLeaderManagedEnvProvenance(t, workDir, "ws-leader", "issue-leader", "other-agent")

	task := leaderReuseTestTask("task-mismatched-provenance")
	task.PriorWorkDir = workDir
	if _, ok := shouldReusePriorWorkdir(task, nil, root); ok {
		t.Fatalf("leader reused workdir %q with provenance owned by another agent", workDir)
	}
}

// TestShouldReusePriorWorkdirSquadLeaderRejectsMismatchedTaskMarker keeps its
// original intent — a marker for another agent must be refused — now with a
// matching provenance in place so the check reaches the marker comparison.
func TestShouldReusePriorWorkdirSquadLeaderRejectsMismatchedTaskMarker(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "ws-leader", "12345678", "workdir")
	writeLeaderTaskMarker(t, workDir, "other-agent", "issue-leader")
	writeLeaderManagedEnvProvenance(t, workDir, "ws-leader", "issue-leader", "agent-leader")

	task := leaderReuseTestTask("task-mismatched-marker")
	task.PriorWorkDir = workDir
	if _, ok := shouldReusePriorWorkdir(task, nil, root); ok {
		t.Fatalf("leader reused workdir %q with a marker for another agent", workDir)
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
	if _, ok := shouldReusePriorWorkdir(task, nil, root); ok {
		t.Fatalf("leader reused regular file %q as a workdir", workDir)
	}
}

func TestShouldReusePriorWorkdirSquadLeaderRejectsEmptyAgentID(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "ws-leader", "12345678", "workdir")
	writeLeaderTaskMarker(t, workDir, "agent-leader", "issue-leader")
	writeLeaderManagedEnvProvenance(t, workDir, "ws-leader", "issue-leader", "agent-leader")

	task := leaderReuseTestTask("task-empty-agent")
	task.AgentID = ""
	task.PriorWorkDir = workDir
	if _, ok := shouldReusePriorWorkdir(task, nil, root); ok {
		t.Fatal("leader with an empty AgentID must not reuse a prior workdir")
	}
}

func TestShouldReusePriorWorkdirSquadLeaderRejectsSymlinkEscape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	external := t.TempDir()
	parent := filepath.Join(root, "ws-leader", "12345678")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	// A managed-shape path whose final segment is a symlink escaping the root.
	// EvalSymlinks + IsLocal must reject it so a symlink can't smuggle a user
	// directory past the containment check.
	workDir := filepath.Join(parent, "workdir")
	if err := os.Symlink(external, workDir); err != nil {
		t.Fatalf("symlink workdir -> external: %v", err)
	}

	task := leaderReuseTestTask("task-symlink-escape")
	task.PriorWorkDir = workDir
	if _, ok := shouldReusePriorWorkdir(task, nil, root); ok {
		t.Fatalf("leader reused a workdir symlinked outside WorkspacesRoot (%q -> %q)", workDir, external)
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

func writeChatTaskMarker(t *testing.T, workDir, agentID, chatSessionID string) {
	t.Helper()

	markerPath := filepath.Join(workDir, execenv.TaskContextMarkerRelPath)
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		t.Fatalf("mkdir marker dir: %v", err)
	}
	marker := []byte(`{"managed_by":"` + execenv.TaskContextMarkerManagedBy + `","agent_id":"` + agentID + `","chat_session_id":"` + chatSessionID + `"}`)
	if err := os.WriteFile(markerPath, marker, 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
}

func writeLeaderManagedEnvProvenance(t *testing.T, workDir, workspaceID, issueID, agentID string) {
	t.Helper()

	envRoot := filepath.Dir(workDir)
	if err := os.MkdirAll(envRoot, 0o755); err != nil {
		t.Fatalf("mkdir env root: %v", err)
	}
	if err := execenv.WriteManagedEnvProvenance(envRoot, execenv.ManagedEnvProvenance{
		WorkspaceID: workspaceID,
		IssueID:     issueID,
		AgentID:     agentID,
	}); err != nil {
		t.Fatalf("write managed env provenance: %v", err)
	}
}

func writeChatManagedEnvProvenance(t *testing.T, workDir, workspaceID, chatSessionID, agentID string) {
	t.Helper()

	envRoot := filepath.Dir(workDir)
	if err := os.MkdirAll(envRoot, 0o755); err != nil {
		t.Fatalf("mkdir env root: %v", err)
	}
	if err := execenv.WriteManagedEnvProvenance(envRoot, execenv.ManagedEnvProvenance{
		WorkspaceID:   workspaceID,
		ChatSessionID: chatSessionID,
		AgentID:       agentID,
	}); err != nil {
		t.Fatalf("write managed env provenance: %v", err)
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

// TestLockReusablePriorEnvRootNeverWritesOutsideWorkspacesRoot pins the
// ordering that makes the reuse lock safe. Taking the lock writes .task_lock
// INTO the directory, so it must happen only after the prior workdir is proven
// to be a canonical managed env root.
//
// An earlier revision locked filepath.Dir(task.PriorWorkDir) up front, guarded
// only by a lexical containment check. filepath.Rel does not resolve symlinks,
// so a link inside the workspaces root pointing anywhere else passed the guard
// and the lock file was created in the target — a user's directory, or any
// path outside the root entirely.
func TestLockReusablePriorEnvRootNeverWritesOutsideWorkspacesRoot(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		// build returns the PriorWorkDir to offer, plus the directory tree that
		// must come back untouched.
		build func(t *testing.T, workspacesRoot, outside string) string
	}{
		{
			name: "symlink inside the root pointing outside it",
			build: func(t *testing.T, workspacesRoot, outside string) string {
				target := filepath.Join(outside, "someone-elses-repo")
				if err := os.MkdirAll(filepath.Join(target, "workdir"), 0o755); err != nil {
					t.Fatalf("seed target: %v", err)
				}
				link := filepath.Join(workspacesRoot, "ws-1", "escape")
				if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
					t.Fatalf("seed link parent: %v", err)
				}
				if err := os.Symlink(target, link); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				return filepath.Join(link, "workdir")
			},
		},
		{
			name: "plain path outside the root",
			build: func(t *testing.T, workspacesRoot, outside string) string {
				dir := filepath.Join(outside, "local-project", "workdir")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatalf("seed: %v", err)
				}
				return dir
			},
		},
		{
			name: "managed-looking shape but no provenance",
			build: func(t *testing.T, workspacesRoot, outside string) string {
				dir := filepath.Join(workspacesRoot, "ws-1", "0123456789ab", "workdir")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatalf("seed: %v", err)
				}
				return dir
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			base := t.TempDir()
			workspacesRoot := filepath.Join(base, "workspaces")
			outside := filepath.Join(base, "outside")
			if err := os.MkdirAll(workspacesRoot, 0o755); err != nil {
				t.Fatalf("seed root: %v", err)
			}
			if err := os.MkdirAll(outside, 0o755); err != nil {
				t.Fatalf("seed outside: %v", err)
			}

			priorWorkDir := tc.build(t, workspacesRoot, outside)
			d := &Daemon{logger: discardLogger()}
			d.cfg.WorkspacesRoot = workspacesRoot

			claim, _, _, ok := d.lockReusablePriorEnvRoot(Task{
				ID:           "01a01ec0-e69d-7000-8000-0123456789ab",
				WorkspaceID:  "ws-1",
				AgentID:      "agent-1",
				IssueID:      "issue-1",
				PriorWorkDir: priorWorkDir,
			}, nil, "")
			if ok {
				claim.Release()
				t.Fatal("unvalidated prior workdir was accepted for reuse")
			}
			if claim != nil {
				claim.Release()
				t.Fatal("declined reuse still returned a claim")
			}
			if found := findTaskLocks(t, outside); len(found) > 0 {
				t.Fatalf("prior-workdir guard wrote the lock outside workspaces root: %v", found)
			}
		})
	}
}

// findTaskLocks returns every .task_lock under dir.
func findTaskLocks(t *testing.T, dir string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !e.IsDir() && e.Name() == ".task_lock" {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return found
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestLockReusablePriorEnvRootLocksAValidatedRoot is the positive half: a fully
// provenanced managed workdir IS accepted, the lock lands inside the workspaces
// root, and a second continuation of the same task is excluded from it while
// the first holds it. Declining is then correct rather than fatal — the caller
// falls back to a fresh Prepare.
func TestLockReusablePriorEnvRootLocksAValidatedRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "ws-leader", "0123456789ab", "workdir")
	writeLeaderTaskMarker(t, workDir, "agent-leader", "issue-leader")
	writeLeaderManagedEnvProvenance(t, workDir, "ws-leader", "issue-leader", "agent-leader")

	task := leaderReuseTestTask("task-reuse")
	task.PriorWorkDir = workDir

	d := &Daemon{logger: discardLogger()}
	d.cfg.WorkspacesRoot = root

	claim, canonical, _, ok := d.lockReusablePriorEnvRoot(task, nil, "")
	if !ok {
		t.Fatal("a fully provenanced managed workdir was refused for reuse")
	}
	if claim == nil {
		t.Fatal("accepted reuse without taking the lock")
	}
	if canonical == "" {
		t.Fatal("accepted reuse without returning the canonical workdir")
	}
	// The lock must be inside the env root we validated, and nowhere else.
	// Compare resolved paths: canonical comes back through EvalSymlinks, and on
	// macOS the temp root itself is a symlink (/tmp -> /private/tmp).
	locks := findTaskLocks(t, root)
	if len(locks) != 1 {
		t.Fatalf("found %v .task_lock files, want exactly one", locks)
	}
	gotDir, err := filepath.EvalSymlinks(filepath.Dir(locks[0]))
	if err != nil {
		t.Fatalf("resolve lock dir: %v", err)
	}
	if wantDir := filepath.Dir(canonical); gotDir != wantDir {
		t.Fatalf("lock landed in %s, want %s", gotDir, wantDir)
	}

	// A concurrent continuation of the same task must not get the same root.
	if second, _, _, ok := d.lockReusablePriorEnvRoot(task, nil, ""); ok {
		second.Release()
		t.Fatal("two continuations locked the same prior workdir at once")
	}

	claim.Release()
	again, _, _, ok := d.lockReusablePriorEnvRoot(task, nil, "")
	if !ok {
		t.Fatal("prior workdir stayed locked after release")
	}
	again.Release()
}

// TestLockReusablePriorEnvRootSurvivesRetargetAfterValidation is the TOCTOU
// half review found: the canonical path returned by validation is a string,
// not a handle. Between proving eligibility and opening the lock, the
// validated env root can be renamed away and a symlink to somewhere outside
// dropped in its place; an ordinary open follows it and creates .task_lock out
// there, and no later re-check can undo a write that already happened.
//
// The swap is made deterministic with reuseLockTestHook rather than raced.
func TestLockReusablePriorEnvRootSurvivesRetargetAfterValidation(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "workspaces")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("seed outside: %v", err)
	}

	workDir := filepath.Join(root, "ws-leader", "0123456789ab", "workdir")
	writeLeaderTaskMarker(t, workDir, "agent-leader", "issue-leader")
	writeLeaderManagedEnvProvenance(t, workDir, "ws-leader", "issue-leader", "agent-leader")
	envRoot := filepath.Dir(workDir)

	task := leaderReuseTestTask("task-retarget")
	task.PriorWorkDir = workDir

	d := &Daemon{logger: discardLogger()}
	d.cfg.WorkspacesRoot = root

	// Validation has already passed by the time this runs.
	reuseLockTestHook = func() {
		if err := os.Rename(envRoot, envRoot+"-moved"); err != nil {
			t.Fatalf("retarget rename: %v", err)
		}
		if err := os.Symlink(outside, envRoot); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	t.Cleanup(func() { reuseLockTestHook = nil })

	claim, _, _, ok := d.lockReusablePriorEnvRoot(task, nil, "")
	if claim != nil {
		claim.Release()
	}
	if ok {
		t.Fatal("reuse was accepted after the validated root was retargeted")
	}
	if locks := findTaskLocks(t, outside); len(locks) > 0 {
		t.Fatalf("validated canonical root was retargeted and the lock was written outside: %v", locks)
	}
}

// TestLockReusablePriorEnvRootRejectsIdentitySwap is the other half, and the
// reason comparing canonical path STRINGS is not sufficient. Here the
// validated root is moved aside and a brand new, equally well-provenanced
// directory is created at exactly the same path. Every name-based check still
// agrees — the canonical path is character-for-character what validation
// returned — but it is a different directory. Accepting it would leave the
// daemon holding a lock on the old inode while Reuse ran in the new one, i.e.
// unprotected in a directory another execution may own. Only comparing
// identity (os.SameFile) can tell them apart.
func TestLockReusablePriorEnvRootRejectsIdentitySwap(t *testing.T) {
	root := t.TempDir()

	workDir := filepath.Join(root, "ws-leader", "aaaa56789abc", "workdir")
	writeLeaderTaskMarker(t, workDir, "agent-leader", "issue-leader")
	writeLeaderManagedEnvProvenance(t, workDir, "ws-leader", "issue-leader", "agent-leader")
	envRoot := filepath.Dir(workDir)

	task := leaderReuseTestTask("task-swap")
	task.PriorWorkDir = workDir

	d := &Daemon{logger: discardLogger()}
	d.cfg.WorkspacesRoot = root

	// After validation, swap in a different directory at the SAME path, just
	// as legitimate: same workspace, agent and issue.
	reuseLockTestHook = func() {
		if err := os.Rename(envRoot, envRoot+"-moved"); err != nil {
			t.Fatalf("move the validated root aside: %v", err)
		}
		writeLeaderTaskMarker(t, workDir, "agent-leader", "issue-leader")
		writeLeaderManagedEnvProvenance(t, workDir, "ws-leader", "issue-leader", "agent-leader")
	}
	t.Cleanup(func() { reuseLockTestHook = nil })

	claim, used, _, ok := d.lockReusablePriorEnvRoot(task, nil, "")
	if claim != nil {
		claim.Release()
	}
	if ok {
		t.Fatalf("accepted reuse after the validated directory was replaced at the same path (would use %s)", used)
	}
}

// sameDir reports whether two paths name the same directory, independent of
// how they spell it.
func sameDir(t *testing.T, a, b string) bool {
	t.Helper()
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

// TestLockReusablePriorEnvRootSurvivesWorkspacesRootSwap covers the subtler
// half of the pinning problem. os.Root guarantees you cannot escape the tree
// it opened — it does not guarantee it opened the tree you meant. Opening the
// workspaces root from a name AFTER validation would let the whole root be
// renamed aside and a symlink to a look-alike tree left behind: Root then
// faithfully pins the replacement, and the lock file is created out there.
//
// Pinning the root before any validation runs is what makes that ordering
// impossible.
func execenvLockForTest(wsRoot *os.Root, rel, envRoot string) (*execenv.EnvRootClaim, os.FileInfo, error) {
	return execenv.LockEnvRootForReuse(wsRoot, rel, envRoot)
}

func TestLockReusablePriorEnvRootSurvivesWorkspacesRootSwap(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "workspaces")
	outside := filepath.Join(base, "outside")

	workDir := filepath.Join(root, "ws-leader", "0123456789ab", "workdir")
	writeLeaderTaskMarker(t, workDir, "agent-leader", "issue-leader")
	writeLeaderManagedEnvProvenance(t, workDir, "ws-leader", "issue-leader", "agent-leader")

	// A look-alike tree with the same relative shape, outside the root.
	decoy := filepath.Join(outside, "ws-leader", "0123456789ab")
	if err := os.MkdirAll(decoy, 0o755); err != nil {
		t.Fatalf("seed decoy: %v", err)
	}

	task := leaderReuseTestTask("task-rootswap")
	task.PriorWorkDir = workDir

	d := &Daemon{logger: discardLogger()}
	d.cfg.WorkspacesRoot = root

	reuseLockTestHook = func() {
		if err := os.Rename(root, root+"-moved"); err != nil {
			t.Fatalf("move the workspaces root aside: %v", err)
		}
		if err := os.Symlink(outside, root); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	t.Cleanup(func() { reuseLockTestHook = nil })

	claim, _, _, ok := d.lockReusablePriorEnvRoot(task, nil, "")
	if claim != nil {
		claim.Release()
	}
	if ok {
		t.Fatal("reuse was accepted after the workspaces root itself was replaced")
	}
	if locks := findTaskLocks(t, outside); len(locks) > 0 {
		t.Fatalf("OpenRoot pinned the replacement tree and wrote the lock outside: %v", locks)
	}
}

// TestLockEnvRootForReuseTakesIdentityAndLockFromOneHandle pins that the
// directory whose identity is reported and the directory the lock file lands
// in are the same one. Resolving the env root's relative path twice from the
// workspaces Root — once to stat, once to create — would let it be A on the
// first resolution and B on the second, reporting A's identity while holding
// B's lock. Both now come from a single sub-Root pinned on the env root.
func TestLockEnvRootForReuseTakesIdentityAndLockFromOneHandle(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	rel := filepath.Join("ws-leader", "0123456789ab")
	envRoot := filepath.Join(root, rel)
	if err := os.MkdirAll(envRoot, 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}

	wsRoot, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("open workspaces root: %v", err)
	}
	defer wsRoot.Close()

	claim, info, err := execenvLockForTest(wsRoot, rel, envRoot)
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	if claim == nil || info == nil {
		t.Fatal("expected a claim and the locked directory identity")
	}
	defer claim.Release()

	// The reported identity must be the directory the lock file is actually in.
	lockDirInfo, err := os.Stat(filepath.Dir(findTaskLocks(t, root)[0]))
	if err != nil {
		t.Fatalf("stat lock dir: %v", err)
	}
	if !os.SameFile(info, lockDirInfo) {
		t.Fatal("reported identity is not the directory holding the lock file")
	}
}

// TestRunTaskDeclinesReuseWhenTheClaimedDirectoryIsSwappedBeforeUse covers the
// last window: the claim is settled, and only THEN does Reuse resolve the
// prior workdir by name. A file descriptor cannot cross into the preparation
// helper process, so a name is the only thing that can be handed over — which
// means this window cannot be closed by pinning alone. What it can be is
// detected: the environment Reuse produced is checked back against the
// directory the lock is held on, so a substitution ends the run in a fresh
// environment instead of executing in a directory nothing holds.
func TestRunTaskDeclinesReuseWhenTheClaimedDirectoryIsSwappedBeforeUse(t *testing.T) {
	d, argsFile, cleanup := newLeaderReuseTestDaemon(t)
	defer cleanup()
	_ = argsFile

	first := leaderReuseTestTask("task-first")
	firstResult, err := d.runTask(context.Background(), first, "claude", 0, d.logger)
	if err != nil {
		t.Fatalf("first runTask: %v", err)
	}
	envRoot := filepath.Dir(firstResult.WorkDir)

	// Swap the claimed directory for an equally valid one after the claim is
	// settled and before Reuse opens it by name.
	var swapped bool
	reuseBeforeUseTestHook = func() {
		if swapped {
			return
		}
		swapped = true
		// The replacement must be one Reuse would happily accept, or the test
		// passes because Reuse declined on its own merits rather than because
		// the identity check caught the substitution.
		replacement := envRoot + "-replacement"
		replacementWorkDir := filepath.Join(replacement, "workdir")
		writeLeaderTaskMarker(t, replacementWorkDir, "agent-leader", "issue-leader")
		writeLeaderManagedEnvProvenance(t, replacementWorkDir, "ws-leader", "issue-leader", "agent-leader")
		if err := os.Rename(envRoot, envRoot+"-moved"); err != nil {
			t.Fatalf("move claimed dir aside: %v", err)
		}
		if err := os.Rename(replacement, envRoot); err != nil {
			t.Fatalf("install replacement: %v", err)
		}
	}
	t.Cleanup(func() { reuseBeforeUseTestHook = nil })

	second := leaderReuseTestTask("task-second")
	second.PriorSessionID = firstResult.SessionID
	second.PriorWorkDir = firstResult.WorkDir
	secondResult, err := d.runTask(context.Background(), second, "claude", 0, d.logger)
	if err != nil {
		t.Fatalf("second runTask: %v", err)
	}
	if !swapped {
		t.Fatal("the swap hook never ran; the test proved nothing")
	}
	// envRoot's NAME now resolves to the replacement, so a run that reused it
	// lands there. Compare the env root the second task actually ran in.
	if sameDir(t, filepath.Dir(secondResult.WorkDir), envRoot) {
		t.Fatal("ran in the substituted directory instead of declining to a fresh environment")
	}
	if secondResult.WorkDir == "" {
		t.Fatal("second task produced no environment at all")
	}
}
