package gitexclude

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallHidesFilesInOneLinkedWorktree(t *testing.T) {
	bare, cleanup := bareRepoWithCommit(t)
	t.Cleanup(cleanup)
	a := filepath.Join(t.TempDir(), "wt-a")
	b := filepath.Join(t.TempDir(), "wt-b")
	gitOK(t, "-C", bare, "worktree", "add", a, "main")
	gitOK(t, "-C", bare, "worktree", "add", "-b", "other", b, "main")

	commonExclude := gitPath(t, a, "info/exclude")
	before, _ := os.ReadFile(commonExclude)

	if err := Install(context.Background(), a, []string{"CLAUDE.md", "AGENTS.md", ".claude"}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := Install(context.Background(), a, []string{"CLAUDE.md", ".omp"}); err != nil {
		t.Fatalf("reinstall: %v", err)
	}

	writeRuntimeFiles(t, a)
	if status := gitStdout(t, "-C", a, "status", "--porcelain"); status != "" {
		t.Fatalf("runtime files visible in installed worktree:\n%s", status)
	}
	gitOK(t, "-C", a, "add", "-A")
	if staged := gitStdout(t, "-C", a, "diff", "--cached", "--name-only"); staged != "" {
		t.Fatalf("git add -A staged runtime files:\n%s", staged)
	}
	source := gitStdout(t, "-C", a, "check-ignore", "-v", "--", "CLAUDE.md")
	if !strings.Contains(source, "multica-excludes") {
		t.Fatalf("check-ignore source = %q", source)
	}
	if !strings.Contains(gitStdout(t, "-C", a, "check-ignore", "-v", "--", ".claude/settings.local.json"), ".claude") {
		t.Fatal("nested .claude file was not ignored")
	}
	if !strings.Contains(gitStdout(t, "-C", a, "check-ignore", "-v", "--", ".omp"), ".omp") {
		t.Fatal("pattern added by the second install was dropped")
	}

	excludes := mustRead(t, filepath.Join(gitAbsDir(t, a), "info", excludesFileName))
	if strings.Count(excludes, "\nCLAUDE.md\n") != 1 {
		t.Fatalf("CLAUDE.md pattern duplicated:\n%s", excludes)
	}

	writeRuntimeFiles(t, b)
	if gitExit(t, "-C", b, "check-ignore", "-q", "--", "CLAUDE.md") == 0 &&
		strings.Contains(gitStdout(t, "-C", b, "check-ignore", "-v", "--", "CLAUDE.md"), "multica-excludes") {
		t.Fatal("exclude leaked to sibling worktree")
	}
	if gitStdout(t, "-C", b, "rev-parse", "--is-bare-repository") != "false" {
		t.Fatal("sibling worktree became bare")
	}
	if _, err := exec.Command("git", "-C", b, "status", "--porcelain").CombinedOutput(); err != nil {
		t.Fatalf("sibling git status failed: %v", err)
	}
	after, _ := os.ReadFile(commonExclude)
	if string(after) != string(before) || strings.Contains(string(after), "CLAUDE.md") {
		t.Fatalf("common info/exclude changed:\n%s", after)
	}
}

func TestInstallDoesNotLeakFromPlainRepoToSibling(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "plain")
	gitOK(t, "init", repo)
	gitCommitEmpty(t, repo)
	sib := filepath.Join(t.TempDir(), "sib")

	if err := Install(context.Background(), repo, []string{"CLAUDE.md"}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	infoExclude := filepath.Join(gitAbsDir(t, repo), "info", "exclude")
	if data, err := os.ReadFile(infoExclude); err == nil && strings.Contains(string(data), "CLAUDE.md") {
		t.Fatalf("wrote shared info/exclude:\n%s", data)
	}
	if gitExit(t, "-C", repo, "check-ignore", "-q", "--", "CLAUDE.md") != 0 {
		t.Fatal("plain repo did not ignore CLAUDE.md")
	}

	gitOK(t, "-C", repo, "worktree", "add", "--detach", sib, "HEAD")
	if gitExit(t, "-C", sib, "check-ignore", "-q", "--", "CLAUDE.md") == 0 &&
		strings.Contains(gitStdout(t, "-C", sib, "check-ignore", "-v", "--", "CLAUDE.md"), "multica-excludes") {
		t.Fatal("plain-repo exclude leaked to sibling worktree")
	}
	if err := os.WriteFile(filepath.Join(sib, "CLAUDE.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	status := gitStdout(t, "-C", sib, "status", "--porcelain")
	if !strings.Contains(status, "CLAUDE.md") && gitExit(t, "-C", sib, "check-ignore", "-q", "--", "CLAUDE.md") != 0 {
		t.Fatalf("sibling neither shows nor ignores CLAUDE.md:\n%s", status)
	}
}

func TestInstallKeepsGlobalExcludes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	global := filepath.Join(home, ".gitconfig")
	ignores := filepath.Join(home, "global-ignore")
	if err := os.WriteFile(ignores, []byte("kept-global.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(global, []byte("[core]\n\texcludesFile = "+ignores+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	repo := filepath.Join(t.TempDir(), "repo")
	gitOK(t, "init", repo)
	gitCommitEmpty(t, repo)
	if err := Install(context.Background(), repo, []string{"CLAUDE.md"}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	for _, path := range []string{"CLAUDE.md", "kept-global.txt"} {
		if gitExit(t, "-C", repo, "check-ignore", "-q", "--", path) != 0 {
			t.Fatalf("expected %s to be ignored", path)
		}
	}
	source := gitStdout(t, "-C", repo, "check-ignore", "-v", "--", "kept-global.txt")
	if !strings.Contains(source, "multica-excludes") {
		t.Fatalf("global pattern was not carried into the worktree excludes file: %s", source)
	}
}

func bareRepoWithCommit(t *testing.T) (string, func()) {
	t.Helper()
	root := t.TempDir()
	bare := filepath.Join(root, "cache.git")
	src := filepath.Join(root, "src")
	gitOK(t, "init", "--bare", bare)
	gitOK(t, "clone", bare, src)
	gitCommitEmpty(t, src)
	gitOK(t, "-C", src, "push", "-q", "origin", "HEAD:main")
	return bare, func() {}
}

func gitCommitEmpty(t *testing.T, repo string) {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "commit", "--allow-empty", "-m", "init")
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit: %s: %v", out, err)
	}
}

func writeRuntimeFiles(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude", "settings.local.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitAbsDir(t *testing.T, repo string) string {
	t.Helper()
	return gitStdout(t, "-C", repo, "rev-parse", "--absolute-git-dir")
}

func gitPath(t *testing.T, repo, path string) string {
	t.Helper()
	return gitStdout(t, "-C", repo, "rev-parse", "--path-format=absolute", "--git-path", path)
}

func gitOK(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %s: %v", strings.Join(args, " "), out, err)
	}
}

func gitStdout(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %s: %v", strings.Join(args, " "), out, err)
	}
	return strings.TrimSpace(string(out))
}

func gitExit(t *testing.T, args ...string) int {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	err := cmd.Run()
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errorsAsExit(err, &exit) {
		return exit.ExitCode()
	}
	t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	return -1
}

func errorsAsExit(err error, target **exec.ExitError) bool {
	exit, ok := err.(*exec.ExitError)
	if ok {
		*target = exit
	}
	return ok
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
