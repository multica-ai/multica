//go:build windows

package agent

import (
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"testing"
)

// TestPlatformCopilotInvocation_RewritesCmdLauncherToPowerShellFile is the
// core Windows test: when LookPath resolves copilot to the npm-installed .cmd
// launcher and a sibling copilot.ps1 exists, we should invoke PowerShell with
// -File <ps1> and forward every original arg unchanged — including the
// multi-line -p prompt that would otherwise be mangled by cmd.exe's %*
// re-expansion inside copilot.cmd.
func TestPlatformCopilotInvocation_RewritesCmdLauncherToPowerShellFile(t *testing.T) {
	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "copilot.cmd")
	ps1Path := filepath.Join(dir, "copilot.ps1")
	writeFile(t, cmdPath, "@echo off\r\npowershell -NoProfile -ExecutionPolicy Bypass -File \"%~dp0copilot.ps1\" %*\r\n")
	writeFile(t, ps1Path, "# fake copilot.ps1\r\n")

	fakePS := filepath.Join(dir, "powershell.exe")
	writeFile(t, fakePS, "")
	stubPowerShell(t, fakePS, true)

	multiLinePrompt := "You are running as a local coding agent.\n\n# Context\nDo the task.\n"
	args := []string{
		"-p", multiLinePrompt,
		"--output-format", "json",
		"--allow-all",
		"--no-ask-user",
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	gotExec, gotArgs, ok := platformCopilotInvocation(cmdPath, args, logger)
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

	// Confirm the multi-line prompt value survived intact. Find the -p flag and
	// check the next element, so the assertion stays correct even if
	// rewriteCmdToPS1 prepends additional PowerShell flags in the future.
	promptIdx := -1
	for i, a := range gotArgs {
		if a == "-p" {
			promptIdx = i + 1
			break
		}
	}
	if promptIdx < 0 || promptIdx >= len(gotArgs) {
		t.Fatalf("could not find -p flag in gotArgs: %#v", gotArgs)
	}
	if gotArgs[promptIdx] != multiLinePrompt {
		t.Errorf("multi-line prompt was mangled:\n got  %q\n want %q", gotArgs[promptIdx], multiLinePrompt)
	}
}

// TestPlatformCopilotInvocation_SkipsWhenNotCmdOrBat ensures we leave argv
// alone when the user explicitly resolved copilot to something that isn't a
// batch launcher (e.g. a real binary or a node shebang shim).
func TestPlatformCopilotInvocation_SkipsWhenNotCmdOrBat(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, "copilot.exe")
	writeFile(t, exePath, "")
	// A sibling .ps1 must not trick us into rewriting a non-launcher exec.
	writeFile(t, filepath.Join(dir, "copilot.ps1"), "")

	stubPowerShell(t, filepath.Join(dir, "powershell.exe"), true)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, _, ok := platformCopilotInvocation(exePath, []string{"-p", "hello"}, logger); ok {
		t.Fatalf("expected ok=false for non-.cmd/.bat launcher")
	}
}

// TestPlatformCopilotInvocation_SkipsWhenPS1Missing covers the rare case
// where a .cmd was found but its companion .ps1 is missing (e.g. a partial
// install). We must fall back to the original launcher rather than
// synthesising an invalid powershell -File invocation.
func TestPlatformCopilotInvocation_SkipsWhenPS1Missing(t *testing.T) {
	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "copilot.cmd")
	writeFile(t, cmdPath, "@echo off\r\n")

	stubPowerShell(t, filepath.Join(dir, "powershell.exe"), true)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, _, ok := platformCopilotInvocation(cmdPath, []string{"-p", "hello"}, logger); ok {
		t.Fatalf("expected ok=false when copilot.ps1 is missing")
	}
}

// TestPlatformCopilotInvocation_SkipsWhenPowerShellMissing covers a stripped
// environment in which neither pwsh.exe nor powershell.exe can be resolved.
// We must not fabricate an empty-string argv[0].
func TestPlatformCopilotInvocation_SkipsWhenPowerShellMissing(t *testing.T) {
	dir := t.TempDir()
	cmdPath := filepath.Join(dir, "copilot.cmd")
	ps1Path := filepath.Join(dir, "copilot.ps1")
	writeFile(t, cmdPath, "@echo off\r\n")
	writeFile(t, ps1Path, "# fake\r\n")

	stubPowerShell(t, "", false)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, _, ok := platformCopilotInvocation(cmdPath, []string{"-p", "hello"}, logger); ok {
		t.Fatalf("expected ok=false when no powershell host is available")
	}
}
