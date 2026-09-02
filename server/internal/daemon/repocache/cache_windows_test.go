//go:build windows

package repocache

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// The isolated checkout exists so a Codex task can commit inside its own
// workdir (multica-ai/multica#2925 on Linux, #6449 on Windows). The rest of
// the suite proves the shape of that checkout on whatever platform CI runs,
// but two of its guarantees are claims about Windows itself: that Git really
// puts the gitdir inside the task directory there, and that --no-hardlinks
// really yields private object files on NTFS — where a hard link would
// otherwise share one file and one security descriptor with the daemon-owned
// cache. Neither can be proven on the ubuntu backend job, so they live here
// and run in the windows-execenv CI job.

// TestIsolatedCheckoutIsCommittableOnWindows walks the exact sequence a task
// performs: check out, branch, stage, commit — then verifies the shared cache
// was neither moved nor linked into.
func TestIsolatedCheckoutIsCommittableOnWindows(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	barePath := cache.Lookup("ws-1", sourceRepo)
	baseRef := getRemoteDefaultBranch(barePath)
	baseCommit := gitRefCommit(t, barePath, baseRef)

	// Exercise the production checkout under the longest readable env-root
	// segments. This needs a real Windows runner: Git creates the task branch as
	// a nested file below .git/refs/heads, where MAX_PATH applies to the complete
	// path and a too-generous readable prefix fails with "Filename too long".
	// Keep the base representative of the default user-level workspace root.
	// t.TempDir embeds the full test name; nesting the production layout under
	// it would spend that synthetic length twice and test the fixture, not the
	// readable-path budget. Keep the short base, but derive the task suffix from
	// this test's unique source path so concurrent go test processes cannot wipe
	// each other's checkout while resetting the fixture.
	workspacesRoot := filepath.Join(os.TempDir(), "mw")
	taskSuffixHash := sha256.Sum256([]byte(sourceRepo))
	taskID := "a1b2c3d4-e5f6-7890-abcd-" + hex.EncodeToString(taskSuffixHash[:6])
	envRoot := execenv.PredictRootDir(execenv.RootDirParams{
		WorkspacesRoot:  workspacesRoot,
		WorkspaceID:     "a05b0e10-ee7a-4603-a72d-a548b2390cb2",
		WorkspaceSlug:   strings.Repeat("workspace", 20),
		TaskID:          taskID,
		IssueIdentifier: strings.Repeat("issue", 20),
	})
	if err := os.RemoveAll(envRoot); err != nil {
		t.Fatalf("clear worst-case readable env root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(envRoot) })
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("create worst-case readable workdir: %v", err)
	}

	result, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID:         "ws-1",
		RepoURL:             sourceRepo,
		WorkDir:             workDir,
		AgentName:           "Windows Codex",
		TaskID:              taskID,
		IsolatedGitMetadata: true,
	})
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	// 1. The gitdir a sandbox would have to make writable must be the one
	// inside the task workdir, not the shared cache outside it.
	gitDir := absoluteGitDir(t, result.Path)
	if !isUnder(gitDir, result.Path) {
		t.Fatalf("git dir %s is outside the task checkout %s", gitDir, result.Path)
	}
	taskRef := filepath.Join(gitDir, "refs", "heads", filepath.FromSlash(result.BranchName))
	if _, err := os.Stat(taskRef); err != nil {
		t.Fatalf("task branch ref was not created at %s: %v", taskRef, err)
	}

	// 2. Branch, stage, commit — the sequence that fails on a linked worktree.
	runGitAuthored(t, result.Path, "checkout", "-b", "feature/windows-commit")
	if err := os.WriteFile(filepath.Join(result.Path, "windows.txt"), []byte("committable\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGitAuthored(t, result.Path, "add", "windows.txt")
	runGitAuthored(t, result.Path, "commit", "-m", "windows isolated commit")
	if got := gitHead(t, result.Path); got == baseCommit {
		t.Fatal("commit did not advance the checkout")
	}

	// 3. The shared cache must be untouched by all of the above.
	if got := gitRefCommit(t, barePath, baseRef); got != baseCommit {
		t.Fatalf("commit mutated shared cache base: got %s, want %s", got, baseCommit)
	}
	assertObjectsAreNotHardLinked(t, barePath, result.Path)
}

// TestIsolatedCheckoutAcrossVolumesOnWindows covers the cross-drive case an
// NTFS hard link cannot express at all: hard links are confined to a single
// volume, so a cache on C: and a workdir on D: is precisely where linking
// would fail. Skips visibly when the runner has only one writable volume.
func TestIsolatedCheckoutAcrossVolumesOnWindows(t *testing.T) {
	t.Parallel()
	cacheRoot := t.TempDir()
	workDir := tempDirOnOtherVolume(t, cacheRoot)

	sourceRepo := createTestRepo(t)
	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	barePath := cache.Lookup("ws-1", sourceRepo)
	baseCommit := gitRefCommit(t, barePath, getRemoteDefaultBranch(barePath))

	result, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID:         "ws-1",
		RepoURL:             sourceRepo,
		WorkDir:             workDir,
		AgentName:           "Windows Codex",
		TaskID:              "b2c3d4e5-f6a7-8901-bcde-f12345678901",
		IsolatedGitMetadata: true,
	})
	if err != nil {
		t.Fatalf("cross-volume CreateWorktree failed: %v", err)
	}
	if gitDir := absoluteGitDir(t, result.Path); !isUnder(gitDir, result.Path) {
		t.Fatalf("git dir %s is outside the task checkout %s", gitDir, result.Path)
	}
	runGitAuthored(t, result.Path, "commit", "--allow-empty", "-m", "cross-volume commit")
	if got := gitHead(t, result.Path); got == baseCommit {
		t.Fatal("commit did not advance the cross-volume checkout")
	}
}

func absoluteGitDir(t *testing.T, repoPath string) string {
	t.Helper()
	out, err := runGitOutput("-C", repoPath, "rev-parse", "--absolute-git-dir")
	if err != nil {
		t.Fatalf("rev-parse --absolute-git-dir in %s: %v", repoPath, err)
	}
	return strings.TrimSpace(string(out))
}

// isUnder reports whether path lies inside dir, comparing the same
// absolute/symlink-free form the daemon's own path checks use.
func isUnder(path, dir string) bool {
	rel, err := filepath.Rel(resolvePath(dir), resolvePath(path))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func resolvePath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return filepath.Clean(path)
}

// tempDirOnOtherVolume returns a writable temp directory on a volume different
// from other's, skipping the test when the runner exposes only one.
func tempDirOnOtherVolume(t *testing.T, other string) string {
	t.Helper()
	skipVolume := strings.ToUpper(filepath.VolumeName(resolvePath(other)))
	for _, letter := range "DEFGHIJKLMNOPQRSTUVWXYZC" {
		volume := string(letter) + ":"
		if strings.ToUpper(volume) == skipVolume {
			continue
		}
		dir, err := os.MkdirTemp(volume+`\`, "multica-repocache-")
		if err != nil {
			continue
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		return dir
	}
	t.Skipf("no writable volume other than %s on this runner", skipVolume)
	return ""
}
