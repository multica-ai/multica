package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveIntelliJCommandUsesEnv(t *testing.T) {
	t.Setenv("MULTICA_INTELLIJ_COMMAND", "custom-idea")

	if got := resolveIntelliJCommand(Config{IntelliJCommand: "config-idea"}); got != "custom-idea" {
		t.Fatalf("resolveIntelliJCommand = %q, want env override", got)
	}
}

func TestOpenIntelliJRejectsMissingWorkDir(t *testing.T) {
	err := openIntelliJ(context.Background(), filepath.Join(t.TempDir(), "missing"), "go", "")
	if err == nil {
		t.Fatal("openIntelliJ succeeded for missing directory")
	}
}

func TestOpenIntelliJRejectsFileWorkDir(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(file, []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	err := openIntelliJ(context.Background(), file, "go", "")
	if err == nil {
		t.Fatal("openIntelliJ succeeded for file work_dir")
	}
}

func TestOpenIntelliJStartsCommandForDirectory(t *testing.T) {
	if _, err := lookPath("go"); err != nil {
		t.Skip("go command is not available on PATH")
	}

	if err := openIntelliJ(context.Background(), t.TempDir(), "go", ""); err != nil {
		t.Fatalf("openIntelliJ: %v", err)
	}
}

func TestOpenIntelliJSwitchesGitWorktreeToRequestedBranch(t *testing.T) {
	if _, err := lookPath("git"); err != nil {
		t.Skip("git command is not available on PATH")
	}
	if _, err := lookPath("go"); err != nil {
		t.Skip("go command is not available on PATH")
	}

	repo := t.TempDir()
	runGitForOpenIDETest(t, repo, "init", "-b", "main")
	runGitForOpenIDETest(t, repo, "commit", "--allow-empty", "-m", "initial")
	runGitForOpenIDETest(t, repo, "branch", "agent/agent/167d0b95")

	if err := openIntelliJ(context.Background(), repo, "go", "agent/agent/167d0b95"); err != nil {
		t.Fatalf("openIntelliJ: %v", err)
	}

	out, err := exec.Command("git", "-C", repo, "branch", "--show-current").Output()
	if err != nil {
		t.Fatalf("show current branch: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "agent/agent/167d0b95" {
		t.Fatalf("current branch = %q, want agent/agent/167d0b95", got)
	}
}

func TestOpenIntelliJCreatesMissingRequestedBranch(t *testing.T) {
	if _, err := lookPath("git"); err != nil {
		t.Skip("git command is not available on PATH")
	}
	if _, err := lookPath("go"); err != nil {
		t.Skip("go command is not available on PATH")
	}

	repo := t.TempDir()
	runGitForOpenIDETest(t, repo, "init", "-b", "main")
	runGitForOpenIDETest(t, repo, "commit", "--allow-empty", "-m", "initial")

	if err := openIntelliJ(context.Background(), repo, "go", "agent/agent/167d0b95"); err != nil {
		t.Fatalf("openIntelliJ: %v", err)
	}

	out, err := exec.Command("git", "-C", repo, "branch", "--show-current").Output()
	if err != nil {
		t.Fatalf("show current branch: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "agent/agent/167d0b95" {
		t.Fatalf("current branch = %q, want agent/agent/167d0b95", got)
	}
}

func runGitForOpenIDETest(t *testing.T, repo string, args ...string) {
	t.Helper()
	full := append([]string{"-C", repo}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
}
