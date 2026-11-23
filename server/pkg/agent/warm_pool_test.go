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
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskEnvFileOverlay(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "task-env.json")

	// Write Turn 1 volatile env plus a forbidden non-volatile key that must be ignored.
	err := WriteTaskEnvFile(envFile, map[string]string{
		"MULTICA_TOKEN":                "mat_turn_1",
		"MULTICA_TASK_ID":              "task-1",
		"MULTICA_TASK_CONFIG_ROOT":     "/tmp/cfg-1",
		"MULTICA_TASK_SLOT":            "2",
		"MULTICA_QUICK_CREATE_TASK_ID": "task-1",
		"MULTICA_SERVER_URL":           "https://evil.example.com",
		"PATH":                         "/evil/bin",
	})
	if err != nil {
		t.Fatalf("WriteTaskEnvFile turn 1: %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(envFile)
		if err != nil {
			t.Fatalf("stat envFile: %v", err)
		}
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Fatalf("envFile mode = %o, want 0600", mode)
		}
	}

	t.Setenv(TaskEnvFileEnv, envFile)
	t.Setenv("MULTICA_SERVER_URL", "https://real.example.com")
	t.Setenv("MULTICA_TOKEN", "mat_stale_parent")
	t.Setenv("MULTICA_AUTOPILOT_RUN_ID", "stale-autopilot")

	if err := ApplyTaskEnvFileOverlay(); err != nil {
		t.Fatalf("ApplyTaskEnvFileOverlay turn 1: %v", err)
	}
	if got := os.Getenv("MULTICA_TOKEN"); got != "mat_turn_1" {
		t.Fatalf("MULTICA_TOKEN = %q, want mat_turn_1", got)
	}
	if got := os.Getenv("MULTICA_TASK_ID"); got != "task-1" {
		t.Fatalf("MULTICA_TASK_ID = %q, want task-1", got)
	}
	if got := os.Getenv("MULTICA_AUTOPILOT_RUN_ID"); got != "" {
		t.Fatalf("stale MULTICA_AUTOPILOT_RUN_ID was not cleared: %q", got)
	}
	if got := os.Getenv("MULTICA_SERVER_URL"); got != "https://real.example.com" {
		t.Fatalf("non-volatile MULTICA_SERVER_URL overwritten: %q", got)
	}

	// Turn 2 updates token & task ID and drops MULTICA_QUICK_CREATE_TASK_ID.
	if err := WriteTaskEnvFile(envFile, map[string]string{
		"MULTICA_TOKEN":   "mat_turn_2",
		"MULTICA_TASK_ID": "task-2",
	}); err != nil {
		t.Fatalf("WriteTaskEnvFile turn 2: %v", err)
	}
	if err := ApplyTaskEnvFileOverlay(); err != nil {
		t.Fatalf("ApplyTaskEnvFileOverlay turn 2: %v", err)
	}
	if got := os.Getenv("MULTICA_TOKEN"); got != "mat_turn_2" {
		t.Fatalf("MULTICA_TOKEN = %q, want mat_turn_2", got)
	}
	if got := os.Getenv("MULTICA_QUICK_CREATE_TASK_ID"); got != "" {
		t.Fatalf("MULTICA_QUICK_CREATE_TASK_ID leaked from turn 1: %q", got)
	}

	// ClearTaskEnvFile must cause all volatile keys to become unset.
	if err := ClearTaskEnvFile(envFile); err != nil {
		t.Fatalf("ClearTaskEnvFile: %v", err)
	}
	if err := ApplyTaskEnvFileOverlay(); err != nil {
		t.Fatalf("ApplyTaskEnvFileOverlay after clear: %v", err)
	}
	if got := os.Getenv("MULTICA_TOKEN"); got != "" {
		t.Fatalf("MULTICA_TOKEN still set after clear: %q", got)
	}
	if got := os.Getenv("MULTICA_TASK_ID"); got != "" {
		t.Fatalf("MULTICA_TASK_ID still set after clear: %q", got)
	}
}

type fakeWarmProc struct {
	fp     string
	alive  atomic.Bool
	closed atomic.Int32
}

func newFakeWarmProc(fp string) *fakeWarmProc {
	p := &fakeWarmProc{fp: fp}
	p.alive.Store(true)
	return p
}

func (p *fakeWarmProc) Fingerprint() string { return p.fp }
func (p *fakeWarmProc) Alive() bool         { return p.alive.Load() }
func (p *fakeWarmProc) Close() bool {
	p.alive.Store(false)
	p.closed.Add(1)
	return true
}

func TestWarmSessionPoolLifecycle(t *testing.T) {
	t.Parallel()

	pool := NewWarmSessionPool(WarmSessionPoolConfig{
		IdleTTL: 200 * time.Millisecond,
		MaxIdle: 2,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	defer pool.Close()

	// Park & hit.
	p1 := newFakeWarmProc("fp-1")
	if !pool.Park("codex", "s1", p1) {
		t.Fatal("Park(s1) returned false")
	}
	if pool.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", pool.Len())
	}
	got := pool.Acquire("codex", "s1", "fp-1")
	if got != p1 {
		t.Fatalf("Acquire(s1) = %v, want %v", got, p1)
	}
	if pool.Len() != 0 {
		t.Fatalf("Len() after acquire = %d, want 0", pool.Len())
	}

	// Fingerprint mismatch evicts and closes synchronously.
	pool.Park("codex", "s1", p1)
	if got := pool.Acquire("codex", "s1", "fp-changed"); got != nil {
		t.Fatalf("Acquire with changed fingerprint = %v, want nil", got)
	}
	if p1.closed.Load() != 1 {
		t.Fatalf("stale process close count = %d, want 1", p1.closed.Load())
	}

	// LRU eviction when MaxIdle == 2.
	pa := newFakeWarmProc("fp-a")
	pb := newFakeWarmProc("fp-b")
	pc := newFakeWarmProc("fp-c")
	pool.Park("codex", "sa", pa)
	time.Sleep(5 * time.Millisecond)
	pool.Park("codex", "sb", pb)
	time.Sleep(5 * time.Millisecond)
	pool.Park("codex", "sc", pc)
	if pa.closed.Load() != 1 {
		t.Fatalf("oldest idle entry pa was not evicted: closed=%d", pa.closed.Load())
	}
	if pool.Len() != 2 {
		t.Fatalf("Len() after LRU eviction = %d, want 2", pool.Len())
	}

	// Idle TTL expiry.
	time.Sleep(300 * time.Millisecond)
	if pool.Len() != 0 {
		t.Fatalf("Len() after idle TTL = %d, want 0", pool.Len())
	}
	if pb.closed.Load() != 1 || pc.closed.Load() != 1 {
		t.Fatalf("idle entries not closed on TTL expiry: pb=%d pc=%d", pb.closed.Load(), pc.closed.Load())
	}
}

func TestNewWarmSessionPoolFromEnvDisableSwitch(t *testing.T) {
	t.Setenv(warmPoolEnvEnabled, "0")
	if pool := NewWarmSessionPoolFromEnv(slog.New(slog.NewTextHandler(io.Discard, nil))); pool != nil {
		pool.Close()
		t.Fatal("expected nil pool when MULTICA_WARM_POOL=0")
	}

	t.Setenv(warmPoolEnvEnabled, "1")
	t.Setenv(warmPoolEnvIdleTTL, "3m")
	t.Setenv(warmPoolEnvMaxIdle, "4")
	pool := NewWarmSessionPoolFromEnv(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if pool == nil {
		t.Fatal("expected non-nil pool when MULTICA_WARM_POOL=1")
	}
	defer pool.Close()
	if pool.idleTTL != 3*time.Minute || pool.maxIdle != 4 {
		t.Fatalf("pool config = (%v, %d), want (3m, 4)", pool.idleTTL, pool.maxIdle)
	}
}

func TestCodexWarmPoolReuseAndTokenIsolation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	spawnLog := filepath.Join(dir, "spawns.log")
	codexHome := filepath.Join(dir, "codex-home")
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte("model = \"gpt-6.1-sol\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Fake codex app-server that serves multiple turns on the same process,
	// reads MULTICA_TOKEN from MULTICA_TASK_ENV_FILE on each turn, and reports
	// cumulative thread token usage.
	fakeCodex := filepath.Join(dir, "codex")
	script := `#!/bin/sh
printf '%s\n' "$$" >> '` + spawnLog + `'
turn_num=0
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"userAgent":"fake-codex"}}\n' "$id"
      ;;
    *'"method":"thread/start"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"thread":{"id":"thr-warm-1"}}}\n' "$id"
      ;;
    *'"method":"thread/resume"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"thread":{"id":"thr-warm-1"}}}\n' "$id"
      ;;
    *'"method":"turn/start"'*)
      turn_num=$((turn_num + 1))
      printf '{"jsonrpc":"2.0","id":%s,"result":{"turn":{"id":"turn-%s"}}}\n' "$id" "$turn_num"
      printf '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-warm-1","turn":{"id":"turn-%s"}}}\n' "$turn_num"
      tok=$(sed -n 's/.*"MULTICA_TOKEN":"\([^"]*\)".*/\1/p' "$MULTICA_TASK_ENV_FILE")
      tid=$(sed -n 's/.*"MULTICA_TASK_ID":"\([^"]*\)".*/\1/p' "$MULTICA_TASK_ENV_FILE")
      static_tok="${MULTICA_TOKEN:-EMPTY}"
      total_in=$((turn_num * 100))
      total_out=$((turn_num * 10))
      printf '{"jsonrpc":"2.0","method":"thread/tokenUsage/updated","params":{"threadId":"thr-warm-1","turnId":"turn-%s","tokenUsage":{"total":{"inputTokens":%d,"outputTokens":%d},"last":{"inputTokens":100,"outputTokens":10}}}}\n' "$turn_num" "$total_in" "$total_out"
      printf '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr-warm-1","turnId":"turn-%s","item":{"type":"agentMessage","id":"msg-%s","phase":"final_answer","text":"pid=$$ turn=%s token=%s task=%s static=%s"}}}\n' "$turn_num" "$turn_num" "$turn_num" "$tok" "$tid" "$static_tok"
      printf '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr-warm-1","turn":{"id":"turn-%s","status":"completed"}}}\n' "$turn_num"
      ;;
  esac
done
`
	writeTestExecutable(t, fakeCodex, []byte(script))

	pool := NewWarmSessionPool(WarmSessionPoolConfig{
		IdleTTL: 30 * time.Second,
		MaxIdle: 4,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	defer pool.Close()

	runTurn := func(taskID, token, resumeID, model, briefHash string) Result {
		t.Helper()
		b, err := New("codex", Config{
			ExecutablePath: fakeCodex,
			Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
			TaskID:         taskID,
			WarmPool:       pool,
			Env: map[string]string{
				"CODEX_HOME":           codexHome,
				"MULTICA_TOKEN":        token,
				"MULTICA_TASK_ID":      taskID,
				"MULTICA_AGENT_ID":     "agent-1",
				"MULTICA_WORKSPACE_ID": "ws-1",
			},
		})
		if err != nil {
			t.Fatalf("New(codex): %v", err)
		}
		sess, err := b.Execute(context.Background(), "hello", ExecOptions{
			Cwd:               dir,
			Model:             model,
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

	// Turn 1: cold spawn, gets parked.
	r1 := runTurn("task-1", "mat_turn_1", "", "gpt-6.1-sol", "brief-v1")
	if r1.Status != "completed" || r1.SessionID != "thr-warm-1" {
		t.Fatalf("Turn 1 result = %+v", r1)
	}
	if !strings.Contains(r1.Output, "turn=1 token=mat_turn_1 task=task-1 static=EMPTY") {
		t.Fatalf("Turn 1 output = %q", r1.Output)
	}
	if pool.Len() != 1 {
		t.Fatalf("pool.Len() after Turn 1 = %d, want 1", pool.Len())
	}

	// Verify idle task-env file holds NO live token while parked.
	pool.mu.Lock()
	parked := pool.entries[warmPoolKey{provider: "codex", sessionID: "thr-warm-1"}].proc.(*codexProcess)
	envFilePath := parked.taskEnvFile
	pool.mu.Unlock()
	idleData, err := os.ReadFile(envFilePath)
	if err != nil {
		t.Fatalf("read idle task-env.json: %v", err)
	}
	if strings.Contains(string(idleData), "mat_turn_1") {
		t.Fatalf("idle task-env.json leaked Turn 1 token: %s", string(idleData))
	}

	// Turn 2: warm hit on SAME PID with Turn 2's fresh token and delta usage!
	r2 := runTurn("task-2", "mat_turn_2", "thr-warm-1", "gpt-6.1-sol", "brief-v1")
	if r2.Status != "completed" || r2.SessionID != "thr-warm-1" {
		t.Fatalf("Turn 2 result = %+v", r2)
	}
	if !strings.Contains(r2.Output, "turn=2 token=mat_turn_2 task=task-2 static=EMPTY") {
		t.Fatalf("Turn 2 output = %q (expected same process turn=2 with fresh token)", r2.Output)
	}
	if u := r2.Usage["gpt-6.1-sol"]; u.InputTokens != 100 || u.OutputTokens != 10 {
		t.Fatalf("Turn 2 delta usage = %+v, want input=100 output=10 (no double counting)", u)
	}

	spawns, err := os.ReadFile(spawnLog)
	if err != nil {
		t.Fatal(err)
	}
	if lines := splitNonEmptyLines(string(spawns)); len(lines) != 1 {
		t.Fatalf("expected 1 codex spawn across Turn 1 & Turn 2, got %d (%v)", len(lines), lines)
	}

	// Turn 3: changing briefHash must evict PID 1 and spawn a fresh process.
	r3 := runTurn("task-3", "mat_turn_3", "thr-warm-1", "gpt-6.1-sol", "brief-v2")
	if r3.Status != "completed" {
		t.Fatalf("Turn 3 result = %+v", r3)
	}
	if !strings.Contains(r3.Output, "turn=1 token=mat_turn_3 task=task-3") {
		t.Fatalf("Turn 3 output = %q (expected fresh spawn turn=1)", r3.Output)
	}
	spawns, _ = os.ReadFile(spawnLog)
	if lines := splitNonEmptyLines(string(spawns)); len(lines) != 2 {
		t.Fatalf("expected 2 codex spawns after brief change, got %d (%v)", len(lines), lines)
	}

	// Pool.Close() cleans up the parked process and its temp dir.
	if !pool.Close() {
		t.Fatal("pool.Close() returned false")
	}
	if pool.Len() != 0 {
		t.Fatalf("pool.Len() after Close = %d, want 0", pool.Len())
	}
}

func TestClaudeWarmPoolReuseStreamingAndTokenIsolation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	spawnLog := filepath.Join(dir, "claude-spawns.log")
	fakeClaude := filepath.Join(dir, "claude")
	script := `#!/bin/sh
printf '%s\n' "$$" >> '` + spawnLog + `'
turn=0
while IFS= read -r line; do
  case "$line" in
    *'"type":"user"'*)
      turn=$((turn + 1))
      tok=$(sed -n 's/.*"MULTICA_TOKEN":"\([^"]*\)".*/\1/p' "$MULTICA_TASK_ENV_FILE")
      tid=$(sed -n 's/.*"MULTICA_TASK_ID":"\([^"]*\)".*/\1/p' "$MULTICA_TASK_ENV_FILE")
      static_tok="${MULTICA_TOKEN:-EMPTY}"
      if [ "$turn" = "1" ]; then
        printf '{"type":"system","subtype":"init","session_id":"claude-warm-1"}\n'
      fi
      # Emit partial stream_event deltas followed by the final assistant snapshot
      printf '{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg-%s"}}}\n' "$turn"
      printf '{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"plan-%s"}}}\n' "$turn"
      printf '{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"hello "}}}\n'
      printf '{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"%s:%s:%s"}}}\n' "$tok" "$tid" "$static_tok"
      printf '{"type":"stream_event","event":{"type":"message_stop"}}\n'
      printf '{"type":"assistant","message":{"id":"msg-%s","role":"assistant","model":"claude-opus-5-5","content":[{"type":"thinking","thinking":"plan-%s"},{"type":"text","text":"hello %s:%s:%s"}]}}\n' "$turn" "$turn" "$tok" "$tid" "$static_tok"
      cum_in=$((turn * 50))
      cum_out=$((turn * 15))
      printf '{"type":"result","subtype":"success","session_id":"claude-warm-1","result":"hello %s:%s:%s","is_error":false,"modelUsage":{"claude-opus-5-5":{"inputTokens":%d,"outputTokens":%d}}}\n' "$tok" "$tid" "$static_tok" "$cum_in" "$cum_out"
      ;;
  esac
done
`
	writeTestExecutable(t, fakeClaude, []byte(script))

	pool := NewWarmSessionPool(WarmSessionPoolConfig{
		IdleTTL: 30 * time.Second,
		MaxIdle: 4,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	defer pool.Close()

	runTurn := func(taskID, token, resumeID, model string) (Result, []Message) {
		t.Helper()
		b, err := New("claude", Config{
			ExecutablePath: fakeClaude,
			Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
			TaskID:         taskID,
			WarmPool:       pool,
			Env: map[string]string{
				"MULTICA_TOKEN":        token,
				"MULTICA_TASK_ID":      taskID,
				"MULTICA_AGENT_ID":     "agent-c1",
				"MULTICA_WORKSPACE_ID": "ws-1",
			},
		})
		if err != nil {
			t.Fatalf("New(claude): %v", err)
		}
		sess, err := b.Execute(context.Background(), "say hi", ExecOptions{
			Cwd:             dir,
			Model:           model,
			ResumeSessionID: resumeID,
			Timeout:         10 * time.Second,
		})
		if err != nil {
			t.Fatalf("Execute(%s): %v", taskID, err)
		}
		var msgs []Message
		for m := range sess.Messages {
			msgs = append(msgs, m)
		}
		return <-sess.Result, msgs
	}

	// Turn 1: cold spawn + stream_event deltas + zero duplication against assistant snapshot.
	r1, msgs1 := runTurn("task-c1", "mat_c1", "", "claude-opus-5-5")
	if r1.Status != "completed" || r1.SessionID != "claude-warm-1" {
		t.Fatalf("Turn 1 result = %+v", r1)
	}
	if r1.Output != "hello mat_c1:task-c1:EMPTY" {
		t.Fatalf("Turn 1 output = %q", r1.Output)
	}
	var streamedText1, streamedThink1 strings.Builder
	for _, m := range msgs1 {
		if m.Type == MessageText {
			streamedText1.WriteString(m.Content)
		}
		if m.Type == MessageThinking {
			streamedThink1.WriteString(m.Content)
		}
	}
	if streamedText1.String() != "hello mat_c1:task-c1:EMPTY" {
		t.Fatalf("Turn 1 streamed text = %q (possible duplication or drop)", streamedText1.String())
	}
	if streamedThink1.String() != "plan-1" {
		t.Fatalf("Turn 1 streamed thinking = %q, want plan-1", streamedThink1.String())
	}
	if pool.Len() != 1 {
		t.Fatalf("pool.Len() after Turn 1 = %d, want 1", pool.Len())
	}

	// Turn 2: warm hit on SAME PID, fresh token, cumulative modelUsage delta subtracted!
	r2, msgs2 := runTurn("task-c2", "mat_c2", "claude-warm-1", "claude-opus-5-5")
	if r2.Status != "completed" || r2.SessionID != "claude-warm-1" {
		t.Fatalf("Turn 2 result = %+v", r2)
	}
	if r2.Output != "hello mat_c2:task-c2:EMPTY" {
		t.Fatalf("Turn 2 output = %q", r2.Output)
	}
	if u := r2.Usage["claude-opus-5-5"]; u.InputTokens != 50 || u.OutputTokens != 15 {
		t.Fatalf("Turn 2 usage = %+v, want delta input=50 output=15", u)
	}
	var streamedText2 strings.Builder
	for _, m := range msgs2 {
		if m.Type == MessageText {
			streamedText2.WriteString(m.Content)
		}
	}
	if streamedText2.String() != "hello mat_c2:task-c2:EMPTY" {
		t.Fatalf("Turn 2 streamed text = %q", streamedText2.String())
	}

	spawns, err := os.ReadFile(spawnLog)
	if err != nil {
		t.Fatal(err)
	}
	if lines := splitNonEmptyLines(string(spawns)); len(lines) != 1 {
		t.Fatalf("expected 1 claude spawn across Turn 1 & Turn 2, got %d (%v)", len(lines), lines)
	}

	// Turn 3: model change evicts warm process and spawns fresh.
	r3, _ := runTurn("task-c3", "mat_c3", "claude-warm-1", "claude-sonnet-5")
	if r3.Status != "completed" {
		t.Fatalf("Turn 3 result = %+v", r3)
	}
	spawns, _ = os.ReadFile(spawnLog)
	if lines := splitNonEmptyLines(string(spawns)); len(lines) != 2 {
		t.Fatalf("expected 2 claude spawns after model change, got %d (%v)", len(lines), lines)
	}

	pool.Close()
	if pool.Len() != 0 {
		t.Fatalf("pool.Len() after Close = %d, want 0", pool.Len())
	}
}

func TestClaudeWarmPoolErrorResultClosesStdinImmediately(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	fakeClaude := filepath.Join(dir, "claude")
	// Emits an error result and keeps reading stdin until EOF, matching a live
	// stream-json Claude process. Without closing stdin immediately, this would
	// stall for the full 10s claudeErrorResultGrace() before returning.
	script := `#!/bin/sh
while IFS= read -r line; do
  case "$line" in
    *'"type":"user"'*)
      printf '{"type":"system","subtype":"init","session_id":"claude-err-1"}\n'
      case "$line" in
        *prompt_too_long*)
          printf '{"type":"result","subtype":"success","session_id":"claude-err-1","result":"context full","is_error":false,"terminal_reason":"prompt_too_long"}\n'
          ;;
        *)
          printf '{"type":"result","subtype":"error","session_id":"claude-err-1","result":"rate limit exceeded","is_error":true}\n'
          ;;
      esac
      ;;
  esac
done
`
	writeTestExecutable(t, fakeClaude, []byte(script))

	pool := NewWarmSessionPool(WarmSessionPoolConfig{
		IdleTTL: 30 * time.Second,
		MaxIdle: 4,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	defer pool.Close()

	for _, tc := range []struct {
		name      string
		prompt    string
		wantError string
	}{
		{name: "is_error", prompt: "trigger api error", wantError: "rate limit exceeded"},
		{name: "terminal_reason_prompt_too_long", prompt: "trigger prompt_too_long", wantError: "prompt_too_long"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := New("claude", Config{
				ExecutablePath: fakeClaude,
				Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
				TaskID:         "task-err-" + tc.name,
				WarmPool:       pool,
				Env: map[string]string{
					"MULTICA_TOKEN":        "mat_err",
					"MULTICA_TASK_ID":      "task-err-" + tc.name,
					"MULTICA_AGENT_ID":     "agent-err",
					"MULTICA_WORKSPACE_ID": "ws-err",
				},
			})
			if err != nil {
				t.Fatalf("New(claude): %v", err)
			}

			start := time.Now()
			sess, err := b.Execute(context.Background(), tc.prompt, ExecOptions{
				Cwd:     dir,
				Model:   "claude-opus-5-5",
				Timeout: 15 * time.Second,
			})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			for range sess.Messages {
			}
			res := <-sess.Result
			elapsed := time.Since(start)

			if res.Status != "failed" {
				t.Fatalf("status = %q, want failed (res=%+v)", res.Status, res)
			}
			if !strings.Contains(res.Error, tc.wantError) {
				t.Fatalf("error = %q, want substring %q", res.Error, tc.wantError)
			}
			if elapsed >= 2*time.Second {
				t.Fatalf("error result took %v (expected immediate stdin EOF exit, not 10s grace wait)", elapsed)
			}
			if pool.Len() != 0 {
				t.Fatalf("errored session was parked: pool.Len() = %d", pool.Len())
			}
		})
	}
}

// A pooled turn ends at its result event while the process keeps writing.
// Events that land during the turn's teardown must not reach the finished
// turn: its message channel is about to close and its control-write
// WaitGroup is being waited on.
func TestClaudeWarmPoolIgnoresEventsAfterResult(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir := t.TempDir()
	fakeClaude := filepath.Join(dir, "claude")
	script := `#!/bin/sh
turn=0
while IFS= read -r line; do
  case "$line" in
    *'"type":"user"'*)
      turn=$((turn + 1))
      printf '{"type":"system","subtype":"init","session_id":"claude-trailing-1"}\n'
      printf '{"type":"assistant","message":{"id":"msg-%s","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"turn %s"}]}}\n' "$turn" "$turn"
      printf '{"type":"result","subtype":"success","session_id":"claude-trailing-1","result":"turn %s","is_error":false}\n' "$turn"
      i=0
      while [ "$i" -lt 40 ]; do
        printf '{"type":"system","subtype":"status","session_id":"claude-trailing-1"}\n'
        printf '{"type":"log","log":{"level":"info","message":"after result %s"}}\n' "$i"
        printf '{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"late"}}}\n'
        printf '{"type":"assistant","message":{"id":"late-%s-%s","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"late"}]}}\n' "$turn" "$i"
        printf '{"type":"control_request","request_id":"late-%s-%s","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{}}}\n' "$turn" "$i"
        i=$((i + 1))
      done
      ;;
  esac
done
`
	writeTestExecutable(t, fakeClaude, []byte(script))

	pool := NewWarmSessionPool(WarmSessionPoolConfig{
		IdleTTL: 30 * time.Second,
		MaxIdle: 4,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	defer pool.Close()

	resume := ""
	for i := 1; i <= 40; i++ {
		taskID := fmt.Sprintf("task-trailing-%d", i)
		b, err := New("claude", Config{
			ExecutablePath: fakeClaude,
			Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
			TaskID:         taskID,
			WarmPool:       pool,
			Env:            map[string]string{"MULTICA_TASK_ID": taskID},
		})
		if err != nil {
			t.Fatalf("New(claude): %v", err)
		}
		sess, err := b.Execute(context.Background(), "go", ExecOptions{
			Cwd:             dir,
			Model:           "claude-opus-5-5",
			ResumeSessionID: resume,
			Timeout:         10 * time.Second,
		})
		if err != nil {
			t.Fatalf("Execute(%s): %v", taskID, err)
		}
		for range sess.Messages {
		}
		res := <-sess.Result
		if res.Status != "completed" || res.SessionID != "claude-trailing-1" {
			t.Fatalf("%s result = %+v", taskID, res)
		}
		resume = res.SessionID
	}
}

func TestClaudeStreamCoalescerLeadingCoalescingAndReconciliation(t *testing.T) {
	t.Parallel()

	ch := make(chan Message, 32)
	clock := time.Unix(1_700_000_000, 0)
	c := newClaudeStreamCoalescer(ch, func() time.Time { return clock })

	sendDelta := func(after time.Duration, deltaType, key, val string) {
		clock = clock.Add(after)
		raw, _ := json.Marshal(map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{
				"type": deltaType,
				key:    val,
			},
		})
		c.handleEvent(raw)
	}

	// Leading text delta flushes immediately; second waits inside 50ms window;
	// third after 50ms flushes coalesced chunk.
	sendDelta(0, "text_delta", "text", "Hello ")
	sendDelta(10*time.Millisecond, "text_delta", "text", "warm ")
	sendDelta(claudeStreamFlushInterval, "text_delta", "text", "world")
	sendDelta(5*time.Millisecond, "text_delta", "text", "!")

	// Assistant snapshot arrives with an extra trailing suffix " :)" — flush + reconcile.
	b := &claudeBackend{}
	assistantMsg := claudeSDKMessage{
		Type:    "assistant",
		Message: json.RawMessage(`{"id":"m1","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"Hello warm world! :)"}]}`),
	}
	turn := b.handleAssistantWithStream(assistantMsg, ch, map[string]TokenUsage{}, map[string]struct{}{}, c)
	if turn.text != "Hello warm world! :)" {
		t.Fatalf("turn.text = %q, want full text", turn.text)
	}

	close(ch)
	var chunks []string
	for m := range ch {
		if m.Type == MessageText {
			chunks = append(chunks, m.Content)
		}
	}
	want := []string{"Hello ", "warm world", "!", " :)"}
	if !slices.Equal(chunks, want) {
		t.Fatalf("streamed chunks = %q, want %q", chunks, want)
	}
}

func TestClaudeStreamCoalescerBackwardCompatWithoutStreamEvents(t *testing.T) {
	t.Parallel()

	ch := make(chan Message, 8)
	c := newClaudeStreamCoalescer(ch, nil)
	b := &claudeBackend{}

	assistantMsg := claudeSDKMessage{
		Type:    "assistant",
		Message: json.RawMessage(`{"id":"m1","role":"assistant","model":"claude-opus-5-5","content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"final answer"}]}`),
	}
	turn := b.handleAssistantWithStream(assistantMsg, ch, map[string]TokenUsage{}, map[string]struct{}{}, c)
	if turn.text != "final answer" {
		t.Fatalf("turn.text = %q, want final answer", turn.text)
	}
	close(ch)
	var got []Message
	for m := range ch {
		got = append(got, m)
	}
	if len(got) != 2 || got[0].Type != MessageThinking || got[0].Content != "hmm" || got[1].Type != MessageText || got[1].Content != "final answer" {
		t.Fatalf("unexpected legacy assistant messages: %+v", got)
	}
}

func TestCachedDiscoveryStaleWhileRevalidate(t *testing.T) {
	// Serial test: mutates package-global cache state and clock.
	origTTL := modelCacheTTL
	origStaleTTL := modelCacheStaleTTL
	origNow := modelCacheNow
	t.Cleanup(func() {
		modelCacheMu.Lock()
		modelCacheTTL = origTTL
		modelCacheStaleTTL = origStaleTTL
		modelCacheNow = origNow
		delete(modelCache, "test-swr")
		delete(modelCacheInflight, "test-swr")
		modelCacheMu.Unlock()
	})

	var clockMu sync.Mutex
	now := time.Unix(1_700_000_000, 0)
	modelCacheTTL = 10 * time.Minute
	modelCacheStaleTTL = 20 * time.Minute
	modelCacheNow = func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return now
	}
	advance := func(d time.Duration) {
		clockMu.Lock()
		now = now.Add(d)
		clockMu.Unlock()
	}

	var calls atomic.Int32
	refreshReady := make(chan struct{})
	refreshDone := make(chan struct{})

	fn := func(ctx context.Context) (Catalog, error) {
		n := calls.Add(1)
		if n == 2 {
			<-refreshReady
			defer close(refreshDone)
			return Catalog{Models: []Model{{ID: "m-v2", Label: "V2"}}}, nil
		}
		return Catalog{Models: []Model{{ID: "m-v1", Label: "V1"}}}, nil
	}

	// 1. Cold miss populates cache synchronously.
	c1, err := cachedDiscoveryWithContext(context.Background(), "test-swr", fn)
	if err != nil || len(c1.Models) != 1 || c1.Models[0].ID != "m-v1" {
		t.Fatalf("cold miss = (%+v, %v)", c1, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}

	// 2. Fresh hit (at +5m) serves immediately without calling fn.
	advance(5 * time.Minute)
	c2, err := cachedDiscoveryWithContext(context.Background(), "test-swr", fn)
	if err != nil || c2.Models[0].ID != "m-v1" || calls.Load() != 1 {
		t.Fatalf("fresh hit = (%+v, %v, calls=%d)", c2, err, calls.Load())
	}

	// 3. Stale hit (at +12m) returns m-v1 immediately and triggers ONE background refresh.
	advance(7 * time.Minute)
	c3, err := cachedDiscoveryWithContext(context.Background(), "test-swr", fn)
	if err != nil || c3.Models[0].ID != "m-v1" {
		t.Fatalf("stale hit = (%+v, %v)", c3, err)
	}
	// Concurrent stale call while refresh is in flight must not spawn a second refresh.
	c4, err := cachedDiscoveryWithContext(context.Background(), "test-swr", fn)
	if err != nil || c4.Models[0].ID != "m-v1" {
		t.Fatalf("second stale hit = (%+v, %v)", c4, err)
	}
	close(refreshReady)
	select {
	case <-refreshDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for background refresh")
	}
	if calls.Load() != 2 {
		t.Fatalf("expected single-flight background refresh (calls=2), got %d", calls.Load())
	}

	// 4. Subsequent hit observes refreshed catalog m-v2.
	c5, err := cachedDiscoveryWithContext(context.Background(), "test-swr", fn)
	if err != nil || c5.Models[0].ID != "m-v2" {
		t.Fatalf("post-refresh hit = (%+v, %v)", c5, err)
	}
}
