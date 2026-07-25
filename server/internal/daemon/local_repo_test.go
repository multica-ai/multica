package daemon

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNormalizeTaskReposCanonicalizesAndDeduplicatesLocalGitRepository(t *testing.T) {
	repo := t.TempDir()
	runLocalRepoGit(t, repo, "init")
	runLocalRepoGit(t, repo, "config", "user.email", "test@example.com")
	runLocalRepoGit(t, repo, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("local\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runLocalRepoGit(t, repo, "add", "README.md")
	runLocalRepoGit(t, repo, "commit", "-m", "local only")

	link := filepath.Join(t.TempDir(), "repo-link")
	if err := os.Symlink(repo, link); err != nil {
		t.Fatal(err)
	}
	repos, err := normalizeTaskRepos([]RepoData{
		{URL: (&url.URL{Scheme: "file", Path: repo}).String()},
		{URL: (&url.URL{Scheme: "file", Path: link}).String()},
	})
	if err != nil {
		t.Fatalf("normalizeTaskRepos: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("len(repos) = %d, want 1: %+v", len(repos), repos)
	}
	realRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	want := (&url.URL{Scheme: "file", Path: realRepo}).String()
	if repos[0].URL != want {
		t.Fatalf("URL = %q, want %q", repos[0].URL, want)
	}
}

func TestNormalizeRepoURLRejectsNonGitDirectory(t *testing.T) {
	raw := (&url.URL{Scheme: "file", Path: t.TempDir()}).String()
	if _, _, err := normalizeRepoURL(raw); err == nil {
		t.Fatal("expected non-Git directory to be rejected")
	}
}

func runLocalRepoGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmdArgs := append([]string{"-C", dir}, args...)
	if out, err := exec.Command("git", cmdArgs...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
}
