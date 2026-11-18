package execenv

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorktreeRuntimeBriefStaysOutOfAgentCommits(t *testing.T) {
	for _, tc := range []struct{ provider, target string }{
		{"claude", "CLAUDE.md"}, {"codex", "AGENTS.md"},
		{"claude", "AGENTS.md"}, {"codex", "CLAUDE.md"},
	} {
		provider := tc.provider
		for _, subdir := range []string{".", "docs [guide]"} {
			t.Run(provider+"/"+tc.target+"/"+subdir, func(t *testing.T) {
				repo := newTestRepo(t)
				localDir := filepath.Join(repo, subdir)
				if err := os.MkdirAll(localDir, 0o755); err != nil {
					t.Fatal(err)
				}
				path := runtimeConfigPath(localDir, provider)
				target := filepath.Join(localDir, tc.target)
				original := "Repository instructions.\n"
				if err := os.WriteFile(target, []byte(original), 0o644); err != nil {
					t.Fatal(err)
				}
				if path != target {
					if err := os.Symlink(tc.target, path); err != nil {
						t.Fatal(err)
					}
				}
				gitRun(t, repo, "add", ".")
				gitRun(t, repo, "commit", "-m", "add instructions")
				wt := prepareForTest(t, localDir)
				if _, err := InjectRuntimeConfig(wt.WorkDir, provider, TaskContextForEnv{}); err != nil {
					t.Fatal(err)
				}
				if err := wt.ProtectRuntimeConfig(provider); err != nil {
					t.Fatal(err)
				}
				if got := readFile(t, runtimeConfigPath(wt.WorkDir, provider)); !strings.Contains(got, runtimeMarkerBegin) {
					t.Fatal("runtime brief is not readable by the agent")
				}
				if got := gitRun(t, wt.Path, "status", "--porcelain"); got != "" {
					t.Fatalf("brief dirtied worktree: %s", got)
				}
				if got := gitRun(t, wt.Path, "diff"); got != "" {
					t.Fatalf("brief appears in diff: %s", got)
				}
				gitRun(t, wt.Path, "add", "-A")
				if got := gitRun(t, wt.Path, "diff", "--cached"); got != "" {
					t.Fatalf("git add staged brief: %s", got)
				}
				if err := os.WriteFile(filepath.Join(wt.Path, "tracked.txt"), []byte("agent change\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				gitRun(t, wt.Path, "commit", "-am", "agent change")
				rel, err := filepath.Rel(repo, target)
				if err != nil {
					t.Fatal(err)
				}
				rel = filepath.ToSlash(rel)
				if wt.RuntimeConfigSkipWorktree != rel {
					t.Fatalf("protected %q, want brief target %q", wt.RuntimeConfigSkipWorktree, rel)
				}
				if got := gitRun(t, wt.Path, "show", "HEAD:"+rel); got != strings.TrimSpace(original) {
					t.Fatalf("agent committed runtime brief: %s", got)
				}
				for _, entry := range []string{filepath.Base(path), tc.target} {
					entry = filepath.ToSlash(filepath.Join(subdir, entry))
					if got := gitRun(t, repo, "ls-files", "-v", "--", ":(literal)"+entry); !strings.HasPrefix(got, "H ") {
						t.Fatalf("user's index changed: %s", got)
					}
				}
				if err := wt.RestoreRuntimeConfigTracking(); err != nil {
					t.Fatal(err)
				}
				if got := gitRun(t, wt.Path, "ls-files", "-v", "--", ":(literal)"+rel); !strings.HasPrefix(got, "H ") {
					t.Fatalf("target tracking was not restored: %s", got)
				}
				if err := CleanupRuntimeConfig(wt.WorkDir, provider); err != nil {
					t.Fatal(err)
				}
				finalizeOK(t, wt)
				if got := readFile(t, path); got != original {
					t.Fatalf("user's instruction file changed: %q", got)
				}
			})
		}
	}
}

func TestWorktreeRuntimeConfigEditsSurviveFinalize(t *testing.T) {
	for _, provider := range []string{"codex", "claude"} {
		for _, agentCommit := range []bool{false, true} {
			t.Run(provider+"/"+map[bool]string{false: "auto commit", true: "leftover ref"}[agentCommit], func(t *testing.T) {
				repo := newTestRepo(t)
				if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte("Original instructions.\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				if provider == "claude" {
					if err := os.Symlink("AGENTS.md", filepath.Join(repo, "CLAUDE.md")); err != nil {
						t.Fatal(err)
					}
				}
				gitRun(t, repo, "add", ".")
				gitRun(t, repo, "commit", "-m", "add instructions")
				wt := prepareForTest(t, repo)
				if _, err := InjectRuntimeConfig(wt.WorkDir, provider, TaskContextForEnv{}); err != nil {
					t.Fatal(err)
				}
				if err := wt.ProtectRuntimeConfig(provider); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(wt.WorkDir, "AGENTS.md")
				content := strings.Replace(readFile(t, path), "Original instructions.", "Updated instructions.", 1)
				if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
				if agentCommit {
					if err := os.WriteFile(filepath.Join(wt.Path, "tracked.txt"), []byte("agent change\n"), 0o644); err != nil {
						t.Fatal(err)
					}
					gitRun(t, wt.Path, "add", "-A")
					gitRun(t, wt.Path, "commit", "-m", "agent change")
				}
				if err := wt.RestoreRuntimeConfigTracking(); err != nil {
					t.Fatal(err)
				}
				if err := CleanupRuntimeConfig(wt.WorkDir, provider); err != nil {
					t.Fatal(err)
				}
				outcome := finalizeOK(t, wt)
				ref := outcome.Branch
				if agentCommit {
					ref = outcome.LeftoverRef
				}
				if ref == "" {
					t.Fatal("instruction edit was not preserved")
				}
				if got := gitRun(t, repo, "show", ref+":AGENTS.md"); got != "Updated instructions." {
					t.Fatalf("instruction edit lost or brief committed: %q", got)
				}
				if agentCommit {
					if got := gitRun(t, repo, "show", outcome.Branch+":AGENTS.md"); got != "Original instructions." {
						t.Fatalf("agent committed protected instructions: %q", got)
					}
				}
				if got := readFile(t, filepath.Join(repo, "AGENTS.md")); got != "Original instructions.\n" {
					t.Fatalf("user's instructions changed: %q", got)
				}
			})
		}
	}
}

func TestWorktreeRuntimeConfigSymlinkBoundaries(t *testing.T) {
	for _, targetKind := range []string{"parent in worktree", "untracked", "user checkout", "directory symlink", "sibling prefix"} {
		t.Run(targetKind, func(t *testing.T) {
			repo := newTestRepo(t)
			original := "User instructions.\n"
			if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte(original), 0o644); err != nil {
				t.Fatal(err)
			}
			gitRun(t, repo, "add", ".")
			gitRun(t, repo, "commit", "-m", "add instructions")
			userIndex := readFile(t, filepath.Join(repo, ".git", "index"))
			wt := prepareForTest(t, repo)
			defer wt.Discard(worktreeTestLogger())
			target := filepath.Join(repo, "AGENTS.md")
			linkTarget := target
			switch targetKind {
			case "parent in worktree":
				wt.WorkDir = filepath.Join(wt.Path, "docs [guide]")
				if err := os.MkdirAll(wt.WorkDir, 0o755); err != nil {
					t.Fatal(err)
				}
				target = filepath.Join(wt.Path, "AGENTS.md")
				linkTarget = filepath.Join("..", "AGENTS.md")
			case "untracked":
				target = filepath.Join(wt.Path, "untracked.md")
				linkTarget = "untracked.md"
			case "directory symlink":
				if err := os.Symlink(repo, filepath.Join(wt.Path, "shared")); err != nil {
					t.Fatal(err)
				}
				linkTarget = filepath.Join("shared", "AGENTS.md")
			case "sibling prefix":
				outside := wt.Path + "-outside"
				if err := os.MkdirAll(outside, 0o755); err != nil {
					t.Fatal(err)
				}
				target = filepath.Join(outside, "AGENTS.md")
				linkTarget = target
			}
			if err := os.WriteFile(target, []byte(original), 0o644); err != nil {
				t.Fatal(err)
			}
			config := runtimeConfigPath(wt.WorkDir, "claude")
			if err := os.Symlink(linkTarget, config); err != nil {
				t.Fatal(err)
			}
			gitRun(t, wt.Path, "add", "--", config)
			if err := wt.ProtectRuntimeConfig("claude"); err != nil {
				t.Fatal(err)
			}
			want := ""
			if targetKind == "parent in worktree" {
				want = "AGENTS.md"
			}
			if wt.RuntimeConfigSkipWorktree != want {
				t.Fatalf("protected %q, want %q", wt.RuntimeConfigSkipWorktree, want)
			}
			if got := gitRun(t, wt.Path, "ls-files", "-v", "--", config); !strings.HasPrefix(got, "H ") {
				t.Fatalf("symlink itself was hidden: %s", got)
			}
			if err := wt.RestoreRuntimeConfigTracking(); err != nil {
				t.Fatal(err)
			}
			if got := gitRun(t, wt.Path, "ls-files", "-v", "--", "AGENTS.md"); !strings.HasPrefix(got, "H ") {
				t.Fatalf("target tracking was not restored: %s", got)
			}
			if got := readFile(t, target); got != original {
				t.Fatalf("target changed: %q", got)
			}
			if got := readFile(t, filepath.Join(repo, ".git", "index")); got != userIndex {
				t.Fatal("user's index bytes changed")
			}
		})
	}
}

func TestWorktreeRuntimeConfigTrackingRestoredOnAbort(t *testing.T) {
	repo := newTestRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte("Instructions.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("AGENTS.md", filepath.Join(repo, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "add instructions")
	wt := prepareForTest(t, repo)
	if _, err := InjectRuntimeConfig(wt.WorkDir, "claude", TaskContextForEnv{}); err != nil {
		t.Fatal(err)
	}
	if err := wt.ProtectRuntimeConfig("claude"); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(wt)
	if err != nil {
		t.Fatal(err)
	}
	var restored LocalWorktree
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	restored.AbortWithReason(errors.New("cleanup failed"))
	outcome, err := restored.Finalize(worktreeTestLogger())
	if err == nil || outcome.PreservedPath != wt.Path {
		t.Fatalf("Finalize = %+v, %v; want preserved worktree", outcome, err)
	}
	if got := gitRun(t, wt.Path, "ls-files", "-v", "--", "AGENTS.md"); !strings.HasPrefix(got, "H ") {
		t.Fatalf("skip-worktree survived abort: %s", got)
	}
	wt.Discard(worktreeTestLogger())
}

func TestWorktreeRuntimeConfigProtectionSkipsUntrackedFiles(t *testing.T) {
	repo := newTestRepo(t)
	wt := prepareForTest(t, repo)
	if _, err := InjectRuntimeConfig(wt.WorkDir, "claude", TaskContextForEnv{}); err != nil {
		t.Fatal(err)
	}
	if err := wt.ProtectRuntimeConfig("claude"); err != nil {
		t.Fatal(err)
	}
	if wt.RuntimeConfigSkipWorktree != "" {
		t.Fatal("untracked instruction file recorded as protected")
	}
	if err := CleanupRuntimeConfig(wt.WorkDir, "claude"); err != nil {
		t.Fatal(err)
	}
	finalizeOK(t, wt)
}

func TestWorktreeRuntimeConfigProtectionSkipsMissingFiles(t *testing.T) {
	wt := prepareForTest(t, newTestRepo(t))
	defer wt.Discard(worktreeTestLogger())
	if err := wt.ProtectRuntimeConfig("claude"); err != nil {
		t.Fatal(err)
	}
	if wt.RuntimeConfigSkipWorktree != "" {
		t.Fatal("missing instruction file recorded as protected")
	}
}

func TestWorktreeRuntimeConfigRestoreFailurePreservesWorktree(t *testing.T) {
	repo := newTestRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte("Instructions.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("AGENTS.md", filepath.Join(repo, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "add instructions")
	wt := prepareForTest(t, repo)
	if err := wt.ProtectRuntimeConfig("claude"); err != nil {
		t.Fatal(err)
	}
	updated := "Updated instructions.\n"
	if err := os.WriteFile(filepath.Join(wt.Path, "AGENTS.md"), []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	gitDir := gitRun(t, wt.Path, "rev-parse", "--absolute-git-dir")
	lock := filepath.Join(gitDir, "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	outcome, err := wt.Finalize(worktreeTestLogger())
	if err == nil || outcome.PreservedPath != wt.Path || outcome.Branch != "" {
		t.Fatalf("Finalize = %+v, %v; want preserved worktree without delivery", outcome, err)
	}
	if !strings.Contains(err.Error(), "the work is preserved in the worktree at "+wt.Path) {
		t.Fatalf("restore failure did not report the recovery path: %v", err)
	}
	if got := readFile(t, filepath.Join(wt.Path, "AGENTS.md")); got != updated {
		t.Fatalf("restore failure lost instruction edits: %q", got)
	}
	if wt.RuntimeConfigSkipWorktree == "" {
		t.Fatal("failed restore discarded the path needed to retry")
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	wt.Discard(worktreeTestLogger())
	if wt.RuntimeConfigSkipWorktree != "" {
		t.Fatal("discard did not restore tracking after the lock was released")
	}
}
