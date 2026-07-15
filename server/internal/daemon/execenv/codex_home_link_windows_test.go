//go:build windows

package execenv

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateDirLinkCreatesIndependentSnapshot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := filepath.Join(root, "owner-cache")
	dst := filepath.Join(root, "task-cache")
	if err := os.MkdirAll(filepath.Join(src, "plugin"), 0o755); err != nil {
		t.Fatalf("create source: %v", err)
	}
	sourceFile := filepath.Join(src, "plugin", "manifest.json")
	if err := os.WriteFile(sourceFile, []byte("owner-v1"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	if err := createDirLink(src, dst); err != nil {
		t.Fatalf("createDirLink: %v", err)
	}

	info, err := os.Lstat(dst)
	if err != nil {
		t.Fatalf("lstat snapshot: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("task cache must be a snapshot, not a link to owner state")
	}

	taskFile := filepath.Join(dst, "plugin", "manifest.json")
	data, err := os.ReadFile(taskFile)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if !bytes.Equal(data, []byte("owner-v1")) {
		t.Fatalf("snapshot content = %q", data)
	}

	if err := os.WriteFile(sourceFile, []byte("owner-v2"), 0o644); err != nil {
		t.Fatalf("update source: %v", err)
	}
	data, err = os.ReadFile(taskFile)
	if err != nil {
		t.Fatalf("read snapshot after owner update: %v", err)
	}
	if !bytes.Equal(data, []byte("owner-v1")) {
		t.Fatalf("task snapshot tracked owner mutation: %q", data)
	}

	if err := os.WriteFile(taskFile, []byte("task-write"), 0o644); err != nil {
		t.Fatalf("write task-local snapshot: %v", err)
	}
	ownerData, err := os.ReadFile(sourceFile)
	if err != nil {
		t.Fatalf("read owner source: %v", err)
	}
	if !bytes.Equal(ownerData, []byte("owner-v2")) {
		t.Fatalf("task write affected owner cache: %q", ownerData)
	}
}

func TestCreateDirLinkRejectsNestedDirectoryLink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := filepath.Join(root, "owner-cache")
	dst := filepath.Join(root, "task-cache")
	outside := filepath.Join(root, "owner-private")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("create source: %v", err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("create outside dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(src, "escape")); err != nil {
		t.Skipf("directory symlink unavailable on this Windows session: %v", err)
	}

	err := createDirLink(src, dst)
	if err == nil {
		t.Fatal("expected linked source entry to be rejected")
	}
	if !errors.Is(err, errUnsafeCodexPluginCacheEntry) {
		t.Fatalf("error = %v, want unsafe-entry classification", err)
	}
	if _, statErr := os.Lstat(dst); !os.IsNotExist(statErr) {
		t.Fatalf("failed snapshot was published, lstat error: %v", statErr)
	}
}

func TestCreateDirLinkRejectsOversizedSnapshotWithoutPublishing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := filepath.Join(root, "owner-cache")
	dst := filepath.Join(root, "task-cache")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("create source: %v", err)
	}
	oversized := make([]byte, codexPluginCacheMaxFileBytes+1)
	if err := os.WriteFile(filepath.Join(src, "oversized.bin"), oversized, 0o644); err != nil {
		t.Fatalf("write oversized source: %v", err)
	}

	err := createDirLink(src, dst)
	if err == nil {
		t.Fatal("expected oversized snapshot to be rejected")
	}
	if !errors.Is(err, errCodexPluginCacheLimit) {
		t.Fatalf("error = %v, want limit classification", err)
	}
	if _, statErr := os.Lstat(dst); !os.IsNotExist(statErr) {
		t.Fatalf("failed snapshot was published, lstat error: %v", statErr)
	}
}
