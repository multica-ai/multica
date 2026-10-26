package daemon

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCleanTaskArtifacts_PreservesCheckoutBoundaries(t *testing.T) {
	t.Parallel()

	for _, layout := range []string{"clone", "linked worktree"} {
		for _, location := range []struct {
			name  string
			repo  string
			outer bool
		}{
			{name: "matched root", repo: "workdir/target"},
			{name: "nested checkout", repo: "workdir/node_modules/dependency"},
			{name: "nested checkout inside another repo", repo: "workdir/outer/node_modules/dependency", outer: true},
		} {
			t.Run(layout+"/"+location.name, func(t *testing.T) {
				d := newGCTestDaemon(t, http.NewServeMux())
				root := t.TempDir()
				repo := filepath.Join(root, filepath.FromSlash(location.repo))
				if location.outer {
					outer := filepath.Join(root, "workdir", "outer")
					if err := os.MkdirAll(outer, 0o755); err != nil {
						t.Fatal(err)
					}
					runGitForGC(t, outer, "init")
				}
				source := repo
				if layout == "linked worktree" {
					source = t.TempDir()
				}
				file := filepath.Join(source, "src", "main.go")
				if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte("tracked source"), 0o644); err != nil {
					t.Fatal(err)
				}
				runGitForGC(t, source, "init")
				runGitForGC(t, source, "add", "src/main.go")
				runGitForGC(t, source, "-c", "commit.gpgsign=false", "commit", "-m", "tracked source")
				if layout == "linked worktree" {
					runGitForGC(t, source, "worktree", "add", "--detach", repo)
				}
				writeFile(t, filepath.Join(root, "workdir", ".turbo", "cache"), 8)

				removed, bytes, perPattern := d.cleanTaskArtifacts(context.Background(), root, []string{"target", "node_modules", ".turbo"})
				if removed != 1 || bytes != 8 || perPattern[".turbo"] != 1 {
					t.Fatalf("removed=%d bytes=%d patterns=%v, want only the unrelated cache removed", removed, bytes, perPattern)
				}
				if got, err := os.ReadFile(filepath.Join(repo, "src", "main.go")); err != nil || string(got) != "tracked source" {
					t.Fatalf("checkout source was removed: data=%q err=%v", got, err)
				}
				if _, err := os.Lstat(filepath.Join(repo, ".git")); err != nil {
					t.Fatalf("checkout metadata was removed: %v", err)
				}
			})
		}
	}
}

func TestCleanTaskArtifacts_UsesLiteralTrackedPaths(t *testing.T) {
	t.Parallel()

	for _, prefix := range []string{":(top)fixture", ":(exclude)fixture", "dist[1]", "star*fixture"} {
		t.Run(prefix, func(t *testing.T) {
			if runtime.GOOS == "windows" && strings.ContainsAny(prefix, ":*") {
				t.Skip("the fixture name is not valid on Windows")
			}
			d := newGCTestDaemon(t, http.NewServeMux())
			root := t.TempDir()
			repo := filepath.Join(root, "workdir", "repo")
			rel := filepath.Join(prefix, ".next", "server", "page.js")
			tracked := filepath.Join(repo, rel)
			if err := os.MkdirAll(filepath.Dir(tracked), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(tracked, []byte("tracked output"), 0o644); err != nil {
				t.Fatal(err)
			}
			runGitForGC(t, repo, "init")
			runGitForGC(t, repo, "--literal-pathspecs", "add", "--", filepath.ToSlash(rel))
			writeFile(t, filepath.Join(repo, "node_modules", "cache"), 8)

			removed, bytes, perPattern := d.cleanTaskArtifacts(context.Background(), root, []string{".next", "node_modules"})
			if removed != 1 || bytes != 8 || perPattern["node_modules"] != 1 {
				t.Fatalf("removed=%d bytes=%d patterns=%v, want only untracked node_modules removed", removed, bytes, perPattern)
			}
			if got, err := os.ReadFile(tracked); err != nil || string(got) != "tracked output" {
				t.Fatalf("literal tracked path was removed: data=%q err=%v", got, err)
			}
		})
	}
}

func TestCleanTaskArtifacts_PreservesInvalidGitMetadata(t *testing.T) {
	t.Parallel()

	d := newGCTestDaemon(t, http.NewServeMux())
	root := t.TempDir()
	repo := filepath.Join(root, "workdir", "repo")
	writeFile(t, filepath.Join(repo, ".next", "cache"), 8)
	if err := os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: missing"), 0o644); err != nil {
		t.Fatal(err)
	}
	if removed, _, _ := d.cleanTaskArtifacts(context.Background(), root, []string{".next"}); removed != 0 {
		t.Fatalf("removed=%d, want fail-closed retention", removed)
	}
}

func TestCleanTaskArtifacts_CancellationPreservesArtifacts(t *testing.T) {
	t.Parallel()

	d := newGCTestDaemon(t, http.NewServeMux())
	root := t.TempDir()
	artifact := filepath.Join(root, "workdir", "node_modules", "cache")
	writeFile(t, artifact, 8)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if removed, _, _ := d.cleanTaskArtifacts(ctx, root, []string{"node_modules"}); removed != 0 {
		t.Fatalf("removed=%d after cancellation, want 0", removed)
	}
	if _, err := os.Stat(artifact); err != nil {
		t.Fatalf("cancelled cleanup removed artifact: %v", err)
	}
}

func TestCleanTaskArtifacts_DebugLogsRetainedBytes(t *testing.T) {
	t.Parallel()

	d := newGCTestDaemon(t, http.NewServeMux())
	var logs bytes.Buffer
	d.logger = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	root := t.TempDir()
	repo := filepath.Join(root, "workdir", "repo")
	writeFile(t, filepath.Join(repo, ".next", "tracked"), 8)
	writeFile(t, filepath.Join(repo, ".next", "cache"), 16)
	runGitForGC(t, repo, "init")
	runGitForGC(t, repo, "add", ".next/tracked")
	if removed, _, _ := d.cleanTaskArtifacts(context.Background(), root, []string{".next"}); removed != 0 {
		t.Fatalf("removed=%d, want tracked directory retained", removed)
	}
	if log := logs.String(); !strings.Contains(log, `"level":"DEBUG"`) || !strings.Contains(log, `"retained_bytes":24`) {
		t.Fatalf("missing debug retention byte count: %s", log)
	}
}
