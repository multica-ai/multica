package execenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureSpotlightExclusion(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspaces")
	if err := EnsureSpotlightExclusion(root); err != nil {
		t.Fatalf("EnsureSpotlightExclusion: %v", err)
	}

	marker := filepath.Join(root, SpotlightExclusionMarker)
	info, err := os.Stat(marker)
	if err != nil {
		t.Fatalf("stat marker: %v", err)
	}
	if info.IsDir() || info.Size() != 0 {
		t.Fatalf("marker info = dir:%v size:%d, want empty file", info.IsDir(), info.Size())
	}

	if err := EnsureSpotlightExclusion(root); err != nil {
		t.Fatalf("idempotent EnsureSpotlightExclusion: %v", err)
	}
}

func TestPrepare_WritesSpotlightExclusion(t *testing.T) {
	root := t.TempDir()
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: root,
		WorkspaceID:    "ws-spotlight-prepare",
		TaskID:         "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		Task:           TaskContextForEnv{IssueID: "issue-spotlight-prepare"},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	defer env.Cleanup(true)

	if _, err := os.Stat(filepath.Join(root, SpotlightExclusionMarker)); err != nil {
		t.Fatalf("Prepare did not create Spotlight marker: %v", err)
	}
}

func TestReuse_SelfHealsSpotlightExclusion(t *testing.T) {
	root := t.TempDir()
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: root,
		WorkspaceID:    "ws-spotlight-reuse",
		TaskID:         "bbbbbbbb-cccc-dddd-eeee-ffffffffffff",
		Task:           TaskContextForEnv{IssueID: "issue-spotlight-reuse"},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	defer env.Cleanup(true)

	marker := filepath.Join(root, SpotlightExclusionMarker)
	if err := os.Remove(marker); err != nil {
		t.Fatalf("remove marker: %v", err)
	}
	if reused := Reuse(ReuseParams{
		WorkspacesRoot: root,
		WorkDir:        env.WorkDir,
		Task:           TaskContextForEnv{IssueID: "issue-spotlight-reuse"},
	}, testLogger()); reused == nil {
		t.Fatal("Reuse returned nil")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("Reuse did not restore Spotlight marker: %v", err)
	}
}
