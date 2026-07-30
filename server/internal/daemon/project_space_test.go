package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func projectSpaceTask(workspaceID, projectID string) Task {
	return Task{
		WorkspaceID: workspaceID,
		ProjectID:   projectID,
		ProjectResources: []ProjectResourceData{{
			ResourceType: "project_space",
			ResourceRef:  json.RawMessage(`{"version":1}`),
		}},
	}
}

func TestTaskProjectSpaceReturnsExactWritableProject(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "workspaces", "workspace-1", "projects", "project-1")
	if err := os.MkdirAll(filepath.Join(projectDir, ".ai"), 0o750); err != nil {
		t.Fatal(err)
	}
	d := &Daemon{cfg: Config{ProjectSpaceRoot: root}}
	got, err := d.taskProjectSpace(projectSpaceTask("workspace-1", "project-1"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("taskProjectSpace() = %q, want %q", got, want)
	}
}

func TestTaskProjectSpaceFailsClosed(t *testing.T) {
	root := t.TempDir()
	d := &Daemon{cfg: Config{ProjectSpaceRoot: root}}
	if got, err := d.taskProjectSpace(Task{
		WorkspaceID: "workspace-1",
		ProjectID:   "project-1",
	}); err != nil || got != "" {
		t.Fatalf("task without resource = %q, %v", got, err)
	}
	if _, err := d.taskProjectSpace(projectSpaceTask("workspace-1", "../project-1")); err == nil {
		t.Fatal("expected invalid project id to be rejected")
	}
	if _, err := d.taskProjectSpace(projectSpaceTask("workspace-1", "missing")); err == nil {
		t.Fatal("expected missing project space to be rejected")
	}
}

func TestTaskProjectSpaceRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	parent := filepath.Join(root, "workspaces", "workspace-1", "projects")
	if err := os.MkdirAll(parent, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(parent, "project-1")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	d := &Daemon{cfg: Config{ProjectSpaceRoot: root}}
	if _, err := d.taskProjectSpace(projectSpaceTask("workspace-1", "project-1")); err == nil {
		t.Fatal("expected project symlink escape to be rejected")
	}
}
