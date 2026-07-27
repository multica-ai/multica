package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
// session/prompt returns stopReason=end_turn, session/set_model returns
// success or error based on QWENPAW_SET_MODEL_FAIL env var.
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
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":10,"outputTokens":20}}}\n' "$id"
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

func TestQwenpawSetModelFail(t *testing.T) {
	t.Parallel()
	bin := writeFakeQwenpawScript(t, fakeQwenpawACPScript())

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	b, err := New("qwenpaw", Config{
		ExecutablePath: bin,
		Logger:         logger,
		Env:            map[string]string{"QWENPAW_SET_MODEL_FAIL": "1"},
	})
	if err != nil {
		t.Fatalf("New(qwenpaw) error: %v", err)
	}

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd:    t.TempDir(),
		Model:  "qwen-max",
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
	if !strings.Contains(result.Error, "could not switch to model") {
		t.Fatalf("expected could not switch to model error, got %q", result.Error)
	}
}

func TestQwenpawListModels(t *testing.T) {
	t.Parallel()
	models, err := ListModels(context.Background(), "qwenpaw", "")
	if err != nil {
		t.Fatalf("ListModels(qwenpaw) error: %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("expected empty list for qwenpaw, got %d models", len(models))
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
// is reported as status=timeout.
func TestQwenpawTimeout(t *testing.T) {
	t.Parallel()
	script := `#!/bin/sh
# Sleep forever on session/new to trigger a timeout
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true,"mcpCapabilities":{"http":true,"sse":true}}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      sleep 30
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_late"}}\n' "$id"
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

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd:    t.TempDir(),
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
	if usage.InputTokens != 10 {
		t.Fatalf("expected 10 input tokens, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 20 {
		t.Fatalf("expected 20 output tokens, got %d", usage.OutputTokens)
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