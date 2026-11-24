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

// fakeOpencodeWarmScript is a minimal `opencode` fake: the `acp` subcommand
// speaks the slice of ACP the warm transport drives (initialize → session/new
// → optional set_model → session/prompt), the `export` subcommand dumps one
// assistant message per prompted turn. Each prompt reads the per-turn
// credentials from MULTICA_TASK_ENV_FILE and echoes them into the answer
// text, so tests can assert the overlay plumbing: the static environment must
// carry no token at all (static=EMPTY).
func fakeOpencodeWarmScript(spawnLog, turnFile string) string {
	return `#!/bin/sh
if [ "$1" != "acp" ]; then
  # export mode: export <sessionId> [--sanitize]
  n=0
  if [ -f "` + turnFile + `" ]; then n=$(cat "` + turnFile + `"); fi
  if [ "$n" -gt 0 ]; then
    in=$((n * 100)); out=$((n * 10))
    printf '{"messages":[{"role":"assistant","time":{"created":%s},"tokens":{"input":%d,"output":%d}}]}\n' "$((${NOW_MS}999))" "$in" "$out"
  else
    printf '{"messages":[]}\n'
  fi
  exit 0
fi
printf '%s\n' "$$" >> '` + spawnLog + `'
turn=0
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses-warm-1"}}\n' "$id"
      ;;
    *'"method":"session/load"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses-warm-1"}}\n' "$id"
      ;;
    *'"method":"session/set_model"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      turn=$((turn + 1))
      printf '%s' "$turn" > "` + turnFile + `"
      tok=$(sed -n 's/.*"MULTICA_TOKEN":"\([^"]*\)".*/\1/p' "$MULTICA_TASK_ENV_FILE")
      tid=$(sed -n 's/.*"MULTICA_TASK_ID":"\([^"]*\)".*/\1/p' "$MULTICA_TASK_ENV_FILE")
      static_tok="${MULTICA_TOKEN:-EMPTY}"
      static_task="${MULTICA_TASK_ID:-EMPTY}"
      if [ -n "$OC_PROMPT_FAIL" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"prompt failed"}}\n' "$id"
        continue
      fi
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses-warm-1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"turn=%s token=%s task=%s static=%s/%s"}}}}\n' "$turn" "$tok" "$tid" "$static_tok" "$static_task"
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":%d,"outputTokens":%d}}}\n' "$id" $((turn * 100)) $((turn * 10))
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id"
      ;;
  esac
done
`
}

func TestOpencodeWarmPoolReuseAndTokenIsolation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	spawnLog := filepath.Join(dir, "spawns.log")
	turnFile := filepath.Join(dir, "turns.txt")

	// NOW_MS anchors the export timestamps past the turn clock: the export's
	// per-turn messages must always fall inside the window the usage reader
	// filters on (created >= turnStart).
	os.Setenv("NOW_MS", "170000000000")
	t.Cleanup(func() { os.Unsetenv("NOW_MS") })

	fakeOpencode := filepath.Join(dir, "opencode")
	writeTestExecutable(t, fakeOpencode, []byte(fakeOpencodeWarmScript(spawnLog, turnFile)))

	pool := NewWarmSessionPool(WarmSessionPoolConfig{
		IdleTTL: 30 * time.Second,
		MaxIdle: 4,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() { pool.Close() })

	runTurn := func(taskID, token, resumeID, briefHash string) Result {
		t.Helper()
		b, err := New("opencode", Config{
			ExecutablePath: fakeOpencode,
			Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
			TaskID:         taskID,
			BuiltinRuntime: true,
			CLIVersion:     "1.19.4",
			WarmPool:       pool,
			Env: map[string]string{
				"MULTICA_TOKEN":        token,
				"MULTICA_TASK_ID":      taskID,
				"MULTICA_AGENT_ID":     "agent-1",
				"MULTICA_WORKSPACE_ID": "ws-1",
			},
		})
		if err != nil {
			t.Fatalf("New(opencode): %v", err)
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
	r1 := runTurn("task-1", "mat_turn_1", "", "brief-v1")
	if r1.Status != "completed" || r1.SessionID != "ses-warm-1" {
		t.Fatalf("Turn 1 result = %+v", r1)
	}
	if !strings.Contains(r1.Output, "turn=1 token=mat_turn_1 task=task-1 static=EMPTY/EMPTY") {
		t.Fatalf("Turn 1 output = %q (expected overlay delivery and stripped static env)", r1.Output)
	}
	if u := r1.Usage["unknown"]; u.InputTokens != 100 || u.OutputTokens != 10 {
		t.Fatalf("Turn 1 usage = %+v, want input=100 output=10", u)
	}
	if pool.Len() != 1 {
		t.Fatalf("pool.Len() after Turn 1 = %d, want 1", pool.Len())
	}

	// Verify the idle overlay holds NO live token while parked.
	pool.mu.Lock()
	parked := pool.entries[warmPoolKey{provider: "opencode", sessionID: "ses-warm-1"}].proc.(*opencodeWarmProcess)
	envFilePath := parked.taskEnvFile
	pool.mu.Unlock()
	idleData, err := os.ReadFile(envFilePath)
	if err != nil {
		t.Fatalf("read idle task-env.json: %v", err)
	}
	if strings.Contains(string(idleData), "mat_turn_1") {
		t.Fatalf("idle task-env.json leaked Turn 1 token: %s", string(idleData))
	}

	// Turn 2: warm hit on the SAME process with Turn 2's fresh overlay token.
	r2 := runTurn("task-2", "mat_turn_2", "ses-warm-1", "brief-v1")
	if r2.Status != "completed" || r2.SessionID != "ses-warm-1" {
		t.Fatalf("Turn 2 result = %+v", r2)
	}
	if !strings.Contains(r2.Output, "turn=2 token=mat_turn_2 task=task-2 static=EMPTY/EMPTY") {
		t.Fatalf("Turn 2 output = %q (expected same process turn=2 with fresh token)", r2.Output)
	}
	if u := r2.Usage["unknown"]; u.InputTokens != 200 || u.OutputTokens != 20 {
		t.Fatalf("Turn 2 usage = %+v, want input=200 output=20", u)
	}
	if n := spawnCount(); n != 1 {
		t.Fatalf("expected 1 opencode spawn across Turn 1 & 2, got %d", n)
	}

	// Turn 3: changing the brief hash must evict the parked process and spawn
	// a fresh one; the turn counter restarts.
	r3 := runTurn("task-3", "mat_turn_3", "ses-warm-1", "brief-v2")
	if r3.Status != "completed" {
		t.Fatalf("Turn 3 result = %+v", r3)
	}
	if !strings.Contains(r3.Output, "turn=1 token=mat_turn_3 task=task-3") {
		t.Fatalf("Turn 3 output = %q (expected a fresh process restarting at turn=1)", r3.Output)
	}
	if n := spawnCount(); n != 2 {
		t.Fatalf("expected 2 opencode spawns after the fingerprint change, got %d", n)
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
func TestOpencodeWarmFailedTurnNotParked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	spawnLog := filepath.Join(dir, "spawns.log")
	turnFile := filepath.Join(dir, "turns.txt")
	os.Setenv("NOW_MS", "170000000000")
	t.Cleanup(func() { os.Unsetenv("NOW_MS") })

	fakeOpencode := filepath.Join(dir, "opencode")
	writeTestExecutable(t, fakeOpencode, []byte(fakeOpencodeWarmScript(spawnLog, turnFile)))

	pool := NewWarmSessionPool(WarmSessionPoolConfig{
		IdleTTL: 30 * time.Second,
		MaxIdle: 4,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() { pool.Close() })

	b, err := New("opencode", Config{
		ExecutablePath: fakeOpencode,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		TaskID:         "task-1",
		BuiltinRuntime: true,
		CLIVersion:     "1.19.4",
		WarmPool:       pool,
		Env: map[string]string{
			"MULTICA_TOKEN":        "mat_turn_1",
			"MULTICA_TASK_ID":      "task-1",
			"MULTICA_AGENT_ID":     "agent-1",
			"MULTICA_WORKSPACE_ID": "ws-1",
			"OC_PROMPT_FAIL":       "1",
		},
	})
	if err != nil {
		t.Fatalf("New(opencode): %v", err)
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

// The warm gate holds the claim to the same eligibility contract the one-shot
// ACP transport enforces: no pool, or a claim that opencodeUsesACP rejects
// (custom profile, custom_args, forced run transport) must never spawn a
// pooled process.
func TestOpencodeWarmGateMatchesACPTransportGate(t *testing.T) {
	t.Parallel()
	base := func() Config {
		return Config{BuiltinRuntime: true, CLIVersion: "1.19.4"}
	}
	if useACP, reason := opencodeUsesACP(base(), ExecOptions{}); !useACP {
		t.Fatalf("baseline claim rejected by the ACP gate: %s", reason)
	}
	withPool := base()
	withPool.WarmPool = NewWarmSessionPool(WarmSessionPoolConfig{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	defer withPool.WarmPool.Close()
	if useACP, reason := opencodeUsesACP(withPool, ExecOptions{CustomArgs: []string{"--agent", "plan"}}); useACP {
		t.Fatalf("custom_args claim must stay on the run transport: %s", reason)
	}
}

// The fingerprint must not move between turns of the same conversation: the
// resume pointer selects the conversation, it is not part of how the process
// was built.
func TestOpencodeWarmFingerprintIgnoresResumePointer(t *testing.T) {
	t.Parallel()
	b := &opencodeBackend{cfg: Config{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Env:    map[string]string{"MULTICA_AGENT_ID": "agent-1"},
	}}
	envMap := map[string]string{"PATH": "/bin", "PWD": "/work"}
	fp1 := b.opencodeWarmFingerprint("opencode", ExecOptions{Cwd: "/work", Model: "m", ResumeSessionID: "ses-a", WarmPoolBriefHash: "b1"}, envMap)
	fp2 := b.opencodeWarmFingerprint("opencode", ExecOptions{Cwd: "/work", Model: "m", ResumeSessionID: "", WarmPoolBriefHash: "b1"}, envMap)
	if fp1 != fp2 {
		t.Fatal("resume pointer changed the fingerprint")
	}
	fp3 := b.opencodeWarmFingerprint("opencode", ExecOptions{Cwd: "/work", Model: "other", ResumeSessionID: "ses-a", WarmPoolBriefHash: "b1"}, envMap)
	if fp3 == fp1 {
		t.Fatal("model change did not change the fingerprint")
	}
	// A volatile credential rides the overlay, not the fingerprint.
	envTok := map[string]string{"PATH": "/bin", "PWD": "/work", "MULTICA_TOKEN": "secret"}
	fp4 := b.opencodeWarmFingerprint("opencode", ExecOptions{Cwd: "/work", Model: "m", WarmPoolBriefHash: "b1"}, envTok)
	if fp4 != fp1 {
		t.Fatal("volatile task env leaked into the fingerprint")
	}
}

// A turn cancelled while a synchronous setup write is wedged must return
// promptly: the stdin-close guard stays armed across the WHOLE setup (QA
// regression — an initialize-only guard let a blocked session/new write hold
// the turn until the CLI exited on its own). The fake answers initialize and
// then stops reading stdin entirely; a 1MiB MCP payload wedges the
// session/new write past the pipe buffer, and the run is cancelled 100ms in.
func TestOpencodeWarmCancelledSetupWithBlockedWriteReturns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	fakeOpencode := filepath.Join(dir, "opencode")
	script := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1}}\n' "$id"
      # The wedge: answer initialize, then stop reading stdin entirely
      # (replace the shell with a sleeper) so the next setup write wedges
      # once the pipe buffer fills.
      exec sleep 30
      ;;
  esac
done
`
	writeTestExecutable(t, fakeOpencode, []byte(script))

	// ~1MiB of local MCP servers: every entry survives the capability filter
	// (local entries carry no transport type), so the session/new write is
	// far larger than the pipe buffer.
	var b strings.Builder
	b.WriteString(`{"mcpServers":{"`)
	for i := 0; i < 9000; i++ {
		if i > 0 {
			b.WriteString(`,"`)
		}
		b.WriteString(fmt.Sprintf("srv%04d", i))
		b.WriteString(`":{"command":"node","args":["s.js","` + strings.Repeat("x", 90) + `"],"env":{"K":"` + strings.Repeat("v", 10) + `"}}`)
	}
	b.WriteString(`}}`)
	bigMcp := json.RawMessage(b.String())
	if len(bigMcp) < 900_000 {
		t.Fatalf("mcp payload too small to wedge the pipe: %d bytes", len(bigMcp))
	}

	pool := NewWarmSessionPool(WarmSessionPoolConfig{
		IdleTTL: 30 * time.Second,
		MaxIdle: 4,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() { pool.Close() })

	cfg := Config{
		ExecutablePath: fakeOpencode,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		TaskID:         "task-1",
		BuiltinRuntime: true,
		CLIVersion:     "2.2.0",
		WarmPool:       pool,
		Env: map[string]string{
			"MULTICA_TOKEN":        "mat_x",
			"MULTICA_TASK_ID":      "task-1",
			"MULTICA_AGENT_ID":     "agent-1",
			"MULTICA_WORKSPACE_ID": "ws-1",
		},
	}
	// Sanity: the V2 env build must not refuse the MCP config before the
	// turn starts (the MCP rides session/new, not the environment).
	if _, err := openCodeEnvironment(cfg, ExecOptions{Cwd: dir}); err != nil {
		t.Fatalf("openCodeEnvironment refused the V2 run: %v", err)
	}

	bk, err := New("opencode", cfg)
	if err != nil {
		t.Fatalf("New(opencode): %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	sess, err := bk.Execute(ctx, "hello", ExecOptions{
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
	if elapsed > 8*time.Second {
		t.Fatalf("cancelled setup took %s to return; the blocked session/new write was not released (want <8s, ~3s)", elapsed)
	}
	if res.Status != "timeout" && res.Status != "failed" && res.Status != "aborted" {
		t.Fatalf("cancelled setup result = %+v", res)
	}
}
