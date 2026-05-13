package daemon

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func TestLooksLikePerTaskWorktree(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"per-task happy", "agent-1-a1b2c3d4", true},
		{"per-task uppercase hex", "agent-1-A1B2C3D4", true},
		{"per-task multi-dash agent", "code-reviewer-12345678", true},
		{"legacy bare agent", "agent-1", false},
		{"too-short suffix", "agent-1-abc", false},
		{"non-hex suffix", "agent-1-zzzzzzzz", false},
		{"no dash", "agent1", false},
		{"empty agent prefix", "-12345678", false},
		{"trailing slash garbage", "agent-1-12345678/", false}, // dir scan won't pass slash, but be defensive
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := looksLikePerTaskWorktree(c.in)
			if got != c.want {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

// TestSweepPerTaskWorktrees_OrphansAndGracePeriod covers the three classes of
// entries the sweeper distinguishes: active (skip), in-grace (skip), orphan
// (remove). Uses t.TempDir() for both WorktreesRoot and WorkspacesRoot so we
// can plant fixtures with explicit mtimes and GCMeta.
func TestSweepPerTaskWorktrees_OrphansAndGracePeriod(t *testing.T) {
	t.Parallel()
	workspacesRoot := t.TempDir()
	worktreesRoot := t.TempDir()

	// Plant fixtures:
	//   1. agent-1-aaaaaaaa — orphan, old → should be removed
	//   2. agent-1-bbbbbbbb — orphan, fresh (in grace) → should remain
	//   3. agent-1-cccccccc — active (has matching GCMeta.WtPath) → should remain
	//   4. agent-2          — legacy, no per-task suffix → should remain (ignored)
	//   5. random-file       — not a directory → should remain (ignored)
	old := filepath.Join(worktreesRoot, "agent-1-aaaaaaaa")
	fresh := filepath.Join(worktreesRoot, "agent-1-bbbbbbbb")
	active := filepath.Join(worktreesRoot, "agent-1-cccccccc")
	legacy := filepath.Join(worktreesRoot, "agent-2")
	for _, p := range []string{old, fresh, active, legacy} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(worktreesRoot, "stray-file"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Backdate the orphan to before the grace period; leave fresh + active
	// at "now". MkdirAll's resulting mtime is current, so explicitly age the
	// orphan.
	farPast := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(old, farPast, farPast); err != nil {
		t.Fatal(err)
	}

	// Plant an envRoot with GCMeta pointing at the "active" worktree so the
	// sweeper finds the live reference. Workspace dir structure mirrors prod:
	// <workspacesRoot>/<workspaceID>/<task_short>/.gc_meta.json
	wsDir := filepath.Join(workspacesRoot, "ws-001")
	taskDir := filepath.Join(wsDir, "cccccccc")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := execenv.GCMeta{
		IssueID:                 "issue-1",
		WorkspaceID:             "ws-001",
		WtPath:                  active,
		BarePath:                "/srv/multica-bare.git",
		FeatureFlagState:        true,
		TargetProjectResourceID: "pr-abc",
		TargetRepo:              "rabbeet/multica",
	}
	mb, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, ".gc_meta.json"), mb, 0o644); err != nil {
		t.Fatal(err)
	}

	d := &Daemon{
		logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
		cfg: Config{
			WorkspacesRoot: workspacesRoot,
			WorktreesRoot:  worktreesRoot,
		},
	}

	d.sweepPerTaskWorktrees()

	// Verify outcomes.
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("expected old orphan removed: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("expected fresh (in-grace) preserved: %v", err)
	}
	if _, err := os.Stat(active); err != nil {
		t.Errorf("expected active worktree preserved: %v", err)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Errorf("expected legacy 'agent-2' preserved: %v", err)
	}
}

func TestCollectActiveWtPaths(t *testing.T) {
	t.Parallel()
	workspacesRoot := t.TempDir()

	// Plant two envRoots — one with WtPath, one legacy without.
	plantEnvRoot := func(ws, task string, meta execenv.GCMeta) {
		dir := filepath.Join(workspacesRoot, ws, task)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(meta)
		if err := os.WriteFile(filepath.Join(dir, ".gc_meta.json"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	plantEnvRoot("ws-001", "abcdef00", execenv.GCMeta{
		IssueID: "issue-1", WorkspaceID: "ws-001",
		WtPath: "/srv/agent-worktrees/agent-1-abcdef00",
	})
	plantEnvRoot("ws-001", "11112222", execenv.GCMeta{
		IssueID: "issue-2", WorkspaceID: "ws-001",
		// no WtPath — legacy
	})

	// Add a .repos dir to verify it's skipped (hidden-prefix scan).
	if err := os.MkdirAll(filepath.Join(workspacesRoot, ".repos"), 0o755); err != nil {
		t.Fatal(err)
	}

	d := &Daemon{
		logger: slog.Default(),
		cfg:    Config{WorkspacesRoot: workspacesRoot},
	}

	active := d.collectActiveWtPaths()
	if _, ok := active["/srv/agent-worktrees/agent-1-abcdef00"]; !ok {
		t.Errorf("expected wt with WtPath to be in active set, got: %v", active)
	}
	if len(active) != 1 {
		t.Errorf("expected 1 active wt, got %d: %v", len(active), active)
	}
}

func TestCollectActiveWtPaths_MissingWorkspacesRoot(t *testing.T) {
	t.Parallel()
	d := &Daemon{logger: slog.Default(), cfg: Config{WorkspacesRoot: ""}}
	got := d.collectActiveWtPaths()
	if len(got) != 0 {
		t.Fatalf("expected empty map for unset WorkspacesRoot, got %v", got)
	}
}

// Ensure the sweeper doesn't panic on a missing worktrees root (e.g. feature
// configured but admin removed the dir).
func TestSweepPerTaskWorktrees_MissingRoot(t *testing.T) {
	t.Parallel()
	d := &Daemon{
		logger: slog.Default(),
		cfg:    Config{WorktreesRoot: filepath.Join(t.TempDir(), "does-not-exist")},
	}
	// Should not panic, should be a no-op.
	d.sweepPerTaskWorktrees()
}

// Documents the substring patterns we treat as auth markers, so changes to
// that list show up as test diffs.
func TestClassifyFetchError_AuthMarkerCoverage(t *testing.T) {
	t.Parallel()
	markers := []string{
		"authentication failed",
		"could not read username",
		"could not read password",
		"401",
		"403",
		"invalid credentials",
	}
	for _, m := range markers {
		m := m
		t.Run(strings.ReplaceAll(m, " ", "_"), func(t *testing.T) {
			t.Parallel()
			err := classifyFetchError(&stringErr{msg: "fatal: " + m + " (origin)"})
			if !mustWrapErrFetchAuth(err) {
				t.Fatalf("marker %q did not classify as auth (got %v)", m, err)
			}
		})
	}
}

type stringErr struct{ msg string }

func (e *stringErr) Error() string { return e.msg }

func mustWrapErrFetchAuth(err error) bool {
	if err == nil {
		return false
	}
	for e := err; e != nil; {
		if e == ErrFetchAuth {
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}
