package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPrimeModelSelectionUnsupported(t *testing.T) {
	t.Parallel()
	if ModelSelectionSupported("prime") {
		t.Fatal("ModelSelectionSupported(prime) should return false — the model is fixed process-globally and never read over ACP")
	}
	// Other providers should remain supported.
	if !ModelSelectionSupported("claude") {
		t.Fatal("ModelSelectionSupported(claude) should remain true")
	}
}

func TestPrimeThinkingControlUnsupported(t *testing.T) {
	t.Parallel()
	if ThinkingControlSupported("prime") {
		t.Fatal("ThinkingControlSupported(prime) should return false — Prime never reads a thinking-level field over ACP")
	}
}

func TestNewReturnsPrimeBackend(t *testing.T) {
	t.Parallel()
	b, err := New("prime", Config{ExecutablePath: "/nonexistent/prime-agent"})
	if err != nil {
		t.Fatalf("New(prime) error: %v", err)
	}
	if _, ok := b.(*primeBackend); !ok {
		t.Fatalf("expected *primeBackend, got %T", b)
	}
}

func TestPrimeIsSupportedType(t *testing.T) {
	t.Parallel()
	if !IsSupportedType("prime") {
		t.Fatal("IsSupportedType(prime) should be true")
	}
}

func TestPrimeLaunchHeader(t *testing.T) {
	t.Parallel()
	if got := LaunchHeader("prime"); got == "" {
		t.Fatal("LaunchHeader(prime) should not be empty")
	}
}

// fakePrimeACPScript impersonates `prime-agent --mode acp` for unit tests.
// It implements the ACP surface this investigation confirmed Prime Agent
// v0.7.1 actually exposes: initialize, session/new, session/prompt,
// session/close — and deliberately NOT session/resume or session/load,
// mirroring the real binary (see okf/prime-agent/session-model.md).
func fakePrimeACPScript() string {
	return "#!/bin/sh\n" + fakePrimeACPScriptBody()
}

// fakePrimeACPScriptBody is the shebang-less remainder of fakePrimeACPScript,
// factored out so tests that need to prepend a setup line (like capturing the
// process environment) can build "#!/bin/sh\n<setup>\n" + this without
// duplicating the whole ACP dialogue.
func fakePrimeACPScriptBody() string {
	return `while IFS= read -r line; do
  if [ -n "$PRIME_REQUESTS_FILE" ]; then
    printf '%s\n' "$line" >> "$PRIME_REQUESTS_FILE"
  fi
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":false,"promptCapabilities":{"image":true,"embeddedContext":true},"sessionCapabilities":{"close":{}}},"agentInfo":{"name":"prime-agent","title":"Prime Agent","version":"0.7.1"}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_prime_new"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":10,"outputTokens":20,"cacheReadTokens":3,"cacheWriteTokens":2,"costUsdTicks":900}}}\n' "$id"
      ;;
    *'"method":"session/close"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id"
      ;;
  esac
done
`
}

func writeFakePrimeScript(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "prime-agent")
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatalf("write fake prime-agent: %v", err)
	}
	return bin
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestPrimeSessionNew(t *testing.T) {
	t.Parallel()
	bin := writeFakePrimeScript(t, fakePrimeACPScript())
	reqFile := filepath.Join(t.TempDir(), "requests.txt")

	b, err := New("prime", Config{
		ExecutablePath: bin,
		Logger:         testLogger(),
		Env:            map[string]string{"PRIME_REQUESTS_FILE": reqFile},
	})
	if err != nil {
		t.Fatalf("New(prime) error: %v", err)
	}

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	for msg := range session.Messages {
		if msg.Type == MessageText {
			t.Logf("received message: %s", msg.Content)
		}
	}

	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
	}
	// Result.SessionID is deliberately never reported (see TestPrimeSessionIDNeverReported)
	// — but the RPC exchange with the real process must still have used the
	// session id from session/new for session/prompt/session/close.
	if result.SessionID != "" {
		t.Fatalf("expected Result.SessionID to be empty (never reported), got %q", result.SessionID)
	}
	raw, err := os.ReadFile(reqFile)
	if err != nil {
		t.Fatalf("read requests file: %v", err)
	}
	if !strings.Contains(string(raw), `"sessionId":"ses_prime_new"`) {
		t.Fatalf("expected the RPC exchange to use the session/new session id internally, got:\n%s", string(raw))
	}
	if result.Usage == nil {
		t.Fatal("expected usage to be non-nil")
	}
}

// TestPrimeNeverAttemptsResume is the single highest-risk-mitigation test for
// this backend: Prime Agent has no session/resume or session/load method
// (agentCapabilities.loadSession is false, confirmed empirically — see
// prime-acp-test.md). ExecOptions.ResumeSessionID is set unconditionally by
// the daemon for every provider (daemon.go:5853,6218), so primeBackend must
// ignore it and always take the session/new branch rather than translating it
// into a resume-style RPC the real binary would reject with "method not
// found".
func TestPrimeNeverAttemptsResume(t *testing.T) {
	t.Parallel()
	bin := writeFakePrimeScript(t, fakePrimeACPScript())
	reqFile := filepath.Join(t.TempDir(), "requests.txt")

	b, err := New("prime", Config{
		ExecutablePath: bin,
		Logger:         testLogger(),
		Env:            map[string]string{"PRIME_REQUESTS_FILE": reqFile},
	})
	if err != nil {
		t.Fatalf("New(prime) error: %v", err)
	}

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd: t.TempDir(),
		// A prior task turn's session id, exactly as the daemon would set it
		// unconditionally regardless of provider.
		ResumeSessionID: "ses_from_a_prior_turn",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("expected completed (fresh session/new despite ResumeSessionID), got status=%q error=%q", result.Status, result.Error)
	}
	// Result.SessionID is deliberately never reported (see
	// TestPrimeSessionIDNeverReported) — this specifically prevents a future
	// task's ResumeSessionID from being seeded from this turn.
	if result.SessionID != "" {
		t.Fatalf("expected Result.SessionID to be empty (never reported), got %q", result.SessionID)
	}

	raw, err := os.ReadFile(reqFile)
	if err != nil {
		t.Fatalf("read requests file: %v", err)
	}
	requests := string(raw)

	if !strings.Contains(requests, `"method":"session/new"`) {
		t.Fatalf("expected session/new to be called, got requests:\n%s", requests)
	}
	if strings.Contains(requests, `"method":"session/resume"`) {
		t.Fatalf("prime backend must never call session/resume (Prime Agent has no such method), got:\n%s", requests)
	}
	if strings.Contains(requests, `"method":"session/load"`) {
		t.Fatalf("prime backend must never call session/load (Prime Agent has no such method), got:\n%s", requests)
	}
}

// TestPrimeSessionIDNeverReported pins a fix found in a post-implementation
// audit: primeBackend.Execute uses the real ACP session id internally (for
// session/prompt and session/close), but must never surface it as
// Result.SessionID. The daemon persists a completed task's Result.SessionID
// as the next related task's ExecOptions.ResumeSessionID AND separately keys
// TaskContextForEnv.PriorSessionResumed / ExecOptions.ResumeExpected purely
// off task.PriorSessionID != "" (daemon.go:5687,6236) — independent of
// whether the backend would ever act on it. Since Prime never resumes
// anything (TestPrimeNeverAttemptsResume), a non-empty Result.SessionID here
// would make the daemon believe a continuation was expected on every
// follow-up turn and log/behave accordingly, even though every Prime turn is
// a cold start. Returning "" keeps that fact visible instead of implying
// continuity that does not exist.
func TestPrimeSessionIDNeverReported(t *testing.T) {
	t.Parallel()
	bin := writeFakePrimeScript(t, fakePrimeACPScript())

	b, err := New("prime", Config{ExecutablePath: bin, Logger: testLogger()})
	if err != nil {
		t.Fatalf("New(prime) error: %v", err)
	}

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	for range session.Messages {
	}

	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
	}
	if result.SessionID != "" {
		t.Fatalf("Result.SessionID must always be empty for prime (got %q) — a non-empty value "+
			"would make the daemon treat the next related task as a resume, which Prime never honors",
			result.SessionID)
	}
}

// TestPrimeCallsSessionClose verifies primeBackend calls session/close on a
// successful turn. Unlike most ACP backends in this package (which rely on
// transport teardown alone because their agent doesn't implement it), Prime
// Agent's ACP mode does implement session/close (sessionCapabilities.close is
// advertised in initialize), so calling it is the correct, idiomatic
// teardown rather than relying only on stdin EOF.
func TestPrimeCallsSessionClose(t *testing.T) {
	t.Parallel()
	bin := writeFakePrimeScript(t, fakePrimeACPScript())
	reqFile := filepath.Join(t.TempDir(), "requests.txt")

	b, err := New("prime", Config{
		ExecutablePath: bin,
		Logger:         testLogger(),
		Env:            map[string]string{"PRIME_REQUESTS_FILE": reqFile},
	})
	if err != nil {
		t.Fatalf("New(prime) error: %v", err)
	}

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	for range session.Messages {
	}
	<-session.Result

	raw, err := os.ReadFile(reqFile)
	if err != nil {
		t.Fatalf("read requests file: %v", err)
	}
	requests := string(raw)
	if !strings.Contains(requests, `"method":"session/close"`) {
		t.Fatalf("expected session/close to be called, got requests:\n%s", requests)
	}
	if !strings.Contains(requests, `"sessionId":"ses_prime_new"`) {
		t.Fatalf("expected session/close to reference the created sessionId, got requests:\n%s", requests)
	}
}

// TestPrimeSessionNewSendsEmptyMcpServers pins a regression found by a real
// prime-agent v0.7.1 smoke test: the ACP SDK's session/new request schema
// requires an `mcpServers` field to be present (rejecting the request with
// "-32602 Invalid params: mcpServers Required value is missing" otherwise),
// even though Prime's own handler never reads its contents. Phase 1 does not
// implement MCP injection for Prime, so this must always be an empty array,
// never populated from ExecOptions.McpConfig.
func TestPrimeSessionNewSendsEmptyMcpServers(t *testing.T) {
	t.Parallel()
	bin := writeFakePrimeScript(t, fakePrimeACPScript())
	reqFile := filepath.Join(t.TempDir(), "requests.txt")

	b, err := New("prime", Config{
		ExecutablePath: bin,
		Logger:         testLogger(),
		Env:            map[string]string{"PRIME_REQUESTS_FILE": reqFile},
	})
	if err != nil {
		t.Fatalf("New(prime) error: %v", err)
	}

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd: t.TempDir(),
		// McpConfig set on purpose: it must NOT leak into the mcpServers
		// array sent to Prime — Phase 1 does not implement MCP for Prime.
		McpConfig: []byte(`{"some-server":{"command":"whatever"}}`),
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	for range session.Messages {
	}
	<-session.Result

	raw, err := os.ReadFile(reqFile)
	if err != nil {
		t.Fatalf("read requests file: %v", err)
	}
	requests := string(raw)

	if !strings.Contains(requests, `"method":"session/new"`) {
		t.Fatalf("expected a session/new request, got:\n%s", requests)
	}
	if !strings.Contains(requests, `"mcpServers":[]`) {
		t.Fatalf("expected session/new to send an empty mcpServers array, got:\n%s", requests)
	}
	if strings.Contains(requests, "some-server") {
		t.Fatalf("ExecOptions.McpConfig must never reach Prime's session/new (Phase 1 does not implement MCP for this provider), got:\n%s", requests)
	}
}

func TestPrimeBlockedArgs(t *testing.T) {
	t.Parallel()
	if mode, ok := primeBlockedArgs["--mode"]; !ok || mode != blockedWithValue {
		t.Fatalf("expected --mode to be blockedWithValue in primeBlockedArgs, got %v (present=%v)", mode, ok)
	}
	if mode, ok := primeBlockedArgs["--cwd"]; !ok || mode != blockedWithValue {
		t.Fatalf("expected --cwd to be blockedWithValue in primeBlockedArgs, got %v (present=%v)", mode, ok)
	}
}

// TestPrimeBlockedModeAndCwdArgs verifies user-defined --mode/--cwd in
// custom_args cannot override the daemon-controlled ACP transport mode or
// working directory (cmd.Dir is the sole source of truth for cwd — see the
// primeBackend doc comment).
func TestPrimeBlockedModeAndCwdArgs(t *testing.T) {
	t.Parallel()

	argsFile := filepath.Join(t.TempDir(), "args.txt")
	script := fmt.Sprintf(`#!/bin/sh
echo "$@" > "%s"
while IFS= read -r line; do
  id=$(printf '%%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":false}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"sessionId":"ses_prime_blocked"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":5,"outputTokens":10}}}\n' "$id"
      ;;
    *'"method":"session/close"'*)
      printf '{"jsonrpc":"2.0","id":%%s,"result":{}}\n' "$id"
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id"
      ;;
  esac
done
`, argsFile)

	bin := writeFakePrimeScript(t, script)

	b, err := New("prime", Config{ExecutablePath: bin, Logger: testLogger()})
	if err != nil {
		t.Fatalf("New(prime) error: %v", err)
	}

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd:        t.TempDir(),
		CustomArgs: []string{"--mode", "text", "--cwd", "/evil/path"},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	for range session.Messages {
	}
	<-session.Result

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	args := string(raw)

	if strings.Contains(args, "/evil/path") {
		t.Fatal("user-defined --cwd value should be blocked")
	}
	if strings.Contains(args, "text") {
		t.Fatal("user-defined --mode value should be blocked")
	}
	if !strings.Contains(args, "--mode acp") {
		t.Fatalf("expected daemon-controlled --mode acp in command args, got:\n%s", args)
	}
}

// TestPrimeTimeout tests that a context timeout during session/new is
// reported as status=timeout. The fake script responds to initialize
// immediately, then sleeps 30s on session/new so the 5s context deadline
// expires during the session/new RPC.
func TestPrimeTimeout(t *testing.T) {
	t.Parallel()

	script := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":false}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      sleep 30
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_late"}}\n' "$id"
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id"
      ;;
  esac
done`

	bin := writeFakePrimeScript(t, script)

	b, err := New("prime", Config{ExecutablePath: bin, Logger: testLogger()})
	if err != nil {
		t.Fatalf("New(prime) error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session, err := b.Execute(ctx, "test prompt", ExecOptions{Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	for range session.Messages {
	}

	result := <-session.Result
	if result.Status != "timeout" {
		t.Fatalf("expected timeout, got status=%q error=%q", result.Status, result.Error)
	}
}

func TestPrimeBackendUsage(t *testing.T) {
	t.Parallel()
	bin := writeFakePrimeScript(t, fakePrimeACPScript())

	b, err := New("prime", Config{ExecutablePath: bin, Logger: testLogger()})
	if err != nil {
		t.Fatalf("New(prime) error: %v", err)
	}

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	for range session.Messages {
	}

	result := <-session.Result
	if result.Usage == nil {
		t.Fatal("expected usage in result")
	}
	usage, ok := result.Usage["unknown"]
	if !ok {
		t.Fatalf("expected usage entry for model 'unknown', got %+v", result.Usage)
	}
	want := TokenUsage{InputTokens: 10, OutputTokens: 20, CacheReadTokens: 3, CacheWriteTokens: 2, CostUSDTicks: 900}
	if usage != want {
		t.Fatalf("usage = %+v, want %+v", usage, want)
	}
}

// TestPrimeUsageModelIgnored verifies that opts.Model is never used as the
// usage attribution key — Prime's model selection is unsupported over ACP
// (ModelSelectionSupported("prime") is false), so usage must always be
// attributed to "unknown".
func TestPrimeUsageModelIgnored(t *testing.T) {
	t.Parallel()
	bin := writeFakePrimeScript(t, fakePrimeACPScript())

	b, err := New("prime", Config{ExecutablePath: bin, Logger: testLogger()})
	if err != nil {
		t.Fatalf("New(prime) error: %v", err)
	}

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd:   t.TempDir(),
		Model: "must-not-be-reported",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	for range session.Messages {
	}

	result := <-session.Result
	if _, ok := result.Usage["must-not-be-reported"]; ok {
		t.Fatal("opts.Model must not be used as usage attribution key for prime")
	}
	if _, ok := result.Usage["unknown"]; !ok {
		t.Fatalf("expected usage entry for model 'unknown', got %+v", result.Usage)
	}
}

// TestPrimeExtraArgsReachTheCommandLine pins MULTICA_PRIME_ARGS end to end:
// config.go reads it, daemon.go forwards it as ExecOptions.ExtraArgs, and
// ExtraArgs must land before CustomArgs, matching the precedence documented
// for every other backend that accepts both.
func TestPrimeExtraArgsReachTheCommandLine(t *testing.T) {
	t.Parallel()

	argsFile := filepath.Join(t.TempDir(), "args.txt")
	script := fmt.Sprintf(`#!/bin/sh
echo "$@" > "%s"
while IFS= read -r line; do
  id=$(printf '%%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":false}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"sessionId":"ses_prime_extra"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":5,"outputTokens":10}}}\n' "$id"
      ;;
    *'"method":"session/close"'*)
      printf '{"jsonrpc":"2.0","id":%%s,"result":{}}\n' "$id"
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id"
      ;;
  esac
done
`, argsFile)

	bin := writeFakePrimeScript(t, script)

	b, err := New("prime", Config{ExecutablePath: bin, Logger: testLogger()})
	if err != nil {
		t.Fatalf("New(prime) error: %v", err)
	}

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd:        t.TempDir(),
		ExtraArgs:  []string{"--daemon-wide"},
		CustomArgs: []string{"--per-agent"},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	for range session.Messages {
	}
	<-session.Result

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	args := string(raw)

	if !strings.Contains(args, "--daemon-wide") {
		t.Fatalf("expected ExtraArgs in command args, got:\n%s", args)
	}
	extra := strings.Index(args, "--daemon-wide")
	custom := strings.Index(args, "--per-agent")
	if custom < 0 {
		t.Fatalf("expected CustomArgs in command args, got:\n%s", args)
	}
	if extra > custom {
		t.Fatalf("ExtraArgs must precede CustomArgs, got:\n%s", args)
	}
}

// TestPrimeSetsRlmMaxDepthZero pins the blocker-1 fix: Prime's IPython-hosted
// rlm.run tool can spawn a fire-and-forget subagent that keeps streaming
// after session/prompt returns, which ACP has no RPC to wait for. Phase 1
// disables the capability entirely rather than tracking it, by forcing
// RLM_MAX_DEPTH=0 into the spawned process's environment — verified against
// prime-agent v0.7.1 source to be the sole gate _startRlmChildRun checks
// before creating a child (see the primeBackend doc comment). This test
// proves the env var actually reaches the child process.
func TestPrimeSetsRlmMaxDepthZero(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	captureFile := filepath.Join(tempDir, "env-capture.txt")
	script := "#!/bin/sh\nenv > \"$PRIME_ENV_CAPTURE_FILE\"\n" + fakePrimeACPScriptBody()
	bin := writeFakePrimeScript(t, script)

	b, err := New("prime", Config{
		ExecutablePath: bin,
		Logger:         testLogger(),
		Env:            map[string]string{"PRIME_ENV_CAPTURE_FILE": captureFile},
	})
	if err != nil {
		t.Fatalf("New(prime) error: %v", err)
	}

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
	}

	captured := readCapturedEnv(t, captureFile)
	if got, ok := captured["RLM_MAX_DEPTH"]; !ok || got != "0" {
		t.Fatalf("expected RLM_MAX_DEPTH=0 in the spawned process env, got %q (present=%v)", got, ok)
	}
}

// TestPrimeListModels pins a fix found in a post-implementation audit:
// ListModels("prime") used to fall through to the switch's default case and
// return an "unknown agent type" error, even though
// ModelSelectionSupported("prime") is false — exactly the same situation
// QwenPaw is already in, and the model-picker UI/API relies on
// ListModels not erroring for such providers. Mirrors
// TestQwenpawListModels: points at a real, executable fake that records its
// own invocation, since a non-existent path cannot prove ListModels never
// spawns a discovery subprocess (a missing binary would also silently
// produce an empty catalog for the wrong reason).
func TestPrimeListModels(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	marker := filepath.Join(dir, "invoked")
	bin := writeFakePrimeScript(t, "#!/bin/sh\ntouch '"+marker+"'\nexit 0\n")

	cat, err := ListModels(context.Background(), "prime", Command{Path: bin})
	if err != nil {
		t.Fatalf("prime ListModels should not error, got: %v", err)
	}
	if len(cat.Models) != 0 {
		t.Fatalf("prime ListModels should return empty catalog, got %d models", len(cat.Models))
	}
	if cat.Fallback {
		t.Error("prime's empty catalog is deliberate, not a discovery fallback")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("prime ListModels executed the CLI; it must return an empty catalog without spawning a discovery subprocess")
	}
}

// TestPrimeExecutableNotFound mirrors the "provider not installed" path
// every other backend's Execute exercises via exec.LookPath.
func TestPrimeExecutableNotFound(t *testing.T) {
	t.Parallel()
	b, err := New("prime", Config{ExecutablePath: "/nonexistent/prime-agent", Logger: testLogger()})
	if err != nil {
		t.Fatalf("New(prime) error: %v", err)
	}
	if _, err := b.Execute(context.Background(), "test prompt", ExecOptions{Cwd: t.TempDir()}); err == nil {
		t.Fatal("expected an error when prime-agent is not installed")
	}
}

// TestPrimeSessionNewMalformedResponse verifies a session/new response with
// no sessionId is treated as a failure rather than silently proceeding with
// an empty session id.
func TestPrimeSessionNewMalformedResponse(t *testing.T) {
	t.Parallel()

	script := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":false}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id"
      ;;
  esac
done
`
	bin := writeFakePrimeScript(t, script)

	b, err := New("prime", Config{ExecutablePath: bin, Logger: testLogger()})
	if err != nil {
		t.Fatalf("New(prime) error: %v", err)
	}

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	for range session.Messages {
	}

	result := <-session.Result
	if result.Status != "failed" {
		t.Fatalf("expected failed, got status=%q", result.Status)
	}
	if !strings.Contains(result.Error, "no session ID") {
		t.Fatalf("expected 'no session ID' error, got %q", result.Error)
	}
}

// TestPrimeFailsClosedOnGlobalRlmMaxDepth pins the boundary RLM_MAX_DEPTH=0
// cannot defend on its own.
//
// Prime resolves rlmMaxDepth from its persisted global settings.json before it
// looks at RLM_MAX_DEPTH, so an operator who ran `/rlm-max-depth <n> --global`
// silently re-enables fire-and-forget subagents for Multica runs too. Those
// children outlive session/prompt, so Multica would report the task complete
// and tear the session down while they are still working. Multica cannot
// outrank the setting — isolating PRIME_AGENT_CODING_AGENT_DIR would take
// auth.json with it, and ACP has no per-session override — so the run is
// refused instead.
//
// The cases that must NOT refuse are the point of the table: each is a value
// Prime itself discards (isNonNegativeInteger), falling through to the env var
// that already disables subagents. Refusing those would break working setups.
// ExecutablePath is a path that does not exist, so a run that gets past the
// gate fails at the executable lookup and nothing is ever spawned.
func TestPrimeFailsClosedOnGlobalRlmMaxDepth(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		settings  string // "" writes no settings.json at all
		wantRefus bool
	}{
		{"non-zero global override refuses the run", `{"rlmMaxDepth": 2}`, true},
		{"depth 1 is still a subagent", `{"rlmMaxDepth": 1}`, true},
		{"zero is the state Multica wants", `{"rlmMaxDepth": 0}`, false},
		{"absent key falls through to the env var", `{"defaultModel": "x"}`, false},
		{"no settings file at all", "", false},
		{"negative is discarded by Prime", `{"rlmMaxDepth": -1}`, false},
		{"fractional is not a safe integer", `{"rlmMaxDepth": 1.5}`, false},
		{"string is not a number", `{"rlmMaxDepth": "2"}`, false},
		{"malformed settings are not an override", `{`, false},
		// Number.MAX_SAFE_INTEGER is the last value Prime honours; one past it
		// fails isNonNegativeInteger there and falls back to RLM_MAX_DEPTH=0,
		// so refusing it here would reject a host Prime runs safely.
		{"max safe integer is still an override", `{"rlmMaxDepth": 9007199254740991}`, true},
		{"one past max safe integer is discarded", `{"rlmMaxDepth": 9007199254740992}`, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			agentDir := t.TempDir()
			if tc.settings != "" {
				if err := os.WriteFile(filepath.Join(agentDir, "settings.json"), []byte(tc.settings), 0o600); err != nil {
					t.Fatalf("write settings.json: %v", err)
				}
			}

			backend, err := New("prime", Config{
				ExecutablePath: filepath.Join(t.TempDir(), "prime-agent-does-not-exist"),
				Logger:         testLogger(),
				Env:            map[string]string{"PRIME_AGENT_CODING_AGENT_DIR": agentDir},
			})
			if err != nil {
				t.Fatalf("new prime backend: %v", err)
			}

			_, err = backend.Execute(context.Background(), "prompt", ExecOptions{Cwd: t.TempDir()})
			if err == nil {
				t.Fatal("Execute returned no error; the missing executable should always stop it")
			}
			refused := strings.Contains(err.Error(), "rlmMaxDepth")
			if refused != tc.wantRefus {
				t.Fatalf("refused=%v want %v; error was: %v", refused, tc.wantRefus, err)
			}
			if refused && !strings.Contains(err.Error(), agentDir) {
				t.Fatalf("the refusal must name the settings file the operator has to edit: %v", err)
			}
			if !refused && !strings.Contains(err.Error(), "executable not found") {
				t.Fatalf("expected the run to reach the executable lookup, got: %v", err)
			}
		})
	}
}

// TestPrimeAgentDirForMatchesTheChildProcess pins the resolver against
// getAgentDir's actual semantics, because the fail-closed gate is only as good
// as its agreement with the file the child really reads.
//
// Three ways they can diverge, each covered below. Prime tests the raw string
// with `if (envDir)`, so it never trims and treats "" and " " differently. The
// merged environment lets a configured value shadow an inherited one, empty
// value included. And a relative value resolves against the child's working
// directory — opts.Cwd — not the daemon's.
func TestPrimeAgentDirForMatchesTheChildProcess(t *testing.T) {
	t.Parallel()

	// The env slices below never name HOME/USERPROFILE, so the resolver takes
	// the account-home fallback. Read it through the same seam the resolver
	// uses rather than os.UserHomeDir, which answers from the daemon's own
	// environment and is exactly what this resolver must not do.
	home, err := primeAccountHome()
	if err != nil {
		t.Skipf("no account home directory on this host: %v", err)
	}
	defaultDir := filepath.Join(home, ".prime", "agent")

	cases := []struct {
		name string
		env  []string
		cwd  string
		want string
	}{
		{"unset falls back to the default", []string{"PATH=/usr/bin"}, "/work", defaultDir},
		{"absolute value is used as-is", []string{"PRIME_AGENT_CODING_AGENT_DIR=/opt/prime"}, "/work", "/opt/prime"},
		{
			"explicitly empty is falsy for Prime, not absent",
			[]string{"PRIME_AGENT_CODING_AGENT_DIR=/inherited", "PRIME_AGENT_CODING_AGENT_DIR="},
			"/work", defaultDir,
		},
		{
			"whitespace is truthy for Prime and is a relative path",
			[]string{"PRIME_AGENT_CODING_AGENT_DIR= "},
			"/work", filepath.Join("/work", " "),
		},
		{
			"relative value resolves against the child's cwd",
			[]string{"PRIME_AGENT_CODING_AGENT_DIR=prime-config"},
			"/work", filepath.Join("/work", "prime-config"),
		},
		{
			"a later entry shadows an earlier one, as exec does",
			[]string{"PRIME_AGENT_CODING_AGENT_DIR=/first", "PRIME_AGENT_CODING_AGENT_DIR=/second"},
			"/work", "/second",
		},
		{"tilde expands to the home directory", []string{"PRIME_AGENT_CODING_AGENT_DIR=~/p"}, "/work", filepath.Join(home, "p")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := primeAgentDirFor(tc.env, tc.cwd)
			if err != nil {
				t.Fatalf("primeAgentDirFor: %v", err)
			}
			if got != tc.want {
				t.Fatalf("primeAgentDirFor = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestPrimeFailsClosedOnRelativeGlobalAgentDir is the concrete escape the
// resolver test guards against, driven through Execute: a relative
// PRIME_AGENT_CODING_AGENT_DIR points at a settings.json inside the task
// workdir, which a gate resolving against the daemon's own directory would
// never open — leaving the fire-and-forget completion problem live.
func TestPrimeFailsClosedOnRelativeGlobalAgentDir(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	agentDir := filepath.Join(cwd, "prime-config")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "settings.json"), []byte(`{"rlmMaxDepth": 1}`), 0o600); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	backend, err := New("prime", Config{
		ExecutablePath: filepath.Join(t.TempDir(), "prime-agent-does-not-exist"),
		Logger:         testLogger(),
		Env:            map[string]string{"PRIME_AGENT_CODING_AGENT_DIR": "prime-config"},
	})
	if err != nil {
		t.Fatalf("new prime backend: %v", err)
	}

	_, err = backend.Execute(context.Background(), "prompt", ExecOptions{Cwd: cwd})
	if err == nil || !strings.Contains(err.Error(), "rlmMaxDepth") {
		t.Fatalf("a relative agent dir under the task workdir must still be seen by the gate, got: %v", err)
	}
	if !strings.Contains(err.Error(), agentDir) {
		t.Fatalf("the refusal must name the resolved settings file: %v", err)
	}
}

// withPrimeGOOS points the resolver at a target platform for one test.
func withPrimeGOOS(t *testing.T, goos string) {
	t.Helper()
	previous := primeGOOS
	primeGOOS = goos
	t.Cleanup(func() { primeGOOS = previous })
}

// withPrimeAccountHome points the account-home fallback at a fixed answer for
// one test, so both the resolved and the unresolvable branch can be driven on
// any host. Without it these cases would depend on the runner's passwd entry —
// and the previous version of this test skipped itself whenever HOME was
// absent, which silently retired the very case it was meant to cover.
func withPrimeAccountHome(t *testing.T, dir string, err error) {
	t.Helper()
	previous := primeAccountHome
	primeAccountHome = func() (string, error) { return dir, err }
	t.Cleanup(func() { primeAccountHome = previous })
}

// TestPrimeHomeDirFollowsTheChildEnvironment closes the last way the
// fail-closed gate could read a different settings.json from the one Prime
// opens.
//
// PRIME_AGENT_CODING_AGENT_DIR is only half of the resolution; when it is
// unset, the path is <home>/.prime/agent, and `home` is itself environment-
// derived. custom_env accepts any key and has no blocklist, so an agent can
// set HOME (POSIX) or USERPROFILE (Windows) and move the child's os.homedir()
// somewhere the daemon's os.UserHomeDir() never points. Reading the daemon's
// home would then miss a real global override.
//
// The states are distinct, none of them is the daemon's own home, and the two
// platforms disagree about one of them:
//
//   - set to a path: that path, on both runtimes.
//   - set and empty, POSIX: os.homedir() is "" because uv_os_getenv reports a
//     zero-length hit rather than a miss, and uv_os_homedir returns it
//     unchanged. getAgentDir then returns the relative ".prime/agent". Go's
//     os.UserHomeDir calls the same variable an error, so mirroring it here
//     would have produced the daemon's home.
//   - set and shorter than three bytes, Windows: not decidable from here — the
//     bundled libuv changed behaviour inside prime-agent's supported Node
//     range — so the adapter refuses instead of resolving. That case has no
//     value to pin here; it lives in the fail-closed test below.
//   - absent: libuv falls through to the account database. os.UserHomeDir
//     fails instead, so the fallback has to come from os/user.
//
// HOMEDRIVE and HOMEPATH are covered here precisely because they must NOT
// matter: os/user keys off the process token on Windows, and so does libuv's
// uv_os_homedir before consulting USERPROFILE. Neither side moves, so they
// cannot desynchronise the two — a test that asserted otherwise would be
// pinning behaviour Prime does not have.
func TestPrimeHomeDirFollowsTheChildEnvironment(t *testing.T) {
	const accountHome = "/account/home"
	withPrimeAccountHome(t, accountHome, nil)

	cases := []struct {
		name string
		goos string
		env  []string
		want string
	}{
		{"posix: HOME from custom_env wins", "linux", []string{"HOME=/agent/home"}, "/agent/home"},
		{"posix: absent HOME falls back to the account database", "linux", []string{"PATH=/usr/bin"}, accountHome},
		{"posix: empty HOME is an empty home, not an absent one", "linux", []string{"HOME="}, ""},
		{
			"posix: an empty entry still shadows an earlier path",
			"linux", []string{"HOME=/inherited", "HOME="}, "",
		},
		{"posix: a later entry shadows an earlier one", "linux", []string{"HOME=/first", "HOME=/second"}, "/second"},
		{"posix: USERPROFILE is not consulted", "linux", []string{"USERPROFILE=C:\\agent"}, accountHome},

		{"windows: USERPROFILE from custom_env wins", "windows", []string{`USERPROFILE=C:\agent`}, `C:\agent`},
		{"windows: names are case-insensitive", "windows", []string{`USERPROFILE=C:\inherited`, `userprofile=C:\agent`}, `C:\agent`},
		{"windows: three bytes is the shortest USERPROFILE the adapter accepts", "windows", []string{`USERPROFILE=C:\`}, `C:\`},
		{"windows: absent USERPROFILE falls back to the process token", "windows", []string{"PATH=C:\\Windows"}, accountHome},
		{"windows: HOME is not consulted", "windows", []string{"HOME=/agent/home"}, accountHome},
		{
			"windows: HOMEDRIVE and HOMEPATH move neither runtime",
			"windows", []string{`HOMEDRIVE=D:`, `HOMEPATH=\agent`}, accountHome,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withPrimeGOOS(t, tc.goos)
			got, err := primeHomeDir(tc.env)
			if err != nil {
				t.Fatalf("primeHomeDir: %v", err)
			}
			if got != tc.want {
				t.Fatalf("primeHomeDir = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestPrimeHomeDirFailsClosedWhenTheAccountHomeIsUnknown pins the third
// outcome. An absent HOME with no readable account entry is not "no global
// settings to inspect": the child would still resolve a home of its own and
// could still read an rlmMaxDepth from it. Returning a directory the daemon
// cannot justify — its own — is what reopened the bug; returning an error is
// what lets Execute refuse.
func TestPrimeHomeDirFailsClosedWhenTheAccountHomeIsUnknown(t *testing.T) {
	withPrimeGOOS(t, "linux")

	t.Run("lookup fails", func(t *testing.T) {
		withPrimeAccountHome(t, "", errors.New("no passwd entry"))
		if _, err := primeHomeDir([]string{"PATH=/usr/bin"}); !errors.Is(err, errPrimeHomeUnresolved) {
			t.Fatalf("primeHomeDir error = %v, want errPrimeHomeUnresolved", err)
		}
	})

	t.Run("lookup succeeds with an empty directory", func(t *testing.T) {
		withPrimeAccountHome(t, "", nil)
		if _, err := primeHomeDir([]string{"PATH=/usr/bin"}); !errors.Is(err, errPrimeHomeUnresolved) {
			t.Fatalf("primeHomeDir error = %v, want errPrimeHomeUnresolved", err)
		}
	})

	t.Run("an explicit empty value is still resolved, not refused", func(t *testing.T) {
		withPrimeAccountHome(t, "", errors.New("no passwd entry"))
		home, err := primeHomeDir([]string{"HOME="})
		if err != nil {
			t.Fatalf("an explicitly empty HOME is a proven home, not an unresolvable one: %v", err)
		}
		if home != "" {
			t.Fatalf("primeHomeDir = %q, want an empty home", home)
		}
	})
}

// TestPrimeHomeDirRefusesAShortWindowsUserprofile pins a deliberate refusal,
// not a reproduction of any one runtime's behaviour.
//
// uv_os_homedir's POSIX path returns whatever uv_os_getenv found, empty
// included, at every Node version in range. Its Windows path does not agree
// with itself across that range:
//
//	if (r == 0 && *size < 3) { return UV_ENOENT; }   (deps/uv/src/win/util.c)
//
// is present in Node 22.14.0 and absent in Node 22.8.0, which bundles libuv
// 1.48.0 and is the floor prime-agent v0.7.1 declares in
// cli/node-version-check.ts. The same USERPROFILE therefore makes os.homedir()
// throw on one supported runtime and resolve to a relative .prime\agent on
// another, and ACP carries no runtime version the adapter could branch on.
//
// Committing to either reading would point the gate at a settings file the
// child may well not open — the same class of divergence as reading the
// daemon's home. Refusing is the only answer that is right on both, so these
// cases assert the refusal rather than a resolved path.
//
// The account fallback is left failing throughout, so a case that wrongly took
// it would surface as the wrong error rather than passing.
func TestPrimeHomeDirRefusesAShortWindowsUserprofile(t *testing.T) {
	withPrimeAccountHome(t, "", errors.New("account database must not be consulted here"))

	for _, value := range []string{"", "C", `C:`} {
		t.Run("windows: "+strconv.Quote(value), func(t *testing.T) {
			withPrimeGOOS(t, "windows")
			if _, err := primeHomeDir([]string{"USERPROFILE=" + value}); !errors.Is(err, errPrimeHomeUnresolved) {
				t.Fatalf("primeHomeDir error = %v, want errPrimeHomeUnresolved", err)
			}
		})

		t.Run("posix keeps the same value: "+strconv.Quote(value), func(t *testing.T) {
			withPrimeGOOS(t, "linux")
			home, err := primeHomeDir([]string{"HOME=" + value})
			if err != nil {
				t.Fatalf("POSIX has no length check on HOME: %v", err)
			}
			if home != value {
				t.Fatalf("primeHomeDir = %q, want %q", home, value)
			}
		})
	}
}

// TestPrimeAgentDirForFollowsAnEmptyHomeToTheChildCwd is the resolver half of
// the empty-home case: join("", CONFIG_DIR_NAME) is relative on the Node side,
// so the child opens it under its own working directory.
func TestPrimeAgentDirForFollowsAnEmptyHomeToTheChildCwd(t *testing.T) {
	withPrimeGOOS(t, "linux")
	withPrimeAccountHome(t, "/account/home", nil)

	got, err := primeAgentDirFor([]string{"HOME="}, "/work")
	if err != nil {
		t.Fatalf("primeAgentDirFor: %v", err)
	}
	if want := filepath.Join("/work", ".prime", "agent"); got != want {
		t.Fatalf("primeAgentDirFor = %q, want %q", got, want)
	}
}

// TestPrimeFailsClosedWhenCustomEnvMovesHome is the escape driven end to end:
// custom_env relocates the child's home, the override lives under that home's
// .prime/agent, and a gate reading the daemon's home would never open it.
func TestPrimeFailsClosedWhenCustomEnvMovesHome(t *testing.T) {
	home := t.TempDir()
	agentDir := filepath.Join(home, ".prime", "agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "settings.json"), []byte(`{"rlmMaxDepth": 3}`), 0o600); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	// The key the running platform actually resolves home from; primeGOOS is
	// left alone so this exercises the real code path on this runner.
	backend, err := New("prime", Config{
		ExecutablePath: filepath.Join(t.TempDir(), "prime-agent-does-not-exist"),
		Logger:         testLogger(),
		Env:            map[string]string{primeHomeEnvKey(): home},
	})
	if err != nil {
		t.Fatalf("new prime backend: %v", err)
	}

	_, err = backend.Execute(context.Background(), "prompt", ExecOptions{Cwd: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "rlmMaxDepth") {
		t.Fatalf("custom_env moved the child's home; the gate must follow it, got: %v", err)
	}
	if !strings.Contains(err.Error(), agentDir) {
		t.Fatalf("the refusal must name the settings file under the relocated home: %v", err)
	}
}

// TestPrimeFailsClosedWhenTheChildHomeIsEmpty drives the empty-value case end
// to end.
//
// With HOME set and empty, the child's os.homedir() is "" and getAgentDir
// returns the relative ".prime/agent", which the child opens under its own
// working directory. A gate that treated the empty value as unset would read
// the daemon's ~/.prime/agent instead and never see this file — the run would
// start with subagents re-enabled and Multica would report it complete while
// they were still working.
func TestPrimeFailsClosedWhenTheChildHomeIsEmpty(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		// A USERPROFILE this short is refused rather than resolved on Windows,
		// because the supported Node versions disagree about what it means, so
		// there is no relative agent dir to find here. That refusal is pinned
		// by TestPrimeHomeDirRefusesAShortWindowsUserprofile.
		t.Skip("an empty USERPROFILE is refused on Windows, not resolved")
	}

	cwd := t.TempDir()
	agentDir := filepath.Join(cwd, ".prime", "agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "settings.json"), []byte(`{"rlmMaxDepth": 2}`), 0o600); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	// primeGOOS is left alone: primeHomeEnvKey names whichever variable this
	// runner actually resolves home from, so the real code path is exercised.
	backend, err := New("prime", Config{
		ExecutablePath: filepath.Join(t.TempDir(), "prime-agent-does-not-exist"),
		Logger:         testLogger(),
		Env:            map[string]string{primeHomeEnvKey(): ""},
	})
	if err != nil {
		t.Fatalf("new prime backend: %v", err)
	}

	_, err = backend.Execute(context.Background(), "prompt", ExecOptions{Cwd: cwd})
	if err == nil || !strings.Contains(err.Error(), "rlmMaxDepth") {
		t.Fatalf("an empty home makes the child's agent dir relative to its cwd; the gate must follow it, got: %v", err)
	}
	if !strings.Contains(err.Error(), agentDir) {
		t.Fatalf("the refusal must name the settings file under the task workdir: %v", err)
	}
}

// unsetChildHomeEnv removes the home variable from the daemon's own
// environment for one test, so buildEnv hands the child an environment that
// genuinely lacks it. t.Setenv is called first only for its cleanup, which
// restores whatever the value was — including restoring it as absent.
//
// Tests using this cannot be parallel; t.Setenv enforces that.
func unsetChildHomeEnv(t *testing.T) {
	t.Helper()
	key := primeHomeEnvKey()
	t.Setenv(key, "")
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
}

// TestPrimeFailsClosedWhenTheChildHomeIsAbsent drives the absent-value case
// end to end. Node falls through to the account database here and reads a real
// settings.json; Go's os.UserHomeDir fails instead, and the previous resolver
// turned that failure into an empty directory and skipped the check entirely.
func TestPrimeFailsClosedWhenTheChildHomeIsAbsent(t *testing.T) {
	unsetChildHomeEnv(t)

	accountHome := t.TempDir()
	withPrimeAccountHome(t, accountHome, nil)

	agentDir := filepath.Join(accountHome, ".prime", "agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "settings.json"), []byte(`{"rlmMaxDepth": 4}`), 0o600); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	backend, err := New("prime", Config{
		ExecutablePath: filepath.Join(t.TempDir(), "prime-agent-does-not-exist"),
		Logger:         testLogger(),
	})
	if err != nil {
		t.Fatalf("new prime backend: %v", err)
	}

	_, err = backend.Execute(context.Background(), "prompt", ExecOptions{Cwd: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "rlmMaxDepth") {
		t.Fatalf("an absent home sends the child to the account database; the gate must follow it, got: %v", err)
	}
	if !strings.Contains(err.Error(), agentDir) {
		t.Fatalf("the refusal must name the settings file under the account home: %v", err)
	}
}

// TestPrimeRefusesWhenTheChildHomeCannotBeProven is the fail-closed end of the
// same path. Neither the environment nor the account database can name the
// child's home, so the daemon cannot say which settings.json the run would
// read. Starting anyway would be the original fire-and-forget bug with no
// evidence either way, so Execute refuses before the executable lookup and
// says which variable would fix it.
func TestPrimeRefusesWhenTheChildHomeCannotBeProven(t *testing.T) {
	unsetChildHomeEnv(t)
	withPrimeAccountHome(t, "", errors.New("user: Current requires cgo"))

	backend, err := New("prime", Config{
		ExecutablePath: filepath.Join(t.TempDir(), "prime-agent-does-not-exist"),
		Logger:         testLogger(),
	})
	if err != nil {
		t.Fatalf("new prime backend: %v", err)
	}

	_, err = backend.Execute(context.Background(), "prompt", ExecOptions{Cwd: t.TempDir()})
	if err == nil {
		t.Fatal("an unprovable home must refuse the run, not skip the check")
	}
	if !strings.Contains(err.Error(), "cannot determine which prime-agent settings.json") {
		t.Fatalf("the refusal must say the settings file could not be located: %v", err)
	}
	if !strings.Contains(err.Error(), primeHomeEnvKey()) {
		t.Fatalf("the refusal must name the variable that would fix it: %v", err)
	}
	if strings.Contains(err.Error(), "executable not found") {
		t.Fatalf("the gate must run before the executable lookup: %v", err)
	}
}

// TestPrimeStillRunsWhenTheProvenHomeHasNoSettings keeps the fail-closed gate
// from becoming a fail-always one: a home the resolver *can* prove, with no
// global settings.json under it, must let the run reach the executable lookup.
func TestPrimeStillRunsWhenTheProvenHomeHasNoSettings(t *testing.T) {
	unsetChildHomeEnv(t)
	withPrimeAccountHome(t, t.TempDir(), nil)

	backend, err := New("prime", Config{
		ExecutablePath: filepath.Join(t.TempDir(), "prime-agent-does-not-exist"),
		Logger:         testLogger(),
	})
	if err != nil {
		t.Fatalf("new prime backend: %v", err)
	}

	_, err = backend.Execute(context.Background(), "prompt", ExecOptions{Cwd: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "executable not found") {
		t.Fatalf("a proven home with no settings.json must not block the run, got: %v", err)
	}
}

// TestPrimeLogsTheAcpModeWithoutLeakingCustomArgs pins what the launch log
// keeps and what it drops for this backend.
//
// `--mode acp` is the adapter's own invocation and stays readable, which is
// what makes the log worth having. Everything a user or operator can put on
// the command line — MULTICA_PRIME_ARGS, custom_args — is a value, and values
// are what leaked before #7206: flag names survive, their arguments do not.
//
// The trust is pinned to the argv index the adapter owns, not to the token, so
// the second case feeds the identical literal in from custom_args and expects
// it redacted. A global allowlist on the string "acp" would pass the first
// case and fail this one.
func TestPrimeLogsTheAcpModeWithoutLeakingCustomArgs(t *testing.T) {
	t.Parallel()

	const secret = "sk-super-secret-token"

	cases := []struct {
		name      string
		primeArgs []string
		keep      []string
		// acpTokens is how many times the literal appears in the log: once for
		// the adapter's own --mode value, never for a copy from custom_args.
		acpTokens int
	}{
		{
			name:      "adapter invocation survives, argument values do not",
			primeArgs: []string{"--mode", "acp", "--api-key", secret, secret},
			keep:      []string{"provider=prime", "--mode", "acp", "--api-key"},
			acpTokens: 1,
		},
		{
			name:      "the same literal from custom_args is still redacted",
			primeArgs: []string{"--mode", "acp", "--resume", "acp", secret},
			keep:      []string{"provider=prime", "--mode", "--resume"},
			acpTokens: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf strings.Builder
			backend, err := New("prime", Config{Logger: slog.New(slog.NewTextHandler(&buf, nil))})
			if err != nil {
				t.Fatalf("new prime backend: %v", err)
			}
			primeCfg := backend.(*primeBackend).cfg

			cmd := primeCfg.commandAt("prime-agent").exec(context.Background(), tc.primeArgs...)
			primeCfg.logAgentCommand(cmd, newAgentCommandLogArgs(tc.primeArgs, trustAgentCommandPositional(1, "acp")))

			logged := buf.String()
			if strings.Contains(logged, secret) {
				t.Fatalf("the launch log must not carry argument values: %s", logged)
			}
			for _, want := range tc.keep {
				if !strings.Contains(logged, want) {
					t.Fatalf("the launch log must keep %q for diagnostics: %s", want, logged)
				}
			}
			if got := strings.Count(logged, "acp"); got != tc.acpTokens {
				t.Fatalf("the literal acp appears %d times, want %d — trust follows the argv index, not the token: %s",
					got, tc.acpTokens, logged)
			}
		})
	}
}

// TestPrimeThinkingNeverReachesResultOutput covers the half of the deliverable
// boundary the shared acpDeliverableCases table cannot reach: reasoning.
//
// Prime streams reasoning as `agent_thought_chunk` (verified against
// prime-agent v0.7.1's own packages/coding-agent/test/acp-events.test.ts),
// which the ACP transport surfaces as MessageThinking. The shared regression
// test drives narration, which arrives as ordinary MessageText and is bounded
// by the tool call; a thought chunk is bounded by nothing, so a turn with no
// tool call at all would still leak it if Result.Output were rebuilt from
// every message rather than from the tracker's text stream.
//
// Reasoning must stay visible in session.Messages — the UI renders it — while
// never reaching Result.Output, which becomes the channel reply and the
// auto-generated issue comment.
func TestPrimeThinkingNeverReachesResultOutput(t *testing.T) {
	t.Parallel()

	const (
		thought = "The user probably means the deployment diagram."
		answer  = "It is the deployment diagram."
	)

	notify := func(update string) string {
		return `      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_prime_new","update":` +
			update + `}}\n'`
	}
	script := "#!/bin/sh\n" + `while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":false}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_prime_new"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
` + notify(`{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"`+thought+`"}}`) + `
` + notify(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"`+answer+`"}}`) + `
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      ;;
    *'"method":"session/close"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
  esac
done
`

	b, err := New("prime", Config{
		ExecutablePath: writeFakePrimeScript(t, script),
		Logger:         testLogger(),
		// Prime's fail-closed rlmMaxDepth gate resolves the settings path from
		// the child environment, so pin it away from the developer's own home.
		Env: map[string]string{primeHomeEnvKey(): t.TempDir()},
	})
	if err != nil {
		t.Fatalf("New(prime) error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	session, err := b.Execute(ctx, "which diagram?", ExecOptions{Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var sawThinking bool
	for msg := range session.Messages {
		if msg.Type == MessageThinking && strings.Contains(msg.Content, thought) {
			sawThinking = true
		}
	}

	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed (error=%q)", result.Status, result.Error)
	}
	if !sawThinking {
		t.Error("reasoning never reached session.Messages as MessageThinking; the UI would lose it")
	}
	if strings.Contains(result.Output, thought) {
		t.Errorf("Result.Output = %q leaked the reasoning chunk %q", result.Output, thought)
	}
	if result.Output != answer {
		t.Errorf("Result.Output = %q, want the deliverable answer %q", result.Output, answer)
	}
}

// TestPrimeSelfCancelledTurnIsAborted covers a turn Prime ends on its own.
//
// Multica never sends session/cancel, so stopReason "cancelled" means Prime
// stopped for its own reasons and the output is truncated. Reporting
// "completed" would present a partial answer as a finished one — every other
// ACP backend here (hermes, kimi, kiro, qoder, traecli, grok, mcode) reports
// "aborted".
//
// The second assertion is the one that is easy to lose: session/close must
// still be sent. This path returns normally over a live connection, unlike an
// externally cancelled run whose transport is already being torn down, and
// Prime is one of the few ACP backends that actually implements session/close.
func TestPrimeSelfCancelledTurnIsAborted(t *testing.T) {
	t.Parallel()

	reqFile := filepath.Join(t.TempDir(), "requests.txt")
	script := "#!/bin/sh\n" + `while IFS= read -r line; do
  printf '%s\n' "$line" >> "` + reqFile + `"
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":false}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_prime_new"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"cancelled"}}\n' "$id"
      ;;
    *'"method":"session/close"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
  esac
done
`

	b, err := New("prime", Config{
		ExecutablePath: writeFakePrimeScript(t, script),
		Logger:         testLogger(),
		Env:            map[string]string{primeHomeEnvKey(): t.TempDir()},
	})
	if err != nil {
		t.Fatalf("New(prime) error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	session, err := b.Execute(ctx, "do something long", ExecOptions{Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	for range session.Messages {
	}

	result := <-session.Result
	if result.Status != "aborted" {
		t.Errorf("Status = %q, want aborted — Prime ended the turn itself, so the output is truncated", result.Status)
	}

	requests, err := os.ReadFile(reqFile)
	if err != nil {
		t.Fatalf("read captured requests: %v", err)
	}
	if !strings.Contains(string(requests), `"method":"session/close"`) {
		t.Errorf("session/close was never sent; the connection was still live and Prime's session was left open.\nrequests:\n%s", requests)
	}
}
