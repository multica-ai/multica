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

func TestNewReturnsDimBackend(t *testing.T) {
	t.Parallel()
	b, err := New("dim", Config{ExecutablePath: "/nonexistent/dim"})
	if err != nil {
		t.Fatalf("New(dim) error: %v", err)
	}
	if _, ok := b.(*dimBackend); !ok {
		t.Fatalf("expected *dimBackend, got %T", b)
	}
}

func TestDimModelSelectionSupported(t *testing.T) {
	t.Parallel()
	// Dim's session/set_model is session-scoped, so model override works.
	if !ModelSelectionSupported("dim") {
		t.Fatal("ModelSelectionSupported(dim) should return true")
	}
}

// fakeDimACPScript impersonates `dim acp` for unit tests. Dim 0.3.10+
// releases its per-process session lock shortly after the owning process
// exits, so a follow-up run resumes via the standard ACP session/load. This
// fake answers session/load with a resumed session (retaining the requested
// id, matching the real server which does not echo sessionId on load). It
// records every request line to DIM_REQUESTS_FILE so tests can assert which
// RPCs were (and were not) sent.
func fakeDimACPScript() string {
	return `#!/bin/sh
while IFS= read -r line; do
  if [ -n "$DIM_REQUESTS_FILE" ]; then
    printf '%s\n' "$line" >> "$DIM_REQUESTS_FILE"
  fi
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      if [ -n "$DIM_INIT_FAIL_HANG" ]; then
        # Return an error, then ignore TERM and hang so the process does not
        # exit on stdin EOF — exercises the force-kill cleanup path.
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"initialize failed"}}\n' "$id"
        trap '' TERM
        sleep 60 &
        wait
      else
        printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentInfo":{"name":"dimcode","version":"0.3.10"},"agentCapabilities":{"loadSession":true,"mcpCapabilities":{"http":true,"sse":false}}}}\n' "$id"
      fi
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_dim_new","models":{"availableModels":[{"modelId":"dim/model-a","name":"Model A"}],"currentModelId":"dim/model-a"}}}\n' "$id"
      ;;
    *'"method":"session/load"'*)
      # The real dim ACP server resumes the session and returns configOptions
      # (no sessionId field — the id is the one the client requested). When
      # DIM_LOAD_NOT_FOUND is set, emulate a session that no longer exists so
      # tests can exercise the ResumeRejected path.
      if [ -n "$DIM_LOAD_NOT_FOUND" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32002,"message":"ACP session not found","data":{"sessionId":"ses_prior"}}}\n' "$id"
      else
        printf '{"jsonrpc":"2.0","id":%s,"result":{"configOptions":[{"id":"permission","currentValue":"full-access"},{"id":"mode","currentValue":"agent"}],"models":{"currentModelId":"dim/model-a"}}}\n' "$id"
      fi
      ;;
    *'"method":"session/set_config_option"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/set_model"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      # When DIM_PROMPT_NO_STOP is set, omit stopReason so extractPromptResult
      # never fires onPromptDone — exercises the bounded final-notification
      # wait (the backend must still complete, not hang on a missing notify).
      if [ -n "$DIM_PROMPT_NO_STOP" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"result":{"usage":{"inputTokens":10,"outputTokens":20}}}\n' "$id"
      else
        printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":10,"outputTokens":20}}}\n' "$id"
      fi
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

func writeFakeDimScript(t *testing.T, requestsFile string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "dim")
	if err := os.WriteFile(bin, []byte(fakeDimACPScript()), 0o755); err != nil {
		t.Fatalf("write fake dim: %v", err)
	}
	return bin
}

func newDimTestBackend(t *testing.T, requestsFile string) Backend {
	t.Helper()
	return newDimTestBackendWithEnv(t, requestsFile, nil)
}

func newDimTestBackendWithEnv(t *testing.T, requestsFile string, extra map[string]string) Backend {
	t.Helper()
	bin := writeFakeDimScript(t, requestsFile)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	env := map[string]string{"DIM_REQUESTS_FILE": requestsFile}
	for k, v := range extra {
		env[k] = v
	}
	b, err := New("dim", Config{
		ExecutablePath: bin,
		Logger:         logger,
		Env:            env,
	})
	if err != nil {
		t.Fatalf("New(dim) error: %v", err)
	}
	return b
}

func readDimRequests(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read dim requests: %v", err)
	}
	return string(data)
}

// TestDimSessionNew verifies the happy path: initialize → session/new →
// set_config_option(permission/mode) → prompt, with a fresh session.
func TestDimSessionNew(t *testing.T) {
	t.Parallel()
	requestsFile := filepath.Join(t.TempDir(), "requests.jsonl")
	b := newDimTestBackend(t, requestsFile)

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("expected status=completed, got %q (error=%q)", result.Status, result.Error)
	}
	if result.SessionID != "ses_dim_new" {
		t.Fatalf("expected session id ses_dim_new, got %q", result.SessionID)
	}

	reqs := readDimRequests(t, requestsFile)
	if strings.Contains(reqs, "session/load") {
		t.Fatal("backend must never send session/load (dim sessions are process-bound)")
	}
	// The ACP server hardcodes read-only permission; the backend must raise
	// it to full-access and pin agent mode before the prompt.
	if !strings.Contains(reqs, `"configId":"permission"`) || !strings.Contains(reqs, `"value":"full-access"`) {
		t.Fatal("expected set_config_option permission=full-access")
	}
	if !strings.Contains(reqs, `"configId":"mode"`) || !strings.Contains(reqs, `"value":"agent"`) {
		t.Fatal("expected set_config_option mode=agent")
	}
	if !strings.Contains(reqs, "session/prompt") {
		t.Fatal("expected session/prompt")
	}
}

// TestDimResumeLoadsSession verifies that when the daemon asks to resume a
// prior session, the backend resumes it via the standard ACP session/load,
// reuses the resumed session id, and does NOT re-issue set_config_option
// (a loaded session retains its permission/mode). ResumeRejected must stay
// false because the resume succeeded.
func TestDimResumeLoadsSession(t *testing.T) {
	t.Parallel()
	requestsFile := filepath.Join(t.TempDir(), "requests.jsonl")
	b := newDimTestBackend(t, requestsFile)

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd:             t.TempDir(),
		ResumeSessionID: "ses_prior",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("expected status=completed, got %q (error=%q)", result.Status, result.Error)
	}
	if result.SessionID != "ses_prior" {
		t.Fatalf("expected the resumed session id ses_prior, got %q", result.SessionID)
	}
	if result.ResumeRejected {
		t.Fatal("ResumeRejected must be false when session/load succeeded")
	}

	reqs := readDimRequests(t, requestsFile)
	if !strings.Contains(reqs, "session/load") {
		t.Fatal("expected a session/load when a resume was requested")
	}
	if strings.Contains(reqs, "session/new") {
		t.Fatal("backend must not send session/new when the resume succeeded")
	}
	// A resumed session already carries permission/mode, so the config block
	// must be skipped — no set_config_option after a successful load.
	if strings.Contains(reqs, "set_config_option") {
		t.Fatal("set_config_option must not be sent on a resumed session")
	}
}

// TestDimResumeNotFound verifies that when session/load reports the prior
// session is gone (ACP -32002 session not found), the backend fails with
// ResumeRejected=true so the daemon retries on a fresh session instead of
// silently losing the conversation.
func TestDimResumeNotFound(t *testing.T) {
	t.Parallel()
	requestsFile := filepath.Join(t.TempDir(), "requests.jsonl")
	b := newDimTestBackendWithEnv(t, requestsFile, map[string]string{"DIM_LOAD_NOT_FOUND": "1"})

	ctx := context.Background()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd:             t.TempDir(),
		ResumeSessionID: "ses_prior",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "failed" {
		t.Fatalf("expected status=failed, got %q", result.Status)
	}
	if !result.ResumeRejected {
		t.Fatal("expected ResumeRejected=true when session/load reports not found")
	}

	reqs := readDimRequests(t, requestsFile)
	if !strings.Contains(reqs, "session/load") {
		t.Fatal("expected a session/load attempt")
	}
	if strings.Contains(reqs, "session/new") {
		t.Fatal("backend must not fall through to session/new itself; the daemon owns the fresh retry")
	}
}

// TestDimCleanupKillsHangingChild verifies the deferred cleanup force-kills a
// child that ignores stdin EOF and SIGTERM after an early RPC failure, so
// Result still closes (review #3 failure-path race). Without the bounded
// force-kill, cmd.Wait() would hang forever and resCh would never close.
func TestDimCleanupKillsHangingChild(t *testing.T) {
	t.Parallel()
	requestsFile := filepath.Join(t.TempDir(), "requests.jsonl")
	b := newDimTestBackendWithEnv(t, requestsFile, map[string]string{"DIM_INIT_FAIL_HANG": "1"})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd:     t.TempDir(),
		Timeout: 20 * time.Second,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	// Result must close within a bounded window — the force-kill (bounded by
	// dimProcessWaitTimeout) guarantees the hung child cannot block it.
	select {
	case result := <-session.Result:
		if result.Status != "failed" {
			t.Fatalf("expected status=failed from the initialize error, got %q", result.Status)
		}
	case <-time.After(dimProcessWaitTimeout + 10*time.Second):
		t.Fatal("Result never closed: the hanging child was not force-killed within the cleanup timeout")
	}
}

// TestDimPromptMissingNotificationStillCompletes verifies the success path
// does not hang when the runtime returns session/prompt without a stopReason
// (so onPromptDone never fires). The bounded final-notification wait must
// fall through and let Result close (review #3 success-path race).
func TestDimPromptMissingNotificationStillCompletes(t *testing.T) {
	t.Parallel()
	requestsFile := filepath.Join(t.TempDir(), "requests.jsonl")
	b := newDimTestBackendWithEnv(t, requestsFile, map[string]string{"DIM_PROMPT_NO_STOP": "1"})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd:     t.TempDir(),
		Timeout: 20 * time.Second,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result := <-session.Result:
		// The turn completed (session/prompt returned ok); the missing
		// stopReason only means no usage/stopReason was captured. Status must
		// still be completed, not hang or fail.
		if result.Status != "completed" {
			t.Fatalf("expected status=completed, got %q (error=%q)", result.Status, result.Error)
		}
	case <-time.After(dimNotificationQuietTime + 15*time.Second):
		t.Fatal("Result never closed: the missing prompt notification was not bounded by the quiet-time wait")
	}
}
