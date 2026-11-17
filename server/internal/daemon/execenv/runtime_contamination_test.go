package execenv

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestRuntimeContaminationChecksIntermediateCommits(t *testing.T) {
	repo := newTestRepo(t)
	wt := prepareForTest(t, repo)
	defer wt.Discard(worktreeTestLogger())
	path := filepath.Join(wt.Path, ".provider", "skills", "fixture.md")
	manifest := &sidecarManifest{}
	if err := recordMkdirAll(filepath.Dir(path), 0755, manifest); err != nil {
		t.Fatal(err)
	}
	if err := recordWriteFile(path, []byte("Generated skill fixture"), 0644, manifest); err != nil {
		t.Fatal(err)
	}
	wt.RuntimeSidecarHashes = manifest.Hashes
	if _, err := InjectRuntimeConfig(wt.WorkDir, "claude", TaskContextForEnv{}); err != nil {
		t.Fatal(err)
	}
	gitRun(t, wt.Path, "add", "-A")
	gitRun(t, wt.Path, "commit", "-m", "accidental runtime commit")
	badCommit := gitRun(t, wt.Path, "rev-parse", "HEAD")
	if err := CleanupRuntimeConfig(wt.WorkDir, "claude"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(wt.Path, ".provider")); err != nil {
		t.Fatal(err)
	}
	gitRun(t, wt.Path, "add", "-A")
	gitRun(t, wt.Path, "commit", "-m", "remove runtime files")
	head := gitRun(t, wt.Path, "rev-parse", "HEAD")
	out := finalizeOK(t, wt)
	if out.RuntimeScanError != "" {
		t.Fatal(out.RuntimeScanError)
	}
	for _, path := range []string{"CLAUDE.md", ".provider/skills/fixture.md"} {
		if !slices.Contains(out.RuntimeWarnings, RuntimeContamination{Commit: badCommit, Path: path}) {
			t.Errorf("missed intermediate pollution %s: %+v", path, out.RuntimeWarnings)
		}
	}
	if got := gitRun(t, repo, "rev-parse", out.Branch); got != head {
		t.Fatal("detector rewrote history")
	}
}

func TestRuntimeContaminationPreservesLegitimateSidecarNames(t *testing.T) {
	repo := newTestRepo(t)
	wt := prepareForTest(t, repo)
	defer wt.Discard(worktreeTestLogger())
	path := filepath.Join(wt.Path, ".multica", "project", "resources.json")
	manifest := &sidecarManifest{}
	if err := recordMkdirAll(filepath.Dir(path), 0755, manifest); err != nil {
		t.Fatal(err)
	}
	if err := recordWriteFile(path, []byte(`{"project_id":"runtime"}`), 0644, manifest); err != nil {
		t.Fatal(err)
	}
	wt.RuntimeSidecarHashes = manifest.Hashes
	writeFile(t, path, `{"project_id":"legitimate project content"}`)
	gitRun(t, wt.Path, "add", "-A")
	gitRun(t, wt.Path, "commit", "-m", "intentional project config")
	out := finalizeOK(t, wt)
	if out.RuntimeScanError != "" || len(out.RuntimeWarnings) > 0 {
		t.Fatalf("legitimate content flagged: %+v", out)
	}
}

func TestRuntimeContaminationHandlesLargeBlobsAndSubmodules(t *testing.T) {
	repo := newTestRepo(t)
	wt := prepareForTest(t, repo)
	defer wt.Discard(worktreeTestLogger())
	writeFile(t, filepath.Join(wt.Path, "large.dat"), strings.Repeat("\x00", 100000)+"\r\n"+runtimeMarkerBegin+"\r\n")
	writeFile(t, filepath.Join(wt.Path, "legitimate.go"), "const marker = `"+runtimeMarkerBegin+"`\n")
	gitRun(t, wt.Path, "add", "-A")
	gitRun(t, wt.Path, "update-index", "--add", "--cacheinfo", "160000,"+wt.BaseCommit+",submodule")
	gitRun(t, wt.Path, "commit", "-m", "blob and submodule fixtures")
	tip := gitRun(t, wt.Path, "rev-parse", "HEAD")
	warnings, err := wt.runtimeContamination(tip)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(warnings, []RuntimeContamination{{Commit: tip, Path: "large.dat"}}) {
		t.Fatalf("unexpected findings: %+v", warnings)
	}
}
