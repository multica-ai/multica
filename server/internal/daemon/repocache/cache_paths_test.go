package repocache

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBareDirNameBounded(t *testing.T) {
	t.Parallel()
	for _, repoURL := range []string{
		"http://tfs.example.internal:8080/tfs/engineering-collection/engineering-project/_git/enterprise-repository-client",
		"https://example.com/" + strings.Repeat("namespace/", 30) + strings.Repeat("repository", 30),
		"https://example.com/" + strings.Repeat("仓库", 30),
	} {
		name := bareDirName(repoURL)
		if len(name) > 33 || !strings.HasSuffix(name, ".git") {
			t.Errorf("cache name %q must fit 33 bytes and end in .git", name)
		}
		for _, ch := range name {
			if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || strings.ContainsRune("-_.", ch)) {
				t.Errorf("cache name %q contains an unsafe character %q", name, ch)
				break
			}
		}
	}
}

func TestCheckoutReusesLegacyCache(t *testing.T) {
	t.Parallel()
	for _, isolated := range []bool{false, true} {
		name := "linked"
		if isolated {
			name = "isolated"
		}
		t.Run(name, func(t *testing.T) {
			source := createTestRepo(t)
			cache := New(t.TempDir(), testLogger())
			legacyPath := filepath.Join(cache.root, "ws", legacyBareDirName(source))
			if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := gitCloneBareContext(context.Background(), source, legacyPath); err != nil {
				t.Fatal(err)
			}
			params := WorktreeParams{
				WorkspaceID:         "ws",
				RepoURL:             source,
				WorkDir:             t.TempDir(),
				AgentName:           "test",
				TaskID:              "12345678-0000-0000-0000-000000000000",
				IsolatedGitMetadata: isolated,
			}
			result, err := cache.CreateWorktree(params)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(result.Path, "local.txt"), []byte("uncommitted work"), 0o644); err != nil {
				t.Fatal(err)
			}
			// Simulate a daemon restart after upgrading. Fetch and checkout must
			// keep the cache and the running task's work exactly where they are.
			cache = New(cache.root, testLogger())
			if err := cache.Sync("ws", []RepoInfo{{URL: source}}); err != nil {
				t.Fatal(err)
			}
			if got := cache.Lookup("ws", source); got != legacyPath {
				t.Fatalf("cache moved: got %s, want %s", got, legacyPath)
			}
			if _, err := os.Stat(filepath.Join(cache.root, "ws", bareDirName(source))); !os.IsNotExist(err) {
				t.Fatalf("upgrade created a duplicate cache: %v", err)
			}
			kept, err := cache.CreateWorktree(params)
			if err != nil {
				t.Fatalf("reuse legacy checkout: %v", err)
			}
			if kept.Path != result.Path || kept.BranchName != result.BranchName || kept.Kept == "" {
				t.Fatalf("legacy checkout was replaced: %+v", kept)
			}
			if data, err := os.ReadFile(filepath.Join(kept.Path, "local.txt")); err != nil || string(data) != "uncommitted work" {
				t.Fatalf("local work changed: %q, %v", data, err)
			}
		})
	}
}

func TestBareDirNameCanonicalURLForms(t *testing.T) {
	t.Parallel()
	for _, repoURL := range []string{
		"https://github.com/org/my-repo.git",
		"https://GITHUB.COM/org/my-repo/",
		"git@github.com:org/my-repo",
		"ssh://git@github.com/org/my-repo.git",
	} {
		if got, want := bareDirName(repoURL), "my-repo-b98ef226e6b8.git"; got != want {
			t.Errorf("bareDirName(%q) = %q, want %q", repoURL, got, want)
		}
	}
}

func TestCreateWorktreeHookFailureRollsBack(t *testing.T) {
	t.Parallel()
	source := createTestRepo(t)
	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws", []RepoInfo{{URL: source}}); err != nil {
		t.Fatal(err)
	}
	barePath := cache.Lookup("ws", source)
	hook := filepath.Join(barePath, "hooks", "post-checkout")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "checkout")
	const branch = "agent/test/failed-hook"
	baseRef := getRemoteDefaultBranch(barePath)
	if _, err := createWorktreeContext(context.Background(), barePath, path, branch, baseRef); err == nil {
		t.Fatal("checkout with a failing hook must fail")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("partial checkout survived hook failure: %v", err)
	}
	if gitRefExistsContext(context.Background(), barePath, "refs/heads/"+branch) {
		t.Fatal("new branch survived hook failure")
	}
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	if got, err := createWorktreeContext(context.Background(), barePath, path, branch, baseRef); err != nil || got != branch {
		t.Fatalf("retry after hook failure = %q, %v", got, err)
	}
}

func TestCreateWorktreeFailureDoesNotLeakBranch(t *testing.T) {
	t.Parallel()
	for _, collision := range []bool{false, true} {
		name := "new branch"
		if collision {
			name = "existing branch"
		}
		t.Run(name, func(t *testing.T) {
			source := createTestRepo(t)
			cache := New(t.TempDir(), testLogger())
			if err := cache.Sync("ws", []RepoInfo{{URL: source}}); err != nil {
				t.Fatal(err)
			}
			barePath := cache.Lookup("ws", source)
			baseRef := getRemoteDefaultBranch(barePath)
			const branch = "agent/test/failed-checkout"
			if collision {
				if err := runGit("-C", barePath, "branch", branch, baseRef); err != nil {
					t.Fatal(err)
				}
			}
			before, err := runGitOutput("-C", barePath, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads/")
			if err != nil {
				t.Fatal(err)
			}
			// Git creates the branch before trying to make the checkout directory.
			parent := filepath.Join(t.TempDir(), "blocked")
			if err := os.WriteFile(parent, []byte("keep me"), 0o644); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(parent, "checkout")
			if _, err := createWorktreeContext(context.Background(), barePath, path, branch, baseRef); err == nil {
				t.Fatal("checkout through a file must fail")
			}
			after, err := runGitOutput("-C", barePath, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads/")
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatalf("failed checkout changed branches:\nbefore: %s\nafter: %s", before, after)
			}
			if data, err := os.ReadFile(parent); err != nil || string(data) != "keep me" {
				t.Fatalf("failed checkout modified the blocking file: %q, %v", data, err)
			}
			if err := os.Remove(parent); err != nil {
				t.Fatal(err)
			}
			if _, err := createWorktreeContext(context.Background(), barePath, path, branch, baseRef); err != nil {
				t.Fatalf("retry failed: %v", err)
			}
		})
	}
}

func TestCheckoutLongRepoURL(t *testing.T) {
	clearGitConfigEnv(t)
	source := createTestRepo(t)
	const repoURL = "http://tfs.example.internal:8080/tfs/engineering-collection/engineering-project/_git/enterprise-repository-client"
	// Keep the remote local while exercising the enterprise URL through Sync,
	// Lookup, fetch and linked-worktree creation, including on Windows CI.
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url."+filepath.ToSlash(source)+".insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", repoURL)
	root, err := os.MkdirTemp("", "mc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	workspacesRoot := filepath.Join(root, strings.Repeat("w", max(1, 65-len(root)-1)))
	cache := New(filepath.Join(workspacesRoot, ".repos"), testLogger())
	const wsID = "11111111-1111-1111-1111-111111111111"
	if err := cache.Sync(wsID, []RepoInfo{{URL: repoURL}}); err != nil {
		t.Fatalf("sync long URL: %v", err)
	}
	barePath := cache.Lookup(wsID, repoURL)
	if len(barePath) >= 210 {
		t.Fatalf("cache path exceeds Git's reported path budget: %s (%d)", barePath, len(barePath))
	}
	result, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: wsID,
		RepoURL:     repoURL,
		WorkDir:     filepath.Join(root, "workdir"),
		AgentName:   "test",
		TaskID:      "12345678-0000-0000-0000-000000000000",
	})
	if err != nil {
		t.Fatalf("checkout long URL: %v", err)
	}
	if filepath.Base(result.Path) != "enterprise-repository-client" {
		t.Fatalf("checkout changed the visible repository name: %s", result.Path)
	}
	admin := worktreeRegistration(t, result.Path)
	if index := filepath.Join(admin, "index"); len(index) >= 260 {
		t.Fatalf("worktree index exceeds MAX_PATH: %s (%d)", index, len(index))
	}
	if got := gitHead(t, result.Path); got != gitHead(t, source) {
		t.Fatalf("checkout has %s, want source HEAD", got)
	}
}
