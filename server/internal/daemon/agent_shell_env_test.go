package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderZdotdirFiles_PrependsBinDirAndSourcesUserRC(t *testing.T) {
	binDir := "/opt/multica/bin"
	userZDOTDIR := "/home/dev"

	files := renderZdotdirFiles(binDir, userZDOTDIR)

	// .zshenv / .zprofile / .zshrc must both source the user's matching rc and
	// then prepend binDir. Order matters: the prepend has to come last so it
	// wins over anything the user's rc (e.g. brew shellenv) put on PATH.
	for _, name := range []string{".zshenv", ".zprofile", ".zshrc"} {
		content := files[name]
		userRC := filepath.Join(userZDOTDIR, name)
		srcIdx := strings.Index(content, "source '"+userRC+"'")
		if srcIdx < 0 {
			t.Fatalf("%s: expected it to source the user's %s, got:\n%s", name, userRC, content)
		}
		prependIdx := strings.Index(content, "export PATH='"+binDir+"':$PATH")
		if prependIdx < 0 {
			t.Fatalf("%s: expected binDir prepend, got:\n%s", name, content)
		}
		if prependIdx < srcIdx {
			t.Fatalf("%s: binDir prepend must come AFTER sourcing user rc, else user rc shadows us:\n%s", name, content)
		}
	}

	// .zlogin should only source the user rc — no extra prepend (the three
	// above already cover every invocation form).
	if strings.Contains(files[".zlogin"], "export PATH=") {
		t.Errorf(".zlogin should not prepend PATH, got:\n%s", files[".zlogin"])
	}
}

func TestRenderZdotdirFiles_EmptyUserZDOTDIRStillPrepends(t *testing.T) {
	files := renderZdotdirFiles("/opt/multica/bin", "")
	for _, name := range []string{".zshenv", ".zprofile", ".zshrc"} {
		if strings.Contains(files[name], "source ") {
			t.Errorf("%s: no user rc should be sourced when userZDOTDIR is empty, got:\n%s", name, files[name])
		}
		if !strings.Contains(files[name], "export PATH='/opt/multica/bin':$PATH") {
			t.Errorf("%s: binDir must still be prepended, got:\n%s", name, files[name])
		}
	}
}

func TestShellSingleQuote_EscapesEmbeddedQuote(t *testing.T) {
	got := shellSingleQuote("a'b")
	want := `'a'\''b'`
	if got != want {
		t.Errorf("shellSingleQuote(\"a'b\") = %s, want %s", got, want)
	}
}

// TestManagedZdotdir_BinDirWinsOverUserRC is the decisive end-to-end check: it
// reproduces the brew-shellenv-shadows-multica bug in a hermetic temp tree and
// proves the managed ZDOTDIR makes binDir win. Skips when zsh is unavailable.
func TestManagedZdotdir_BinDirWinsOverUserRC(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not available")
	}

	tmp := t.TempDir()

	// "brew" dir holds a stale `multica` the user's rc puts first on PATH.
	brewDir := filepath.Join(tmp, "brew")
	mustMkdir(t, brewDir)
	mustWriteExec(t, filepath.Join(brewDir, "multica"), "#!/bin/sh\necho STALE\n")

	// daemon binDir holds the real `multica` we want to win.
	binDir := filepath.Join(tmp, "bin")
	mustMkdir(t, binDir)
	mustWriteExec(t, filepath.Join(binDir, "multica"), "#!/bin/sh\necho FRESH\n")

	// User's real ZDOTDIR with a .zprofile that prepends brewDir — exactly what
	// `eval "$(brew shellenv)"` does.
	userZ := filepath.Join(tmp, "userz")
	mustMkdir(t, userZ)
	mustWrite(t, filepath.Join(userZ, ".zprofile"), "export PATH='"+brewDir+"':$PATH\n")

	// Baseline: login zsh with the user's real ZDOTDIR resolves the STALE one.
	base := runZsh(t, zsh, userZ, "command -v multica && multica")
	if !strings.Contains(base, "STALE") {
		t.Fatalf("baseline should resolve stale brew multica, got: %q", base)
	}

	// Managed ZDOTDIR: write our generated files into a managed dir that points
	// back at the user's real ZDOTDIR, then run login zsh against it.
	managed := filepath.Join(tmp, "managed")
	mustMkdir(t, managed)
	for name, content := range renderZdotdirFiles(binDir, userZ) {
		mustWrite(t, filepath.Join(managed, name), content)
	}
	got := runZsh(t, zsh, managed, "command -v multica && multica")
	if !strings.Contains(got, "FRESH") {
		t.Fatalf("managed ZDOTDIR should make daemon binDir win, got: %q", got)
	}
	if !strings.Contains(got, filepath.Join(binDir, "multica")) {
		t.Fatalf("managed ZDOTDIR should resolve multica to binDir, got: %q", got)
	}
}

func runZsh(t *testing.T, zsh, zdotdir, script string) string {
	t.Helper()
	cmd := exec.Command(zsh, "-lc", script)
	// Strip binDir/brewDir from the inherited PATH so only the rc files decide.
	cmd.Env = append(os.Environ(), "ZDOTDIR="+zdotdir, "PATH=/usr/bin:/bin")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("zsh run failed: %v\noutput: %s", err, out)
	}
	return string(out)
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustWriteExec(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
