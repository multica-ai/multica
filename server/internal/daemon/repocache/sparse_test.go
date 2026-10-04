package repocache

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func sparseTestRepo(t *testing.T) string {
	t.Helper()
	source := createTestRepo(t)
	for name, content := range map[string]string{
		".multica/sparse-profile": "/*\n!/media/\n",
		"keep.txt":                "keep\n",
		"media/picture.png":       "large-media-placeholder\n",
	} {
		path := filepath.Join(source, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGitAuthored(t, source, "add", ".")
	runGitAuthored(t, source, "commit", "-m", "add sparse profile and media")
	return source
}

func sparseTestCache(t *testing.T) (*Cache, string) {
	t.Helper()
	source := sparseTestRepo(t)
	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: source}}); err != nil {
		t.Fatal(err)
	}
	return cache, source
}

func sparseTestParams(source, workDir, taskID string, isolated bool) WorktreeParams {
	return WorktreeParams{
		WorkspaceID: "ws-1", RepoURL: source, WorkDir: workDir,
		AgentName: "Agent", TaskID: taskID, IsolatedGitMetadata: isolated,
	}
}

func assertSparseTree(t *testing.T, path string, sparse bool) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(path, "keep.txt")); err != nil {
		t.Fatalf("source file missing: %v", err)
	}
	_, mediaErr := os.Stat(filepath.Join(path, "media", "picture.png"))
	if sparse && !os.IsNotExist(mediaErr) {
		t.Fatalf("media must be excluded, err=%v", mediaErr)
	}
	if !sparse && mediaErr != nil {
		t.Fatalf("full checkout must contain media: %v", mediaErr)
	}
	if out, err := runGitOutput("-C", path, "status", "--porcelain"); err != nil || len(out) != 0 {
		t.Fatalf("checkout is dirty: %q, %v", out, err)
	}
}

func TestVersionedSparseCheckoutAndFullOptOut(t *testing.T) {
	for _, isolated := range []bool{false, true} {
		name := "linked"
		if isolated {
			name = "isolated"
		}
		t.Run(name, func(t *testing.T) {
			cache, source := sparseTestCache(t)
			params := sparseTestParams(source, t.TempDir(), "task-1", isolated)
			result, err := cache.CreateWorktree(params)
			if err != nil {
				t.Fatal(err)
			}
			assertSparseTree(t, result.Path, true)
			if out, err := runGitOutput("-C", result.Path, "sparse-checkout", "list"); err != nil || !strings.Contains(string(out), "!/media/") {
				t.Fatalf("sparse profile = %q, %v", out, err)
			}
			// A sparse checkout changes the working tree, not the commit tree.
			runGitAuthored(t, result.Path, "commit", "--allow-empty", "-m", "sparse commit")
			if out, err := runGitOutput("-C", result.Path, "ls-tree", "-r", "--name-only", "HEAD"); err != nil || !strings.Contains(string(out), "media/picture.png") {
				t.Fatalf("sparse commit lost excluded media: %q, %v", out, err)
			}
			// --full must widen even an existing checkout kept on this task branch.
			params.Full = true
			widened, err := cache.CreateWorktree(params)
			if err != nil {
				t.Fatal(err)
			}
			if !widened.SparseWidened {
				t.Fatal("--full did not report widening the sparse checkout")
			}
			assertSparseTree(t, result.Path, false)
			if out, err := runGitOutput("-C", result.Path, "sparse-checkout", "list"); err == nil {
				t.Fatalf("sparse checkout still enabled: %q", out)
			}
		})
	}
}

func TestFullCheckoutFromStartSkipsProfile(t *testing.T) {
	cache, source := sparseTestCache(t)
	params := sparseTestParams(source, t.TempDir(), "full-task", false)
	params.Full = true
	result, err := cache.CreateWorktree(params)
	if err != nil {
		t.Fatal(err)
	}
	assertSparseTree(t, result.Path, false)
	if result.SparseWidened {
		t.Fatal("new full checkout must not report widening an existing tree")
	}
	if out, err := runGitOutput("-C", result.Path, "sparse-checkout", "list"); err == nil {
		t.Fatalf("new --full checkout unexpectedly sparse: %q", out)
	}
}

func TestMissingSparseProfileKeepsFullCheckout(t *testing.T) {
	source := createTestRepo(t)
	path := filepath.Join(source, "keep.txt")
	if err := os.WriteFile(path, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitAuthored(t, source, "add", "keep.txt")
	runGitAuthored(t, source, "commit", "-m", "add source")
	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: source}}); err != nil {
		t.Fatal(err)
	}
	result, err := cache.CreateWorktree(sparseTestParams(source, t.TempDir(), "task-1", false))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(result.Path, "keep.txt")); err != nil {
		t.Fatal(err)
	}
	if out, err := runGitOutput("-C", result.Path, "sparse-checkout", "list"); err == nil {
		t.Fatalf("repository without profile unexpectedly sparse: %q", out)
	}
}

func TestOversizeSparseProfileFailsBeforeCreatingCheckout(t *testing.T) {
	source := sparseTestRepo(t)
	if err := os.WriteFile(filepath.Join(source, sparseProfilePath), []byte(strings.Repeat("x", maxSparseProfileBytes+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitAuthored(t, source, "add", sparseProfilePath)
	runGitAuthored(t, source, "commit", "-m", "oversize profile")
	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: source}}); err != nil {
		t.Fatal(err)
	}
	workDir := t.TempDir()
	if _, err := cache.CreateWorktree(sparseTestParams(source, workDir, "task-1", false)); err == nil || !strings.Contains(err.Error(), "at most") {
		t.Fatalf("oversize profile error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, repoNameFromURL(source))); !os.IsNotExist(err) {
		t.Fatalf("failed checkout left a worktree behind: %v", err)
	}
}

func TestFullCheckoutPreservesExistingLocalEdits(t *testing.T) {
	cache, source := sparseTestCache(t)
	params := sparseTestParams(source, t.TempDir(), "task-1", false)
	result, err := cache.CreateWorktree(params)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(result.Path, "keep.txt"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(result.Path, "local.txt"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	params.Full = true
	if _, err := cache.CreateWorktree(params); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"keep.txt": "edited\n", "local.txt": "untracked\n", "media/picture.png": "large-media-placeholder\n"} {
		got, err := os.ReadFile(filepath.Join(result.Path, name))
		if err != nil || string(got) != want {
			t.Fatalf("%s after --full = %q, %v; want %q", name, got, err, want)
		}
	}
}

func TestSparseCheckoutConcurrentWorktreeConfigIsolation(t *testing.T) {
	cache, source := sparseTestCache(t)
	barePath := cache.Lookup("ws-1", source)
	const count = 10
	results := make([]*WorktreeResult, count)
	errs := make([]error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = cache.CreateWorktree(sparseTestParams(source, t.TempDir(), "task-"+string(rune('a'+i)), false))
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("checkout %d: %v", i, err)
		}
		assertSparseTree(t, results[i].Path, true)
		out, err := runGitOutput("-C", results[i].Path, "config", "--worktree", "--bool", "core.sparseCheckout")
		if err != nil || strings.TrimSpace(string(out)) != "true" {
			t.Fatalf("checkout %d sparse config = %q, %v", i, out, err)
		}
	}
	if out, err := runGitOutput("-C", barePath, "config", "--local", "--get-regexp", "^core\\.sparse"); err == nil || len(out) > 0 {
		t.Fatalf("mirror must not contain sparse config: %q, %v", out, err)
	}
}

func TestSparseCheckoutReusePreservesSelectionUntilFresh(t *testing.T) {
	for _, isolated := range []bool{false, true} {
		name := "linked"
		if isolated {
			name = "isolated"
		}
		t.Run(name, func(t *testing.T) {
			cache, source := sparseTestCache(t)
			workDir := t.TempDir()
			params := sparseTestParams(source, workDir, "task-1", isolated)
			first, err := cache.CreateWorktree(params)
			if err != nil {
				t.Fatal(err)
			}
			assertSparseTree(t, first.Path, true)

			// A newer ref now includes media. Reusing a checkout must preserve
			// its current sparse selection, even when moving to another task branch.
			if err := os.WriteFile(filepath.Join(source, sparseProfilePath), []byte("/*\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			runGitAuthored(t, source, "add", sparseProfilePath)
			runGitAuthored(t, source, "commit", "-m", "widen profile")
			params.TaskID = "task-2"
			if _, err := cache.CreateWorktree(params); err != nil {
				t.Fatal(err)
			}
			assertSparseTree(t, first.Path, true)

			params.TaskID = "task-3"
			params.Fresh = true
			if _, err := cache.CreateWorktree(params); err != nil {
				t.Fatal(err)
			}
			assertSparseTree(t, first.Path, false)
			if out, err := runGitOutput("-C", first.Path, "sparse-checkout", "list"); err != nil || !strings.Contains(string(out), "/*") {
				t.Fatalf("fresh checkout did not apply new profile: %q, %v", out, err)
			}
		})
	}
}

func TestSparseCheckoutLinkedToIsolatedMigrationKeepsSelection(t *testing.T) {
	cache, source := sparseTestCache(t)
	workDir := t.TempDir()
	params := sparseTestParams(source, workDir, "task-1", false)
	first, err := cache.CreateWorktree(params)
	if err != nil {
		t.Fatal(err)
	}
	assertSparseTree(t, first.Path, true)
	params.TaskID = "task-2"
	params.IsolatedGitMetadata = true
	second, err := cache.CreateWorktree(params)
	if err != nil {
		t.Fatal(err)
	}
	if !isIsolatedCheckout(second.Path) {
		t.Fatal("checkout was not migrated to isolated metadata")
	}
	assertSparseTree(t, second.Path, true)
}
