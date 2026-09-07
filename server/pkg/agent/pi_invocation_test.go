package agent

import (
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"testing"
)

// TestChoosePiInvocation_PassthroughForNonLauncher verifies that when the
// resolved executable is not a Windows .cmd/.bat launcher, both argv[0] and
// the argv list are returned unchanged on every platform. This guards
// against accidental rewriting on macOS/Linux and for direct binary
// launches on Windows.
func TestChoosePiInvocation_PassthroughForNonLauncher(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	execName := "pi"
	lookedUp := filepath.Join(t.TempDir(), "pi") // no .cmd / .bat
	args := []string{
		"-p",
		"--mode", "json",
		"--session", "/tmp/pi-session.jsonl",
		"You are running as a chat assistant for a Multica workspace.\n\nUser message:\n我需要创建一个issue\n",
	}

	gotExec, gotArgs := choosePiInvocation(execName, lookedUp, args, logger)

	if gotExec != execName {
		t.Errorf("argv0 changed unexpectedly: got %q want %q", gotExec, execName)
	}
	if !reflect.DeepEqual(gotArgs, args) {
		t.Errorf("argv changed unexpectedly:\n got  %#v\n want %#v", gotArgs, args)
	}
}

// TestPiPowerShellCommandArgs pins the argv shape of the -Command route,
// which is the only PowerShell route that leaves stdin bytes untouched
// (#7355). Runs on every platform: it tests the pure helper, not the
// Windows-tagged rewrite.
func TestPiPowerShellCommandArgs(t *testing.T) {
	ps1 := `C:\Users\X\npm\pi.ps1`
	args := []string{"-p", "--mode", "json"}

	got := piPowerShellCommandArgs(ps1, args)
	want := []string{
		"-NoProfile",
		"-ExecutionPolicy", "Bypass",
		"-Command", "& '" + ps1 + "' @args",
		"-p", "--mode", "json",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("argv mismatch:\n got  %#v\n want %#v", got, want)
	}
}

func TestPiPowerShellCommandArgs_EscapesSingleQuotesInPath(t *testing.T) {
	ps1 := `C:\Users\O'Brien\npm\pi.ps1`
	got := piPowerShellCommandArgs(ps1, nil)
	want := []string{
		"-NoProfile",
		"-ExecutionPolicy", "Bypass",
		"-Command", "& 'C:\\Users\\O''Brien\\npm\\pi.ps1' @args",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("argv mismatch:\n got  %#v\n want %#v", got, want)
	}
}

func TestPiPowerShellCommandArgs_ForwardsMultiLineArgVerbatim(t *testing.T) {
	multiLine := "line one\n\nline two\n"
	got := piPowerShellCommandArgs(`C:\pi.ps1`, []string{"-p", multiLine})
	if len(got) != 7 {
		t.Fatalf("len(got) = %d, want 7: %#v", len(got), got)
	}
	if got[len(got)-1] != multiLine {
		t.Errorf("multi-line arg was not forwarded verbatim:\n got  %q\n want %q", got[len(got)-1], multiLine)
	}
}
