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
