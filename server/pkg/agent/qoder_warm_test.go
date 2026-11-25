package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeQoderWarmScript is a minimal `qodercli` fake: the `--yolo --acp`
// invocation speaks the slice of ACP the warm transport drives (initialize →
// session/resume|session/new → optional set_model → session/prompt). Each
// prompt reads the per-turn credentials from MULTICA_TASK_ENV_FILE and echoes
// them into the answer text, so tests can assert the overlay plumbing: the
// static environment must carry no token at all (static=EMPTY).
//
// The usage_update snapshot is deliberately larger on turn 1 (5000/500) than
// on every later turn (300/30): the per-process usage accumulator must be
// reset per turn, or turn 2 would report turn 1's maxima.
func fakeQoderWarmScript(spawnLog, turnFile string) string {
	return `#!/bin/sh
if [ -n "$QODER_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$QODER_ARGS_FILE"
  done
fi
printf '%s\n' "$$" >> '` + spawnLog + `'
turn=0
while IFS= read -r line; do
  if [ -n "$QODER_REQUESTS_FILE" ]; then
    case "$line" in
      *'"method":"initialize"'*) printf 'initialize\n' >> "$QODER_REQUESTS_FILE";;
      *'"method":"session/resume"'*) printf 'session/resume\n' >> "$QODER_REQUESTS_FILE";;
      *'"method":"session/new"'*) printf 'session/new\n' >> "$QODER_REQUESTS_FILE";;
      *'"method":"session/set_model"'*) printf 'session/set_model\n' >> "$QODER_REQUESTS_FILE";;
      *'"method":"session/prompt"'*) printf 'session/prompt\n' >> "$QODER_REQUESTS_FILE";;
    esac
  fi
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/resume"'*)
      if [ -n "$QODER_RESUME_FAIL" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32002,"message":"ACP session not found"}}\n' "$id"
      else
        printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses-warm-1"}}\n' "$id"
      fi
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses-warm-1","models":{"currentModelId":"qoder-chat-1"}}}\n' "$id"
      ;;
    *'"method":"session/set_model"'*)
      if [ -n "$QODER_SET_MODEL_FAIL" ]; then
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
      if [ -n "$QODER_PROMPT_FAIL" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"prompt failed"}}\n' "$id"
        continue
      fi
      if [ -n "$QODER_PROMPT_NOT_FOUND" ]; then
        # The runtime echoed the requested id from session/resume but the
        # session is gone by prompt time — the cold path's resume-rejection
        # shape, surfaced late.
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32002,"message":"ACP session not found"}}\n' "$id"
        continue
      fi
      if [ "$turn" -eq 1 ]; then
        printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses-warm-1","update":{"sessionUpdate":"usage_update","usage":{"inputTokens":5000,"outputTokens":500}}}}\n'
      else
        printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses-warm-1","update":{"sessionUpdate":"usage_update","usage":{"inputTokens":300,"outputTokens":30}}}}\n'
      fi
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses-warm-1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"turn=%s token=%s task=%s static=%s/%s"}}}}\n' "$turn" "$tok" "$tid" "$static_tok" "$static_task"
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id"
      ;;
  esac
done
`
}

func TestQoderWarmPoolReuseAndTokenIsolation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	spawnLog := filepath.Join(dir, "spawns.log")
	turnFile := filepath.Join(dir, "turns.txt")

	fakeQoder := filepath.Join(dir, "qodercli")
	writeTestExecutable(t, fakeQoder, []byte(fakeQoderWarmScript(spawnLog, turnFile)))

	pool := NewWarmSessionPool(WarmSessionPoolConfig{
		IdleTTL: 30 * time.Second,
		MaxIdle: 4,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() { pool.Close() })

	runTurn := func(taskID, token, resumeID, briefHash string) Result {
		t.Helper()
		b, err := New("qoder", Config{
			ExecutablePath: fakeQoder,
			Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
			TaskID:         taskID,
			BuiltinRuntime: true,
			WarmPool:       pool,
			Env: map[string]string{
				"MULTICA_TOKEN":        token,
				"MULTICA_TASK_ID":      taskID,
				"MULTICA_AGENT_ID":     "agent-1",
				"MULTICA_WORKSPACE_ID": "ws-1",
			},
		})
		if err != nil {
			t.Fatalf("New(qoder): %v", err)
		}
		sess, err := b.Execute(context.Background(), "hello", ExecOptions{
			Cwd:               dir,
			ResumeSessionID:   resumeID,
			WarmPoolBriefHash: briefHash,
			Timeout:           10 * time.Second,
		})
		if err != nil {
			t.Fatalf("Execute(%s): %v", taskID, err)
		}
		for range sess.Messages {
		}
		return <-sess.Result
	}

	spawnCount := func() int {
		t.Helper()
		data, err := os.ReadFile(spawnLog)
		if os.IsNotExist(err) {
			return 0
		}
		if err != nil {
			t.Fatal(err)
		}
		return strings.Count(strings.TrimSpace(string(data)), "\n") + 1
	}

	// Turn 1: cold spawn, gets parked.
	r1 := runTurn("task-1", "mat_t1", "", "brief-v1")
	if r1.Status != "completed" || r1.SessionID != "ses-warm-1" {
		t.Fatalf("Turn 1 result = %+v", r1)
	}
	if !strings.Contains(r1.Output, "turn=1 token=mat_t1 task=task-1 static=EMPTY/EMPTY") {
		t.Fatalf("Turn 1 output = %q (expected overlay delivery and stripped static env)", r1.Output)
	}
	if u := r1.Usage["qoder-chat-1"]; u.InputTokens != 5000 || u.OutputTokens != 500 {
		t.Fatalf("Turn 1 usage = %+v, want input=5000 output=500 keyed by the session's model", u)
	}
	if pool.Len() != 1 {
		t.Fatalf("pool.Len() after Turn 1 = %d, want 1", pool.Len())
	}

	// Verify the idle overlay holds NO live token while parked.
	pool.mu.Lock()
	parked := pool.entries[warmPoolKey{provider: "qoder", sessionID: "ses-warm-1"}].proc.(*qoderWarmProcess)
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
	// The usage must be this turn's (300/30), not Turn 1's maxima (5000/500)
	// — the per-process usage accumulator is reset per turn.
	r2 := runTurn("task-2", "mat_t2", "ses-warm-1", "brief-v1")
	if r2.Status != "completed" || r2.SessionID != "ses-warm-1" {
		t.Fatalf("Turn 2 result = %+v", r2)
	}
	if !strings.Contains(r2.Output, "turn=2 token=mat_t2 task=task-2 static=EMPTY/EMPTY") {
		t.Fatalf("Turn 2 output = %q (expected same process turn=2 with fresh token)", r2.Output)
	}
	if u := r2.Usage["qoder-chat-1"]; u.InputTokens != 300 || u.OutputTokens != 30 {
		t.Fatalf("Turn 2 usage = %+v, want input=300 output=30 (previous turn's usage_update must not leak)", u)
	}
	if n := spawnCount(); n != 1 {
		t.Fatalf("expected 1 qoder spawn across Turn 1 & 2, got %d", n)
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
	if n := spawnCount(); n != 2 {
		t.Fatalf("expected 2 qoder spawns after the fingerprint change, got %d", n)
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

// A failed turn is never parked: the process closes and the pool stays empty,
// so the daemon's next turn cold-starts.
func TestQoderWarmFailedTurnNotParked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	spawnLog := filepath.Join(dir, "spawns.log")
	turnFile := filepath.Join(dir, "turns.txt")

	fakeQoder := filepath.Join(dir, "qodercli")
	writeTestExecutable(t, fakeQoder, []byte(fakeQoderWarmScript(spawnLog, turnFile)))

	pool := NewWarmSessionPool(WarmSessionPoolConfig{
		IdleTTL: 30 * time.Second,
		MaxIdle: 4,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() { pool.Close() })

	b, err := New("qoder", Config{
		ExecutablePath: fakeQoder,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		TaskID:         "task-1",
		BuiltinRuntime: true,
		WarmPool:       pool,
		Env: map[string]string{
			"MULTICA_TOKEN":        "mat_x",
			"MULTICA_TASK_ID":      "task-1",
			"MULTICA_AGENT_ID":     "agent-1",
			"MULTICA_WORKSPACE_ID": "ws-1",
			"QODER_PROMPT_FAIL":    "1",
		},
	})
	if err != nil {
		t.Fatalf("New(qoder): %v", err)
	}
	sess, err := b.Execute(context.Background(), "hello", ExecOptions{
		Cwd:     dir,
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range sess.Messages {
	}
	res := <-sess.Result
	if res.Status != "failed" {
		t.Fatalf("failed-turn result = %+v", res)
	}
	if pool.Len() != 0 {
		t.Fatalf("pool.Len() after a failed turn = %d, want 0", pool.Len())
	}
	data, err := os.ReadFile(spawnLog)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(strings.TrimSpace(string(data)), "\n") + 1; lines != 1 {
		t.Fatalf("expected exactly 1 spawn, got %d", lines)
	}
}

// The fingerprint must not move between turns of the same conversation: the
// resume pointer selects the conversation, it is not part of how the process
// was built. Everything that does shape the process — the model that becomes
// a set_model call, surviving custom args — must change it.
func TestQoderWarmFingerprintIgnoresResumePointer(t *testing.T) {
	t.Parallel()
	b := &qoderBackend{cfg: Config{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Env:    map[string]string{"MULTICA_AGENT_ID": "agent-1"},
	}}
	envMap := map[string]string{"PATH": "/bin", "PWD": "/work"}
	base := ExecOptions{Cwd: "/work", Model: "qoder-chat-2", ResumeSessionID: "ses-a", WarmPoolBriefHash: "b1"}
	fp1 := b.qoderWarmFingerprint("qodercli", base, envMap)
	fp2 := b.qoderWarmFingerprint("qodercli", ExecOptions{Cwd: "/work", Model: "qoder-chat-2", ResumeSessionID: "", WarmPoolBriefHash: "b1"}, envMap)
	if fp1 != fp2 {
		t.Fatal("resume pointer changed the fingerprint")
	}
	fp3 := b.qoderWarmFingerprint("qodercli", ExecOptions{Cwd: "/work", Model: "qoder-chat-9", ResumeSessionID: "ses-a", WarmPoolBriefHash: "b1"}, envMap)
	if fp3 == fp1 {
		t.Fatal("model change did not change the fingerprint")
	}
	withArgs := base
	withArgs.CustomArgs = []string{"--profile", "fast"}
	fp4 := b.qoderWarmFingerprint("qodercli", withArgs, envMap)
	if fp4 == fp1 {
		t.Fatal("custom args change did not change the fingerprint")
	}
	// A volatile credential rides the overlay, not the fingerprint.
	envTok := map[string]string{"PATH": "/bin", "PWD": "/work", "MULTICA_TOKEN": "secret"}
	fp5 := b.qoderWarmFingerprint("qodercli", base, envTok)
	if fp5 != fp1 {
		t.Fatal("volatile task env leaked into the fingerprint")
	}
}

// A turn cancelled while a synchronous setup write is wedged must return
// promptly: the stdin-close guard stays armed across the WHOLE setup (the
// devin warm QA regression, mirrored). The fake answers initialize and then
// stops reading stdin entirely; a 1MiB MCP config wedges the session/new
// write past the pipe buffer, and the run is cancelled 100ms in. The teardown
// graces are shortened so the assertion measures the released write, not the
// machine load.
func TestQoderWarmCancelledSetupWithBlockedWriteReturns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	fakeQoder := filepath.Join(dir, "qodercli")
	script := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      # The wedge: answer initialize, then stop reading stdin entirely
      # (replace the shell with a sleeper) so the next setup write wedges
      # once the pipe buffer fills.
      exec sleep 30
      ;;
  esac
done
`
	writeTestExecutable(t, fakeQoder, []byte(script))

	// ~1MiB of local MCP servers: every entry survives the capability filter
	// (local entries carry no transport type), so the session/new write is
	// far larger than the pipe buffer.
	var sb strings.Builder
	sb.WriteString(`{"mcpServers":{"`)
	for i := 0; i < 9000; i++ {
		if i > 0 {
			sb.WriteString(`,"`)
		}
		sb.WriteString(fmt.Sprintf("srv%04d", i))
		sb.WriteString(`":{"command":"node","args":["s.js","` + strings.Repeat("x", 90) + `"],"env":{"K":"` + strings.Repeat("v", 10) + `"}}`)
	}
	sb.WriteString(`}}`)
	bigMcp := json.RawMessage(sb.String())
	if len(bigMcp) < 900_000 {
		t.Fatalf("mcp payload too small to wedge the pipe: %d bytes", len(bigMcp))
	}

	pool := NewWarmSessionPool(WarmSessionPoolConfig{
		IdleTTL: 30 * time.Second,
		MaxIdle: 4,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() { pool.Close() })

	// The wedged sleeper never exits on its own, so the teardown would ride
	// the full 3s+3s graces and make the assertion measure machine load.
	// Shorten both so the test isolates the regression signal.
	qoderWarmExitGraceNanos.Store(50 * time.Millisecond.Nanoseconds())
	qoderWarmTerminateGraceNanos.Store(50 * time.Millisecond.Nanoseconds())
	t.Cleanup(func() {
		qoderWarmExitGraceNanos.Store(0)
		qoderWarmTerminateGraceNanos.Store(0)
	})

	b, err := New("qoder", Config{
		ExecutablePath: fakeQoder,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		TaskID:         "task-1",
		BuiltinRuntime: true,
		WarmPool:       pool,
		Env: map[string]string{
			"MULTICA_TOKEN":        "mat_x",
			"MULTICA_TASK_ID":      "task-1",
			"MULTICA_AGENT_ID":     "agent-1",
			"MULTICA_WORKSPACE_ID": "ws-1",
		},
	})
	if err != nil {
		t.Fatalf("New(qoder): %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	sess, err := b.Execute(ctx, "hello", ExecOptions{
		Cwd:       dir,
		McpConfig: bigMcp,
		Timeout:   30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range sess.Messages {
	}
	res := <-sess.Result
	elapsed := time.Since(start)
	if elapsed > 5*time.Second {
		t.Fatalf("cancelled setup took %s to return; the blocked session/new write was not released (want <5s)", elapsed)
	}
	if res.Status != "timeout" && res.Status != "failed" && res.Status != "aborted" {
		t.Fatalf("cancelled setup result = %+v", res)
	}
}

// A runtime that refuses the resumed session (session/resume errors with a
// not-found shape) must stop before prompting: the turn reports
// ResumeRejected with no session id, and the fake saw exactly [initialize,
// session/resume] — no prompt, no look-alike continuation.
func TestQoderWarmEvictedSessionColdFallbackStopsBeforePrompt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	spawnLog := filepath.Join(dir, "spawns.log")
	turnFile := filepath.Join(dir, "turns.txt")
	requestsFile := filepath.Join(dir, "requests.log")

	fakeQoder := filepath.Join(dir, "qodercli")
	writeTestExecutable(t, fakeQoder, []byte(fakeQoderWarmScript(spawnLog, turnFile)))

	pool := NewWarmSessionPool(WarmSessionPoolConfig{
		IdleTTL: 30 * time.Second,
		MaxIdle: 4,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() { pool.Close() })

	newBackend := func(extraEnv map[string]string) *qoderBackend {
		t.Helper()
		env := map[string]string{
			"MULTICA_TOKEN":        "mat_x",
			"MULTICA_TASK_ID":      "task-1",
			"MULTICA_AGENT_ID":     "agent-1",
			"MULTICA_WORKSPACE_ID": "ws-1",
		}
		for k, v := range extraEnv {
			env[k] = v
		}
		b, err := New("qoder", Config{
			ExecutablePath: fakeQoder,
			Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
			TaskID:         "task-1",
			BuiltinRuntime: true,
			WarmPool:       pool,
			Env:            env,
		})
		if err != nil {
			t.Fatalf("New(qoder): %v", err)
		}
		return b.(*qoderBackend)
	}
	run := func(b *qoderBackend, resumeID string) Result {
		t.Helper()
		sess, err := b.Execute(context.Background(), "hello", ExecOptions{
			Cwd:             dir,
			ResumeSessionID: resumeID,
			Timeout:         10 * time.Second,
		})
		if err != nil {
			t.Fatalf("Execute(resume=%q): %v", resumeID, err)
		}
		for range sess.Messages {
		}
		return <-sess.Result
	}

	// Turn 1: a good turn, parked.
	r1 := run(newBackend(nil), "")
	if r1.Status != "completed" || r1.SessionID != "ses-warm-1" || pool.Len() != 1 {
		t.Fatalf("Turn 1 = %+v, pool.Len=%d", r1, pool.Len())
	}
	pool.Evict("qoder", "ses-warm-1")

	// Turn 2: resume the evicted session, but the runtime can no longer
	// restore it (QODER_RESUME_FAIL).
	r2 := run(newBackend(map[string]string{"QODER_RESUME_FAIL": "1", "QODER_REQUESTS_FILE": requestsFile}), "ses-warm-1")
	if r2.Status != "failed" {
		t.Fatalf("cold-fallback turn = %+v, want failed before prompting", r2)
	}
	if !r2.ResumeRejected {
		t.Fatalf("cold-fallback turn must report ResumeRejected so the daemon drops the pointer: %+v", r2)
	}
	if r2.SessionID != "" {
		t.Fatalf("cold-fallback turn leaked a session id %q; it must not present a new session as a continuation", r2.SessionID)
	}
	if pool.Len() != 0 {
		t.Fatalf("pool.Len() after the rejected resume = %d, want 0", pool.Len())
	}

	data, err := os.ReadFile(requestsFile)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(got) != 2 || got[0] != "initialize" || got[1] != "session/resume" {
		t.Fatalf("requests after the rejected resume = %v; want [initialize session/resume] only (no prompt, no fresh session)", got)
	}
	spawns, _ := os.ReadFile(spawnLog)
	if n := strings.Count(strings.TrimSpace(string(spawns)), "\n") + 1; n != 2 {
		t.Fatalf("expected 2 spawns (turn 1 + cold fallback), got %d", n)
	}
}

// The runtime may echo the requested id from session/resume and only reject
// the session at prompt time. The warm path must surface the same signal the
// cold path does — ResumeRejected with no session id — so the daemon drops
// the stale pointer, and the process never parks; the fresh-session retry
// still completes.
func TestQoderWarmPromptTimeSessionNotFoundRejectsResume(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	spawnLog := filepath.Join(dir, "spawns.log")
	turnFile := filepath.Join(dir, "turns.txt")

	fakeQoder := filepath.Join(dir, "qodercli")
	writeTestExecutable(t, fakeQoder, []byte(fakeQoderWarmScript(spawnLog, turnFile)))

	pool := NewWarmSessionPool(WarmSessionPoolConfig{
		IdleTTL: 30 * time.Second,
		MaxIdle: 4,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() { pool.Close() })

	newBackend := func(extraEnv map[string]string) *qoderBackend {
		t.Helper()
		env := map[string]string{
			"MULTICA_TOKEN":        "mat_x",
			"MULTICA_TASK_ID":      "task-1",
			"MULTICA_AGENT_ID":     "agent-1",
			"MULTICA_WORKSPACE_ID": "ws-1",
		}
		for k, v := range extraEnv {
			env[k] = v
		}
		b, err := New("qoder", Config{
			ExecutablePath: fakeQoder,
			Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
			TaskID:         "task-1",
			BuiltinRuntime: true,
			WarmPool:       pool,
			Env:            env,
		})
		if err != nil {
			t.Fatalf("New(qoder): %v", err)
		}
		return b.(*qoderBackend)
	}
	run := func(b *qoderBackend, resumeID string) Result {
		t.Helper()
		sess, err := b.Execute(context.Background(), "hello", ExecOptions{
			Cwd:             dir,
			ResumeSessionID: resumeID,
			Timeout:         10 * time.Second,
		})
		if err != nil {
			t.Fatalf("Execute(resume=%q): %v", resumeID, err)
		}
		for range sess.Messages {
		}
		return <-sess.Result
	}

	// Turn 1: a good turn, parked.
	r1 := run(newBackend(nil), "")
	if r1.Status != "completed" || r1.SessionID != "ses-warm-1" || pool.Len() != 1 {
		t.Fatalf("Turn 1 = %+v, pool.Len=%d", r1, pool.Len())
	}
	pool.Evict("qoder", "ses-warm-1")

	// Turn 2: the cold fallback's session/resume succeeds, but the runtime
	// refuses the session at prompt time (QODER_PROMPT_NOT_FOUND).
	r2 := run(newBackend(map[string]string{"QODER_PROMPT_NOT_FOUND": "1"}), "ses-warm-1")
	if r2.Status != "failed" {
		t.Fatalf("prompt-time rejection = %+v, want failed", r2)
	}
	if !r2.ResumeRejected {
		t.Fatalf("prompt-time rejection must report ResumeRejected so the daemon drops the pointer: %+v", r2)
	}
	if r2.SessionID != "" {
		t.Fatalf("prompt-time rejection leaked session id %q; it must not present the refused session", r2.SessionID)
	}
	if pool.Len() != 0 {
		t.Fatalf("pool.Len() after the rejected resume = %d, want 0", pool.Len())
	}

	// The daemon's fresh-session retry (no resume pointer) works normally.
	r3 := run(newBackend(nil), "")
	if r3.Status != "completed" || r3.SessionID != "ses-warm-1" {
		t.Fatalf("fresh-session retry = %+v", r3)
	}
	data, err := os.ReadFile(spawnLog)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(strings.TrimSpace(string(data)), "\n") + 1; n != 3 {
		t.Fatalf("expected 3 spawns (good, cold fallback, fresh retry), got %d", n)
	}
}

// A model override rides the first-turn setup as a session/set_model call on
// the pooled process, and a set_model failure fails the turn without parking
// (withholding the id on a fresh session, reporting ResumeRejected when the
// runtime says the resumed session is gone).
func TestQoderWarmSetModelRunsOncePerProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	spawnLog := filepath.Join(dir, "spawns.log")
	turnFile := filepath.Join(dir, "turns.txt")
	requestsFile := filepath.Join(dir, "requests.log")

	fakeQoder := filepath.Join(dir, "qodercli")
	writeTestExecutable(t, fakeQoder, []byte(fakeQoderWarmScript(spawnLog, turnFile)))

	pool := NewWarmSessionPool(WarmSessionPoolConfig{
		IdleTTL: 30 * time.Second,
		MaxIdle: 4,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() { pool.Close() })

	newBackend := func(extraEnv map[string]string) *qoderBackend {
		t.Helper()
		env := map[string]string{
			"MULTICA_TOKEN":        "mat_x",
			"MULTICA_TASK_ID":      "task-1",
			"MULTICA_AGENT_ID":     "agent-1",
			"MULTICA_WORKSPACE_ID": "ws-1",
		}
		for k, v := range extraEnv {
			env[k] = v
		}
		b, err := New("qoder", Config{
			ExecutablePath: fakeQoder,
			Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
			TaskID:         "task-1",
			BuiltinRuntime: true,
			WarmPool:       pool,
			Env:            env,
		})
		if err != nil {
			t.Fatalf("New(qoder): %v", err)
		}
		return b.(*qoderBackend)
	}
	run := func(b *qoderBackend, model string) Result {
		t.Helper()
		sess, err := b.Execute(context.Background(), "hello", ExecOptions{
			Cwd:     dir,
			Model:   model,
			Timeout: 10 * time.Second,
		})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		for range sess.Messages {
		}
		return <-sess.Result
	}

	// Turn 1: a good turn with a model override — one set_model.
	r1 := run(newBackend(map[string]string{"QODER_REQUESTS_FILE": requestsFile}), "qoder-chat-2")
	if r1.Status != "completed" || r1.SessionID != "ses-warm-1" || pool.Len() != 1 {
		t.Fatalf("Turn 1 = %+v, pool.Len=%d", r1, pool.Len())
	}
	// Turn 2: warm hit — the session's model is settled, no second set_model.
	r2 := run(newBackend(nil), "qoder-chat-2")
	if r2.Status != "completed" {
		t.Fatalf("Turn 2 = %+v", r2)
	}

	data, err := os.ReadFile(requestsFile)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(data)), "\n")
	want := []string{"initialize", "session/new", "session/set_model", "session/prompt"}
	if len(got) != len(want) {
		t.Fatalf("setup requests = %v, want exactly %v (set_model once per process)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("setup requests = %v, want %v", got, want)
		}
	}
	// Turn 3: a claim with a DIFFERENT model is a different process identity —
	// it evicts the parked one and runs its own set_model.
	r3 := run(newBackend(nil), "qoder-chat-9")
	if r3.Status != "completed" {
		t.Fatalf("Turn 3 (other model) = %+v", r3)
	}
	if pool.Len() != 1 {
		t.Fatalf("pool.Len() after the model change = %d, want 1 (the new process)", pool.Len())
	}

	// A set_model failure is never parked: the turn withholds its session id
	// (a fresh session that never prompted) and closes its own process. The
	// pooled process left behind is r3's — a fresh claim (empty resume
	// pointer) with a different fingerprint cannot acquire it, because
	// Acquire only looks up parked sessions.
	r4 := run(newBackend(map[string]string{"QODER_SET_MODEL_FAIL": "1"}), "qoder-chat-4")
	if r4.Status != "failed" {
		t.Fatalf("set_model failure = %+v, want failed", r4)
	}
	if r4.SessionID != "" || r4.ResumeRejected {
		t.Fatalf("set_model failure on a fresh session must withhold the session id: %+v", r4)
	}
	if pool.Len() != 1 {
		t.Fatalf("pool.Len() after a set_model failure = %d, want 1 (r3's process; the failed turn's own process is closed)", pool.Len())
	}
}
