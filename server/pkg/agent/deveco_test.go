package agent

// Tests for the DevEco Code backend. Self-contained: they use a deveco-specific
// fake script and devecoEvent fixtures, and reuse only the package-wide generic
// test helpers (writeTestExecutable, containsString, …) that every backend's
// tests share. They do not depend on the OpenCode backend or its test fixtures.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"log/slog"
)

func TestNewReturnsDevecoBackend(t *testing.T) {
	t.Parallel()
	b, err := New("deveco", Config{ExecutablePath: "/nonexistent/deveco"})
	if err != nil {
		t.Fatalf("New(deveco) error: %v", err)
	}
	if _, ok := b.(*devecoBackend); !ok {
		t.Fatalf("expected *devecoBackend, got %T", b)
	}
}

// fakeDevecoScript impersonates the `deveco` CLI: it records argv to
// $DEVECO_ARGS_FILE, then emits a minimal completed step on stdout so the
// backend's event loop terminates, and exits 0.
func fakeDevecoScript() string {
	return `#!/bin/sh
if [ -n "$DEVECO_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$DEVECO_ARGS_FILE"
  done
fi
printf '{"type":"step_start","timestamp":1,"sessionID":"ses_fake","part":{"type":"step-start"}}\n'
printf '{"type":"text","timestamp":2,"sessionID":"ses_fake","part":{"type":"text","text":"ok"}}\n'
printf '{"type":"step_finish","timestamp":3,"sessionID":"ses_fake","part":{"type":"step-finish","tokens":{"total":10,"input":7,"output":3,"cache":{"read":0,"write":0}}}}\n'
`
}

// TestDevecoBackendArgvShapeAndNoPrompt pins the two DevEco differences from
// the OpenCode backend: argv is `run --format json --dangerously-skip-permissions
// --dir <wd> [--model …] [--variant …] [--session …] <prompt>`, and — crucially
// — there is never a `--prompt` flag, because DevEco's `run` subcommand does not
// accept one (it would reject the invocation).
func TestDevecoBackendArgvShapeAndNoPrompt(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "deveco")
	writeTestExecutable(t, fakePath, []byte(fakeDevecoScript()))

	workDir := t.TempDir()

	backend, err := New("deveco", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"DEVECO_ARGS_FILE": argsFile},
	})
	if err != nil {
		t.Fatalf("new deveco backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// SystemPrompt is set deliberately; the backend must NOT forward it as
	// --prompt (DevEco has no such flag).
	session, err := backend.Execute(ctx, "do the thing", ExecOptions{
		Cwd:          workDir,
		Model:        "deveco/GLM-5.1",
		SystemPrompt: "you are a helpful agent",
		Timeout:      5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	res := <-session.Result
	if res.Status != "completed" {
		t.Fatalf("result status = %q, error = %q; want completed", res.Status, res.Error)
	}

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	args := splitNonEmptyLines(string(raw))

	if len(args) < 2 || args[0] != "run" {
		t.Fatalf("expected first arg 'run', got %q", args)
	}
	if !containsAdjacent(args, "--format", "json") {
		t.Errorf("expected --format json in argv: %v", args)
	}
	if !containsString(args, "--dangerously-skip-permissions") {
		t.Errorf("expected --dangerously-skip-permissions in argv: %v", args)
	}
	if !containsAdjacent(args, "--dir", workDir) {
		t.Errorf("expected --dir %q in argv: %v", workDir, args)
	}
	if !containsAdjacent(args, "--model", "deveco/GLM-5.1") {
		t.Errorf("expected --model deveco/GLM-5.1 in argv: %v", args)
	}
	if containsString(args, "--prompt") {
		t.Errorf("argv must NOT contain --prompt (DevEco has no such flag): %v", args)
	}
	// The prompt is the final positional arg.
	if len(args) == 0 || args[len(args)-1] != "do the thing" {
		t.Errorf("expected prompt as final positional arg, got %v", args)
	}
}

// ── Event parsing tests with a real `deveco run --format json` fixture ──

func TestDevecoEventParsingTextFixture(t *testing.T) {
	t.Parallel()

	// Real output captured from `deveco run "..." --format json`.
	line := `{"type":"text","timestamp":1783090764802,"sessionID":"ses_0d78241f1ffeAi4CgaYW5ygSLt","part":{"id":"prt_x","messageID":"msg_x","sessionID":"ses_0d78241f1ffeAi4CgaYW5ygSLt","type":"text","text":"pong","time":{"start":1783090764733,"end":1783090764791}}}`

	var event devecoEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if event.Type != "text" {
		t.Errorf("type: got %q, want %q", event.Type, "text")
	}
	if event.SessionID != "ses_0d78241f1ffeAi4CgaYW5ygSLt" {
		t.Errorf("sessionID: got %q", event.SessionID)
	}
	if event.Part.Text != "pong" {
		t.Errorf("part.text: got %q, want %q", event.Part.Text, "pong")
	}
}

func TestDevecoEventParsingStepFinishTokensFixture(t *testing.T) {
	t.Parallel()

	line := `{"type":"step_finish","timestamp":1783090764802,"sessionID":"ses_x","part":{"id":"prt_x","reason":"stop","messageID":"msg_x","type":"step-finish","tokens":{"total":11640,"input":11637,"output":3,"reasoning":0,"cache":{"write":0,"read":0}},"cost":0}}`

	var event devecoEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if event.Type != "step_finish" {
		t.Fatalf("type: got %q, want %q", event.Type, "step_finish")
	}
	if event.Part.Tokens == nil {
		t.Fatal("expected tokens")
	}
	if event.Part.Tokens.Input != 11637 || event.Part.Tokens.Output != 3 {
		t.Errorf("tokens: input=%d output=%d, want 11637/3", event.Part.Tokens.Input, event.Part.Tokens.Output)
	}
	if event.Part.Tokens.Cache == nil || event.Part.Tokens.Cache.Read != 0 {
		t.Errorf("cache tokens: %+v", event.Part.Tokens.Cache)
	}
}

// ── processEvents integration test ──

func TestDevecoProcessEventsHappyPath(t *testing.T) {
	t.Parallel()

	b := &devecoBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := strings.Join([]string{
		`{"type":"step_start","timestamp":1000,"sessionID":"ses_happy","part":{"type":"step-start"}}`,
		`{"type":"text","timestamp":1001,"sessionID":"ses_happy","part":{"type":"text","text":"working"}}`,
		`{"type":"tool_use","timestamp":1002,"sessionID":"ses_happy","part":{"tool":"bash","callID":"call_1","state":{"status":"completed","input":{"command":"ls"},"output":"a.go\n"}}}`,
		`{"type":"step_finish","timestamp":1003,"sessionID":"ses_happy","part":{"type":"step-finish","tokens":{"total":10,"input":7,"output":3,"cache":{"read":0,"write":0}}}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "completed" {
		t.Errorf("status: got %q, want %q", result.status, "completed")
	}
	if result.sessionID != "ses_happy" {
		t.Errorf("sessionID: got %q, want %q", result.sessionID, "ses_happy")
	}
	if result.output != "working" {
		t.Errorf("output: got %q, want %q", result.output, "working")
	}
	if result.usage.InputTokens != 7 || result.usage.OutputTokens != 3 {
		t.Errorf("usage: got %+v", result.usage)
	}

	close(ch)
	var msgs []Message
	for m := range ch {
		msgs = append(msgs, m)
	}
	// status(running), text, tool-use, tool-result = 4 messages.
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d: %+v", len(msgs), msgs)
	}
}

func TestDevecoProcessEventsErrorCausesFailedStatus(t *testing.T) {
	t.Parallel()

	b := &devecoBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := strings.Join([]string{
		`{"type":"step_start","timestamp":1000,"sessionID":"ses_err","part":{"type":"step-start"}}`,
		`{"type":"error","timestamp":1001,"sessionID":"ses_err","error":{"name":"UnknownError","data":{"message":"Model not found: bad/model"}}}`,
		`{"type":"step_finish","timestamp":1002,"sessionID":"ses_err","part":{"type":"step-finish"}}`,
	}, "\n")

	result := b.processEvents(strings.NewReader(lines), ch)

	if result.status != "failed" {
		t.Errorf("status: got %q, want %q", result.status, "failed")
	}
	if result.errMsg != "Model not found: bad/model" {
		t.Errorf("errMsg: got %q", result.errMsg)
	}

	close(ch)
}

func TestDevecoHandleErrorEventNilError(t *testing.T) {
	t.Parallel()

	b := &devecoBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 10)
	status := "completed"
	errMsg := ""

	b.handleErrorEvent(devecoEvent{Type: "error"}, ch, &status, &errMsg)

	if errMsg != "unknown deveco error" {
		t.Errorf("error: got %q, want %q", errMsg, "unknown deveco error")
	}
}

// devecoMainPackageNative returns the postinstall-copied native binary path
// under the scoped main package for a given npm prefix dir.
func devecoMainPackageNative(prefix string) string {
	return filepath.Join(prefix, "node_modules", "@deveco", "deveco-code", "bin", "deveco.exe")
}

func TestResolveDevecoNativeFromShimPrefersMainPackageCopy(t *testing.T) {
	t.Parallel()

	// postinstall copies the CPU-variant-selected binary into the main
	// package's own bin/, so that copy is the primary target.
	prefix := filepath.Join("C:\\nvm4w", "nodejs")
	shim := filepath.Join(prefix, "deveco.cmd")
	native := devecoMainPackageNative(prefix)

	got := resolveDevecoNativeFromShim(shim, fakeStat(native))
	if got != native {
		t.Errorf("got %q, want %q", got, native)
	}
}

func TestResolveDevecoNativeFromShimFallsBackToPlatformPackage(t *testing.T) {
	t.Parallel()

	// When the main-package copy is absent (e.g. postinstall skipped), the
	// resolver falls through to the windows-x64 platform sub-package.
	prefix := filepath.Join("C:\\nvm4w", "nodejs")
	shim := filepath.Join(prefix, "deveco.cmd")
	platform := filepath.Join(prefix, "node_modules", "@deveco", "deveco-code-windows-x64", "bin", "deveco.exe")

	got := resolveDevecoNativeFromShim(shim, fakeStat(platform))
	if got != platform {
		t.Errorf("got %q, want %q", got, platform)
	}
}

func TestResolveDevecoNativeFromShimFallsBackToBaseline(t *testing.T) {
	t.Parallel()

	// Older CPUs without AVX2 get the -baseline platform package. Resolver
	// should find it when neither the main copy nor the default x64 package
	// is present.
	prefix := filepath.Join("C:\\nvm4w", "nodejs")
	shim := filepath.Join(prefix, "deveco.cmd")
	baseline := filepath.Join(prefix, "node_modules", "@deveco", "deveco-code-windows-x64-baseline", "bin", "deveco.exe")

	got := resolveDevecoNativeFromShim(shim, fakeStat(baseline))
	if got != baseline {
		t.Errorf("got %q, want %q", got, baseline)
	}
}

func TestResolveDevecoNativeFromShimReturnsEmptyWhenNativeMissing(t *testing.T) {
	t.Parallel()

	// Shim ends in .cmd but no bundled native binary is present. Caller must
	// keep the original shim path so PATH lookup still wins.
	shim := filepath.Join("C:\\nvm4w", "nodejs", "deveco.cmd")

	got := resolveDevecoNativeFromShim(shim, fakeStat())
	if got != "" {
		t.Errorf("got %q, want empty (missing native binary)", got)
	}
}

func TestResolveDevecoNativeFromShimSkipsNonCmdPath(t *testing.T) {
	t.Parallel()

	// On macOS/Linux exec.LookPath returns the native binary directly (no
	// .cmd), so no rewrite is needed and the helper returns empty.
	cases := []string{
		"/usr/local/bin/deveco",
		"C:\\nvm4w\\nodejs\\deveco.exe",
		"",
	}
	for _, p := range cases {
		if got := resolveDevecoNativeFromShim(p, fakeStat("anything")); got != "" {
			t.Errorf("path %q: got %q, want empty", p, got)
		}
	}
}

func TestResolveDevecoNativeFromShimAcceptsUppercaseExtension(t *testing.T) {
	t.Parallel()

	// Windows filesystem extensions are case-insensitive and PATHEXT tokens
	// are commonly uppercase, so exec.LookPath may return `.CMD`.
	prefix := filepath.Join("C:\\nvm4w", "nodejs")
	shim := filepath.Join(prefix, "deveco.CMD")
	native := devecoMainPackageNative(prefix)

	got := resolveDevecoNativeFromShim(shim, fakeStat(native))
	if got != native {
		t.Errorf("got %q, want %q", got, native)
	}
}
