package execenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExcludeFromGitDoesNotUseSharedInfoExclude(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	gitExcludeOK(t, "init", repo)
	cmd := exec.Command("git", "-C", repo, "commit", "--allow-empty", "-m", "init")
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit: %s: %v", out, err)
	}

	if err := excludeFromGit(repo, "CLAUDE.md"); err != nil {
		t.Fatalf("excludeFromGit: %v", err)
	}
	if err := excludeFromGit(repo, "AGENTS.md"); err != nil {
		t.Fatalf("excludeFromGit second pattern: %v", err)
	}
	for _, path := range []string{"CLAUDE.md", "AGENTS.md"} {
		if gitExcludeExit(t, repo, path) != 0 {
			t.Fatalf("expected %s to be ignored", path)
		}
	}

	gitDir := gitExcludeStdout(t, "-C", repo, "rev-parse", "--absolute-git-dir")
	info, err := os.ReadFile(filepath.Join(gitDir, "info", "exclude"))
	if err == nil && strings.Contains(string(info), "CLAUDE.md") {
		t.Fatalf("pattern written to shared info/exclude:\n%s", info)
	}

	sib := filepath.Join(t.TempDir(), "sib")
	gitExcludeOK(t, "-C", repo, "worktree", "add", "--detach", sib, "HEAD")
	out := gitExcludeCombined(t, "-C", sib, "check-ignore", "-v", "--", "CLAUDE.md")
	if strings.Contains(out, "multica-excludes") {
		t.Fatalf("exclude leaked to sibling worktree: %s", out)
	}
}

func gitExcludeOK(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		t.Fatalf("git %s: %s: %v", strings.Join(args, " "), out, err)
	}
}

func gitExcludeStdout(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		t.Fatalf("git %s: %s: %v", strings.Join(args, " "), out, err)
	}
	return strings.TrimSpace(string(out))
}

func gitExcludeCombined(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			t.Fatalf("git %s: %s: %v", strings.Join(args, " "), out, err)
		}
	}
	return string(out)
}

func gitExcludeExit(t *testing.T, repo, path string) int {
	t.Helper()
	err := exec.Command("git", "-C", repo, "check-ignore", "-q", "--", path).Run()
	if err == nil {
		return 0
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode()
	}
	t.Fatal(err)
	return -1
}
