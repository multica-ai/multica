package execenv

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRuntimeConfigRoundTripPreservesDarwinResourceFork(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(path, []byte("user instructions\n"), 0o644); err != nil {
		t.Fatalf("seed runtime config: %v", err)
	}
	const resourceFork = "com.apple.ResourceFork"
	want := []byte("user-owned resource fork")
	if err := unix.Setxattr(path, resourceFork, want, 0); err != nil {
		t.Skipf("resource forks unavailable: %v", err)
	}

	if err := writeRuntimeConfigFile(path, "runtime brief"); err != nil {
		t.Fatalf("inject runtime config: %v", err)
	}
	if err := CleanupRuntimeConfig(dir, "codex"); err != nil {
		t.Fatalf("cleanup runtime config: %v", err)
	}

	got := make([]byte, len(want))
	n, err := unix.Getxattr(path, resourceFork, got)
	if err != nil {
		t.Fatalf("read resource fork after round trip: %v", err)
	}
	if !bytes.Equal(got[:n], want) {
		t.Fatalf("resource fork after round trip = %q, want %q", got[:n], want)
	}
}
