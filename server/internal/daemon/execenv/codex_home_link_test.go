package execenv

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func assertDirLinkTarget(t *testing.T, dst, src string) {
	t.Helper()

	target, err := os.Readlink(dst)
	if err == nil {
		if target != src {
			t.Errorf("link target = %q, want %q", target, src)
		}
		return
	}
	if runtime.GOOS == "windows" {
		if fi, statErr := os.Stat(dst); statErr != nil || !fi.IsDir() {
			t.Fatalf("expected accessible linked directory, stat err: %v", statErr)
		}
		return
	}
	t.Fatalf("Readlink: %v", err)
}

func TestEnsureDirSymlink_CreatesLink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	src := filepath.Join(dir, "shared-sessions")
	dst := filepath.Join(dir, "task-sessions")

	if err := ensureDirSymlink(src, dst); err != nil {
		t.Fatalf("ensureDirSymlink: %v", err)
	}

	// Source dir should be created.
	if fi, err := os.Stat(src); err != nil || !fi.IsDir() {
		t.Fatal("expected source directory to be created")
	}

	// dst should resolve to src, or be an accessible junction on Windows.
	assertDirLinkTarget(t, dst, src)
}

func TestEnsureDirSymlink_Idempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	src := filepath.Join(dir, "shared")
	dst := filepath.Join(dir, "link")

	if err := ensureDirSymlink(src, dst); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := ensureDirSymlink(src, dst); err != nil {
		t.Fatalf("second call: %v", err)
	}

	assertDirLinkTarget(t, dst, src)
}

func TestEnsureDirSymlink_ReplacesWrongTarget(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	oldSrc := filepath.Join(dir, "old")
	newSrc := filepath.Join(dir, "new")
	dst := filepath.Join(dir, "link")

	os.MkdirAll(oldSrc, 0o755)
	if err := os.Symlink(oldSrc, dst); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("directory symlink unavailable on this Windows session: %v", err)
		}
		t.Fatalf("seed wrong symlink: %v", err)
	}

	if err := ensureDirSymlink(newSrc, dst); err != nil {
		t.Fatalf("ensureDirSymlink: %v", err)
	}

	assertDirLinkTarget(t, dst, newSrc)
}

func TestEnsureDirSymlink_SkipsExistingRegularDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	src := filepath.Join(dir, "shared")
	dst := filepath.Join(dir, "existing")
	os.MkdirAll(dst, 0o755)

	if err := ensureDirSymlink(src, dst); err != nil {
		t.Fatalf("ensureDirSymlink: %v", err)
	}

	// Should not be replaced — still a regular directory.
	fi, _ := os.Lstat(dst)
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Error("expected regular dir to be preserved, not replaced with symlink")
	}
}

func TestEnsureSymlink_SkipsWhenSourceMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	src := filepath.Join(dir, "missing.json")
	dst := filepath.Join(dir, "link.json")

	if err := ensureSymlink(src, dst); err != nil {
		t.Fatalf("ensureSymlink: %v", err)
	}

	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Error("expected dst to not be created when src is missing")
	}
}

func TestEnsureSymlink_SkipsExistingRegularFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	src := filepath.Join(dir, "source.json")
	dst := filepath.Join(dir, "existing.json")
	os.WriteFile(src, []byte("new"), 0o644)
	os.WriteFile(dst, []byte("old"), 0o644)

	if err := ensureSymlink(src, dst); err != nil {
		t.Fatalf("ensureSymlink: %v", err)
	}

	// Should not be replaced.
	data, _ := os.ReadFile(dst)
	if string(data) != "old" {
		t.Errorf("existing file content changed to %q", data)
	}
}

func TestCreateDirLink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "test.txt"), []byte("hello"), 0o644)

	if err := createDirLink(src, dst); err != nil {
		t.Fatalf("createDirLink: %v", err)
	}

	// Should be able to read files through the link.
	data, err := os.ReadFile(filepath.Join(dst, "test.txt"))
	if err != nil {
		t.Fatalf("read through link: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("content = %q, want %q", data, "hello")
	}
}

func TestCreateFileLink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	src := filepath.Join(dir, "source.json")
	dst := filepath.Join(dir, "link.json")
	os.WriteFile(src, []byte(`{"key":"value"}`), 0o644)

	if err := createFileLink(src, dst); err != nil {
		t.Fatalf("createFileLink: %v", err)
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read link: %v", err)
	}
	if string(data) != `{"key":"value"}` {
		t.Errorf("content = %q", data)
	}
}

func TestCopyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	os.WriteFile(src, []byte("content"), 0o644)

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}

	data, _ := os.ReadFile(dst)
	if string(data) != "content" {
		t.Errorf("content = %q", data)
	}

	// Verify it's a copy, not a symlink.
	fi, _ := os.Lstat(dst)
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Error("expected regular file, not symlink")
	}
}
