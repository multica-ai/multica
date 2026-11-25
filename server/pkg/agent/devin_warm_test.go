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

// fakeDevinWarmScript is a minimal `devin` fake: the `acp` subcommand speaks
// the slice of ACP the warm transport drives (initialize → optional
// authenticate → session/new → optional set_config_option → session/prompt).
// Each prompt reads the per-turn credentials from MULTICA_TASK_ENV_FILE and
// echoes them into the answer text, so tests can assert the overlay plumbing:
// the static environment must carry no token at all (static=EMPTY).
//
// The usage_update snapshot is deliberately larger on turn 1 (5000/500) than
// on every later turn (300/30): the per-process usage accumulator must be
// reset per turn, or turn 2 would report turn 1's maxima.
func fakeDevinWarmScript(spawnLog, turnFile string) string {
	return `#!/bin/sh
if [ -n "$DEVIN_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$DEVIN_ARGS_FILE"
  done
fi
printf '%s\n' "$$" >> '` + spawnLog + `'
turn=0
while IFS= read -r line; do
  if [ -n "$DEVIN_REQUESTS_FILE" ]; then
    case "$line" in
      *'"method":"initialize"'*) printf 'initialize\n' >> "$DEVIN_REQUESTS_FILE";;
      *'"method":"authenticate"'*) printf 'authenticate\n' >> "$DEVIN_REQUESTS_FILE";;
      *'"method":"session/new"'*) printf 'session/new\n' >> "$DEVIN_REQUESTS_FILE";;
      *'"method":"session/load"'*) printf 'session/load\n' >> "$DEVIN_REQUESTS_FILE";;
      *'"method":"session/set_config_option"'*) printf 'session/set_config_option\n' >> "$DEVIN_REQUESTS_FILE";;
      *'"method":"session/prompt"'*) printf 'session/prompt\n' >> "$DEVIN_REQUESTS_FILE";;
    esac
  fi
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      if [ -n "$DEVIN_INIT_DELAY" ]; then sleep "$DEVIN_INIT_DELAY"; fi
      load='"loadSession":true'
      if [ -n "$DEVIN_NO_LOAD" ]; then load='"loadSession":false'; fi
      auth=''
      if [ -n "$DEVIN_AUTH" ]; then auth=',"authMethods":[{"id":"windsurf-api-key"}]'; fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{%s}%s}}\n' "$id" "$load" "$auth"
      ;;
    *'"method":"authenticate"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses-warm-1","models":{"currentModelId":"swe-1.7-lightning"},"configOptions":[{"id":"model","category":"model","currentValue":"swe-1.7-lightning","options":[{"value":"swe-1.7-lightning","name":"Lightning"},{"value":"swe-1.9-ultra","name":"Ultra"}]}]}}\n' "$id"
      ;;
    *'"method":"session/load"'*)
      if [ -n "$DEVIN_LOAD_FAIL" ]; then
        # A real runtime refuses to restore a session it no longer has; the
        # daemon must stop instead of opening a look-alike continuation.
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32016,"message":"Session not found: ses-warm-1"}}\n' "$id"
      else
        printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses-warm-1"}}\n' "$id"
      fi
      ;;
    *'"method":"session/set_config_option"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"configOptions":[{"id":"model","category":"model","currentValue":"swe-1.9-ultra"}]}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      turn=$((turn + 1))
      printf '%s' "$turn" > "` + turnFile + `"
      tok=$(sed -n 's/.*"MULTICA_TOKEN":"\([^"]*\)".*/\1/p' "$MULTICA_TASK_ENV_FILE")
      tid=$(sed -n 's/.*"MULTICA_TASK_ID":"\([^"]*\)".*/\1/p' "$MULTICA_TASK_ENV_FILE")
      static_tok="${MULTICA_TOKEN:-EMPTY}"
      static_task="${MULTICA_TASK_ID:-EMPTY}"
      if [ -n "$DEVIN_PROMPT_FAIL" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"prompt failed"}}\n' "$id"
        continue
      fi
      if [ -n "$DEVIN_PROMPT_NOT_FOUND" ]; then
        # The runtime accepted session/load but the session is gone by prompt
        # time — the cold path's resume-rejection shape.
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32016,"message":"Session not found: ses-warm-1"}}\n' "$id"
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

func TestDevinWarmPoolReuseAndTokenIsolation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	spawnLog := filepath.Join(dir, "spawns.log")
	turnFile := filepath.Join(dir, "turns.txt")

	fakeDevin := filepath.Join(dir, "devin")
	writeTestExecutable(t, fakeDevin, []byte(fakeDevinWarmScript(spawnLog, turnFile)))

	pool := NewWarmSessionPool(WarmSessionPoolConfig{
		IdleTTL: 30 * time.Second,
		MaxIdle: 4,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() { pool.Close() })

	runTurn := func(taskID, token, resumeID, briefHash string) Result {
		t.Helper()
		b, err := New("devin", Config{
			ExecutablePath: fakeDevin,
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
			t.Fatalf("New(devin): %v", err)
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
	if u := r1.Usage["swe-1.7-lightning"]; u.InputTokens != 5000 || u.OutputTokens != 500 {
		t.Fatalf("Turn 1 usage = %+v, want input=5000 output=500 keyed by the session's model", u)
	}
	if pool.Len() != 1 {
		t.Fatalf("pool.Len() after Turn 1 = %d, want 1", pool.Len())
	}

	// Verify the idle overlay holds NO live token while parked.
	pool.mu.Lock()
	parked := pool.entries[warmPoolKey{provider: "devin", sessionID: "ses-warm-1"}].proc.(*devinWarmProcess)
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
	if u := r2.Usage["swe-1.7-lightning"]; u.InputTokens != 300 || u.OutputTokens != 30 {
		t.Fatalf("Turn 2 usage = %+v, want input=300 output=30 (previous turn's usage_update must not leak)", u)
	}
	if n := spawnCount(); n != 1 {
		t.Fatalf("expected 1 devin spawn across Turn 1 & 2, got %d", n)
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
		t.Fatalf("expected 2 devin spawns after the fingerprint change, got %d", n)
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
func TestDevinWarmFailedTurnNotParked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	spawnLog := filepath.Join(dir, "spawns.log")
	turnFile := filepath.Join(dir, "turns.txt")

	fakeDevin := filepath.Join(dir, "devin")
	writeTestExecutable(t, fakeDevin, []byte(fakeDevinWarmScript(spawnLog, turnFile)))

	pool := NewWarmSessionPool(WarmSessionPoolConfig{
		IdleTTL: 30 * time.Second,
		MaxIdle: 4,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() { pool.Close() })

	b, err := New("devin", Config{
		ExecutablePath: fakeDevin,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		TaskID:         "task-1",
		BuiltinRuntime: true,
		WarmPool:       pool,
		Env: map[string]string{
			"MULTICA_TOKEN":        "mat_x",
			"MULTICA_TASK_ID":      "task-1",
			"MULTICA_AGENT_ID":     "agent-1",
			"MULTICA_WORKSPACE_ID": "ws-1",
			"DEVIN_PROMPT_FAIL":    "1",
		},
	})
	if err != nil {
		t.Fatalf("New(devin): %v", err)
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
// was built. Everything that does shape the process — model flag, surviving
// custom args — must change it.
func TestDevinWarmFingerprintIgnoresResumePointer(t *testing.T) {
	t.Parallel()
	b := &devinBackend{cfg: Config{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Env:    map[string]string{"MULTICA_AGENT_ID": "agent-1"},
	}}
	envMap := map[string]string{"PATH": "/bin", "PWD": "/work"}
	base := ExecOptions{Cwd: "/work", Model: "swe-1.7-lightning", ResumeSessionID: "ses-a", WarmPoolBriefHash: "b1"}
	fp1 := b.devinWarmFingerprint("devin", base, envMap)
	fp2 := b.devinWarmFingerprint("devin", ExecOptions{Cwd: "/work", Model: "swe-1.7-lightning", ResumeSessionID: "", WarmPoolBriefHash: "b1"}, envMap)
	if fp1 != fp2 {
		t.Fatal("resume pointer changed the fingerprint")
	}
	fp3 := b.devinWarmFingerprint("devin", ExecOptions{Cwd: "/work", Model: "swe-1.9-ultra", ResumeSessionID: "ses-a", WarmPoolBriefHash: "b1"}, envMap)
	if fp3 == fp1 {
		t.Fatal("model change did not change the fingerprint")
	}
	// A surviving custom arg is part of the spawned command line, so it must
	// change the fingerprint too.
	withArgs := base
	withArgs.CustomArgs = []string{"--add-dir", "/tmp/extra"}
	fp4 := b.devinWarmFingerprint("devin", withArgs, envMap)
	if fp4 == fp1 {
		t.Fatal("custom args change did not change the fingerprint")
	}
	// A volatile credential rides the overlay, not the fingerprint.
	envTok := map[string]string{"PATH": "/bin", "PWD": "/work", "MULTICA_TOKEN": "secret"}
	fp5 := b.devinWarmFingerprint("devin", base, envTok)
	if fp5 != fp1 {
		t.Fatal("volatile task env leaked into the fingerprint")
	}
}

// A turn cancelled while a synchronous setup write is wedged must return
// promptly: the stdin-close guard stays armed across the WHOLE setup (the
// opencode warm QA regression, mirrored for devin). The fake answers
// initialize and then stops reading stdin entirely; a 1MiB MCP config wedges
// the session/new write past the pipe buffer, and the run is cancelled 100ms in.
func TestDevinWarmCancelledSetupWithBlockedWriteReturns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	fakeDevin := filepath.Join(dir, "devin")
	script := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
      # The wedge: answer initialize, then stop reading stdin entirely
      # (replace the shell with a sleeper) so the next setup write wedges
      # once the pipe buffer fills.
      exec sleep 30
      ;;
  esac
done
`
	writeTestExecutable(t, fakeDevin, []byte(script))

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
	// Shorten both (the opencodeTerminateGraceNanos relief, mirrored) so the
	// test isolates the regression signal: cancellation must free the turn.
	devinWarmExitGraceNanos.Store(50 * time.Millisecond.Nanoseconds())
	devinWarmTerminateGraceNanos.Store(50 * time.Millisecond.Nanoseconds())
	t.Cleanup(func() {
		devinWarmExitGraceNanos.Store(0)
		devinWarmTerminateGraceNanos.Store(0)
	})

	b, err := New("devin", Config{
		ExecutablePath: fakeDevin,
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
		t.Fatalf("New(devin): %v", err)
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
	// The regression signal is the RELEASED WRITE: before the whole-setup
	// guard the turn returned only when the CLI exited on its own (~30s).
	// The teardown graces are shortened for this test (the wedged sleeper
	// never exits on its own, so their full span is pure wait): what the
	// assertion measures is that cancellation frees the turn promptly, not
	// the absolute teardown duration, which is load-sensitive by design.
	if elapsed > 5*time.Second {
		t.Fatalf("cancelled setup took %s to return; the blocked session/new write was not released (want <5s)", elapsed)
	}
	if res.Status != "timeout" && res.Status != "failed" && res.Status != "aborted" {
		t.Fatalf("cancelled setup result = %+v", res)
	}
}

// A good turn gets evicted from the pool, and the cold fallback cannot
// restore the conversation: the turn must stop before sending a prompt —
// never open a fresh session that only looks like a continuation. The daemon
// receives ResumeRejected so it can drop the pointer and retry as an
// explicitly fresh session.
func TestDevinWarmEvictedSessionColdFallbackStopsBeforePrompt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	spawnLog := filepath.Join(dir, "spawns.log")
	turnFile := filepath.Join(dir, "turns.txt")
	requestsFile := filepath.Join(dir, "requests.log")

	fakeDevin := filepath.Join(dir, "devin")
	writeTestExecutable(t, fakeDevin, []byte(fakeDevinWarmScript(spawnLog, turnFile)))

	pool := NewWarmSessionPool(WarmSessionPoolConfig{
		IdleTTL: 30 * time.Second,
		MaxIdle: 4,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() { pool.Close() })

	newBackend := func(extraEnv map[string]string) *devinBackend {
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
		b, err := New("devin", Config{
			ExecutablePath: fakeDevin,
			Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
			TaskID:         "task-1",
			BuiltinRuntime: true,
			WarmPool:       pool,
			Env:            env,
		})
		if err != nil {
			t.Fatalf("New(devin): %v", err)
		}
		return b.(*devinBackend)
	}
	run := func(b *devinBackend, resumeID string) Result {
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

	// The eviction a full pool or an idle TTL performs: the parked process is
	// closed and the entry is gone.
	pool.Evict("devin", "ses-warm-1")
	if pool.Len() != 0 {
		t.Fatalf("pool.Len() after evict = %d, want 0", pool.Len())
	}

	// Turn 2: resume the evicted session, but the runtime can no longer
	// restore it (DEVIN_LOAD_FAIL answers session/load with -32016).
	r2 := run(newBackend(map[string]string{"DEVIN_LOAD_FAIL": "1", "DEVIN_REQUESTS_FILE": requestsFile}), "ses-warm-1")
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

	// The fake saw initialize + session/load, and NOTHING after: no prompt,
	// no second session/new — the turn stopped before any conversation was
	// opened.
	data, err := os.ReadFile(requestsFile)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(got) != 2 || got[0] != "initialize" || got[1] != "session/load" {
		t.Fatalf("requests after the rejected resume = %v; want [initialize session/load] only (no prompt, no fresh session)", got)
	}
	spawns, _ := os.ReadFile(spawnLog)
	if n := strings.Count(strings.TrimSpace(string(spawns)), "\n") + 1; n != 2 {
		t.Fatalf("expected 2 spawns (turn 1 + cold fallback), got %d", n)
	}
}

// The warm first-turn setup mirrors the one-shot handshake: the advertised
// API-key authentication runs when the credential exists, and the requested
// model is confirmed against the session's configOptions (set_config_option)
// — the usage then lands on the model the session actually runs.
func TestDevinWarmAuthenticateAndModelConfirm(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	spawnLog := filepath.Join(dir, "spawns.log")
	turnFile := filepath.Join(dir, "turns.txt")
	requestsFile := filepath.Join(dir, "requests.log")
	argsFile := filepath.Join(dir, "argv.txt")

	fakeDevin := filepath.Join(dir, "devin")
	writeTestExecutable(t, fakeDevin, []byte(fakeDevinWarmScript(spawnLog, turnFile)))

	pool := NewWarmSessionPool(WarmSessionPoolConfig{
		IdleTTL: 30 * time.Second,
		MaxIdle: 4,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() { pool.Close() })

	b, err := New("devin", Config{
		ExecutablePath: fakeDevin,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		TaskID:         "task-1",
		BuiltinRuntime: true,
		WarmPool:       pool,
		Env: map[string]string{
			"MULTICA_TOKEN":        "mat_x",
			"MULTICA_TASK_ID":      "task-1",
			"MULTICA_AGENT_ID":     "agent-1",
			"MULTICA_WORKSPACE_ID": "ws-1",
			"WINDSURF_API_KEY":     "wak_test",
			"DEVIN_AUTH":           "1",
			"DEVIN_REQUESTS_FILE":  requestsFile,
			"DEVIN_ARGS_FILE":      argsFile,
		},
	})
	if err != nil {
		t.Fatalf("New(devin): %v", err)
	}
	sess, err := b.Execute(context.Background(), "hello", ExecOptions{
		Cwd:     dir,
		Model:   "swe-1.9-ultra",
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range sess.Messages {
	}
	res := <-sess.Result
	if res.Status != "completed" || res.SessionID != "ses-warm-1" {
		t.Fatalf("turn result = %+v", res)
	}
	data, err := os.ReadFile(requestsFile)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(data)), "\n")
	want := []string{"initialize", "authenticate", "session/new", "session/set_config_option", "session/prompt"}
	if len(got) != len(want) {
		t.Fatalf("setup requests = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("setup requests = %v, want %v", got, want)
		}
	}
	if u := res.Usage["swe-1.9-ultra"]; u.InputTokens != 5000 || u.OutputTokens != 500 {
		t.Fatalf("usage = %+v, want it keyed by the CONFIRMED model swe-1.9-ultra with input=5000 output=500", res.Usage)
	}
	// The model flag the process was spawned with is fixed for its lifetime
	// and part of the fingerprint.
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read argv file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(args)), "\n")
	if len(lines) < 3 || lines[0] != "acp" || lines[1] != "--model" || lines[2] != "swe-1.9-ultra" {
		t.Fatalf("spawn argv = %q, want acp --model swe-1.9-ultra first", lines)
	}
}

// A runtime that accepts session/load but refuses the session at prompt time
// has lost the conversation: the turn must report ResumeRejected with no
// session id — the same signal the cold path reports — so the daemon drops
// the stale pointer and retries as an explicitly fresh session, and the
// process never parks.
func TestDevinWarmPromptTimeSessionNotFoundRejectsResume(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	spawnLog := filepath.Join(dir, "spawns.log")
	turnFile := filepath.Join(dir, "turns.txt")

	fakeDevin := filepath.Join(dir, "devin")
	writeTestExecutable(t, fakeDevin, []byte(fakeDevinWarmScript(spawnLog, turnFile)))

	pool := NewWarmSessionPool(WarmSessionPoolConfig{
		IdleTTL: 30 * time.Second,
		MaxIdle: 4,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() { pool.Close() })

	newBackend := func(extraEnv map[string]string) *devinBackend {
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
		b, err := New("devin", Config{
			ExecutablePath: fakeDevin,
			Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
			TaskID:         "task-1",
			BuiltinRuntime: true,
			WarmPool:       pool,
			Env:            env,
		})
		if err != nil {
			t.Fatalf("New(devin): %v", err)
		}
		return b.(*devinBackend)
	}
	run := func(b *devinBackend, resumeID string) Result {
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
	pool.Evict("devin", "ses-warm-1")

	// Turn 2: the cold fallback's session/load succeeds, but the runtime
	// refuses the session at prompt time (DEVIN_PROMPT_NOT_FOUND).
	r2 := run(newBackend(map[string]string{"DEVIN_PROMPT_NOT_FOUND": "1"}), "ses-warm-1")
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
	if n := strings.Count(strings.TrimSpace(mustReadFile(t, spawnLog)), "\n") + 1; n != 3 {
		t.Fatalf("expected 3 spawns (good, cold fallback, fresh retry), got %d", n)
	}
}

// The execution budget covers setup and prompt together: a first turn whose
// handshake eats past the budget reports timeout — never a late success that
// could be parked. The handshake keeps its own shorter cap for the blocked-
// write guard, but it cannot buy the turn extra time.
func TestDevinWarmSetupConsumesExecutionTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	spawnLog := filepath.Join(dir, "spawns.log")
	turnFile := filepath.Join(dir, "turns.txt")

	fakeDevin := filepath.Join(dir, "devin")
	writeTestExecutable(t, fakeDevin, []byte(fakeDevinWarmScript(spawnLog, turnFile)))

	pool := NewWarmSessionPool(WarmSessionPoolConfig{
		IdleTTL: 30 * time.Second,
		MaxIdle: 4,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() { pool.Close() })

	b, err := New("devin", Config{
		ExecutablePath: fakeDevin,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		TaskID:         "task-1",
		BuiltinRuntime: true,
		WarmPool:       pool,
		Env: map[string]string{
			"MULTICA_TOKEN":        "mat_x",
			"MULTICA_TASK_ID":      "task-1",
			"MULTICA_AGENT_ID":     "agent-1",
			"MULTICA_WORKSPACE_ID": "ws-1",
			"DEVIN_INIT_DELAY":     "0.6",
		},
	})
	if err != nil {
		t.Fatalf("New(devin): %v", err)
	}
	start := time.Now()
	sess, err := b.Execute(context.Background(), "hello", ExecOptions{
		Cwd:     dir,
		Timeout: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range sess.Messages {
	}
	res := <-sess.Result
	elapsed := time.Since(start)
	if res.Status != "timeout" {
		t.Fatalf("setup past the budget = %+v, want timeout (never a late success)", res)
	}
	if elapsed > time.Second {
		t.Fatalf("timeout turn took %s to return; the handshake must not escape the execution budget", elapsed)
	}
	if pool.Len() != 0 {
		t.Fatalf("pool.Len() after a timed-out turn = %d, want 0", pool.Len())
	}
	// And the process is really gone: a follow-up turn on the same session id
	// would cold-start (the pool holds nothing to acquire).
	if !pool.Close() {
		t.Fatal("pool.Close() returned false")
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatal(err)
	}
	return string(data)
}

// A runtime that no longer advertises session/load cannot restore an evicted
// conversation either: the turn must stop before prompting with
// ResumeRejected, exactly like the explicit -32016 rejection.
func TestDevinWarmEvictedSessionWithoutLoadCapabilityStopsBeforePrompt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	spawnLog := filepath.Join(dir, "spawns.log")
	turnFile := filepath.Join(dir, "turns.txt")
	requestsFile := filepath.Join(dir, "requests.log")

	fakeDevin := filepath.Join(dir, "devin")
	writeTestExecutable(t, fakeDevin, []byte(fakeDevinWarmScript(spawnLog, turnFile)))

	pool := NewWarmSessionPool(WarmSessionPoolConfig{
		IdleTTL: 30 * time.Second,
		MaxIdle: 4,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() { pool.Close() })

	newBackend := func(extraEnv map[string]string) *devinBackend {
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
		b, err := New("devin", Config{
			ExecutablePath: fakeDevin,
			Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
			TaskID:         "task-1",
			BuiltinRuntime: true,
			WarmPool:       pool,
			Env:            env,
		})
		if err != nil {
			t.Fatalf("New(devin): %v", err)
		}
		return b.(*devinBackend)
	}

	// Turn 1: a good turn under a runtime WITH loadSession, parked.
	r1 := func() Result {
		sess, err := newBackend(nil).Execute(context.Background(), "hello", ExecOptions{
			Cwd:     dir,
			Timeout: 10 * time.Second,
		})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		for range sess.Messages {
		}
		return <-sess.Result
	}()
	if r1.Status != "completed" || pool.Len() != 1 {
		t.Fatalf("Turn 1 = %+v, pool.Len=%d", r1, pool.Len())
	}
	pool.Evict("devin", r1.SessionID)

	// Turn 2: resume, but this runtime no longer advertises loadSession.
	r2 := func() Result {
		sess, err := newBackend(map[string]string{"DEVIN_NO_LOAD": "1", "DEVIN_REQUESTS_FILE": requestsFile}).Execute(context.Background(), "hello", ExecOptions{
			Cwd:             dir,
			ResumeSessionID: r1.SessionID,
			Timeout:         10 * time.Second,
		})
		if err != nil {
			t.Fatalf("Execute(resume): %v", err)
		}
		for range sess.Messages {
		}
		return <-sess.Result
	}()
	if r2.Status != "failed" || !r2.ResumeRejected || r2.SessionID != "" {
		t.Fatalf("cold-fallback turn = %+v, want failed+ResumeRejected with no session id", r2)
	}
	data, err := os.ReadFile(requestsFile)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(got) != 1 || got[0] != "initialize" {
		t.Fatalf("requests = %v, want [initialize] only (no session/load, no prompt)", got)
	}
}
