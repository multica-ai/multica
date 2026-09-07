package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

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
        # Spawn a descendant BEFORE sending the error response, so the PID
        # file is written before the backend's cleanup race begins. Then
        # ignore TERM and hang so the process does not exit on stdin EOF —
        # exercises the force-kill process-group cleanup path.
        trap '' TERM
        sleep 60 &
        child_pid=$!
        if [ -n "$DIM_CHILD_PID_FILE" ]; then
          printf '%s' "$child_pid" > "$DIM_CHILD_PID_FILE"
        fi
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"initialize failed"}}\n' "$id"
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
      # tests can exercise the ResumeRejected path. When DIM_LOAD_HELD_N is set,
      # return "held by another process" for the first N calls then succeed,
      # exercising the bounded retry loop.
      if [ -n "$DIM_LOAD_NOT_FOUND" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32002,"message":"ACP session not found","data":{"sessionId":"ses_prior"}}}\n' "$id"
      elif [ -n "$DIM_LOAD_HELD_N" ]; then
        held_file="${DIM_REQUESTS_FILE}_held_counter"
        count=0
        if [ -f "$held_file" ]; then count=$(cat "$held_file"); fi
        if [ "$count" -lt "$DIM_LOAD_HELD_N" ]; then
          count=$((count + 1))
          echo "$count" > "$held_file"
          printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"Session held by another process","data":{"details":"held"}}}\n' "$id"
        else
          printf '{"jsonrpc":"2.0","id":%s,"result":{"configOptions":[{"id":"permission","currentValue":"full-access"},{"id":"mode","currentValue":"agent"}],"models":{"currentModelId":"dim/model-a"}}}\n' "$id"
        fi
      elif [ -n "$DIM_LOAD_HELD_ALWAYS" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"Session held by another process","data":{"details":"held"}}}\n' "$id"
      else
        printf '{"jsonrpc":"2.0","id":%s,"result":{"configOptions":[{"id":"permission","currentValue":"full-access"},{"id":"mode","currentValue":"agent"}],"models":{"currentModelId":"dim/model-a"}}}\n' "$id"
      fi
      ;;
    *'"method":"session/set_config_option"'*)
      if [ -n "$DIM_CONFIG_FAIL_ONCE" ] && [ ! -f "$DIM_CONFIG_FAIL_ONCE" ]; then
        printf '%s' x > "$DIM_CONFIG_FAIL_ONCE"
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"config set failed"}}\n' "$id"
      elif [ -n "$DIM_CONFIG_FAIL" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"config set failed"}}\n' "$id"
      else
        printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      fi
      ;;
    *'"method":"session/set_model"'*)
      if [ -n "$DIM_MODEL_FAIL" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"set_model failed"}}\n' "$id"
      else
        printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      fi
      ;;
    *'"method":"session/prompt"'*)
      # When DIM_PROMPT_NO_STOP is set, omit stopReason so extractPromptResult
      # never fires onPromptDone — exercises the bounded final-notification
      # wait (the backend must still complete, not hang on a missing notify).
      if [ -n "$DIM_PROMPT_NO_STOP" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"result":{"usage":{"inputTokens":10,"outputTokens":20}}}\n' "$id"
      elif [ -n "$DIM_LATE_NOTIFICATION" ]; then
        # Return the prompt response, then emit a late agent_message_chunk
        # notification after a short delay — exercises the notification
        # quiescence drain (the late text must survive into Result.Output).
        printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":10,"outputTokens":20}}}\n' "$id"
        sleep 0.1
        printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"%s","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"late-answer"}}}}\n' "$DIM_SESSION_ID"
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
		t.Fatal("backend must not send session/load on a fresh-session run")
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
	// The backend must send session/close during teardown so dim releases the
	// per-process session lock immediately (graceful exit), enabling the next
	// run to resume without delay.
	if !strings.Contains(reqs, "session/close") {
		t.Fatal("expected session/close during teardown")
	}
}

// TestDimResumeLoadsSession verifies that when the daemon asks to resume a
// prior session, the backend resumes it via the standard ACP session/load,
// reuses the resumed session id, and re-issues set_config_option (fail-closed
// against a partially configured session — review #4). ResumeRejected must
// stay false because the resume succeeded.
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
	// A resumed session is re-issued set_config_option (permission/mode) to
	// guard against a partially configured session being resumed — see
	// review #4. So set_config_option MUST appear after a successful load.
	if !strings.Contains(reqs, "set_config_option") {
		t.Fatal("set_config_option must be re-applied on a resumed session (fail-closed)")
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

// TestDimCleanupKillsHangingChild verifies the deferred cleanup force-kills the
// entire process group — not just the direct child — when a child ignores stdin
// EOF and SIGTERM after an early RPC failure, so Result still closes AND no
// descendant is orphaned (review #2/#3). Without the group force-kill,
// cmd.Wait() would hang forever and the sleep descendant would survive as a
// PPID=1 orphan.
func TestDimCleanupKillsHangingChild(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	requestsFile := filepath.Join(dir, "requests.jsonl")
	childPidFile := filepath.Join(dir, "child_pid")
	b := newDimTestBackendWithEnv(t, requestsFile, map[string]string{
		"DIM_INIT_FAIL_HANG": "1",
		"DIM_CHILD_PID_FILE": childPidFile,
	})

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

	// Result must close within a bounded window — the group force-kill
	// (bounded by dimProcessWaitTimeout) guarantees the hung child cannot
	// block it.
	select {
	case result := <-session.Result:
		if result.Status != "failed" {
			t.Fatalf("expected status=failed from the initialize error, got %q", result.Status)
		}
	case <-time.After(dimProcessWaitTimeout + 15*time.Second):
		t.Fatal("Result never closed: the hanging child was not force-killed within the cleanup timeout")
	}

	// The descendant (sleep 60) must be gone too — not orphaned to PPID=1.
	// Give the PID file a moment to appear (the child is spawned after the
	// error response is written).
	deadline := time.Now().Add(5 * time.Second)
	var pidStr string
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(childPidFile)
		if err == nil && len(data) > 0 {
			pidStr = string(data)
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if pidStr == "" {
		t.Fatal("child PID file was never written; the fake did not spawn a descendant")
	}
	childPid, err := strconv.Atoi(strings.TrimSpace(pidStr))
	if err != nil {
		t.Fatalf("invalid child PID %q: %v", pidStr, err)
	}
	// Verify the descendant is no longer alive. Poll briefly because the
	// group SIGKILL and the kernel's process-table reaping are asynchronous
	// with respect to Result delivery.
	proc, err := os.FindProcess(childPid)
	if err != nil {
		return // already reaped
	}
	goneDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(goneDeadline) {
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			return // process is gone
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("descendant PID %d is still alive after cleanup: process group was not reaped", childPid)
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

// TestDimDrainsLateFinalNotificationAfterPromptResponse verifies that a
// notification arriving just after the session/prompt response is not lost —
// the activity-based quiescence drain gives the stdout reader time to consume
// it before stdin is closed (review #3, the Dim equivalent of
// TestHermesBackendDrainsLateFinalNotificationAfterPromptResponse).
func TestDimDrainsLateFinalNotificationAfterPromptResponse(t *testing.T) {
	t.Parallel()
	requestsFile := filepath.Join(t.TempDir(), "requests.jsonl")
	b := newDimTestBackendWithEnv(t, requestsFile, map[string]string{
		"DIM_LATE_NOTIFICATION": "1",
		"DIM_SESSION_ID":        "ses_dim_new",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd:     t.TempDir(),
		Timeout: 15 * time.Second,
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
		if result.Status != "completed" {
			t.Fatalf("expected status=completed, got %q (error=%q)", result.Status, result.Error)
		}
		if !strings.Contains(result.Output, "late-answer") {
			t.Fatalf("late notification was lost: expected output to contain %q, got %q", "late-answer", result.Output)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

// TestDimConfigFailClosesSession verifies that when set_config_option fails,
// the backend sends session/close before returning the error, so a partially
// configured session is not left for the next resume to inherit without
// full-access (review #4).
func TestDimConfigFailClosesSession(t *testing.T) {
	t.Parallel()
	requestsFile := filepath.Join(t.TempDir(), "requests.jsonl")
	b := newDimTestBackendWithEnv(t, requestsFile, map[string]string{"DIM_CONFIG_FAIL": "1"})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd:     t.TempDir(),
		Timeout: 15 * time.Second,
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

	reqs := readDimRequests(t, requestsFile)
	// set_config_option must have been attempted.
	if !strings.Contains(reqs, "set_config_option") {
		t.Fatal("expected a set_config_option attempt")
	}
	// session/close must have been sent to clean up the partially configured
	// session so it is not resumed later without full-access.
	if !strings.Contains(reqs, "session/close") {
		t.Fatal("expected session/close after config failure to prevent a partially configured resume")
	}
}

// TestDimConfigFailThenResumeReestablishes verifies the "first setup fails →
// next run resumes" path (review #4): when the first run's set_config_option
// fails, session/close is sent; a second run that resumes the same session
// re-applies set_config_option (fail-closed) and succeeds.
func TestDimConfigFailThenResumeReestablishes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	requestsFile := filepath.Join(dir, "requests.jsonl")
	failFlag := filepath.Join(dir, "config_failed")
	b := newDimTestBackendWithEnv(t, requestsFile, map[string]string{
		"DIM_CONFIG_FAIL_ONCE": failFlag,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Run A: fresh session, set_config_option fails on the first call.
	sessionA, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd:     t.TempDir(),
		Timeout: 20 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute A: %v", err)
	}
	go func() {
		for range sessionA.Messages {
		}
	}()
	resultA := <-sessionA.Result
	if resultA.Status != "failed" {
		t.Fatalf("run A: expected status=failed, got %q", resultA.Status)
	}
	if resultA.SessionID == "" {
		t.Fatal("run A: expected a session id despite the config failure")
	}

	// Run B: resume the same session. set_config_option should succeed this
	// time (DIM_CONFIG_FAIL_ONCE only fails once) and the run should complete.
	sessionB, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd:             t.TempDir(),
		Timeout:         20 * time.Second,
		ResumeSessionID: resultA.SessionID,
	})
	if err != nil {
		t.Fatalf("execute B: %v", err)
	}
	go func() {
		for range sessionB.Messages {
		}
	}()
	resultB := <-sessionB.Result
	if resultB.Status != "completed" {
		t.Fatalf("run B: expected status=completed, got %q (error=%q)", resultB.Status, resultB.Error)
	}
	if resultB.ResumeRejected {
		t.Fatal("run B: ResumeRejected should be false — the session was loadable after session/close")
	}

	// Verify run B re-applied set_config_option (fail-closed on resume).
	reqs := readDimRequests(t, requestsFile)
	if !strings.Contains(reqs, "session/load") {
		t.Fatal("run B: expected session/load")
	}
	// set_config_option should appear at least 3 times: 1 failed in run A,
	// 2 successful in run B (permission + mode).
	count := strings.Count(reqs, "set_config_option")
	if count < 3 {
		t.Fatalf("expected at least 3 set_config_option calls (1 fail + 2 re-apply on resume), got %d", count)
	}
}

// TestDimVersionSupported pins the fail-closed version gate (review #3): only
// a parseable semver >= 0.3.10 is accepted. Empty, non-parseable, and
// too-short versions are rejected so a runtime that cannot prove it meets the
// minimum is blocked from session resume.
func TestDimVersionSupported(t *testing.T) {
	t.Parallel()
	cases := []struct {
		version string
		want    bool
	}{
		{"0.3.10", true},
		{"0.3.9", false},
		{"0.4.0", true},
		{"1.0.0", true},
		{"", false},
		{"abc", false},
		{"0.3", false},
	}
	for _, tc := range cases {
		if got := dimVersionSupported(tc.version); got != tc.want {
			t.Errorf("dimVersionSupported(%q) = %v, want %v", tc.version, got, tc.want)
		}
	}
}

// TestDimSetModelFailClosesSession verifies that when session/set_model fails,
// the backend sends session/close before returning the error, so a partially
// configured session (permission/mode set, model not) is not left for the next
// resume to inherit (review #5).
func TestDimSetModelFailClosesSession(t *testing.T) {
	t.Parallel()
	requestsFile := filepath.Join(t.TempDir(), "requests.jsonl")
	b := newDimTestBackendWithEnv(t, requestsFile, map[string]string{"DIM_MODEL_FAIL": "1"})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd:     t.TempDir(),
		Model:   "some-model",
		Timeout: 15 * time.Second,
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
		t.Fatalf("expected status=failed, got %q (error=%q)", result.Status, result.Error)
	}

	reqs := readDimRequests(t, requestsFile)
	// session/set_model must have been attempted with the requested model.
	if !strings.Contains(reqs, "session/set_model") {
		t.Fatal("expected a session/set_model attempt")
	}
	if !strings.Contains(reqs, `"modelId":"some-model"`) {
		t.Fatal("expected session/set_model to carry the requested model id")
	}
	// session/close must have been sent to clean up the partially configured
	// session (permission/mode set, model not) so it is not resumed later.
	if !strings.Contains(reqs, "session/close") {
		t.Fatal("expected session/close after set_model failure to prevent a partially configured resume")
	}
	// The prompt must NOT have been sent — the model switch failed, so the
	// turn aborts before session/prompt.
	if strings.Contains(reqs, "session/prompt") {
		t.Fatal("backend must not send session/prompt after set_model failure")
	}
}

// TestDimSessionLoadRetrySucceeds verifies that session/load retries on
// "held by another process" and eventually succeeds (review #4 round 4).
func TestDimSessionLoadRetrySucceeds(t *testing.T) {
	t.Parallel()
	requestsFile := filepath.Join(t.TempDir(), "requests.jsonl")
	b := newDimTestBackendWithEnv(t, requestsFile, map[string]string{
		"DIM_LOAD_HELD_N": "2", // held for first 2 calls, succeeds on 3rd
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd:             t.TempDir(),
		Timeout:         50 * time.Second,
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
	if result.ResumeRejected {
		t.Fatal("ResumeRejected must be false — retry succeeded")
	}

	reqs := readDimRequests(t, requestsFile)
	loadCount := strings.Count(reqs, "session/load")
	if loadCount < 3 {
		t.Fatalf("expected at least 3 session/load attempts (2 held + 1 success), got %d", loadCount)
	}
	if strings.Contains(reqs, "session/new") {
		t.Fatal("backend must not fall through to session/new when retry succeeds")
	}
}

// TestDimSessionLoadRetryExhausted verifies that after exhausting retries on
// "held by another process", the backend fails without falling through to
// session/new (review #4 round 4).
func TestDimSessionLoadRetryExhausted(t *testing.T) {
	t.Parallel()
	requestsFile := filepath.Join(t.TempDir(), "requests.jsonl")
	b := newDimTestBackendWithEnv(t, requestsFile, map[string]string{
		"DIM_LOAD_HELD_ALWAYS": "1",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd:             t.TempDir(),
		Timeout:         50 * time.Second,
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
	if result.Status != "failed" && result.Status != "timeout" {
		t.Fatalf("expected status=failed or timeout, got %q", result.Status)
	}

	reqs := readDimRequests(t, requestsFile)
	loadCount := strings.Count(reqs, "session/load")
	// dimSessionLoadRetryAttempts=3, so 1 initial + 3 retries = 4 attempts
	if loadCount != 4 {
		t.Fatalf("expected exactly 4 session/load attempts (1 + 3 retries), got %d", loadCount)
	}
	if strings.Contains(reqs, "session/new") {
		t.Fatal("backend must not fall through to session/new when retries are exhausted")
	}
}

// TestDimSessionLoadRetryCancelled verifies that cancelling the context during
// the retry delay aborts the loop without falling through to session/new
// (review #4 round 4).
func TestDimSessionLoadRetryCancelled(t *testing.T) {
	t.Parallel()
	requestsFile := filepath.Join(t.TempDir(), "requests.jsonl")
	b := newDimTestBackendWithEnv(t, requestsFile, map[string]string{
		"DIM_LOAD_HELD_ALWAYS": "1",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	session, err := b.Execute(ctx, "test prompt", ExecOptions{
		Cwd:             t.TempDir(),
		Timeout:         50 * time.Second,
		ResumeSessionID: "ses_prior",
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	// Wait for the first session/load request to appear, then cancel during
	// the retry delay — this is deterministic rather than a fixed sleep.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(requestsFile); err == nil {
			data, _ := os.ReadFile(requestsFile)
			if strings.Contains(string(data), "session/load") {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()

	result := <-session.Result
	if result.Status != "failed" && result.Status != "aborted" && result.Status != "timeout" {
		t.Fatalf("expected status=failed/aborted/timeout, got %q", result.Status)
	}

	reqs := readDimRequests(t, requestsFile)
	if strings.Contains(reqs, "session/new") {
		t.Fatal("backend must not fall through to session/new when cancelled during retry")
	}
}
