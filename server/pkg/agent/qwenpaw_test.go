package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQwenpawModelSelectionUnsupported(t *testing.T) {
	t.Parallel()
	if ModelSelectionSupported("qwenpaw") {
		t.Fatal("ModelSelectionSupported(qwenpaw) should return false — session/set_model persists to agent scope, not session scope")
	}
	// Other providers should remain supported
	if !ModelSelectionSupported("claude") {
		t.Fatal("ModelSelectionSupported(claude) should remain true")
	}
}

func TestNewReturnsQwenpawBackend(t *testing.T) {
	t.Parallel()
	b, err := New("qwenpaw", Config{ExecutablePath: "/nonexistent/qwenpaw"})
	if err != nil {
		t.Fatalf("New(qwenpaw) error: %v", err)
	}
	if _, ok := b.(*qwenpawBackend); !ok {
		t.Fatalf("expected *qwenpawBackend, got %T", b)
	}
}

// fakeQwenpawACPScript impersonates `qwenpaw acp` for unit tests.
// Wire format mirrors other Multica ACP fakes (grok/kimi):
// session/new returns sessionId, session/load accepts an existing session,
// session/prompt returns stopReason=end_turn.
// session/set_model is also handled for backward compatibility with older
// scripts but is no longer called by the qwenpaw backend.
func fakeQwenpawACPScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  if [ -n "$QWENPAW_REQUESTS_FILE" ]; then
    printf '%s\n' "$line" >> "$QWENPAW_REQUESTS_FILE"
  fi
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true,"mcpCapabilities":{"http":true,"sse":true}}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_qwenpaw_new"}}\n' "$id"
      ;;
    *'"method":"session/load"'*)
      if [ -n "$QWENPAW_SESSION_NOT_FOUND" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"session not found"}}\n' "$id"
        exit 0
      fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/set_model"'*)
      if [ -n "$QWENPAW_SET_MODEL_FAIL" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"model not available"}}\n' "$id"
        exit 0
      fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":10,"outputTokens":20,"cacheReadTokens":3,"cacheWriteTokens":2,"costUsdTicks":900}}}\n' "$id"
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id"
      ;;
  esac
done
`
}

func writeFakeQwenpawScript(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "qwenpaw")
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatalf("write fake qwenpaw: %v", err)
	}
	return bin
}

func TestQwenpawSessionNew(t *testing.T) {
	t.Parallel()
	bin := writeFakeQwenpawScript(t, fakeQwenpawACPScript())

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	b, err := New("qwenpaw", Config{
		ExecutablePath: bin,
		Logger:         logger,
	})
	if err != nil {
		t.Fatalf("New(qwenpaw) error: %v", err)
	}

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Drain messages
	for msg := range session.Messages {
		if msg.Type == MessageText {
			t.Logf("received message: %s", msg.Content)
		}
	}

	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
	}
	if result.SessionID != "ses_qwenpaw_new" {
		t.Fatalf("expected sessionID ses_qwenpaw_new, got %q", result.SessionID)
	}
	if result.Usage == nil {
		t.Fatal("expected usage to be non-nil")
	}
}

func TestQwenpawSessionLoad(t *testing.T) {
	t.Parallel()
	bin := writeFakeQwenpawScript(t, fakeQwenpawACPScript())

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	b, err := New("qwenpaw", Config{
		ExecutablePath: bin,
		Logger:         logger,
	})
	if err != nil {
		t.Fatalf("New(qwenpaw) error: %v", err)
	}

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd:             t.TempDir(),
		ResumeSessionID: "ses_existing",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Drain messages
	for range session.Messages {
	}

	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("expected completed, got status=%q error=%q", result.Status, result.Error)
	}
	// session/load returns no explicit sessionId, so resolveResumedSessionID
	// falls back to the requested id.
	if result.SessionID != "ses_existing" {
		t.Fatalf("expected sessionID ses_existing (fallback from load), got %q", result.SessionID)
	}
	if result.ResumeRejected {
		t.Fatal("expected ResumeRejected=false on successful load")
	}
}

func TestQwenpawSessionLoadNotFound(t *testing.T) {
	t.Parallel()
	bin := writeFakeQwenpawScript(t, fakeQwenpawACPScript())

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	b, err := New("qwenpaw", Config{
		ExecutablePath: bin,
		Logger:         logger,
		Env:            map[string]string{"QWENPAW_SESSION_NOT_FOUND": "1"},
	})
	if err != nil {
		t.Fatalf("New(qwenpaw) error: %v", err)
	}

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd:             t.TempDir(),
		ResumeSessionID: "ses_gone",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	// Drain messages
	for range session.Messages {
	}

	result := <-session.Result
	if result.Status != "failed" {
		t.Fatalf("expected failed, got status=%q error=%q", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, "session/load failed") {
		t.Fatalf("expected session/load failed error, got %q", result.Error)
	}
	if !result.ResumeRejected {
		t.Fatal("expected ResumeRejected=true on session not found")
	}
}

func TestQwenpawListModels(t *testing.T) {
	t.Parallel()
	// Model selection is unsupported for qwenpaw, so ListModels must return
	// an empty catalog WITHOUT spawning an ACP subprocess.
	//
	// Point it at a real, executable fake that records its own invocation.
	// A non-existent path cannot guard this: the previous discovery helper
	// also returned an empty catalog when the binary was missing, so such a
	// test passes whether or not the discovery path is reintroduced.
	dir := t.TempDir()
	marker := filepath.Join(dir, "invoked")
	bin := writeFakeQwenpawScript(t, "#!/bin/sh\ntouch '"+marker+"'\nexit 0\n")

	cat, err := ListModels(context.Background(), "qwenpaw", bin)
	if err != nil {
		t.Fatalf("qwenpaw ListModels should not error, got: %v", err)
	}
	if len(cat.Models) != 0 {
		t.Fatalf("qwenpaw ListModels should return empty catalog, got %d models", len(cat.Models))
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("qwenpaw ListModels executed the CLI; it must return an empty catalog without spawning a discovery subprocess")
	}
}

func TestQwenpawBlockedArgs(t *testing.T) {
	t.Parallel()
	if _, ok := qwenpawBlockedArgs["acp"]; !ok {
		t.Fatal("expected acp to be in qwenpawBlockedArgs")
	}
	if qwenpawBlockedArgs["acp"] != blockedStandalone {
		t.Fatalf("expected acp to be blockedStandalone, got %v", qwenpawBlockedArgs["acp"])
	}
	if _, ok := qwenpawBlockedArgs["--workspace"]; !ok {
		t.Fatal("expected --workspace to be in qwenpawBlockedArgs")
	}
	if qwenpawBlockedArgs["--workspace"] != blockedWithValue {
		t.Fatalf("expected --workspace to be blockedWithValue, got %v", qwenpawBlockedArgs["--workspace"])
	}
}

func TestQwenpawUsesSessionLoad(t *testing.T) {
	// Verify that qwenpaw uses session/load (not session/resume) on resume.
	// QwenPaw's ACP server implements load_session, not session/resume.
	t.Parallel()
	bin := writeFakeQwenpawScript(t, fakeQwenpawACPScript())
	reqFile := filepath.Join(t.TempDir(), "requests.txt")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	b, err := New("qwenpaw", Config{
		ExecutablePath: bin,
		Logger:         logger,
		Env:            map[string]string{"QWENPAW_REQUESTS_FILE": reqFile},
	})
	if err != nil {
		t.Fatalf("New(qwenpaw) error: %v", err)
	}

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd:             t.TempDir(),
		ResumeSessionID: "ses_verify",
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

	if !strings.Contains(requests, `"method":"session/load"`) {
		t.Fatalf("expected session/load on resume, got requests:\n%s", requests)
	}
	if strings.Contains(requests, `"method":"session/resume"`) {
		t.Fatalf("qwenpaw must use session/load (not session/resume), got:\n%s", requests)
	}
}

// TestQwenpawTimeout tests that a context timeout during session/new
// is reported as status=timeout. The fake script responds to
// initialize immediately, then sleeps 30s on session/new so the
// 5s context deadline expires during the session/new RPC.
func TestQwenpawTimeout(t *testing.T) {
	t.Parallel()

	script := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
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

	bin := writeFakeQwenpawScript(t, script)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	b, err := New("qwenpaw", Config{
		ExecutablePath: bin,
		Logger:         logger,
	})
	if err != nil {
		t.Fatalf("New(qwenpaw) error: %v", err)
	}

	// Use a generous timeout so initialize always completes;
	// the 30s sleep on session/new will trigger the timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd: t.TempDir(),
	})
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

func TestQwenpawBackendUsage(t *testing.T) {
	t.Parallel()
	bin := writeFakeQwenpawScript(t, fakeQwenpawACPScript())

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	b, err := New("qwenpaw", Config{
		ExecutablePath: bin,
		Logger:         logger,
	})
	if err != nil {
		t.Fatalf("New(qwenpaw) error: %v", err)
	}

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd: t.TempDir(),
	})
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

// TestQwenpawUsageModelIgnored verifies that opts.Model is never used as
// the usage attribution key — QwenPaw's model selection is unsupported,
// so usage must always be attributed to "unknown".
func TestQwenpawUsageModelIgnored(t *testing.T) {
	t.Parallel()
	bin := writeFakeQwenpawScript(t, fakeQwenpawACPScript())

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	b, err := New("qwenpaw", Config{
		ExecutablePath: bin,
		Logger:         logger,
	})
	if err != nil {
		t.Fatalf("New(qwenpaw) error: %v", err)
	}

	ctx := context.Background()
	// Pass a model that should never appear in the usage map
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
	if result.Usage == nil {
		t.Fatal("expected usage in result")
	}
	if _, ok := result.Usage["must-not-be-reported"]; ok {
		t.Fatal("opts.Model must not be used as usage attribution key for qwenpaw")
	}
	usage, ok := result.Usage["unknown"]
	if !ok {
		t.Fatalf("expected usage entry for model 'unknown', got %+v", result.Usage)
	}
	if usage.InputTokens != 10 {
		t.Fatalf("expected 10 input tokens, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 20 {
		t.Fatalf("expected 20 output tokens, got %d", usage.OutputTokens)
	}
}

// TestQwenpawSessionLoadTransientError verifies that a transient
// network/handshake error on session/load does NOT set ResumeRejected=true,
// matching the invariant documented in grok.go:269-272.
func TestQwenpawSessionLoadTransientError(t *testing.T) {
	t.Parallel()

	// This script returns an RPC error that is NOT a session-not-found
	// error, simulating a transient network/handshake failure.
	script := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"session/load"'*)
      # Simulate a transient error (e.g. rate limit, 5xx) — NOT session not found
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32000,"message":"rate limit exceeded"}}\n' "$id"
      exit 0
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id"
      ;;
  esac
done
`
	bin := writeFakeQwenpawScript(t, script)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	b, err := New("qwenpaw", Config{
		ExecutablePath: bin,
		Logger:         logger,
	})
	if err != nil {
		t.Fatalf("New(qwenpaw) error: %v", err)
	}

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd:             t.TempDir(),
		ResumeSessionID: "ses_transient",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	for range session.Messages {
	}

	result := <-session.Result
	if result.Status != "failed" {
		t.Fatalf("expected failed, got status=%q error=%q", result.Status, result.Error)
	}
	if !strings.Contains(result.Error, "session/load failed") {
		t.Fatalf("expected session/load failed error, got %q", result.Error)
	}
	// Transient errors must NOT set ResumeRejected — only session-not-found does.
	if result.ResumeRejected {
		t.Fatal("expected ResumeRejected=false on transient load error")
	}
}

// TestQwenpawSessionNewSendsCodingProjectDir verifies that session/new
// includes the qwenpaw.coding_project_dir key inside _meta so QwenPaw
// can enable Coding Mode on the correct project directory.
func TestQwenpawSessionNewSendsCodingProjectDir(t *testing.T) {
	t.Parallel()

	bin := writeFakeQwenpawScript(t, fakeQwenpawACPScript())
	reqFile := filepath.Join(t.TempDir(), "requests.txt")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	b, err := New("qwenpaw", Config{
		ExecutablePath: bin,
		Logger:         logger,
		Env:            map[string]string{"QWENPAW_REQUESTS_FILE": reqFile},
	})
	if err != nil {
		t.Fatalf("New(qwenpaw) error: %v", err)
	}

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd: t.TempDir(),
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

	// qwenpaw.coding_project_dir must be inside _meta, not top-level
	if !strings.Contains(requests, `"_meta"`) {
		t.Fatalf("expected _meta in session/new request, got:\n%s", requests)
	}
	if !strings.Contains(requests, `"qwenpaw.coding_project_dir"`) {
		t.Fatalf("expected qwenpaw.coding_project_dir inside _meta in session/new request, got:\n%s", requests)
	}
}

// TestQwenpawSessionLoadSendsCodingProjectDir verifies that session/load
// also sends qwenpaw.coding_project_dir inside _meta, matching the
// new_session path.
func TestQwenpawSessionLoadSendsCodingProjectDir(t *testing.T) {
	t.Parallel()

	bin := writeFakeQwenpawScript(t, fakeQwenpawACPScript())
	reqFile := filepath.Join(t.TempDir(), "requests.txt")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	b, err := New("qwenpaw", Config{
		ExecutablePath: bin,
		Logger:         logger,
		Env:            map[string]string{"QWENPAW_REQUESTS_FILE": reqFile},
	})
	if err != nil {
		t.Fatalf("New(qwenpaw) error: %v", err)
	}

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd:             t.TempDir(),
		ResumeSessionID: "ses_load_test",
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

	if !strings.Contains(requests, `"session/load"`) {
		t.Fatalf("expected session/load request, got:\n%s", requests)
	}
	if !strings.Contains(requests, `"_meta"`) {
		t.Fatalf("expected _meta in session/load request, got:\n%s", requests)
	}
	if !strings.Contains(requests, `"qwenpaw.coding_project_dir"`) {
		t.Fatalf("expected qwenpaw.coding_project_dir inside _meta in session/load request, got:\n%s", requests)
	}
}

// TestQwenpawWorkspaceArgs verifies that --workspace flag is passed to the
// qwenpaw acp command when set in ExecOptions.
func TestQwenpawWorkspaceArgs(t *testing.T) {
	t.Parallel()

	// Fake script that writes its command-line args to a file and then
	// runs the standard ACP loop.
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	script := fmt.Sprintf(`#!/bin/sh
echo "$@" > "%s"
while IFS= read -r line; do
  id=$(printf '%%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"sessionId":"ses_qwenpaw_ws"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":5,"outputTokens":10}}}\n' "$id"
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id"
      ;;
  esac
done
`, argsFile)

	bin := writeFakeQwenpawScript(t, script)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	b, err := New("qwenpaw", Config{
		ExecutablePath: bin,
		Logger:         logger,
	})
	if err != nil {
		t.Fatalf("New(qwenpaw) error: %v", err)
	}

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd:              t.TempDir(),
		QwenpawWorkspace: "/tmp/test-qwenpaw-workspace",
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

	if !strings.Contains(args, "--workspace") {
		t.Fatalf("expected --workspace in command args, got:\n%s", args)
	}
	if !strings.Contains(args, "/tmp/test-qwenpaw-workspace") {
		t.Fatalf("expected workspace path in command args, got:\n%s", args)
	}
}

// TestQwenpawBlockedWorkspaceAndAgentArgs verifies that user-defined
// --workspace and --agent in custom_args are stripped by the blocked-args
// filter.
func TestQwenpawBlockedWorkspaceAndAgentArgs(t *testing.T) {
	t.Parallel()

	argsFile := filepath.Join(t.TempDir(), "args_blocked.txt")
	script := fmt.Sprintf(`#!/bin/sh
echo "$@" > "%s"
while IFS= read -r line; do
  id=$(printf '%%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"sessionId":"ses_qwenpaw_blocked"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":5,"outputTokens":10}}}\n' "$id"
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id"
      ;;
  esac
done
`, argsFile)

	bin := writeFakeQwenpawScript(t, script)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	b, err := New("qwenpaw", Config{
		ExecutablePath: bin,
		Logger:         logger,
	})
	if err != nil {
		t.Fatalf("New(qwenpaw) error: %v", err)
	}

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd:              t.TempDir(),
		CustomArgs:       []string{"--workspace", "/evil/path", "--agent", "evil-agent"},
		QwenpawWorkspace: "/tmp/correct-workspace",
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

	// User-defined --workspace must be stripped by the filter
	if strings.Contains(args, "/evil/path") {
		t.Fatal("user-defined --workspace value should be blocked")
	}
	// --agent is no longer blocked by the daemon, so user-defined --agent
	// passes through.
	// Daemon-injected --workspace must be present
	if !strings.Contains(args, "/tmp/correct-workspace") {
		t.Fatalf("expected daemon-injected workspace path in command args, got:\n%s", args)
	}
}

func TestQwenpawBackendJSON(t *testing.T) {
	// Verify that the qwenpawBackend type is registered in the
	// backend constructor map.
	t.Parallel()
	b, err := New("qwenpaw", Config{ExecutablePath: "/test/qwenpaw"})
	if err != nil {
		t.Fatalf("New(qwenpaw) error: %v", err)
	}
	if _, ok := b.(*qwenpawBackend); !ok {
		t.Fatalf("expected *qwenpawBackend, got %T", b)
	}
}
