package repocache

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// A resumed run commits in the checkout an earlier run created, without
// calling checkout again. The run-start refresh must re-point the author at
// the new run's initiator, and clear it when the new run has none.
func TestRefreshInitiatorAuthorOnResumedRun(t *testing.T) {
	identityTestEnvironment(t)
	for _, mode := range checkoutModes {
		t.Run(mode.name, func(t *testing.T) {
			f := newExistingCheckoutFixture(t, mode.isolated)
			params := WorktreeParams{WorkspaceID: "ws-1", RepoURL: f.source, WorkDir: f.workDir, AgentName: "Agent", TaskID: firstTaskID, IsolatedGitMetadata: mode.isolated, CommitAuthorIsInitiatorEnabled: true, InitiatorType: "member", InitiatorName: "Alice", InitiatorEmail: "alice@example.com"}
			result, err := f.cache.CreateWorktree(params)
			if err != nil {
				t.Fatal(err)
			}
			run := func(kind, name, email string, enabled bool) {
				t.Helper()
				err := f.cache.RefreshInitiatorAuthor(context.Background(), f.workDir, WorktreeParams{CommitAuthorIsInitiatorEnabled: enabled, InitiatorType: kind, InitiatorName: name, InitiatorEmail: email})
				if err != nil {
					t.Fatal(err)
				}
			}
			run("member", "Bob", "bob@example.com", true)
			gitIdentityCommand(t, "-C", result.Path, "commit", "--allow-empty", "-m", "second run")
			if got := gitIdentityCommand(t, "-C", result.Path, "log", "-1", "--format=%an <%ae>|%cn <%ce>"); got != "Bob <bob@example.com>|User <user@example.com>" {
				t.Fatalf("resumed run identity = %q", got)
			}
			run("agent", "", "", true)
			assertCommitIdentity(t, result.Path, "User", "user@example.com")
			run("member", "Bob", "bob@example.com", true)
			run("member", "Bob", "bob@example.com", false)
			assertCommitIdentity(t, result.Path, "User", "user@example.com")
		})
	}
}

func TestRefreshInitiatorAuthorSkipsForeignRepositories(t *testing.T) {
	identityTestEnvironment(t)
	f := newExistingCheckoutFixture(t, false)
	foreign := filepath.Join(f.workDir, "nested", "foreign")
	if err := os.MkdirAll(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIdentityCommand(t, "init", "-q", foreign)
	if err := f.cache.RefreshInitiatorAuthor(context.Background(), f.workDir, WorktreeParams{CommitAuthorIsInitiatorEnabled: true, InitiatorType: "member", InitiatorName: "Bob", InitiatorEmail: "bob@example.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(foreign, ".git", "multica-initiator-author.config")); !os.IsNotExist(err) {
		t.Fatalf("foreign repository was modified: %v", err)
	}
	assertCommitIdentity(t, foreign, "User", "user@example.com")
}
