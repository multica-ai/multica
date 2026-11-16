package repocache

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const migrationRepoURL = "http://tfs.example.internal:8080/tfs/engineering-collection/engineering-project/_git/enterprise-repository-client"
const migrationWorkspaceID = "11111111-1111-1111-1111-111111111111"

func seedLegacyEnterpriseCache(t *testing.T) (*Cache, string, string) {
	t.Helper()
	source := createTestRepo(t)
	root, err := os.MkdirTemp("", "mc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	workspacesRoot := filepath.Join(root, strings.Repeat("w", max(1, 65-len(root)-1)))
	cache := New(filepath.Join(workspacesRoot, ".repos"), testLogger())
	legacyPath := filepath.Join(cache.root, migrationWorkspaceID, legacyBareDirName(migrationRepoURL))
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	// The cache's real origin stays local; its lookup key has the reported
	// enterprise URL shape and 28-character repository basename.
	if err := gitCloneBareContext(context.Background(), source, legacyPath); err != nil {
		t.Fatal(err)
	}
	return cache, legacyPath, filepath.Join(root, "workdir")
}

func TestMigrateUnusedLegacyCache(t *testing.T) {
	t.Parallel()
	for _, onSync := range []bool{false, true} {
		name := "checkout"
		if onSync {
			name = "sync"
		}
		t.Run(name, func(t *testing.T) {
			cache, legacyPath, workDir := seedLegacyEnterpriseCache(t)
			baseRef := getRemoteDefaultBranch(legacyPath)
			commit := gitRefCommit(t, legacyPath, baseRef)
			if err := runGit("-C", legacyPath, "branch", "retained", baseRef); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(legacyPath, "retained.txt"), []byte("keep cache contents"), 0o644); err != nil {
				t.Fatal(err)
			}
			// A failed checkout may have left a recent, incomplete registration.
			stale := filepath.Join(legacyPath, "worktrees", "stale")
			if err := os.MkdirAll(stale, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(stale, "gitdir"), []byte(filepath.Join(workDir, "missing", ".git")+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			shortPath := filepath.Join(cache.root, migrationWorkspaceID, bareDirName(migrationRepoURL))
			if onSync {
				if err := cache.Sync(migrationWorkspaceID, []RepoInfo{{URL: migrationRepoURL}}); err != nil {
					t.Fatal(err)
				}
				if got := cache.Lookup(migrationWorkspaceID, migrationRepoURL); got != shortPath {
					t.Fatalf("sync left the cache at %s, want %s", got, shortPath)
				}
			}
			result, err := cache.CreateWorktree(WorktreeParams{
				WorkspaceID: migrationWorkspaceID,
				RepoURL:     migrationRepoURL,
				WorkDir:     workDir,
				AgentName:   "test",
				TaskID:      "12345678-0000-0000-0000-000000000000",
			})
			if err != nil {
				t.Fatalf("checkout from legacy cache: %v", err)
			}
			if got := cache.Lookup(migrationWorkspaceID, migrationRepoURL); got != shortPath {
				t.Fatalf("checkout left the cache at %s, want %s", got, shortPath)
			}
			if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
				t.Fatalf("legacy directory survived migration: %v", err)
			}
			if data, err := os.ReadFile(filepath.Join(shortPath, "retained.txt")); err != nil || string(data) != "keep cache contents" {
				t.Fatalf("migration lost cache contents: %q, %v", data, err)
			}
			if got := gitRefCommit(t, shortPath, "refs/heads/retained"); got != commit {
				t.Fatalf("migration lost a local branch: %s, want %s", got, commit)
			}
			if got := gitHead(t, result.Path); got != commit {
				t.Fatalf("checkout has %s, want %s", got, commit)
			}
			admin := worktreeRegistration(t, result.Path)
			if !samePath(filepath.Dir(admin), filepath.Join(shortPath, "worktrees")) {
				t.Fatalf("checkout still points at the legacy cache: %s", admin)
			}
			if len(filepath.Join(admin, "index")) >= 260 {
				t.Fatalf("migrated checkout exceeds MAX_PATH: %s", admin)
			}
		})
	}
}

func TestCacheWaitersResolvePathAfterMigration(t *testing.T) {
	t.Parallel()
	source := createTestRepo(t)
	cache := New(t.TempDir(), testLogger())
	legacyPath := filepath.Join(cache.root, "ws", legacyBareDirName(source))
	shortPath := filepath.Join(cache.root, "ws", bareDirName(source))
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := gitCloneBareContext(context.Background(), source, legacyPath); err != nil {
		t.Fatal(err)
	}
	workDir := t.TempDir()
	lock := cache.lockForRepo(legacyPath)
	lock.Lock()
	locked := true
	defer func() {
		if locked {
			lock.Unlock()
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := make(chan error, 2)
	go func() { done <- cache.SyncContext(ctx, "ws", []RepoInfo{{URL: source}}) }()
	go func() {
		_, err := cache.CreateWorktreeContext(ctx, WorktreeParams{
			WorkspaceID: "ws",
			RepoURL:     source,
			WorkDir:     workDir,
			AgentName:   "test",
			TaskID:      "12345678-0000-0000-0000-000000000000",
		})
		done <- err
	}()
	for cache.Activity().ForegroundWaiters < 2 {
		select {
		case <-ctx.Done():
			t.Fatal("operations did not queue behind the cache lock")
		case <-time.After(time.Millisecond):
		}
	}
	// Model another holder completing the rename while these callers wait.
	if err := os.Rename(legacyPath, shortPath); err != nil {
		t.Fatal(err)
	}
	lock.Unlock()
	locked = false
	for range 2 {
		if err := <-done; err != nil {
			t.Fatalf("operation used a stale cache path: %v", err)
		}
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("waiting Sync recreated the old cache: %v", err)
	}
}

func TestMigrationDoesNotOverwriteExistingDestination(t *testing.T) {
	t.Parallel()
	cache, legacyPath, _ := seedLegacyEnterpriseCache(t)
	shortPath := filepath.Join(cache.root, migrationWorkspaceID, bareDirName(migrationRepoURL))
	if err := os.MkdirAll(shortPath, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(shortPath, "keep.txt")
	if err := os.WriteFile(marker, []byte("existing destination"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cache.Sync(migrationWorkspaceID, []RepoInfo{{URL: migrationRepoURL}}); err != nil {
		t.Fatal(err)
	}
	if got := cache.Lookup(migrationWorkspaceID, migrationRepoURL); got != legacyPath {
		t.Fatalf("existing destination replaced legacy cache: %s", got)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "existing destination" {
		t.Fatalf("existing destination changed: %q, %v", data, err)
	}
}

func TestMigrationKeepsLockedWorktree(t *testing.T) {
	t.Parallel()
	source := createTestRepo(t)
	root, err := os.MkdirTemp("", "mc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	cache := New(filepath.Join(root, "repos"), testLogger())
	legacyPath := filepath.Join(cache.root, "ws", legacyBareDirName(source))
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := gitCloneBareContext(context.Background(), source, legacyPath); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "checkout")
	if _, err := createWorktreeContext(context.Background(), legacyPath, path, "agent/test/locked", getRemoteDefaultBranch(legacyPath)); err != nil {
		t.Fatal(err)
	}
	if err := runGit("-C", legacyPath, "worktree", "lock", path); err != nil {
		t.Fatal(err)
	}
	// Git's lock protects a checkout on an offline/removable volume as well
	// as a present one. Pruning must retain it and prevent cache migration.
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if err := cache.Sync("ws", []RepoInfo{{URL: source}}); err != nil {
		t.Fatal(err)
	}
	if got := cache.Lookup("ws", source); got != legacyPath {
		t.Fatalf("locked worktree lost its cache: %s", got)
	}
	if _, err := os.Stat(filepath.Join(cache.root, "ws", bareDirName(source))); !os.IsNotExist(err) {
		t.Fatalf("locked worktree allowed migration: %v", err)
	}
}
