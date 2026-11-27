package agent

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeCursorWarmScript is a minimal `cursor-agent` fake: the `acp` subcommand
// speaks the slice of ACP the warm transport drives (initialize →
// session/new|session/load → set_model → session/prompt). Each prompt reads
// the per-turn credentials from MULTICA_TASK_ENV_FILE and echoes them into the
// answer text, so tests can assert the overlay plumbing: the static
// environment must carry no token at all (static=EMPTY). The prompt result's
// usage is session-cumulative (input 100 + 20 per prior turn), so the tests
// can pin the delta settle.
//
// Failure modes are armed through flag files inside CURSOR_FLAGS_DIR (read on
// every prompt), because a parked process keeps executing the script it
// started with: rewriting the file on disk cannot reach it.
func fakeCursorWarmScript(spawnLog, turnFile string) string {
	return `#!/bin/sh
flags="${CURSOR_FLAGS_DIR:-/nonexistent}"
if [ -n "$CURSOR_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$CURSOR_ARGS_FILE"
  done
fi
printf '%s\n' "$$" >> '` + spawnLog + `'
turn=0
while IFS= read -r line; do
  if [ -n "$CURSOR_REQUESTS_FILE" ]; then
    case "$line" in
      *'"method":"initialize"'*) printf 'initialize\n' >> "$CURSOR_REQUESTS_FILE";;
      *'"method":"session/load"'*) printf 'session/load\n' >> "$CURSOR_REQUESTS_FILE";;
      *'"method":"session/new"'*) printf 'session/new\n' >> "$CURSOR_REQUESTS_FILE";;
      *'"method":"session/set_model"'*) printf 'session/set_model\n' >> "$CURSOR_REQUESTS_FILE";;
      *'"method":"session/prompt"'*) printf 'session/prompt\n' >> "$CURSOR_REQUESTS_FILE";;
    esac
  fi
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"session/load"'*)
      if [ -f "$flags/load-fail" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32002,"message":"ACP session not found"}}\n' "$id"
      else
        printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses-warm-1"}}\n' "$id"
      fi
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses-warm-1","models":{"currentModelId":"composer-2.5"}}}\n' "$id"
      ;;
    *'"method":"session/set_model"'*)
      if [ -f "$flags/set-model-fail" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"model unavailable"}}\n' "$id"
      else
        printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      fi
      ;;
    *'"method":"session/prompt"'*)
      turn=$((turn + 1))
      printf '%s' "$turn" > "` + turnFile + `"
      tok=$(sed -n 's/.*"MULTICA_TOKEN":"\([^"]*\)".*/\1/p' "$MULTICA_TASK_ENV_FILE")
      tid=$(sed -n 's/.*"MULTICA_TASK_ID":"\([^"]*\)".*/\1/p' "$MULTICA_TASK_ENV_FILE")
      static_tok="${MULTICA_TOKEN:-EMPTY}"
      static_task="${MULTICA_TASK_ID:-EMPTY}"
      if [ -f "$flags/prompt-fail" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"prompt failed"}}\n' "$id"
        continue
      fi
      if [ -f "$flags/prompt-not-found" ]; then
        # The runtime echoed the requested id from session/load but the
        # session is gone by prompt time — the cold path's resume-rejection
        # shape, surfaced late.
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32002,"message":"ACP session not found"}}\n' "$id"
        continue
      fi
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses-warm-1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"turn=%s token=%s task=%s static=%s/%s"}}}}\n' "$turn" "$tok" "$tid" "$static_tok" "$static_task"
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":%d,"outputTokens":%d,"cacheReadTokens":20,"cacheWriteTokens":5},"_meta":{"modelId":"composer-2.5"}}}\n' "$id" $((100 + 20 * (turn - 1))) $((10 + turn))
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id"
      ;;
  esac
done
`
}

func newCursorWarmTestPool(t *testing.T) *WarmSessionPool {
	t.Helper()
	pool := NewWarmSessionPool(WarmSessionPoolConfig{
		IdleTTL: 30 * time.Second,
		MaxIdle: 4,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() { pool.Close() })
	return pool
}

func newCursorWarmBackend(t *testing.T, fakeCLI string, pool *WarmSessionPool, extraEnv map[string]string, taskID, token string) Backend {
	t.Helper()
	env := map[string]string{
		"MULTICA_TOKEN":        token,
		"MULTICA_TASK_ID":      taskID,
		"MULTICA_AGENT_ID":     "agent-1",
		"MULTICA_WORKSPACE_ID": "ws-1",
	}
	for k, v := range extraEnv {
		env[k] = v
	}
	b, err := New("cursor", Config{
		ExecutablePath: fakeCLI,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		TaskID:         taskID,
		BuiltinRuntime: true,
		WarmPool:       pool,
		Env:            env,
	})
	if err != nil {
		t.Fatalf("New(cursor): %v", err)
	}
	return b
}

func runCursorWarmTurn(t *testing.T, b Backend, dir, taskID, resumeID, briefHash, model string) Result {
	t.Helper()
	sess, err := b.Execute(context.Background(), "hello", ExecOptions{
		Cwd:               dir,
		ResumeSessionID:   resumeID,
		WarmPoolBriefHash: briefHash,
		Model:             model,
		Timeout:           10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Execute(%s): %v", taskID, err)
	}
	for range sess.Messages {
	}
	return <-sess.Result
}

func countFileLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "\n") + 1
}

func TestCursorWarmPoolReuseAndTokenIsolation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	spawnLog := filepath.Join(dir, "spawns.log")
	turnFile := filepath.Join(dir, "turns.txt")
	flags := filepath.Join(dir, "flags")
	if err := os.MkdirAll(flags, 0o700); err != nil {
		t.Fatal(err)
	}

	fakeCursor := filepath.Join(dir, "cursor-agent")
	writeTestExecutable(t, fakeCursor, []byte(fakeCursorWarmScript(spawnLog, turnFile)))

	pool := newCursorWarmTestPool(t)

	runTurn := func(taskID, token, resumeID, briefHash string) Result {
		b := newCursorWarmBackend(t, fakeCursor, pool, map[string]string{"CURSOR_FLAGS_DIR": flags}, taskID, token)
		return runCursorWarmTurn(t, b, dir, taskID, resumeID, briefHash, "")
	}

	// Turn 1: cold spawn, gets parked. The prompt result's cumulative usage
	// (100/11) is billed in full — a fresh session has no prior baseline.
	r1 := runTurn("task-1", "mat_t1", "", "brief-v1")
	if r1.Status != "completed" || r1.SessionID != "ses-warm-1" {
		t.Fatalf("Turn 1 result = %+v", r1)
	}
	if !strings.Contains(r1.Output, "turn=1 token=mat_t1 task=task-1 static=EMPTY/EMPTY") {
		t.Fatalf("Turn 1 output = %q (expected overlay delivery and stripped static env)", r1.Output)
	}
	if u := r1.Usage["composer-2.5"]; u.InputTokens != 100 || u.OutputTokens != 11 {
		t.Fatalf("Turn 1 usage = %+v, want input=100 output=11 keyed by the billed model", u)
	}
	if pool.Len() != 1 {
		t.Fatalf("pool.Len() after Turn 1 = %d, want 1", pool.Len())
	}

	// Verify the idle overlay holds NO live token while parked.
	pool.mu.Lock()
	parked := pool.entries[warmPoolKey{provider: "cursor", sessionID: "ses-warm-1"}].proc.(*cursorWarmProcess)
	envFilePath := parked.taskEnvFile
	pool.mu.Unlock()
	idleData, err := os.ReadFile(envFilePath)
	if err != nil {
		t.Fatalf("read idle task-env.json: %v", err)
	}
	if strings.Contains(string(idleData), "mat_t1") {
		t.Fatalf("idle task-env.json leaked Turn 1 token: %s", string(idleData))
	}

	// Turn 2: warm hit on the SAME process with Turn 2's fresh overlay token.
	// The cumulative snapshot (120/12) settles as a delta against Turn 1's
	// (100/11): the daemon bills 20/1, not the whole session again.
	r2 := runTurn("task-2", "mat_t2", "ses-warm-1", "brief-v1")
	if r2.Status != "completed" || r2.SessionID != "ses-warm-1" {
		t.Fatalf("Turn 2 result = %+v", r2)
	}
	if !strings.Contains(r2.Output, "turn=2 token=mat_t2 task=task-2 static=EMPTY/EMPTY") {
		t.Fatalf("Turn 2 output = %q (expected same process turn=2 with fresh token)", r2.Output)
	}
	if u := r2.Usage["composer-2.5"]; u.InputTokens != 20 || u.OutputTokens != 1 {
		t.Fatalf("Turn 2 usage = %+v, want input=20 output=1 (the cumulative snapshot must settle as a delta)", u)
	}
	if n := countFileLines(t, spawnLog); n != 1 {
		t.Fatalf("expected 1 cursor spawn across Turn 1 & 2, got %d", n)
	}

	// Turn 3: changing the brief hash must evict the parked process and spawn
	// a fresh one; the turn counter restarts.
	r3 := runTurn("task-3", "mat_t3", "ses-warm-1", "brief-v2")
	if r3.Status != "completed" {
		t.Fatalf("Turn 3 result = %+v", r3)
	}
	if !strings.Contains(r3.Output, "turn=1 token=mat_t3 task=task-3") {
		t.Fatalf("Turn 3 output = %q (expected a fresh process restarting at turn=1)", r3.Output)
	}
	if n := countFileLines(t, spawnLog); n != 2 {
		t.Fatalf("expected 2 cursor spawns after the fingerprint change, got %d", n)
	}
	if pool.Len() != 1 {
		t.Fatalf("pool.Len() after Turn 3 = %d, want 1 (the new process)", pool.Len())
	}

	if !pool.Close() {
		t.Fatal("pool.Close() returned false")
	}
	if pool.Len() != 0 {
		t.Fatalf("pool.Len() after Close = %d, want 0", pool.Len())
	}
}

func TestCursorWarmFailedTurnNotParked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	spawnLog := filepath.Join(dir, "spawns.log")
	turnFile := filepath.Join(dir, "turns.txt")
	flags := filepath.Join(dir, "flags")
	if err := os.MkdirAll(flags, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(flags, "prompt-fail"), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}

	fakeCursor := filepath.Join(dir, "cursor-agent")
	writeTestExecutable(t, fakeCursor, []byte(fakeCursorWarmScript(spawnLog, turnFile)))

	pool := newCursorWarmTestPool(t)

	b := newCursorWarmBackend(t, fakeCursor, pool, map[string]string{"CURSOR_FLAGS_DIR": flags}, "task-1", "mat_t1")
	res := runCursorWarmTurn(t, b, dir, "task-1", "", "brief-v1", "")
	if res.Status != "failed" || !strings.Contains(res.Error, "prompt failed") {
		t.Fatalf("failed-turn result = %+v, want failed with the prompt error", res)
	}
	if pool.Len() != 0 {
		t.Fatalf("pool.Len() after a failed turn = %d, want 0 (a failed turn is never parked)", pool.Len())
	}
	if n := countFileLines(t, spawnLog); n != 1 {
		t.Fatalf("expected 1 spawn, got %d", n)
	}
}

func TestCursorWarmPromptTimeSessionNotFoundRejectsResume(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	spawnLog := filepath.Join(dir, "spawns.log")
	turnFile := filepath.Join(dir, "turns.txt")
	flags := filepath.Join(dir, "flags")
	if err := os.MkdirAll(flags, 0o700); err != nil {
		t.Fatal(err)
	}

	fakeCursor := filepath.Join(dir, "cursor-agent")
	writeTestExecutable(t, fakeCursor, []byte(fakeCursorWarmScript(spawnLog, turnFile)))

	pool := newCursorWarmTestPool(t)

	runTurn := func(taskID, token, resumeID string) Result {
		b := newCursorWarmBackend(t, fakeCursor, pool, map[string]string{"CURSOR_FLAGS_DIR": flags}, taskID, token)
		return runCursorWarmTurn(t, b, dir, taskID, resumeID, "brief-v1", "")
	}

	r1 := runTurn("task-1", "mat_t1", "")
	if r1.Status != "completed" || pool.Len() != 1 {
		t.Fatalf("Turn 1 result = %+v, pool = %d; want a parked process", r1, pool.Len())
	}

	// Arm the not-found answer for the warm hit's prompt: the parked process
	// reads the flag file on every prompt, so the rewrite reaches it.
	if err := os.WriteFile(filepath.Join(flags, "prompt-not-found"), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	r2 := runTurn("task-2", "mat_t2", "ses-warm-1")
	if !r2.ResumeRejected || r2.SessionID != "" {
		t.Fatalf("Turn 2 result = %+v, want ResumeRejected with a cleared session id", r2)
	}
	if r2.Status != "failed" || !strings.Contains(strings.ToLower(r2.Error), "session not found") {
		t.Fatalf("Turn 2 result = %+v, want failed with the resume rejection", r2)
	}
	if pool.Len() != 0 {
		t.Fatalf("pool.Len() after the rejection = %d, want 0 (the process closed)", pool.Len())
	}
}

func TestCursorWarmSessionLoadRejectedFallsBackCold(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	spawnLog := filepath.Join(dir, "spawns.log")
	turnFile := filepath.Join(dir, "turns.txt")
	flags := filepath.Join(dir, "flags")
	if err := os.MkdirAll(flags, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(flags, "load-fail"), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}

	fakeCursor := filepath.Join(dir, "cursor-agent")
	writeTestExecutable(t, fakeCursor, []byte(fakeCursorWarmScript(spawnLog, turnFile)))

	pool := newCursorWarmTestPool(t)

	b := newCursorWarmBackend(t, fakeCursor, pool, map[string]string{"CURSOR_FLAGS_DIR": flags}, "task-1", "mat_t1")
	res := runCursorWarmTurn(t, b, dir, "task-1", "ses-gone", "brief-v1", "")
	if res.Status != "failed" || !res.ResumeRejected || !strings.Contains(strings.ToLower(res.Error), "session not found") {
		t.Fatalf("rejected-resume result = %+v, want failed + ResumeRejected", res)
	}
	if pool.Len() != 0 {
		t.Fatalf("pool.Len() after a rejected resume = %d, want 0", pool.Len())
	}
}

func TestCursorWarmSetModelRunsOncePerProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	spawnLog := filepath.Join(dir, "spawns.log")
	turnFile := filepath.Join(dir, "turns.txt")
	requests := filepath.Join(dir, "requests.log")

	fakeCursor := filepath.Join(dir, "cursor-agent")
	writeTestExecutable(t, fakeCursor, []byte(fakeCursorWarmScript(spawnLog, turnFile)))

	pool := newCursorWarmTestPool(t)

	runTurn := func(taskID, token, resumeID string) Result {
		b := newCursorWarmBackend(t, fakeCursor, pool, map[string]string{"CURSOR_REQUESTS_FILE": requests}, taskID, token)
		return runCursorWarmTurn(t, b, dir, taskID, resumeID, "brief-v1", "composer-2.5[fast=true]")
	}

	r1 := runTurn("task-1", "mat_t1", "")
	r2 := runTurn("task-2", "mat_t2", "ses-warm-1")
	if r1.Status != "completed" || r2.Status != "completed" {
		t.Fatalf("results = %+v / %+v, want both completed", r1, r2)
	}
	if n := countFileLines(t, spawnLog); n != 1 {
		t.Fatalf("expected 1 spawn, got %d", n)
	}
	setModelCalls := 0
	data, err := os.ReadFile(requests)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "session/set_model" {
			setModelCalls++
		}
	}
	if setModelCalls != 1 {
		t.Fatalf("session/set_model ran %d times across two turns on one process, want 1", setModelCalls)
	}
	// Usage keys off the billed model the prompt result reported, and the
	// second turn bills only its own delta.
	if u := r2.Usage["composer-2.5"]; u.InputTokens != 20 {
		t.Fatalf("Turn 2 usage = %+v, want the delta keyed by the billed model", u)
	}
}

// TestCursorWarmEligible pins the warm gate: per-task claims the ACP launch
// cannot carry stay on the one-shot path; turn-scoped claim fields (timeout,
// resume pointer, brief hash, thinking level, model) never gate it.
func TestCursorWarmEligible(t *testing.T) {
	plain := []ExecOptions{
		{},
		{Cwd: "/tmp/w", ResumeSessionID: "ses-1", WarmPoolBriefHash: "h", ThinkingLevel: "high", Model: "composer-2.5", Timeout: time.Minute},
	}
	for _, opts := range plain {
		if !cursorWarmEligible(opts) {
			t.Errorf("cursorWarmEligible(%+v) = false, want true", opts)
		}
	}
	perTask := []struct {
		name string
		opts ExecOptions
	}{
		{"skill invocation", ExecOptions{SkillInvocation: &SkillInvocation{Prompt: "review this"}}},
		{"steering supplement", ExecOptions{EnableTaskSupplement: true}},
		{"custom args", ExecOptions{CustomArgs: []string{"--flag"}}},
		{"extra args", ExecOptions{ExtraArgs: []string{"--flag"}}},
	}
	for _, tc := range perTask {
		if cursorWarmEligible(tc.opts) {
			t.Errorf("%s: cursorWarmEligible = true, want false (the claim stays on the one-shot path)", tc.name)
		}
	}
}
