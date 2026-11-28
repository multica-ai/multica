package codechange

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newRepo makes a repo with one commit at dir and returns that commit.
func newRepo(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "init", "-q", "-b", "main")
	write(t, filepath.Join(dir, "keep.txt"), "one\ntwo\n")
	write(t, filepath.Join(dir, "gone.txt"), "bye\n")
	write(t, filepath.Join(dir, "old-name.txt"), strings.Repeat("same line\n", 20))
	write(t, filepath.Join(dir, ".gitignore"), "ignored.log\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "init")
	return run(t, dir, "rev-parse", "HEAD")
}

func byPath(files []File) map[string]File {
	m := map[string]File{}
	for _, f := range files {
		m[f.Path] = f
	}
	return m
}

func readIndex(t *testing.T, repo string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repo, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSnapshotCoversCommittedUncommittedAndUntracked(t *testing.T) {
	ctx := context.Background()
	work := t.TempDir()
	repoDir := filepath.Join(work, "app")
	base := newRepo(t, repoDir)
	baselines := NewBaselines()
	baselines.Capture(ctx, work, false)

	// A commit the agent made during the run...
	write(t, filepath.Join(repoDir, "keep.txt"), "one\ntwo\nthree\n")
	run(t, repoDir, "commit", "-q", "-am", "agent commit")
	// ...plus an uncommitted edit, a delete, a staged rename, an untracked
	// file and an ignored one.
	write(t, filepath.Join(repoDir, "keep.txt"), "uno\ntwo\nthree\n")
	os.Remove(filepath.Join(repoDir, "gone.txt"))
	run(t, repoDir, "mv", "old-name.txt", "new-name.txt")
	write(t, filepath.Join(repoDir, "src", "new.go"), "package src\n")
	write(t, filepath.Join(repoDir, "ignored.log"), "noise\n")
	statusBefore := run(t, repoDir, "status", "--porcelain")
	indexBefore := readIndex(t, repoDir)

	res := Snapshot(ctx, work, baselines)
	if res.Checkouts != 1 || len(res.Repos) != 1 || res.ScanCapped {
		t.Fatalf("result = %+v, want one changed checkout", res)
	}
	r := res.Repos[0]
	if r.Path != "app" || r.Base != base || r.Branch != "main" || r.Head == base {
		t.Fatalf("repo = %+v", r)
	}
	files := byPath(r.Files)
	if f := files["keep.txt"]; f.Status != "modified" || f.Additions != 2 || f.Deletions != 1 {
		t.Errorf("keep.txt = %+v (committed + uncommitted edits against the run's base)", f)
	}
	if files["gone.txt"].Status != "deleted" {
		t.Errorf("gone.txt = %+v", files["gone.txt"])
	}
	if f := files["new-name.txt"]; f.Status != "renamed" || f.OldPath != "old-name.txt" {
		t.Errorf("new-name.txt = %+v", f)
	}
	if files["src/new.go"].Status != "added" {
		t.Errorf("untracked src/new.go = %+v", files["src/new.go"])
	}
	if _, ok := files["ignored.log"]; ok {
		t.Error("ignored file must not be reported")
	}
	if !strings.Contains(r.Patch, "+package src") || !strings.Contains(r.Patch, "-one") {
		t.Errorf("patch missing content:\n%s", r.Patch)
	}
	if after := run(t, repoDir, "status", "--porcelain"); after != statusBefore {
		t.Errorf("snapshot changed the checkout's state:\nbefore %q\nafter  %q", statusBefore, after)
	}
	if !bytes.Equal(readIndex(t, repoDir), indexBefore) {
		t.Error("snapshot rewrote the checkout's index")
	}
}

// Work already in the tree when the run started (the user's uncommitted
// edits in place, or an earlier turn's leftovers) is not the run's.
func TestWorkingTreeBaselineLeavesPreexistingWorkOut(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	newRepo(t, repo)
	write(t, filepath.Join(repo, "keep.txt"), "user edit\n")
	write(t, filepath.Join(repo, "draft.md"), "user's untracked notes\n")
	baselines := NewBaselines()
	baselines.Capture(ctx, repo, true)

	if got := Snapshot(ctx, repo, baselines); len(got.Repos) != 0 || got.Checkouts != 1 {
		t.Fatalf("untouched dirty tree reported: %+v", got)
	}

	write(t, filepath.Join(repo, "agent.go"), "package agent\n")
	write(t, filepath.Join(repo, "keep.txt"), "user edit\nagent line\n")
	got := Snapshot(ctx, repo, baselines)
	if len(got.Repos) != 1 {
		t.Fatalf("repos = %+v", got.Repos)
	}
	files := byPath(got.Repos[0].Files)
	if len(files) != 2 || files["agent.go"].Status != "added" || files["keep.txt"].Additions != 1 || files["keep.txt"].Deletions != 0 {
		t.Fatalf("files = %+v, want only the run's own changes", got.Repos[0].Files)
	}

	// A HEAD-only baseline (task worktree) measures from the commit instead.
	headOnly := NewBaselines()
	headOnly.Capture(ctx, repo, false)
	if files := byPath(Snapshot(ctx, repo, headOnly).Repos[0].Files); files["draft.md"].Status != "added" {
		t.Fatalf("HEAD-only baseline files = %+v", files)
	}
}

// The first baseline recorded for a checkout wins: a later capture (the
// repo-checkout handler, the post-Prepare pass) must not move it forward.
func TestCaptureKeepsTheFirstBaseline(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	base := newRepo(t, repo)
	baselines := NewBaselines()
	baselines.Capture(ctx, repo, true)
	write(t, filepath.Join(repo, "keep.txt"), "changed\n")
	run(t, repo, "commit", "-q", "-am", "agent commit")
	baselines.Capture(ctx, repo, true)

	got := Snapshot(ctx, repo, baselines)
	if len(got.Repos) != 1 || got.Repos[0].Base != base || len(got.Repos[0].Files) != 1 {
		t.Fatalf("repos = %+v, want measured from %s", got.Repos, base)
	}
}

func TestSnapshotWithoutBaselineUsesUpstreamForkPoint(t *testing.T) {
	ctx := context.Background()
	origin := filepath.Join(t.TempDir(), "origin")
	newRepo(t, origin)
	work := t.TempDir()
	clone := filepath.Join(work, "repo")
	run(t, work, "clone", "-q", origin, clone)
	run(t, clone, "checkout", "-q", "-b", "agent/x", "--track", "origin/main")
	write(t, filepath.Join(clone, "keep.txt"), "changed\n")
	run(t, clone, "commit", "-q", "-am", "agent work")

	got := Snapshot(ctx, work, NewBaselines())
	if len(got.Repos) != 1 || len(got.Repos[0].Files) != 1 || got.Repos[0].Files[0].Path != "keep.txt" {
		t.Fatalf("repos = %+v", got.Repos)
	}
}

func TestSnapshotOfAnUnbornBranchListsEveryFile(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	run(t, repo, "init", "-q", "-b", "main")
	baselines := NewBaselines()
	baselines.Capture(ctx, repo, false)
	write(t, filepath.Join(repo, "a.txt"), "a\n")
	write(t, filepath.Join(repo, "b", "c.txt"), "c\n")

	got := Snapshot(ctx, repo, baselines)
	if len(got.Repos) != 1 || got.Repos[0].Base != "" {
		t.Fatalf("repos = %+v", got.Repos)
	}
	files := byPath(got.Repos[0].Files)
	if files["a.txt"].Status != "added" || files["b/c.txt"].Status != "added" {
		t.Fatalf("files = %+v", got.Repos[0].Files)
	}
}

func TestSnapshotSkipsCleanReposAndHandlesTheRootItself(t *testing.T) {
	ctx := context.Background()
	work := t.TempDir()
	newRepo(t, work)
	baselines := NewBaselines()
	baselines.Capture(ctx, work, false)
	if got := Snapshot(ctx, work, baselines); len(got.Repos) != 0 || got.Checkouts != 1 {
		t.Fatalf("clean repo = %+v", got)
	}
	write(t, filepath.Join(work, "keep.txt"), "edited\n")
	got := Snapshot(ctx, work, baselines)
	if len(got.Repos) != 1 || got.Repos[0].Path != "." {
		t.Fatalf("root as repo = %+v", got.Repos)
	}
}

func TestSnapshotFindsNoCheckoutInAPlainDirectory(t *testing.T) {
	work := t.TempDir()
	write(t, filepath.Join(work, "notes.txt"), "not a repo\n")
	if got := Snapshot(context.Background(), work, NewBaselines()); got.Checkouts != 0 || len(got.Repos) != 0 {
		t.Fatalf("plain directory = %+v", got)
	}
}

// A working directory inside somebody else's checkout (a home directory under
// version control) must not pull that checkout in.
func TestFindIgnoresAParentCheckout(t *testing.T) {
	parent := t.TempDir()
	newRepo(t, parent)
	work := filepath.Join(parent, "workspaces", "task")
	write(t, filepath.Join(work, "file.txt"), "x\n")
	if got := Find(context.Background(), work); len(got) != 0 {
		t.Fatalf("Find = %v, want no checkout", got)
	}
}

func TestSnapshotIgnoresDaemonRuntimeDirectories(t *testing.T) {
	ctx := context.Background()
	work := t.TempDir()
	newRepo(t, work)
	baselines := NewBaselines()
	baselines.Capture(ctx, work, false)
	write(t, filepath.Join(work, ".multica", "context.json"), "{}\n")
	write(t, filepath.Join(work, ".agent_context", "issue.md"), "brief\n")
	if got := Snapshot(ctx, work, baselines); len(got.Repos) != 0 {
		t.Fatalf("runtime files reported as changes: %+v", got.Repos)
	}
}

func TestSnapshotReportsChangesInsideSubmodules(t *testing.T) {
	ctx := context.Background()
	lib := filepath.Join(t.TempDir(), "lib")
	newRepo(t, lib)
	work := t.TempDir()
	super := filepath.Join(work, "super")
	newRepo(t, super)
	run(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", lib, "libs/lib")
	run(t, super, "commit", "-q", "-m", "add submodule")
	baselines := NewBaselines()
	baselines.Capture(ctx, work, true)
	sub := filepath.Join(super, "libs", "lib")
	if _, ok := baselines.get(sub); !ok {
		t.Fatal("submodule baseline missing")
	}

	// The agent edits a file inside the submodule only.
	write(t, filepath.Join(sub, "keep.txt"), "one\ntwo\nedited\n")
	got := Snapshot(ctx, work, baselines)
	if got.Checkouts != 2 || len(got.Repos) != 1 || got.Repos[0].Path != "super/libs/lib" {
		t.Fatalf("result = %+v, want just the submodule (its dirty pointer in the superproject is not a change)", got)
	}
	if f := got.Repos[0].Files; len(f) != 1 || f[0].Path != "keep.txt" || f[0].Additions != 1 {
		t.Fatalf("submodule files = %+v", f)
	}
}

func TestSnapshotCapsFilesAndPatch(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	newRepo(t, repo)
	baselines := NewBaselines()
	baselines.Capture(ctx, repo, false)
	for i := 0; i < MaxFiles+10; i++ {
		write(t, filepath.Join(repo, "gen", fmt.Sprintf("f%04d.txt", i)), "x\n")
	}
	// One file alone is larger than the patch cap.
	write(t, filepath.Join(repo, "big.txt"), strings.Repeat("a long generated line of text\n", MaxPatchBytes/16))

	got := Snapshot(ctx, repo, baselines)
	if len(got.Repos) != 1 {
		t.Fatalf("repos = %+v", got.Repos)
	}
	r := got.Repos[0]
	if len(r.Files) != MaxFiles || !r.FilesTruncated {
		t.Fatalf("files = %d truncated=%v, want %d and truncated", len(r.Files), r.FilesTruncated, MaxFiles)
	}
	if len(r.Patch) > MaxPatchBytes || !r.PatchTruncated {
		t.Fatalf("patch = %d bytes truncated=%v", len(r.Patch), r.PatchTruncated)
	}
	if r.Patch != "" && !strings.HasPrefix(r.Patch, "diff --git ") {
		t.Fatalf("patch not cut at a file boundary: %q", r.Patch[:40])
	}
}

func TestCapPatchCutsAtFileBoundary(t *testing.T) {
	patch := "diff --git a/a b/a\n+aaaa\ndiff --git a/b b/b\n+bbbbbbbbbbbbbbbbbbbb\n"
	got, truncated := capPatch(patch, 40)
	if !truncated || got != "diff --git a/a b/a\n+aaaa\n" {
		t.Fatalf("capPatch = %q, %v", got, truncated)
	}
	if got, truncated := capPatch(patch, 1000); truncated || got != patch {
		t.Fatalf("under limit changed: %q %v", got, truncated)
	}
}

func TestFitKeepsAReportWithinTheUploadBudget(t *testing.T) {
	big := strings.Repeat("+line of a large generated file\n", 20000) // ~640 KiB
	var repos []Repo
	for i := 0; i < 30; i++ {
		files := make([]File, 900)
		for j := range files {
			files[j] = File{Path: strings.Repeat("d/", 20) + "file.txt", Status: "added", Additions: 1}
		}
		repos = append(repos, Repo{Path: "repo", Base: "abc", Files: files, Patch: "diff --git a/x b/x\n" + big})
	}
	fit, dropped := Fit(repos)
	if !dropped || len(fit) != MaxReportRepos {
		t.Fatalf("repos = %d dropped=%v, want capped at %d", len(fit), dropped, MaxReportRepos)
	}
	if size := encodedSize(fit); size > MaxReportBytes {
		t.Fatalf("encoded size %d exceeds budget %d", size, MaxReportBytes)
	}
	for _, r := range fit {
		if len(r.Files) == 0 {
			t.Fatal("a repo lost its whole file list")
		}
		if r.Patch == "" && !r.PatchTruncated {
			t.Fatal("a dropped patch must be marked truncated")
		}
	}
}

func TestFitLeavesASmallReportAlone(t *testing.T) {
	repos := []Repo{{Path: ".", Base: "abc", Files: []File{{Path: "a", Status: "modified"}}, Patch: "diff --git a/a b/a\n+x\n"}}
	fit, dropped := Fit(repos)
	if dropped || fit[0].Patch != repos[0].Patch || fit[0].PatchTruncated || fit[0].FilesTruncated {
		t.Fatalf("small report changed: %+v %v", fit, dropped)
	}
	if _, dropped := Fit(make([]Repo, MaxReportRepos)); dropped {
		t.Fatal("a report at the repo cap dropped nothing")
	}
}
