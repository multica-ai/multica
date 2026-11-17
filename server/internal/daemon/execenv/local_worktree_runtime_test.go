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
	for _, provider := range []string{"claude", "codex"} {
		for _, subdir := range []string{".", "docs [guide]"} {
			t.Run(provider+"/"+subdir, func(t *testing.T) {
				repo := newTestRepo(t)
				localDir := filepath.Join(repo, subdir)
				if err := os.MkdirAll(localDir, 0o755); err != nil {
					t.Fatal(err)
				}
				path := runtimeConfigPath(localDir, provider)
				original := "Repository instructions.\n"
				if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
					t.Fatal(err)
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
				rel, err := filepath.Rel(repo, path)
				if err != nil {
					t.Fatal(err)
				}
				if got := gitRun(t, wt.Path, "show", "HEAD:"+filepath.ToSlash(rel)); got != strings.TrimSpace(original) {
					t.Fatalf("agent committed runtime brief: %s", got)
				}
				if got := gitRun(t, repo, "ls-files", "-v", "--", ":(literal)"+filepath.ToSlash(rel)); !strings.HasPrefix(got, "H ") {
					t.Fatalf("user's index changed: %s", got)
				}
				if err := wt.RestoreRuntimeConfigTracking(); err != nil {
					t.Fatal(err)
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
	for _, agentCommit := range []bool{false, true} {
		t.Run(map[bool]string{false: "auto commit", true: "leftover ref"}[agentCommit], func(t *testing.T) {
			repo := newTestRepo(t)
			if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte("Original instructions.\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			gitRun(t, repo, "add", ".")
			gitRun(t, repo, "commit", "-m", "add instructions")
			wt := prepareForTest(t, repo)
			if _, err := InjectRuntimeConfig(wt.WorkDir, "codex", TaskContextForEnv{}); err != nil {
				t.Fatal(err)
			}
			if err := wt.ProtectRuntimeConfig("codex"); err != nil {
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
			if err := CleanupRuntimeConfig(wt.WorkDir, "codex"); err != nil {
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
		})
	}
}

func TestWorktreeRuntimeConfigTrackingRestoredOnAbort(t *testing.T) {
	repo := newTestRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte("Instructions.\n"), 0o644); err != nil {
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
	if got := gitRun(t, wt.Path, "ls-files", "-v", "--", "CLAUDE.md"); !strings.HasPrefix(got, "H ") {
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

func TestWorktreeRuntimeConfigRestoreFailurePreservesWorktree(t *testing.T) {
	repo := newTestRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte("Instructions.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "add instructions")
	wt := prepareForTest(t, repo)
	if err := wt.ProtectRuntimeConfig("codex"); err != nil {
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
