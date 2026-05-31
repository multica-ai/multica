//go:build windows

package agent

import (
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"testing"
)

// TestPlatformPiInvocation_RewritesCmdLauncherToPowerShellFile is the core
// Windows test: when LookPath resolves pi to the npm-installed .cmd
// launcher and a sibling pi.ps1 exists, we should invoke PowerShell with
// -File <ps1> and forward every original arg unchanged — including the
// multi-line positional prompt that would otherwise be mangled by the
// cmd.exe %* re-expansion inside pi.cmd. This is the regression test for
// #3306: daemon argv carried the full prompt, but Pi's session JSONL only
// recorded the first line.
func TestPlatformPiInvocation_RewritesCmdLauncherToPowerShellFile(t *testing.T) {
	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "pi.cmd")
	ps1Path := filepath.Join(dir, "pi.ps1")
	writeFile(t, cmdPath, "@echo off\r\npowershell -NoProfile -ExecutionPolicy Bypass -File \"%~dp0pi.ps1\" %*\r\n")
	writeFile(t, ps1Path, "# fake pi.ps1\r\n")

	fakePS := filepath.Join(dir, "powershell.exe")
	writeFile(t, fakePS, "")
	stubPowerShell(t, fakePS, true)

	multiLinePrompt := "You are running as a chat assistant for a Multica workspace.\n\nUser message:\n我需要创建一个issue\n"
	args := []string{
		"-p",
		"--mode", "json",
		"--session", `C:\Users\X\.multica\pi-sessions\20260528T040000.jsonl`,
		multiLinePrompt,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	gotExec, gotArgs, ok := platformPiInvocation(cmdPath, args, logger)
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

	// Explicit check: the last argv (the positional prompt) must still
	// contain every line of the original multi-line prompt. This is the
	// concrete property #3306 violates when cmd.exe re-tokenises %*.
	if gotArgs[len(gotArgs)-1] != multiLinePrompt {
		t.Errorf("multi-line prompt was mangled:\n got  %q\n want %q", gotArgs[len(gotArgs)-1], multiLinePrompt)
	}
}

// TestPlatformPiInvocation_SkipsWhenNotCmdOrBat ensures we leave argv alone
// when the user explicitly resolved pi to something that isn't a batch
// launcher (e.g. a real binary or a node script via shebang shim).
func TestPlatformPiInvocation_SkipsWhenNotCmdOrBat(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, "pi.exe")
	writeFile(t, exePath, "")
	// A sibling .ps1 must not trick us into rewriting a non-launcher exec.
	writeFile(t, filepath.Join(dir, "pi.ps1"), "")

	stubPowerShell(t, filepath.Join(dir, "powershell.exe"), true)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, _, ok := platformPiInvocation(exePath, []string{"-p", "hello"}, logger); ok {
		t.Fatalf("expected ok=false for non-.cmd/.bat launcher")
	}
}

// TestPlatformPiInvocation_SkipsWhenPS1Missing covers the rare case where a
// .cmd was found but its companion .ps1 is missing (e.g. a partial install
// or a third-party shim that wraps Pi differently). We must fall back to
// the original launcher rather than synthesising an invalid powershell
// -File invocation against a non-existent script.
func TestPlatformPiInvocation_SkipsWhenPS1Missing(t *testing.T) {
	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "pi.cmd")
	writeFile(t, cmdPath, "@echo off\r\n")

	stubPowerShell(t, filepath.Join(dir, "powershell.exe"), true)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, _, ok := platformPiInvocation(cmdPath, []string{"-p", "hello"}, logger); ok {
		t.Fatalf("expected ok=false when pi.ps1 is missing")
	}
}

// TestPlatformPiInvocation_SkipsWhenPowerShellMissing covers a stripped
// down environment in which neither pwsh.exe nor powershell.exe can be
// resolved. We must not fabricate an empty-string argv[0].
func TestPlatformPiInvocation_SkipsWhenPowerShellMissing(t *testing.T) {
	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "pi.cmd")
	ps1Path := filepath.Join(dir, "pi.ps1")
	writeFile(t, cmdPath, "@echo off\r\n")
	writeFile(t, ps1Path, "# fake\r\n")

	stubPowerShell(t, "", false)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, _, ok := platformPiInvocation(cmdPath, []string{"-p", "hello"}, logger); ok {
		t.Fatalf("expected ok=false when no powershell host is available")
	}
}
