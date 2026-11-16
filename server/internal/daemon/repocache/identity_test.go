package repocache

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func identityTestEnvironment(t *testing.T) string {
	t.Helper()
	for _, key := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL", "GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	global := filepath.Join(t.TempDir(), "gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	gitIdentityCommand(t, "config", "--global", "user.name", "User")
	gitIdentityCommand(t, "config", "--global", "user.email", "user@example.com")
	return global
}

func gitIdentityCommand(t *testing.T, args ...string) string {
	t.Helper()
	out, err := runGitCombinedOutput(args...)
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
	return strings.TrimSpace(string(out))
}

func assertCommitIdentity(t *testing.T, path, name, email string) {
	t.Helper()
	gitIdentityCommand(t, "-C", path, "commit", "--allow-empty", "-m", "identity regression")
	got := gitIdentityCommand(t, "-C", path, "log", "-1", "--format=%an <%ae>|%cn <%ce>")
	want := fmt.Sprintf("%s <%s>|%s <%s>", name, email, name, email)
	if got != want {
		t.Fatalf("commit identity = %q, want %q", got, want)
	}
}

func polluteCacheIdentity(t *testing.T, path string) {
	t.Helper()
	for _, section := range []string{"user", "author", "committer"} {
		gitIdentityCommand(t, "-C", path, "config", "--local", section+".name", "Old Agent")
		gitIdentityCommand(t, "-C", path, "config", "--local", section+".email", "old@example.com")
	}
}

func TestCheckoutIdentityIgnoresSharedCache(t *testing.T) {
	global := identityTestEnvironment(t)
	before, _ := os.ReadFile(global)
	for _, mode := range checkoutModes {
		t.Run(mode.name, func(t *testing.T) {
			f := newExistingCheckoutFixture(t, mode.isolated)
			bare := f.cache.Lookup("ws-1", f.source)
			polluteCacheIdentity(t, bare)
			first := f.checkout(t, firstTaskID, false)
			assertCommitIdentity(t, first.Path, "User", "user@example.com")
			// Reuse, including the keep-local-work branch, must retain isolation.
			again := f.checkout(t, secondTaskID, false)
			assertCommitIdentity(t, again.Path, "User", "user@example.com")
			if got := gitIdentityCommand(t, "-C", bare, "config", "--local", "user.name"); got != "Old Agent" {
				t.Fatalf("shared identity was changed: %q", got)
			}
		})
	}
	after, _ := os.ReadFile(global)
	if string(after) != string(before) {
		t.Fatal("checkout changed global Git config")
	}
}

func TestCheckoutIdentityConcurrentTasksAndOverrides(t *testing.T) {
	identityTestEnvironment(t)
	f := newExistingCheckoutFixture(t, false)
	bare := f.cache.Lookup("ws-1", f.source)
	polluteCacheIdentity(t, bare)
	var wg sync.WaitGroup
	results := make([]*WorktreeResult, 2)
	errs := make([]error, 2)
	params := make([]WorktreeParams, 2)
	for i := range params {
		params[i] = WorktreeParams{WorkspaceID: "ws-1", RepoURL: f.source, WorkDir: t.TempDir(), AgentName: "Agent", TaskID: fmt.Sprintf("task-%d", i)}
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = f.cache.CreateWorktree(params[i])
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	gitIdentityCommand(t, "-C", results[0].Path, "config", "--worktree", "user.name", "Task A")
	gitIdentityCommand(t, "-C", results[0].Path, "config", "--worktree", "user.email", "a@example.com")
	// A plain `git config` still writes shared config in linked worktrees.
	// Even such a later write must not change another protected task.
	gitIdentityCommand(t, "-C", results[0].Path, "config", "user.name", "Later contamination")
	assertCommitIdentity(t, results[0].Path, "Task A", "a@example.com")
	assertCommitIdentity(t, results[1].Path, "User", "user@example.com")
	gitIdentityCommand(t, "config", "--global", "user.name", "Updated User")
	for i := range params {
		if _, err := f.cache.CreateWorktree(params[i]); err != nil {
			t.Fatal(err)
		}
	}
	assertCommitIdentity(t, results[0].Path, "Task A", "a@example.com")
	assertCommitIdentity(t, results[1].Path, "Updated User", "user@example.com")
}

func TestCheckoutIdentityMigratesExistingWorktree(t *testing.T) {
	identityTestEnvironment(t)
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("worktreeConfig=%v", enabled), func(t *testing.T) {
			f := newExistingCheckoutFixture(t, false)
			bare := f.cache.Lookup("ws-1", f.source)
			polluteCacheIdentity(t, bare)
			path := filepath.Join(f.workDir, repoNameFromURL(f.source))
			gitIdentityCommand(t, "-C", bare, "worktree", "add", "-b", taskBranchName(firstTaskID), path)
			other := filepath.Join(t.TempDir(), "active")
			gitIdentityCommand(t, "-C", bare, "worktree", "add", "-b", "other-active-task", other)
			if enabled {
				// Model caches already using the extension before this fix.
				gitIdentityCommand(t, "-C", bare, "config", "core.bare", "false")
				gitIdentityCommand(t, "-C", bare, "config", "extensions.worktreeConfig", "true")
				gitIdentityCommand(t, "-C", path, "config", "--worktree", "user.name", "Intentional")
				gitIdentityCommand(t, "-C", path, "config", "--worktree", "user.email", "intentional@example.com")
			}
			result := f.checkout(t, firstTaskID, false)
			if enabled {
				assertCommitIdentity(t, result.Path, "Intentional", "intentional@example.com")
			} else {
				assertCommitIdentity(t, result.Path, "User", "user@example.com")
				if got := gitIdentityCommand(t, "-C", bare, "rev-parse", "--is-bare-repository"); got != "true" {
					t.Fatalf("cache stopped being bare: %s", got)
				}
			}
			// Do not mutate another active checkout's effective identity or
			// turn it into a bare repository while enabling the extension.
			assertCommitIdentity(t, other, "Old Agent", "old@example.com")
		})
	}
}

func TestCheckoutIdentityUserConfigAndCoauthor(t *testing.T) {
	identityTestEnvironment(t)
	f := newExistingCheckoutFixture(t, false)
	bare := f.cache.Lookup("ws-1", f.source)
	polluteCacheIdentity(t, bare)
	include := filepath.Join(t.TempDir(), "identity.config")
	gitIdentityCommand(t, "config", "--file", include, "user.name", "Conditional User")
	gitIdentityCommand(t, "config", "--file", include, "author.name", "Explicit Author")
	gitIdentityCommand(t, "config", "--global", "includeIf.onbranch:agent/**.path", filepath.ToSlash(include))
	for _, enabled := range []bool{true, false} {
		params := WorktreeParams{WorkspaceID: "ws-1", RepoURL: f.source, WorkDir: f.workDir, AgentName: "Agent", TaskID: firstTaskID, CoAuthoredByEnabled: enabled}
		result, err := f.cache.CreateWorktree(params)
		if err != nil {
			t.Fatal(err)
		}
		gitIdentityCommand(t, "-C", result.Path, "commit", "--allow-empty", "-m", "coauthor identity")
		got := gitIdentityCommand(t, "-C", result.Path, "log", "-1", "--format=%an <%ae>|%cn <%ce>")
		if got != "Explicit Author <user@example.com>|Conditional User <user@example.com>" {
			t.Fatalf("conditional user/author identity = %q", got)
		}
		body := gitIdentityCommand(t, "-C", result.Path, "log", "-1", "--format=%B")
		if strings.Contains(body, "Co-authored-by: multica-agent <github@multica.ai>") != enabled {
			t.Fatalf("coauthor enabled=%v, commit body: %s", enabled, body)
		}
	}
	// Explicit environment values remain Git's highest-priority identity.
	t.Setenv("GIT_AUTHOR_NAME", "Environment")
	t.Setenv("GIT_AUTHOR_EMAIL", "env@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Environment")
	t.Setenv("GIT_COMMITTER_EMAIL", "env@example.com")
	assertCommitIdentity(t, filepath.Join(f.workDir, repoNameFromURL(f.source)), "Environment", "env@example.com")
}

func TestCheckoutIdentityWithoutUserFailsClosed(t *testing.T) {
	global := identityTestEnvironment(t)
	if err := os.WriteFile(global, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	f := newExistingCheckoutFixture(t, false)
	polluteCacheIdentity(t, f.cache.Lookup("ws-1", f.source))
	result := f.checkout(t, firstTaskID, false)
	if out, err := runGitCombinedOutput("-C", result.Path, "commit", "--allow-empty", "-m", "must not use cache author"); err == nil {
		t.Fatalf("commit succeeded without a configured identity: %s", out)
	}
	// Setting an explicit worktree identity restores ordinary commits.
	gitIdentityCommand(t, "-C", result.Path, "config", "--worktree", "user.name", "Configured")
	gitIdentityCommand(t, "-C", result.Path, "config", "--worktree", "user.email", "configured@example.com")
	assertCommitIdentity(t, result.Path, "Configured", "configured@example.com")
}

func TestIsolatedCheckoutPreservesIntentionalIdentity(t *testing.T) {
	identityTestEnvironment(t)
	f := newExistingCheckoutFixture(t, true)
	result := f.checkout(t, firstTaskID, false)
	gitIdentityCommand(t, "-C", result.Path, "config", "user.name", "Local User")
	gitIdentityCommand(t, "-C", result.Path, "config", "user.email", "local@example.com")
	f.checkout(t, firstTaskID, false)
	assertCommitIdentity(t, result.Path, "Local User", "local@example.com")
}

func TestCheckoutIdentityConfigLockFailureIsRetryable(t *testing.T) {
	identityTestEnvironment(t)
	f := newExistingCheckoutFixture(t, false)
	result := f.checkout(t, firstTaskID, false)
	gitDir := gitIdentityCommand(t, "-C", result.Path, "rev-parse", "--absolute-git-dir")
	lock := filepath.Join(gitDir, "config.worktree.lock")
	if err := os.WriteFile(lock, []byte("another writer"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := f.cache.CreateWorktree(WorktreeParams{WorkspaceID: "ws-1", RepoURL: f.source, WorkDir: f.workDir, AgentName: "Agent", TaskID: firstTaskID})
	if err == nil || !strings.Contains(err.Error(), "lock Git config") {
		t.Fatalf("checkout error = %v, want config lock failure", err)
	}
	contents, err := os.ReadFile(lock)
	if err != nil || string(contents) != "another writer" {
		t.Fatalf("another writer's lock changed: %s, %v", contents, err)
	}
	assertCommitIdentity(t, result.Path, "User", "user@example.com")
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	f.checkout(t, firstTaskID, false)
}

func TestCheckoutIdentityRejectsForeignWorktree(t *testing.T) {
	identityTestEnvironment(t)
	f := newExistingCheckoutFixture(t, false)
	foreign := filepath.Join(t.TempDir(), "foreign")
	gitIdentityCommand(t, "-C", f.source, "worktree", "add", "-b", "foreign", foreign)
	err := isolateWorktreeIdentityContext(context.Background(), f.cache.Lookup("ws-1", f.source), foreign)
	if err == nil || !strings.Contains(err.Error(), "does not match cache") {
		t.Fatalf("identity isolation error = %v, want foreign repository rejection", err)
	}
	gitDir := gitIdentityCommand(t, "-C", foreign, "rev-parse", "--absolute-git-dir")
	if _, err := os.Stat(filepath.Join(gitDir, "config.worktree")); !os.IsNotExist(err) {
		t.Fatalf("foreign worktree config was written: %v", err)
	}
}

func TestCheckoutIdentityEnabledWithSharedConfigLocked(t *testing.T) {
	identityTestEnvironment(t)
	f := newExistingCheckoutFixture(t, false)
	first := f.checkout(t, firstTaskID, false)
	firstWorkDir := f.workDir
	f.workDir = t.TempDir()
	second := f.checkout(t, secondTaskID, false)
	secondWorkDir := f.workDir
	bare := f.cache.Lookup("ws-1", f.source)
	lockPath := filepath.Join(bare, "config.lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lock.WriteString("sibling writer"); err != nil {
		lock.Close()
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(lockPath)

	// Reusing either checkout needs no common config write after migration.
	// Create them before taking the lock: Git itself can require that lock
	// when initially recording the new branches' upstream configuration.
	f.workDir = firstWorkDir
	f.checkout(t, firstTaskID, false)
	f.workDir = secondWorkDir
	f.checkout(t, secondTaskID, false)
	for _, path := range []string{first.Path, second.Path} {
		assertCommitIdentity(t, path, "User", "user@example.com")
	}
	contents, err := os.ReadFile(lockPath)
	if err != nil || string(contents) != "sibling writer" {
		t.Fatalf("sibling writer's lock changed: %q, %v", contents, err)
	}
}

func TestEditGitConfigFilePreservesNextWriterLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	lockPath := path + ".lock"
	err := editGitConfigFileWithRename(path, func([]byte, string) ([]byte, error) {
		return []byte("[user]\nname = User\n"), nil
	}, func(oldPath, newPath string) error {
		if err := os.Rename(oldPath, newPath); err != nil {
			return err
		}
		// A subsequent writer can acquire the path immediately after rename,
		// before the publisher returns and its deferred cleanup runs.
		next, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, writeErr := next.WriteString("next writer")
		closeErr := next.Close()
		return errors.Join(writeErr, closeErr)
	})
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(lockPath)
	if err != nil || string(contents) != "next writer" {
		t.Fatalf("next writer's lock changed: %q, %v", contents, err)
	}
	if third, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600); !os.IsExist(err) {
		if third != nil {
			third.Close()
		}
		t.Fatalf("third writer should remain excluded, got %v", err)
	}
}

func TestEditGitConfigFileRollback(t *testing.T) {
	for _, outcome := range []string{"unchanged", "edit error", "rename error"} {
		t.Run(outcome, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config")
			original := "[user]\nname = Original\n"
			if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			failure := errors.New("injected failure")
			err := editGitConfigFileWithRename(path, func(contents []byte, _ string) ([]byte, error) {
				if outcome == "unchanged" {
					return contents, nil
				}
				if outcome == "edit error" {
					return nil, failure
				}
				return []byte("[user]\nname = Updated\n"), nil
			}, func(string, string) error {
				if outcome != "rename error" {
					t.Fatal("unexpected publish on rollback path")
				}
				return failure
			})
			if outcome == "unchanged" && err != nil || outcome != "unchanged" && !errors.Is(err, failure) {
				t.Fatalf("edit error = %v for %s", err, outcome)
			}
			contents, err := os.ReadFile(path)
			if err != nil || string(contents) != original {
				t.Fatalf("config changed on rollback: %q, %v", contents, err)
			}
			if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
				t.Fatalf("owned lock not cleaned up: %v", err)
			}
		})
	}
}
