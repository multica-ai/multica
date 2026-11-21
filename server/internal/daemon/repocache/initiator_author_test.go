package repocache

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckoutInitiatorAuthor(t *testing.T) {
	identityTestEnvironment(t)
	for _, mode := range checkoutModes {
		t.Run(mode.name, func(t *testing.T) {
			f := newExistingCheckoutFixture(t, mode.isolated)
			params := WorktreeParams{WorkspaceID: "ws-1", RepoURL: f.source, WorkDir: f.workDir, AgentName: "Agent", TaskID: firstTaskID, IsolatedGitMetadata: mode.isolated}
			params.CommitAuthorIsInitiatorEnabled = true
			params.InitiatorType = "member"
			params.InitiatorName = "Human"
			params.InitiatorEmail = "human@example.com"
			params.CoAuthoredByEnabled = true
			result, err := f.cache.CreateWorktree(params)
			if err != nil {
				t.Fatal(err)
			}
			gitIdentityCommand(t, "-C", result.Path, "commit", "--allow-empty", "-m", "initiator author")
			got := gitIdentityCommand(t, "-C", result.Path, "log", "-1", "--format=%an <%ae>|%cn <%ce>")
			if got != "Human <human@example.com>|User <user@example.com>" {
				t.Fatalf("commit identity = %q", got)
			}
			body := gitIdentityCommand(t, "-C", result.Path, "log", "-1", "--format=%B")
			if !strings.Contains(body, "Co-authored-by:") {
				t.Fatal("initiator author setting removed the independent co-author trailer")
			}
			for _, tc := range []struct {
				name, kind, human, email string
				enabled                  bool
			}{
				{"disabled", "member", "Human", "human@example.com", false},
				{"agent", "agent", "Agent", "agent@example.com", true},
				{"no email", "member", "Human", "", true},
				{"no name", "member", "", "human@example.com", true},
				{"invalid email", "member", "Human", "invalid", true},
				{"unsafe name", "member", "Human&Other", "human@example.com", true},
				{"control", "member", "Human\tOther", "human@example.com", true},
				{"unsafe email", "member", "Human", "human<@example.com", true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					// Reinstall before every case so each also exercises stale cleanup.
					params.CommitAuthorIsInitiatorEnabled = true
					params.InitiatorType, params.InitiatorName, params.InitiatorEmail = "member", "Human", "human@example.com"
					if _, err := f.cache.CreateWorktree(params); err != nil {
						t.Fatal(err)
					}
					params.CommitAuthorIsInitiatorEnabled = tc.enabled
					params.InitiatorType, params.InitiatorName, params.InitiatorEmail = tc.kind, tc.human, tc.email
					if _, err := f.cache.CreateWorktree(params); err != nil {
						t.Fatal(err)
					}
					assertCommitIdentity(t, result.Path, "User", "user@example.com")
				})
			}
		})
	}
}

func TestInitiatorAuthorValidation(t *testing.T) {
	for _, invalid := range []string{"<", ">", "\"", "\\", "&", "\x00", "\n", "\r", "\t", "\x7f"} {
		for _, field := range []string{"name", "email"} {
			t.Run(field+"/"+fmt.Sprintf("%q", invalid), func(t *testing.T) {
				params := WorktreeParams{CommitAuthorIsInitiatorEnabled: true, InitiatorType: "member", InitiatorName: "Human", InitiatorEmail: "human@example.com"}
				if field == "name" {
					params.InitiatorName += invalid
				} else {
					params.InitiatorEmail += invalid
				}
				if name, email := validInitiatorAuthor(params); name != "" || email != "" {
					t.Fatalf("accepted invalid identity: %q %q", name, email)
				}
			})
		}
	}
}

func TestCheckoutInitiatorAuthorIsPrivate(t *testing.T) {
	identityTestEnvironment(t)
	f := newExistingCheckoutFixture(t, false)
	params := WorktreeParams{WorkspaceID: "ws-1", RepoURL: f.source, WorkDir: f.workDir, TaskID: firstTaskID, AgentName: "Agent", CommitAuthorIsInitiatorEnabled: true, InitiatorType: "member", InitiatorName: "Human", InitiatorEmail: "human@example.com"}
	first, err := f.cache.CreateWorktree(params)
	if err != nil {
		t.Fatal(err)
	}
	params.WorkDir = t.TempDir()
	params.TaskID = secondTaskID
	params.CommitAuthorIsInitiatorEnabled = false
	other, err := f.cache.CreateWorktree(params)
	if err != nil {
		t.Fatal(err)
	}
	assertCommitIdentity(t, other.Path, "User", "user@example.com")
	if got := gitIdentityCommand(t, "-C", first.Path, "config", "author.email"); got != "human@example.com" {
		t.Fatalf("another checkout changed author: %q", got)
	}
}

func TestCheckoutInitiatorAuthorPreservesConfig(t *testing.T) {
	identityTestEnvironment(t)
	for _, mode := range checkoutModes {
		t.Run(mode.name, func(t *testing.T) {
			f := newExistingCheckoutFixture(t, mode.isolated)
			params := WorktreeParams{WorkspaceID: "ws-1", RepoURL: f.source, WorkDir: f.workDir, AgentName: "Agent", TaskID: firstTaskID, IsolatedGitMetadata: mode.isolated}
			result, err := f.cache.CreateWorktree(params)
			if err != nil {
				t.Fatal(err)
			}
			scope := "--worktree"
			config := "config.worktree"
			if mode.isolated {
				scope, config = "--local", "config"
			}
			gitDir := gitIdentityCommand(t, "-C", result.Path, "rev-parse", "--absolute-git-dir")
			path := filepath.Join(gitDir, config)
			gitIdentityCommand(t, "-C", result.Path, "config", scope, "author.name", "Existing")
			gitIdentityCommand(t, "-C", result.Path, "config", scope, "author.email", "existing@example.com")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.cache.CreateWorktree(params); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("disabled setting changed config")
			}
			params.CommitAuthorIsInitiatorEnabled = true
			params.InitiatorType, params.InitiatorName, params.InitiatorEmail = "member", "Human", "human@example.com"
			if _, err := f.cache.CreateWorktree(params); err != nil {
				t.Fatal(err)
			}
			params.CommitAuthorIsInitiatorEnabled = false
			if _, err := f.cache.CreateWorktree(params); err != nil {
				t.Fatal(err)
			}
			after, err = os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("cleanup did not restore original config")
			}
			if got := gitIdentityCommand(t, "-C", result.Path, "config", "author.name"); got != "Existing" {
				t.Fatalf("author = %q", got)
			}
		})
	}
}
