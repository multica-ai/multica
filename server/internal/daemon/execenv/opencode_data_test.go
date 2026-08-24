package execenv

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPrepareOpenCodeDataDir(t *testing.T) {
	root := t.TempDir()
	dir, err := prepareOpenCodeDataDir(root)
	if err != nil {
		t.Fatalf("prepareOpenCodeDataDir: %v", err)
	}
	if want := filepath.Join(root, openCodeDataDirName); dir != want {
		t.Fatalf("dir = %q, want %q", dir, want)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("stat %q: %v (isDir=%v)", dir, err, info.IsDir())
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != fs.FileMode(0o700) {
		t.Fatalf("perm = %v, want 0700", info.Mode().Perm())
	}
	// Idempotent: reuse of a prepared env must not fail on an existing dir.
	if again, err := prepareOpenCodeDataDir(root); err != nil || again != dir {
		t.Fatalf("re-prepare: (%q, %v), want (%q, nil)", again, err, dir)
	}
}
