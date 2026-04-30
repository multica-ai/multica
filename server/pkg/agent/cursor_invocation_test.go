package agent

import (
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"testing"
)

// TestChooseCursorInvocation_PassthroughForNonLauncher verifies that when the
// resolved executable is not a Windows .cmd/.bat launcher, both argv[0] and
// the argv list are returned unchanged on every platform. This guards against
// accidental rewriting on macOS/Linux and for direct binary launches on
// Windows.
func TestChooseCursorInvocation_PassthroughForNonLauncher(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	execName := "cursor-agent"
	lookedUp := filepath.Join(t.TempDir(), "cursor-agent") // no .cmd / .bat
	args := []string{"chat", "-p", "hello\nworld", "--output-format", "stream-json", "--yolo"}

	gotExec, gotArgs := chooseCursorInvocation(execName, lookedUp, args, logger)

	if gotExec != execName {
		t.Errorf("argv0 changed unexpectedly: got %q want %q", gotExec, execName)
	}
	if !reflect.DeepEqual(gotArgs, args) {
		t.Errorf("argv changed unexpectedly:\n got  %#v\n want %#v", gotArgs, args)
	}
}
