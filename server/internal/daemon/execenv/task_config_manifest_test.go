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
