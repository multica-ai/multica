package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverLocalReposFindsDirectGitRepos(t *testing.T) {
	root := t.TempDir()
	alpha := filepath.Join(root, "alpha")
	beta := filepath.Join(root, "Beta")
	hidden := filepath.Join(root, ".hidden")
	notRepo := filepath.Join(root, "not-repo")

	for _, dir := range []string{
		filepath.Join(alpha, ".git"),
		filepath.Join(beta, ".git"),
		filepath.Join(hidden, ".git"),
		notRepo,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	repos := discoverLocalRepos([]string{root})

	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d: %#v", len(repos), repos)
	}
	if repos[0].Name != "alpha" || repos[1].Name != "Beta" {
		t.Fatalf("repos sorted/filtering mismatch: %#v", repos)
	}
	if !localRepoAllowed([]string{root}, alpha) {
		t.Fatal("expected discovered repo to be allowed")
	}
	if localRepoAllowed([]string{root}, root) {
		t.Fatal("root itself should not be accepted as a selected repo")
	}
	if localRepoAllowed([]string{root}, filepath.Join(alpha, "subdir")) {
		t.Fatal("paths inside a repo should not bypass exact repo selection")
	}
}

func TestRepoNativeCodexPathOnlyForCodexIssueTasks(t *testing.T) {
	task := Task{
		Context: json.RawMessage(`{"type":"issue_task","codex_repo_path":" /Users/example/projects/prism "}`),
	}

	if got := repoNativeCodexPath(task, "codex"); got != "/Users/example/projects/prism" {
		t.Fatalf("repoNativeCodexPath() = %q", got)
	}
	if got := repoNativeCodexPath(task, "claude"); got != "" {
		t.Fatalf("non-codex provider should ignore repo path, got %q", got)
	}

	task.Context = json.RawMessage(`{"type":"other","codex_repo_path":"/Users/example/projects/prism"}`)
	if got := repoNativeCodexPath(task, "codex"); got != "" {
		t.Fatalf("non-issue task should ignore repo path, got %q", got)
	}
}
