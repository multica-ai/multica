package selfexec

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveWithUsesOSExecutable(t *testing.T) {
	want := filepath.Join(t.TempDir(), "does-not-need-to-exist")

	got, err := resolveWith(func() (string, error) {
		return want, nil
	}, nil)
	if err != nil {
		t.Fatalf("resolveWith() error = %v", err)
	}
	if got != want {
		t.Fatalf("resolveWith() = %q, want %q", got, want)
	}
}

func TestResolveWithFallsBackToArgv0(t *testing.T) {
	executableErr := errors.New("cannot find executable path")
	failExecutable := func() (string, error) { return "", executableErr }

	t.Run("absolute path", func(t *testing.T) {
		want := writeTestExecutable(t, t.TempDir(), "multica")

		got, err := resolveWith(failExecutable, []string{want})
		if err != nil {
			t.Fatalf("resolveWith() error = %v", err)
		}
		if got != want {
			t.Fatalf("resolveWith() = %q, want %q", got, want)
		}
	})

	t.Run("relative path", func(t *testing.T) {
		dir := t.TempDir()
		want := writeTestExecutable(t, dir, "multica")
		t.Chdir(dir)
		argv0 := "." + string(os.PathSeparator) + filepath.Base(want)

		got, err := resolveWith(failExecutable, []string{argv0})
		if err != nil {
			t.Fatalf("resolveWith() error = %v", err)
		}
		if got != want {
			t.Fatalf("resolveWith() = %q, want %q", got, want)
		}
	})

	t.Run("PATH command", func(t *testing.T) {
		dir := t.TempDir()
		want := writeTestExecutable(t, dir, "multica")
		t.Setenv("PATH", dir)

		got, err := resolveWith(failExecutable, []string{"multica"})
		if err != nil {
			t.Fatalf("resolveWith() error = %v", err)
		}
		if got != want {
			t.Fatalf("resolveWith() = %q, want %q", got, want)
		}
	})
}

func TestResolveWithRejectsInvalidFallback(t *testing.T) {
	executableErr := errors.New("cannot find executable path")
	failExecutable := func() (string, error) { return "", executableErr }

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing argv0", args: nil, want: "argv[0] is empty"},
		{name: "empty argv0", args: []string{""}, want: "argv[0] is empty"},
		{name: "missing executable", args: []string{"multica-does-not-exist"}, want: "multica-does-not-exist"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveWith(failExecutable, tt.args)
			if err == nil {
				t.Fatal("resolveWith() error = nil, want failure")
			}
			if !errors.Is(err, executableErr) {
				t.Fatalf("error = %q, want original os.Executable error", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want %q", err, tt.want)
			}
		})
	}

	if runtime.GOOS != "windows" {
		t.Run("non-executable file", func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "multica")
			if err := os.WriteFile(path, []byte("not executable"), 0o644); err != nil {
				t.Fatalf("write non-executable fixture: %v", err)
			}

			_, err := resolveWith(failExecutable, []string{path})
			if err == nil {
				t.Fatal("resolveWith() error = nil, want failure")
			}
			if !errors.Is(err, executableErr) {
				t.Fatalf("error = %q, want original os.Executable error", err)
			}
		})
	}
}

func writeTestExecutable(t *testing.T, dir, base string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		base += ".exe"
	}
	path := filepath.Join(dir, base)
	if err := os.WriteFile(path, []byte("test executable"), 0o755); err != nil {
		t.Fatalf("write executable fixture: %v", err)
	}
	return path
}
