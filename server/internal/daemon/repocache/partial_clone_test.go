package repocache

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// createFilterableTestRepo builds a source repository that can actually serve
// partial clones, and returns the URL to clone it from.
//
// Two details matter. First, `git clone --filter` is silently ignored for a
// plain filesystem path — git takes the local hardlink shortcut instead of
// speaking the pack protocol — so the URL has to be file://. Second, the
// server side must opt into filtering via uploadpack.allowFilter, exactly as
// GitHub and GitLab do.
//
// The commits carry real file contents, because a blobless clone only differs
// from a full one in whether those blobs were transferred.
func createFilterableTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git setup failed: %s: %v", out, err)
		}
	}
	run("init", dir)
	run("-C", dir, "config", "uploadpack.allowFilter", "true")
	for _, name := range partialCloneTestFiles {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("contents of "+name+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		run("-C", dir, "add", name)
		run("-C", dir, "commit", "-m", "add "+name)
	}
	return "file://" + dir
}

var partialCloneTestFiles = []string{"a.txt", "b.txt"}

// seedBloblessCache creates the workspace's bare cache for sourceRepo as a
// blobless partial clone, using the same two steps Cache.Sync performs for a
// cold miss. Returns the bare repo path.
func seedBloblessCache(t *testing.T, cache *Cache, workspaceID, sourceRepo string) string {
	t.Helper()
	barePath := filepath.Join(cache.root, workspaceID, bareDirName(sourceRepo))
	if err := os.MkdirAll(filepath.Dir(barePath), 0o755); err != nil {
		t.Fatalf("create workspace cache dir: %v", err)
	}
	if out, err := runGitCombinedOutput("clone", "--bare", "--filter=blob:none", sourceRepo, barePath); err != nil {
		t.Fatalf("seed blobless cache: %s: %v", out, err)
	}
	if err := ensureRemoteTrackingLayout(barePath); err != nil {
		t.Fatalf("ensure refspec: %v", err)
	}
	if !isPartialClone(barePath) {
		t.Fatal("seeded cache is not a partial clone; the filter was ignored")
	}
	return barePath
}

// assertCheckoutIsComplete fails if the working tree is missing any tracked
// file. `git status --porcelain` reports missing blobs as deletions, which is
// how a silently-broken partial checkout shows up.
func assertCheckoutIsComplete(t *testing.T, checkoutPath string) {
	t.Helper()
	out, err := runGitOutput("-C", checkoutPath, "status", "--porcelain")
	if err != nil {
		t.Fatalf("git status: %v", err)
	}
	if status := strings.TrimSpace(string(out)); status != "" {
		t.Fatalf("checkout is not clean, tracked files are missing:\n%s", status)
	}
	for _, name := range partialCloneTestFiles {
		data, err := os.ReadFile(filepath.Join(checkoutPath, name))
		if err != nil {
			t.Fatalf("read %s from checkout: %v", name, err)
		}
		if got, want := string(data), "contents of "+name+"\n"; got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
}

// The isolated-checkout path (Linux Codex) clones the cache with
// `git clone --local`. That command neither inherits a promisor remote's
// configuration nor refuses to run against an incomplete object store: it
// exits 0 and leaves every tracked file missing from the working tree. Without
// the promisor config restored on the new checkout, an agent starts work in
// what looks like a repository where someone deleted all the files.
func TestCreateIsolatedCheckoutFromPartialCacheHasFileContents(t *testing.T) {
	t.Parallel()
	sourceRepo := createFilterableTestRepo(t)
	cache := New(t.TempDir(), testLogger())
	seedBloblessCache(t, cache, "ws-1", sourceRepo)

	result, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID:         "ws-1",
		RepoURL:             sourceRepo,
		WorkDir:             t.TempDir(),
		AgentName:           "Linux Codex",
		TaskID:              "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		IsolatedGitMetadata: true,
	})
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}
	assertCheckoutIsComplete(t, result.Path)

	// The checkout must keep resolving blobs on its own once the cache is out
	// of the picture, so origin stays the real remote and carries the
	// promisor marker.
	origin, err := runGitOutput("-C", result.Path, "remote", "get-url", "origin")
	if err != nil {
		t.Fatalf("get origin URL: %v", err)
	}
	if got := strings.TrimSpace(string(origin)); got != sourceRepo {
		t.Fatalf("origin URL = %q, want %q", got, sourceRepo)
	}
	if !isPartialClone(result.Path) {
		t.Fatal("isolated checkout of a partial cache must be marked as a partial clone")
	}
}

// A workdir created while the cache was still complete, then reused after the
// cache became partial, must be repaired on reuse rather than left
// half-populated.
func TestReusedIsolatedCheckoutRepairsPromisorConfig(t *testing.T) {
	t.Parallel()
	sourceRepo := createFilterableTestRepo(t)
	cache := New(t.TempDir(), testLogger())
	seedBloblessCache(t, cache, "ws-1", sourceRepo)

	params := WorktreeParams{
		WorkspaceID:         "ws-1",
		RepoURL:             sourceRepo,
		WorkDir:             t.TempDir(),
		AgentName:           "Linux Codex",
		TaskID:              "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		IsolatedGitMetadata: true,
	}
	first, err := cache.CreateWorktree(params)
	if err != nil {
		t.Fatalf("first CreateWorktree failed: %v", err)
	}
	if err := runGit("-C", first.Path, "config", "--unset", "remote.origin.promisor"); err != nil {
		t.Fatalf("unset promisor: %v", err)
	}

	second, err := cache.CreateWorktree(params)
	if err != nil {
		t.Fatalf("second CreateWorktree failed: %v", err)
	}
	if !isPartialClone(second.Path) {
		t.Fatal("reused isolated checkout must have its promisor config restored")
	}
	assertCheckoutIsComplete(t, second.Path)
}

// The linked-worktree path used by every other runtime shares the cache's own
// object store and config, so it lazily fetches without any extra wiring. This
// pins that difference so the promisor handling is not "fixed" by pushing it
// into the shared cache later.
func TestCreateWorktreeFromPartialCacheHasFileContents(t *testing.T) {
	t.Parallel()
	sourceRepo := createFilterableTestRepo(t)
	cache := New(t.TempDir(), testLogger())
	seedBloblessCache(t, cache, "ws-1", sourceRepo)

	result, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     t.TempDir(),
		AgentName:   "tester",
		TaskID:      "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
	})
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}
	assertCheckoutIsComplete(t, result.Path)
}

func TestIsPartialClone(t *testing.T) {
	t.Parallel()
	sourceRepo := createFilterableTestRepo(t)
	cache := New(t.TempDir(), testLogger())

	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	if isPartialClone(cache.Lookup("ws-1", sourceRepo)) {
		t.Fatal("an ordinary full cache must not be reported as a partial clone")
	}

	blobless := seedBloblessCache(t, New(t.TempDir(), testLogger()), "ws-2", sourceRepo)
	if !isPartialClone(blobless) {
		t.Fatal("a blobless cache must be reported as a partial clone")
	}
	if isPartialClone(filepath.Join(t.TempDir(), "does-not-exist")) {
		t.Fatal("a missing repository must not be reported as a partial clone")
	}
}
