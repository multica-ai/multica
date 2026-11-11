package execenv

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
)

func TestOnDemandHelperPreservesFinalizationStateAndExistingWorktree(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	params := LocalWorktreeParams{LocalPath: repo, EnvRoot: t.TempDir(), TaskID: "on-demand-task",
		AgentName: "agent", ConversationKey: "MUL-8038", WorkspaceID: "workspace", AgentID: "agent", ConversationID: "issue", PreserveExisting: true}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	wt, err := PrepareLocalWorktreeIsolated(ctx, preparationHelperTestCommand(), params, worktreeTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	if wt.SnapshotAt.IsZero() || wt.owner != params.owner() || !wt.tracksState {
		t.Fatalf("helper lost state: %+v", wt)
	}
	writeFile(t, filepath.Join(wt.WorkDir, "precious.txt"), "uncommitted work")
	if _, err := PrepareLocalWorktree(params, worktreeTestLogger()); err == nil || !strings.Contains(err.Error(), "preserving") {
		t.Fatalf("existing checkout was replaced: %v", err)
	}
	if got := readFile(t, filepath.Join(wt.WorkDir, "precious.txt")); got != "uncommitted work" {
		t.Fatal("previous edits lost")
	}
	wt.AbortWithReason(context.Canceled)
	if outcome, err := wt.Finalize(worktreeTestLogger()); err == nil || outcome.PreservedPath == "" {
		t.Fatalf("aborted worktree was not preserved: %+v, %v", outcome, err)
	}
	// Remove the abort only for this test's cleanup and successful delivery.
	wt.aborted = nil
	finalizeOK(t, wt)
	if HasLocalWorktree(params.EnvRoot) {
		t.Fatal("finalized worktree remains")
	}
	branches := LocalWorktreeBranches(repo, params.WorkspaceID, params.AgentID, params.ConversationID)
	if !slices.Contains(branches, wt.Branch) {
		t.Fatalf("missing recorded branch: %v", branches)
	}
	if branches := LocalWorktreeBranches(repo, "other-workspace", params.AgentID, params.ConversationID); len(branches) != 0 {
		t.Fatalf("other workspace's branches exposed: %v", branches)
	}
}

func TestLocalWorktreeWritesKeepSandboxAndUserDirectoryBoundary(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	home, envRoot := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(home, "config.toml"), "sandbox_mode = 'workspace-write'\n[sandbox_workspace_write]\nwritable_roots = ['/already-allowed']\n")
	if err := AllowLocalWorktreeWrites(home, envRoot, repo); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		SandboxMode string `toml:"sandbox_mode"`
		Workspace   struct {
			Roots []string `toml:"writable_roots"`
		} `toml:"sandbox_workspace_write"`
	}
	if err := toml.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config.SandboxMode != "workspace-write" || !slices.Contains(config.Workspace.Roots, "/already-allowed") {
		t.Fatalf("sandbox settings changed: %+v", config)
	}
	common, _ := gitCommonDirFor(repo)
	for _, root := range []string{common, envRoot} {
		root, _ = filepath.EvalSymlinks(root)
		if !slices.Contains(config.Workspace.Roots, root) {
			t.Fatalf("missing writable root %s: %v", root, config.Workspace.Roots)
		}
	}
	if slices.Contains(config.Workspace.Roots, repo) {
		t.Fatal("user's working directory was made writable")
	}
}
