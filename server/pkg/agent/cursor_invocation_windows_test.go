//go:build windows

package agent

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// stubPowerShell installs a deterministic PowerShell lookup for the duration
// of a test and restores the original on cleanup.
func stubPowerShell(t *testing.T, path string, ok bool) {
	t.Helper()
	prev := powerShellLookup
	powerShellLookup = func() (string, bool) { return path, ok }
	t.Cleanup(func() { powerShellLookup = prev })
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestPlatformCursorInvocation_RewritesCmdLauncherToPowerShellFile is the core
// Windows test: when LookPath resolves cursor-agent to the official .cmd
// launcher and a sibling cursor-agent.ps1 exists, we should invoke
// PowerShell with -File <ps1> and forward every original arg unchanged
// (including a multi-line -p prompt that would otherwise be mangled by the
// cmd.exe %* re-expansion in the .cmd launcher).
func TestPlatformCursorInvocation_RewritesCmdLauncherToPowerShellFile(t *testing.T) {
	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "cursor-agent.cmd")
	ps1Path := filepath.Join(dir, "cursor-agent.ps1")
	writeFile(t, cmdPath, "@echo off\r\npowershell -NoProfile -ExecutionPolicy Bypass -File \"%~dp0cursor-agent.ps1\" %*\r\n")
	writeFile(t, ps1Path, "# fake cursor-agent.ps1\r\n")

	fakePS := filepath.Join(dir, "powershell.exe")
	writeFile(t, fakePS, "")
	stubPowerShell(t, fakePS, true)

	args := []string{
		"chat",
		"-p", "line1\nline2\nline3",
		"--output-format", "stream-json",
		"--yolo",
		"--workspace", `C:\some\workspace`,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	gotExec, gotArgs, ok := platformCursorInvocation(cmdPath, args, logger)
	if !ok {
		t.Fatalf("expected platform rewrite to be applied, got ok=false")
	}
	if gotExec != fakePS {
		t.Errorf("argv0: got %q want %q", gotExec, fakePS)
	}

	wantArgs := append([]string{
		"-NoProfile",
		"-ExecutionPolicy", "Bypass",
		"-File", ps1Path,
	}, args...)
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Errorf("argv mismatch:\n got  %#v\n want %#v", gotArgs, wantArgs)
	}
}

// TestPlatformCursorInvocation_SkipsWhenNotCmdOrBat ensures we leave argv
// alone when the user explicitly resolved cursor-agent to something that
// isn't a batch launcher (e.g. a real binary or a node script).
func TestPlatformCursorInvocation_SkipsWhenNotCmdOrBat(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, "cursor-agent.exe")
	writeFile(t, exePath, "")
	// A sibling .ps1 must not trick us into rewriting a non-launcher exec.
	writeFile(t, filepath.Join(dir, "cursor-agent.ps1"), "")

	stubPowerShell(t, filepath.Join(dir, "powershell.exe"), true)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, _, ok := platformCursorInvocation(exePath, []string{"chat"}, logger); ok {
		t.Fatalf("expected ok=false for non-.cmd/.bat launcher")
	}
}

// TestPlatformCursorInvocation_SkipsWhenPS1Missing covers the rare case where
// a .cmd was found but its companion .ps1 is missing (e.g. a partial install).
// We must fall back to the original launcher rather than synthesising an
// invalid powershell -File invocation.
func TestPlatformCursorInvocation_SkipsWhenPS1Missing(t *testing.T) {
	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "cursor-agent.cmd")
	writeFile(t, cmdPath, "@echo off\r\n")

	stubPowerShell(t, filepath.Join(dir, "powershell.exe"), true)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, _, ok := platformCursorInvocation(cmdPath, []string{"chat"}, logger); ok {
		t.Fatalf("expected ok=false when cursor-agent.ps1 is missing")
	}
}

// TestPlatformCursorInvocation_SkipsWhenPowerShellMissing covers a stripped
// down environment in which neither pwsh.exe nor powershell.exe can be
// resolved. We must not fabricate an empty-string argv[0].
func TestPlatformCursorInvocation_SkipsWhenPowerShellMissing(t *testing.T) {
	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "cursor-agent.cmd")
	ps1Path := filepath.Join(dir, "cursor-agent.ps1")
	writeFile(t, cmdPath, "@echo off\r\n")
	writeFile(t, ps1Path, "# fake\r\n")

	stubPowerShell(t, "", false)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, _, ok := platformCursorInvocation(cmdPath, []string{"chat"}, logger); ok {
		t.Fatalf("expected ok=false when no powershell host is available")
	}
}
