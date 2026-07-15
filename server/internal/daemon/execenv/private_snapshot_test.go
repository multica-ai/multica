package execenv

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReadStableRegularFileRejectsHardlinkAndNonRegular(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FIFO fixture is POSIX-only")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(source, []byte(`{"token":"secret"}`), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.Link(source, filepath.Join(dir, "auth-hardlink.json")); err != nil {
		t.Fatalf("create hardlink: %v", err)
	}
	if _, err := readStableRegularFile(source, 1024, nil); err == nil || !strings.Contains(err.Error(), "hard link") {
		t.Fatalf("hardlinked source error = %v, want hard-link rejection", err)
	}

	if _, err := readStableRegularFile(dir, 1024, nil); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory source error = %v, want non-regular rejection", err)
	}
}

func TestReadStableRegularFileRejectsOversizeAndIdentitySwap(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(source, bytes.Repeat([]byte("x"), 33), 0o600); err != nil {
		t.Fatalf("write oversized source: %v", err)
	}
	if _, err := readStableRegularFile(source, 32, nil); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized source error = %v, want size rejection", err)
	}

	if err := os.WriteFile(source, []byte(`{"token":"before"}`), 0o600); err != nil {
		t.Fatalf("reset source: %v", err)
	}
	replacement := filepath.Join(dir, "replacement.json")
	if err := os.WriteFile(replacement, []byte(`{"token":"after"}`), 0o600); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	_, err := readStableRegularFile(source, 1024, func() {
		if renameErr := os.Rename(replacement, source); renameErr != nil {
			t.Fatalf("swap source identity: %v", renameErr)
		}
	})
	if err == nil || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("identity swap error = %v, want fail closed", err)
	}
}

func TestReadStableRegularFileRejectsSymlinkSwapAndHardlinkRace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires elevated privileges on some Windows hosts")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(source, []byte(`{"token":"before"}`), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	openedPath := filepath.Join(dir, "opened-auth.json")
	_, err := readStableRegularFile(source, 1024, func() {
		if renameErr := os.Rename(source, openedPath); renameErr != nil {
			t.Fatalf("move opened source: %v", renameErr)
		}
		if symlinkErr := os.Symlink(openedPath, source); symlinkErr != nil {
			t.Fatalf("swap source to symlink: %v", symlinkErr)
		}
	})
	if err == nil || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("symlink swap error = %v, want fail closed", err)
	}

	if err := os.Remove(source); err != nil {
		t.Fatalf("remove swapped symlink: %v", err)
	}
	if err := os.Rename(openedPath, source); err != nil {
		t.Fatalf("restore source: %v", err)
	}
	hardlink := filepath.Join(dir, "auth-race-link.json")
	_, err = readStableRegularFile(source, 1024, func() {
		if linkErr := os.Link(source, hardlink); linkErr != nil {
			t.Fatalf("create racing hardlink: %v", linkErr)
		}
	})
	if err == nil || !strings.Contains(err.Error(), "hard link") {
		t.Fatalf("hardlink race error = %v, want fail closed", err)
	}
}

func TestWritePrivateSnapshotReplacesSymlinkWithoutFollowing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires elevated privileges on some Windows hosts")
	}
	dir := t.TempDir()
	ownerFile := filepath.Join(dir, "owner.json")
	if err := os.WriteFile(ownerFile, []byte("owner-live"), 0o600); err != nil {
		t.Fatalf("write owner file: %v", err)
	}
	target := filepath.Join(dir, "snapshot.json")
	if err := os.Symlink(ownerFile, target); err != nil {
		t.Fatalf("create stale target symlink: %v", err)
	}
	if err := writePrivateSnapshot(target, []byte("task-private"), 1024); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("lstat snapshot: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot mode = %v, want regular 0600", info.Mode())
	}
	ownerData, err := os.ReadFile(ownerFile)
	if err != nil {
		t.Fatalf("read owner file: %v", err)
	}
	if string(ownerData) != "owner-live" {
		t.Fatalf("owner file was overwritten through symlink: %q", ownerData)
	}
}
