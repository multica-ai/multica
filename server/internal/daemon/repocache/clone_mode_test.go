package repocache

import (
	"strings"
	"testing"
)

// createFilterableTestRepo, assertCheckoutIsComplete and the partial-clone
// checkout regression tests live in partial_clone_test.go; this file covers
// only what the clone-mode setting itself adds.

// missingObjectCount reports how many objects the repository knows about but
// does not have locally. It is non-zero exactly for a partial clone that has
// not backfilled yet.
func missingObjectCount(t *testing.T, repoPath string) int {
	t.Helper()
	out, err := runGitOutput("-C", repoPath, "rev-list", "--objects", "--all", "--missing=print")
	if err != nil {
		t.Fatalf("rev-list --missing: %v", err)
	}
	count := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "?") {
			count++
		}
	}
	return count
}

func TestNormalizeCloneMode(t *testing.T) {
	t.Parallel()
	cases := map[string]CloneMode{
		"":            CloneModeFull,
		"full":        CloneModeFull,
		"on-demand":   CloneModeOnDemand,
		"ON-DEMAND":   CloneModeOnDemand,
		"  on-demand": CloneModeOnDemand,
		"depth":       CloneModeFull,
		"blob:none":   CloneModeFull,
	}
	for input, want := range cases {
		if got := NormalizeCloneMode(input); got != want {
			t.Errorf("NormalizeCloneMode(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSyncOnDemandCreatesPartialClone(t *testing.T) {
	t.Parallel()
	sourceRepo := createFilterableTestRepo(t)
	cache := New(t.TempDir(), testLogger())

	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo, CloneMode: CloneModeOnDemand}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	barePath := cache.Lookup("ws-1", sourceRepo)
	if barePath == "" {
		t.Fatal("expected repo to be cached")
	}
	if !isPartialClone(barePath) {
		t.Fatal("on-demand cache must be configured as a partial clone")
	}
	if missing := missingObjectCount(t, barePath); missing == 0 {
		t.Fatal("on-demand cache should not have downloaded historical blobs")
	}
	// The whole point of choosing blob:none over --depth: commit history stays
	// intact, so arbitrary refs and merge-bases keep resolving.
	out, err := runGitOutput("-C", barePath, "rev-list", "--count", "--all")
	if err != nil {
		t.Fatalf("rev-list --count: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "2" {
		t.Fatalf("on-demand cache commit count = %s, want 2 (full history must be preserved)", got)
	}
}

func TestSyncFullModeDownloadsEverything(t *testing.T) {
	t.Parallel()
	sourceRepo := createFilterableTestRepo(t)
	cache := New(t.TempDir(), testLogger())

	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo, CloneMode: CloneModeFull}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	barePath := cache.Lookup("ws-1", sourceRepo)
	if isPartialClone(barePath) {
		t.Fatal("full cache must not be a partial clone")
	}
	if missing := missingObjectCount(t, barePath); missing != 0 {
		t.Fatalf("full cache should have every object, %d missing", missing)
	}
}

// An empty clone mode is what every repo registered before this feature
// serializes to; it must keep behaving exactly as it did before.
func TestSyncDefaultsToFullClone(t *testing.T) {
	t.Parallel()
	sourceRepo := createFilterableTestRepo(t)
	cache := New(t.TempDir(), testLogger())

	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	if isPartialClone(cache.Lookup("ws-1", sourceRepo)) {
		t.Fatal("a repo with no clone mode must be cloned in full")
	}
}

// The bare cache is shared by every task in the workspace, so a clone-mode
// change must never silently discard or re-download an existing cache.
func TestSyncKeepsExistingCacheWhenCloneModeChanges(t *testing.T) {
	t.Parallel()
	sourceRepo := createFilterableTestRepo(t)
	cache := New(t.TempDir(), testLogger())

	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo, CloneMode: CloneModeFull}}); err != nil {
		t.Fatalf("first sync failed: %v", err)
	}
	barePath := cache.Lookup("ws-1", sourceRepo)

	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo, CloneMode: CloneModeOnDemand}}); err != nil {
		t.Fatalf("second sync failed: %v", err)
	}
	if isPartialClone(barePath) {
		t.Fatal("an existing full cache must stay full; flipping the registry must not rebuild it")
	}
	if missing := missingObjectCount(t, barePath); missing != 0 {
		t.Fatalf("existing full cache lost objects, %d missing", missing)
	}
}

// Fetches on a partial cache must keep the filter. If they didn't, the very
// first background sync would quietly download everything the mode exists to
// avoid.
func TestFetchPreservesPartialClone(t *testing.T) {
	t.Parallel()
	sourceRepo := createFilterableTestRepo(t)
	cache := New(t.TempDir(), testLogger())

	repos := []RepoInfo{{URL: sourceRepo, CloneMode: CloneModeOnDemand}}
	if err := cache.Sync("ws-1", repos); err != nil {
		t.Fatalf("first sync failed: %v", err)
	}
	barePath := cache.Lookup("ws-1", sourceRepo)
	before := missingObjectCount(t, barePath)

	if err := cache.Sync("ws-1", repos); err != nil {
		t.Fatalf("second sync failed: %v", err)
	}
	if !isPartialClone(barePath) {
		t.Fatal("fetch must not convert the cache back to a full clone")
	}
	if after := missingObjectCount(t, barePath); after < before {
		t.Fatalf("fetch backfilled blobs: missing went %d -> %d", before, after)
	}
}

// End-to-end: a cache built by Sync in on-demand mode must produce complete
// checkouts on both paths. The regression tests in partial_clone_test.go seed
// a blobless cache by hand; this one proves the clone-mode wiring reaches the
// same place.
func TestOnDemandSyncedCacheProducesCompleteCheckouts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		isolated bool
	}{
		{name: "linked worktree", isolated: false},
		{name: "isolated git metadata", isolated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sourceRepo := createFilterableTestRepo(t)
			cache := New(t.TempDir(), testLogger())
			if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo, CloneMode: CloneModeOnDemand}}); err != nil {
				t.Fatalf("sync failed: %v", err)
			}

			result, err := cache.CreateWorktree(WorktreeParams{
				WorkspaceID:         "ws-1",
				RepoURL:             sourceRepo,
				WorkDir:             t.TempDir(),
				AgentName:           "tester",
				TaskID:              "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
				IsolatedGitMetadata: tc.isolated,
			})
			if err != nil {
				t.Fatalf("CreateWorktree failed: %v", err)
			}
			assertCheckoutIsComplete(t, result.Path)
		})
	}
}
