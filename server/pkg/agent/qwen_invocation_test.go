package agent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"testing"
)

// TestChooseQwenInvocation_PassthroughForNonLauncher verifies that when the
// resolved executable is not a Windows .cmd/.bat launcher, both argv[0] and
// the argv list are returned unchanged on every platform. This guards against
// accidental rewriting on macOS/Linux and for direct binary launches on
// Windows.
func TestChooseQwenInvocation_PassthroughForNonLauncher(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	execName := "qwen"
	lookedUp := filepath.Join(t.TempDir(), "qwen") // no .cmd / .bat
	args := []string{"--output-format", "stream-json", "--yolo"}

	gotExec, gotArgs := chooseQwenInvocation(execName, lookedUp, args, logger)

	if gotExec != execName {
		t.Errorf("argv0 changed unexpectedly: got %q want %q", gotExec, execName)
	}
	if !reflect.DeepEqual(gotArgs, args) {
		t.Errorf("argv changed unexpectedly:\n got  %#v\n want %#v", gotArgs, args)
	}
}

// TestQwenLaunchesThroughTheInvocationChooser pins the wiring half of #6082.
// The chooser only protects a run that goes through it, and a qwen.go that
// builds its process with Command.exec spawns the npm qwen.cmd launcher
// directly again — every symptom returns while the Windows tests in
// qwen_invocation_windows_test.go keep passing, because they exercise the
// chooser rather than the backend. The assertion is on the source since the
// two launch paths are indistinguishable at runtime on macOS/Linux, where the
// chooser is a passthrough.
func TestQwenLaunchesThroughTheInvocationChooser(t *testing.T) {
	t.Parallel()

	file, err := parser.ParseFile(token.NewFileSet(), "qwen.go", nil, 0)
	if err != nil {
		t.Fatalf("parse qwen.go: %v", err)
	}

	routed := false
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "execVia" {
			return true
		}
		for _, arg := range call.Args {
			if ident, ok := arg.(*ast.Ident); ok && ident.Name == "chooseQwenInvocation" {
				routed = true
			}
		}
		return true
	})

	if !routed {
		t.Fatal("qwen.go must spawn through Command.execVia with chooseQwenInvocation; " +
			"launching the resolved executable directly hands the managed argv back to cmd.exe re-tokenisation on Windows (GH #6082)")
	}
}
