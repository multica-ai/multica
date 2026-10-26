package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestStableExecutablePath pins the mapping both the restart target and
// autostart registration share. The brew branch is the one that matters:
// the keg path os.Executable() reports is deleted by the next
// `brew upgrade`, so anything recorded against it (a systemd ExecStart, a
// LaunchAgent) breaks after the first upgrade.
func TestStableExecutablePath(t *testing.T) {
	t.Parallel()

	keg := "/home/linuxbrew/.linuxbrew/Cellar/multica/0.4.33/bin/multica"

	t.Run("brew install maps to the stable prefix path", func(t *testing.T) {
		got := StableExecutablePath(keg, "/home/linuxbrew/.linuxbrew", true)
		// filepath.Join semantics: identical strings on unix (where brew
		// exists), OS-native separators wherever the test runs.
		if want := filepath.Join("/home/linuxbrew/.linuxbrew", "bin", "multica"); got != want {
			t.Errorf("StableExecutablePath = %q, want %q", got, want)
		}
	})

	t.Run("brew install with unresolved prefix passes through", func(t *testing.T) {
		// The caller decides how to warn; silently inventing a prefix would
		// be worse than recording what we actually ran.
		if got := StableExecutablePath(keg, "", true); got != keg {
			t.Errorf("StableExecutablePath = %q, want the input unchanged", got)
		}
	})

	t.Run("non-brew install resolves symlinks", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "multica")
		if err := os.WriteFile(file, []byte("x"), 0o755); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		want, err := filepath.EvalSymlinks(file)
		if err != nil {
			t.Fatalf("EvalSymlinks fixture: %v", err)
		}
		if got := StableExecutablePath(file, "", false); got != want {
			t.Errorf("StableExecutablePath = %q, want %q", got, want)
		}
	})

	t.Run("non-brew install keeps an unresolvable path", func(t *testing.T) {
		gone := filepath.Join(t.TempDir(), "gone", "multica")
		if got := StableExecutablePath(gone, "", false); got != gone {
			t.Errorf("StableExecutablePath = %q, want the input unchanged", got)
		}
	})
}

// TestStableSelfExecutableResolvesBrewKegPaths feeds brew-style keg paths —
// the exact shape os.Executable() reports under Homebrew — through the
// one-stop resolver and asserts the stable prefix path comes back. Detection
// has to work from the path alone (offline known-Cellar match), because the
// point of the test is the same situation as a CI host or a login entry
// without brew on PATH: the keg path is the only evidence available.
func TestStableSelfExecutableResolvesBrewKegPaths(t *testing.T) {
	t.Parallel()

	cases := []struct {
		keg    string
		prefix string
	}{
		{"/opt/homebrew/Cellar/multica/0.4.33/bin/multica", "/opt/homebrew"},
		{"/home/linuxbrew/.linuxbrew/Cellar/multica/0.4.33/bin/multica", "/home/linuxbrew/.linuxbrew"},
		{"/usr/local/Cellar/multica/0.4.33/bin/multica", "/usr/local"},
	}
	for _, c := range cases {
		want := filepath.Join(c.prefix, "bin", "multica")
		if got := StableSelfExecutable(c.keg); got != want {
			t.Errorf("StableSelfExecutable(%q) = %q, want %q", c.keg, got, want)
		}
	}

	t.Run("non-brew path passes through", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "multica")
		if err := os.WriteFile(file, []byte("x"), 0o755); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		want, err := filepath.EvalSymlinks(file)
		if err != nil {
			t.Fatalf("EvalSymlinks fixture: %v", err)
		}
		if got := StableSelfExecutable(file); got != want {
			t.Errorf("StableSelfExecutable = %q, want %q", got, want)
		}
	})
}
