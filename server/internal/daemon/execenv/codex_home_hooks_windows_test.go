//go:build windows

package execenv

import (
	"os"
	"path/filepath"
	"testing"
)

// T13 (D5, Windows): when the per-task hooks/ was materialized as a directory
// junction (createDirLink's fallback when symlinks are unavailable), clearing
// the stale per-task resource must remove only the junction — never recurse
// through it and delete the shared ~/.codex/hooks target contents.
func TestRemoveOptionalPathDoesNotDeleteJunctionTarget(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	src := filepath.Join(dir, "shared-hooks")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("create shared hooks dir: %v", err)
	}
	keep := filepath.Join(src, "notify.sh")
	if err := os.WriteFile(keep, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatalf("write shared hook helper: %v", err)
	}

	dst := filepath.Join(dir, "task-hooks")
	if err := createDirLink(src, dst); err != nil {
		t.Fatalf("createDirLink (symlink or junction): %v", err)
	}

	if err := removeOptionalPath(dst); err != nil {
		t.Fatalf("removeOptionalPath: %v", err)
	}

	// The link/junction is gone.
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Fatalf("expected junction/link removed, lstat err = %v", err)
	}
	// The shared target and its contents survive.
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("shared hooks dir must survive: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("shared hook helper must survive junction removal: %v", err)
	}
}
