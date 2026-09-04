package execenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTaskConfigManifestReservationAndTargetedCleanup(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "workdir")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(work, "deploy", "terraform", "backend.hcl")
	temp := target + ".tmp"
	if err := RegisterSidecarFiles(root, target, temp); err != nil {
		t.Fatal(err)
	}
	if !SidecarFileRegistered(root, target) {
		t.Fatal("reserved target not visible in manifest")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(temp, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CleanupSidecarFiles(root, target, temp); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("target survived cleanup: %v", err)
	}
	if _, err := os.Stat(temp); !os.IsNotExist(err) {
		t.Fatalf("temporary file survived cleanup: %v", err)
	}
}

func TestCleanupSidecarManifestsReclaimsInterruptedTask(t *testing.T) {
	root := t.TempDir()
	envRoot := filepath.Join(root, "workspace", "task")
	work := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(filepath.Join(work, "deploy"), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(work, "deploy", "backend.hcl")
	if err := RegisterSidecarFiles(envRoot, target); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	cleaned, err := CleanupSidecarManifests(root)
	if err != nil {
		t.Fatal(err)
	}
	if cleaned != 1 {
		t.Fatalf("cleaned manifests = %d, want 1", cleaned)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("interrupted target survived restart cleanup: %v", err)
	}
}

func TestCleanupTaskConfigManifestsRejectsTamperedAbsolutePaths(t *testing.T) {
	root := t.TempDir()
	envRoot := filepath.Join(root, "workspace", "task")
	work := filepath.Join(envRoot, "workdir")
	outside := filepath.Join(root, "outside.hcl")
	if err := os.MkdirAll(envRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("must survive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(envRoot, taskConfigIntentFile), []byte(`{"task_id":"task-1","work_dir":"`+work+`","paths":["../../outside.hcl"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if cleaned, err := CleanupTaskConfigManifests(root); cleaned != 1 || err == nil {
		t.Fatalf("tampered manifest cleanup = cleaned %d, err %v; want one rejected manifest", cleaned, err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside file was affected: %v", err)
	}
}

func TestCleanupTaskConfigManifestsRejectsTamperedWorkDirOutsideEnvRoot(t *testing.T) {
	root := t.TempDir()
	envRoot := filepath.Join(root, "workspace", "task")
	outsideWork := filepath.Join(root, "outside-work")
	outsideFile := filepath.Join(outsideWork, "backend.hcl")
	if err := os.MkdirAll(outsideWork, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outsideFile, []byte("must survive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(envRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(envRoot, taskConfigIntentFile), []byte(`{"task_id":"task-1","work_dir":"`+outsideWork+`","paths":["backend.hcl"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CleanupTaskConfigManifests(root); err == nil {
		t.Fatal("tampered workdir intent was accepted")
	}
	if _, err := os.Stat(outsideFile); err != nil {
		t.Fatalf("outside file was affected: %v", err)
	}
}

func TestCleanupTaskConfigManifestsRejectsTamperedSiblingWorkDir(t *testing.T) {
	root := t.TempDir()
	envRoot := filepath.Join(root, "workspace", "task")
	siblingWork := filepath.Join(envRoot, "other-work")
	siblingFile := filepath.Join(siblingWork, "backend.hcl")
	if err := os.MkdirAll(siblingWork, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(siblingFile, []byte("must survive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(envRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(envRoot, taskConfigIntentFile), []byte(`{"task_id":"task-1","work_dir":"other-work","paths":["backend.hcl"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CleanupTaskConfigManifests(root); err == nil {
		t.Fatal("tampered sibling workdir intent was accepted")
	}
	if _, err := os.Stat(siblingFile); err != nil {
		t.Fatalf("sibling file was affected: %v", err)
	}
}

func TestTaskConfigIntentSupportsDaemonWorktree(t *testing.T) {
	root := t.TempDir()
	worktree := filepath.Join(root, "worktree")
	if err := os.MkdirAll(worktree, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(worktree, "deploy", "terraform", "backend.hcl")
	if err := RegisterTaskConfigIntent(root, "task-1", worktree, target); err != nil {
		t.Fatalf("RegisterTaskConfigIntent(worktree): %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := CleanupTaskConfigIntent(root)
	if err != nil {
		t.Fatalf("CleanupTaskConfigIntent(worktree): %v", err)
	}
	if len(paths) != 1 || paths[0] != target {
		t.Fatalf("cleanup paths = %#v, want %q", paths, target)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("worktree config survived cleanup: %v", err)
	}
}
