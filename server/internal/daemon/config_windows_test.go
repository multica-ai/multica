//go:build windows

package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalExecutablePathResolvesDirectoryJunction(t *testing.T) {
	targetDir := t.TempDir()
	targetBin := filepath.Join(targetDir, "bin")
	if err := os.MkdirAll(targetBin, 0o755); err != nil {
		t.Fatal(err)
	}
	targetExecutable := filepath.Join(targetBin, "codex.exe")
	if err := os.WriteFile(targetExecutable, []byte("test"), 0o755); err != nil {
		t.Fatal(err)
	}

	shimRoot := t.TempDir()
	shimBin := filepath.Join(shimRoot, "bin")
	createJunction(t, targetBin, shimBin)
	shimExecutable := filepath.Join(shimBin, "codex.exe")

	if _, err := filepath.EvalSymlinks(shimExecutable); err == nil {
		t.Fatal("EvalSymlinks unexpectedly resolved a directory junction; test would not reproduce the bug")
	}
	if got := canonicalExecutablePath(shimExecutable); !strings.EqualFold(got, targetExecutable) {
		t.Fatalf("canonicalExecutablePath() = %q, want junction target %q", got, targetExecutable)
	}
}

func TestCanonicalExecutablePathKeepsRegularExecutable(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "codex.exe")
	if err := os.WriteFile(executable, []byte("test"), 0o755); err != nil {
		t.Fatal(err)
	}

	if got := canonicalExecutablePath(executable); !strings.EqualFold(got, executable) {
		t.Fatalf("canonicalExecutablePath() = %q, want %q", got, executable)
	}
}

func TestCanonicalExecutablePathFallsBackForMissingExecutable(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing", "codex.exe")
	want, err := filepath.Abs(missing)
	if err != nil {
		t.Fatal(err)
	}

	if got := canonicalExecutablePath(missing); got != want {
		t.Fatalf("canonicalExecutablePath() = %q, want fallback %q", got, want)
	}
}

func TestTrimWindowsExtendedPathPrefix(t *testing.T) {
	for _, test := range []struct {
		name string
		path string
		want string
	}{
		{name: "drive path", path: `\\?\C:\tools\codex.exe`, want: `C:\tools\codex.exe`},
		{name: "UNC path", path: `\\?\UNC\server\share\codex.exe`, want: `\\server\share\codex.exe`},
		{name: "ordinary path", path: `C:\tools\codex.exe`, want: `C:\tools\codex.exe`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := trimWindowsExtendedPathPrefix(test.path); got != test.want {
				t.Fatalf("trimWindowsExtendedPathPrefix(%q) = %q, want %q", test.path, got, test.want)
			}
		})
	}
}

func TestResolveAgentExecutablePathCanonicalizesPATHJunction(t *testing.T) {
	targetExecutable, shimBin := stageJunctionExecutable(t)
	t.Setenv("PATH", shimBin)
	t.Setenv("PATHEXT", ".EXE")

	got, err := resolveAgentExecutablePath("codex")
	if err != nil {
		t.Fatalf("resolveAgentExecutablePath: %v", err)
	}
	if !strings.EqualFold(got, targetExecutable) {
		t.Fatalf("resolveAgentExecutablePath() = %q, want junction target %q", got, targetExecutable)
	}
}

func TestResolveAgentExecutablePathCanonicalizesConfiguredJunctionPath(t *testing.T) {
	targetExecutable, shimBin := stageJunctionExecutable(t)
	configured := filepath.Join(shimBin, "codex.exe")

	got, err := resolveAgentExecutablePath(configured)
	if err != nil {
		t.Fatalf("resolveAgentExecutablePath: %v", err)
	}
	if !strings.EqualFold(got, targetExecutable) {
		t.Fatalf("resolveAgentExecutablePath() = %q, want junction target %q", got, targetExecutable)
	}
}

func stageJunctionExecutable(t *testing.T) (targetExecutable, shimBin string) {
	t.Helper()
	targetBin := filepath.Join(t.TempDir(), "release", "bin")
	if err := os.MkdirAll(targetBin, 0o755); err != nil {
		t.Fatal(err)
	}
	targetExecutable = filepath.Join(targetBin, "codex.exe")
	if err := os.WriteFile(targetExecutable, []byte("test"), 0o755); err != nil {
		t.Fatal(err)
	}
	shimBin = filepath.Join(t.TempDir(), "installer", "bin")
	createJunction(t, targetBin, shimBin)
	return targetExecutable, shimBin
}
