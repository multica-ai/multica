package gitops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test User",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test User",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %s: %v", args, out, err)
	}
	return strings.TrimSpace(string(out))
}

func createSourceRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "initial commit")
	return dir
}

func TestPublishWorkspacePushesDirtyRepo(t *testing.T) {
	source := createSourceRepo(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, source, "clone", "--bare", source, remote)

	workspaceRoot := t.TempDir()
	repoPath := filepath.Join(workspaceRoot, "repo")
	runGit(t, workspaceRoot, "clone", remote, repoPath)

	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("hello\nchanged\n"), 0o644); err != nil {
		t.Fatalf("write dirty file: %v", err)
	}

	results, err := PublishWorkspace(workspaceRoot, PublishOptions{
		Remote:        "origin",
		CommitMessage: "multica auto publish test",
	})
	if err != nil {
		t.Fatalf("PublishWorkspace failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 publish result, got %d", len(results))
	}
	if !results[0].Committed {
		t.Fatal("expected dirty repo to be committed")
	}
	if !results[0].Pushed {
		t.Fatal("expected repo to be pushed")
	}

	remoteHead := runGit(t, remote, "rev-parse", "refs/heads/main")
	localHead := runGit(t, repoPath, "rev-parse", "HEAD")
	if remoteHead != localHead {
		t.Fatalf("remote head %s does not match local head %s", remoteHead, localHead)
	}

	log := runGit(t, repoPath, "log", "-1", "--pretty=%s")
	if log != "multica auto publish test" {
		t.Fatalf("unexpected commit message %q", log)
	}
}

func TestDiscoverPublishRootsFindsChildRepo(t *testing.T) {
	workspaceRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(workspaceRoot, "notes"), 0o755); err != nil {
		t.Fatalf("mkdir notes: %v", err)
	}

	source := createSourceRepo(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, source, "clone", "--bare", source, remote)
	runGit(t, workspaceRoot, "clone", remote, filepath.Join(workspaceRoot, "repo"))

	roots, err := discoverPublishRoots(workspaceRoot)
	if err != nil {
		t.Fatalf("discoverPublishRoots failed: %v", err)
	}
	if len(roots) != 1 {
		t.Fatalf("expected 1 root, got %d", len(roots))
	}
	if filepath.Base(roots[0]) != "repo" {
		t.Fatalf("unexpected repo root: %s", roots[0])
	}
}
