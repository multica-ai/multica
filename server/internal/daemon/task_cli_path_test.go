package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTaskCLIPathCanonicalBinaryPrependsItsDirectory(t *testing.T) {
	name := "multica"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binDir := t.TempDir()
	got, err := taskCLIPath(filepath.Join(binDir, name), t.TempDir(), "inherited")
	if err != nil {
		t.Fatalf("taskCLIPath: %v", err)
	}
	want := binDir + string(os.PathListSeparator) + "inherited"
	if got != want {
		t.Fatalf("PATH = %q, want %q", got, want)
	}
}

func TestTaskCLIPathRenamedBinaryCreatesResolvableAlias(t *testing.T) {
	name := "multica-local-build"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binDir := t.TempDir()
	selfBin := filepath.Join(binDir, name)
	if err := os.WriteFile(selfBin, []byte("fixture"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := taskCLIPath(selfBin, t.TempDir(), "inherited")
	if err != nil {
		t.Fatalf("taskCLIPath: %v", err)
	}
	parts := strings.Split(got, string(os.PathListSeparator))
	if len(parts) != 3 || parts[1] != binDir || parts[2] != "inherited" {
		t.Fatalf("PATH = %q, want alias dir, %q, inherited", got, binDir)
	}

	t.Setenv("PATH", got)
	resolved, err := exec.LookPath("multica")
	if err != nil {
		t.Fatalf("multica did not resolve from PATH %q: %v", got, err)
	}
	if filepath.Dir(resolved) != parts[0] {
		t.Fatalf("multica resolved to %q, want alias under %q", resolved, parts[0])
	}
}
