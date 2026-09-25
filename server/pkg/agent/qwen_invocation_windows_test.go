//go:build windows

package agent

import (
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"testing"
)

// TestPlatformQwenInvocation_RewritesCmdLauncherToPowerShellFile is the core
// Windows test for #6082: when LookPath resolves qwen to the npm-generated
// .cmd launcher and a sibling qwen.ps1 exists, we should invoke PowerShell
// with -File <ps1> and forward every managed arg unchanged. The task prompt is
// deliberately absent here because it is delivered on stdin.
func TestPlatformQwenInvocation_RewritesCmdLauncherToPowerShellFile(t *testing.T) {
	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "qwen.cmd")
	ps1Path := filepath.Join(dir, "qwen.ps1")
	writeFile(t, cmdPath, "@echo off\r\npowershell -NoProfile -ExecutionPolicy Bypass -File \"%~dp0qwen.ps1\" %*\r\n")
	writeFile(t, ps1Path, "# fake qwen.ps1\r\n")

	fakePS := filepath.Join(dir, "powershell.exe")
	writeFile(t, fakePS, "")
	stubPowerShell(t, fakePS, true)

	args := []string{
		"--output-format", "stream-json",
		"--yolo",
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	gotExec, gotArgs, ok := platformQwenInvocation(cmdPath, args, logger)
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

	// Without the protocol flags Qwen answers in plain text and the daemon
	// never sees a stream-json result event.
	if idx := prefixIndex(gotArgs, []string{"--output-format", "stream-json"}); idx < 0 {
		t.Errorf("the stream-json protocol flags did not survive: %#v", gotArgs)
	}
}

// TestPlatformQwenInvocation_SkipsWhenNotCmdOrBat ensures we leave argv alone
// when qwen resolves to something that isn't a batch launcher (the npm
// shebang binstub on macOS/Linux, or a real binary on Windows).
func TestPlatformQwenInvocation_SkipsWhenNotCmdOrBat(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, "qwen.exe")
	writeFile(t, exePath, "")
	// A sibling .ps1 must not trick us into rewriting a non-launcher exec.
	writeFile(t, filepath.Join(dir, "qwen.ps1"), "")

	stubPowerShell(t, filepath.Join(dir, "powershell.exe"), true)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, _, ok := platformQwenInvocation(exePath, []string{"--output-format", "stream-json"}, logger); ok {
		t.Fatalf("expected ok=false for non-.cmd/.bat launcher")
	}
}

// TestPlatformQwenInvocation_SkipsWhenPS1Missing covers a partial install
// where the .cmd was found but its companion .ps1 is gone. We must fall back
// to the original launcher rather than synthesise an invalid powershell -File
// invocation.
func TestPlatformQwenInvocation_SkipsWhenPS1Missing(t *testing.T) {
	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "qwen.cmd")
	writeFile(t, cmdPath, "@echo off\r\n")

	stubPowerShell(t, filepath.Join(dir, "powershell.exe"), true)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, _, ok := platformQwenInvocation(cmdPath, []string{"--output-format", "stream-json"}, logger); ok {
		t.Fatalf("expected ok=false when qwen.ps1 is missing")
	}
}

// TestPlatformQwenInvocation_SkipsWhenPowerShellMissing covers a stripped down
// environment in which neither pwsh.exe nor powershell.exe can be resolved. We
// must not fabricate an empty-string argv[0].
func TestPlatformQwenInvocation_SkipsWhenPowerShellMissing(t *testing.T) {
	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "qwen.cmd")
	ps1Path := filepath.Join(dir, "qwen.ps1")
	writeFile(t, cmdPath, "@echo off\r\n")
	writeFile(t, ps1Path, "# fake\r\n")

	stubPowerShell(t, "", false)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, _, ok := platformQwenInvocation(cmdPath, []string{"--output-format", "stream-json"}, logger); ok {
		t.Fatalf("expected ok=false when no powershell host is available")
	}
}
