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

// fakeAntigravityWarmScript is a minimal `agy` fake: the `acp --add-dir <cwd>`
// invocation speaks the slice of ACP the warm transport drives (initialize →
// session/new|session/load → set_config_option → session/prompt). Each prompt
// reads the per-turn credentials from MULTICA_TASK_ENV_FILE and echoes them
// into the answer text. The prompt result's usage frame is per-turn (input
// 50 per turn, output 5 per turn) — NOT cumulative — so the tests can pin
// that each turn bills its own frame with no settle.
//
// Failure modes are armed through flag files inside ANTIGRAVITY_FLAGS_DIR
// (read on every request), because a parked process keeps executing the
// script it started with.
func fakeAntigravityWarmScript(spawnLog, turnFile, cwd string) string {
	return `#!/bin/sh
flags="${ANTIGRAVITY_FLAGS_DIR:-/nonexistent}"
if [ "$#" -eq 3 ] && [ "$1" = acp ] && [ "$2" = --add-dir ] && [ "$3" = "` + cwd + `" ]; then :; else
  printf "invalid agy argv: $*\n" >&2
  exit 11
fi
printf '%s\n' "$$" >> '` + spawnLog + `'
turn=0
while IFS= read -r line; do
  if [ -n "$ANTIGRAVITY_REQUESTS_FILE" ]; then
    case "$line" in
      *'"method":"initialize"'*) printf 'initialize\n' >> "$ANTIGRAVITY_REQUESTS_FILE";;
      *'"method":"session/load"'*) printf 'session/load\n' >> "$ANTIGRAVITY_REQUESTS_FILE";;
      *'"method":"session/new"'*) printf 'session/new\n' >> "$ANTIGRAVITY_REQUESTS_FILE";;
      *'"method":"session/set_config_option"'*) printf 'set_config_option\n' >> "$ANTIGRAVITY_REQUESTS_FILE";;
      *'"method":"session/prompt"'*) printf 'session/prompt\n' >> "$ANTIGRAVITY_REQUESTS_FILE";;
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
      if [ -f "$flags/no-model-option" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses-warm-1","models":{"currentModelId":"gemini-4-neon"}}}\n' "$id"
      else
        printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses-warm-1","models":{"currentModelId":"gemini-4-neon"},"configOptions":[{"id":"model","currentValue":"gemini-4-neon"}]}}\n' "$id"
      fi
      ;;
    *'"method":"session/set_config_option"'*)
      if [ -f "$flags/set-model-fail" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32603,"message":"model unavailable"}}\n' "$id"
      else
        printf '{"jsonrpc":"2.0","id":%s,"result":{"configOptions":[{"id":"model","currentValue":"gemini-4-neon[fast=true]"}]}}\n' "$id"
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
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses-warm-1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"turn=%s token=%s task=%s static=%s/%s"}}}}\n' "$turn" "$tok" "$tid" "$static_tok" "$static_task"
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":%d,"outputTokens":%d}}}\n' "$id" $((50 * turn)) $((5 * turn))
      ;;
    *)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not found"}}\n' "$id"
      ;;
  esac
done
`
}

func newAntigravityWarmTestEnv(t *testing.T) (dir, spawnLog string, pool *WarmSessionPool) {
	t.Helper()
	dir = t.TempDir()
	spawnLog = filepath.Join(dir, "spawns.log")
	flags := filepath.Join(dir, "flags")
	if err := os.MkdirAll(flags, 0o700); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(dir, "work")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	fakeCLI := filepath.Join(dir, "agy")
	writeTestExecutable(t, fakeCLI, []byte(fakeAntigravityWarmScript(spawnLog, filepath.Join(dir, "turns.txt"), cwd)))
	pool = newCursorWarmTestPool(t)
	return dir, spawnLog, pool
}

func newAntigravityWarmBackend(t *testing.T, pool *WarmSessionPool, extraEnv map[string]string, taskID, token string) Backend {
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
	b, err := New("antigravity", Config{
		ExecutablePath: fakeAntigravityPath(t),
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		TaskID:         taskID,
		BuiltinRuntime: true,
		CLIVersion:     "1.5.5",
		WarmPool:       pool,
		Env:            env,
	})
	if err != nil {
		t.Fatalf("New(antigravity): %v", err)
	}
	return b
}

var antigravityFakePath string

func fakeAntigravityPath(t *testing.T) string {
	t.Helper()
	if antigravityFakePath == "" {
		t.Fatal("antigravity fake CLI not staged for this test")
	}
	return antigravityFakePath
}

func runAntigravityWarmTurn(t *testing.T, b Backend, cwd, taskID, resumeID, briefHash, model string) Result {
	t.Helper()
	sess, err := b.Execute(context.Background(), "hello", ExecOptions{
		Cwd:               cwd,
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

func TestAntigravityWarmPoolReuseAndTokenIsolation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir, spawnLog, pool := newAntigravityWarmTestEnv(t)
	cwd := filepath.Join(dir, "work")
	flags := filepath.Join(dir, "flags")
	antigravityFakePath = filepath.Join(dir, "agy")
	t.Cleanup(func() { antigravityFakePath = "" })

	runTurn := func(taskID, token, resumeID, briefHash, model string) Result {
		b := newAntigravityWarmBackend(t, pool, map[string]string{"ANTIGRAVITY_FLAGS_DIR": flags}, taskID, token)
		return runAntigravityWarmTurn(t, b, cwd, taskID, resumeID, briefHash, model)
	}

	// Turn 1 with a model: cold spawn, the model rides the session's
	// set_config_option, the per-turn usage frame bills in full.
	r1 := runTurn("task-1", "mat_t1", "", "brief-v1", "gemini-4-neon[fast=true]")
	if r1.Status != "completed" || r1.SessionID != "ses-warm-1" {
		t.Fatalf("Turn 1 result = %+v", r1)
	}
	if !strings.Contains(r1.Output, "turn=1 token=mat_t1 task=task-1 static=EMPTY/EMPTY") {
		t.Fatalf("Turn 1 output = %q (expected overlay delivery and stripped static env)", r1.Output)
	}
	if u := r1.Usage["gemini-4-neon[fast=true]"]; u.InputTokens != 50 || u.OutputTokens != 5 {
		t.Fatalf("Turn 1 usage = %+v, want input=50 output=5 keyed by the selected model", u)
	}
	if pool.Len() != 1 {
		t.Fatalf("pool.Len() after Turn 1 = %d, want 1", pool.Len())
	}

	// The idle overlay holds no live token while parked.
	pool.mu.Lock()
	parked := pool.entries[warmPoolKey{provider: "antigravity", sessionID: "ses-warm-1"}].proc.(*antigravityWarmProcess)
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
	// The per-turn frame bills in full again (100/10) — no cumulative settle
	// against Turn 1's frame. The model travels with the claim: keeping it
	// identical is what lets the fingerprint match.
	r2 := runTurn("task-2", "mat_t2", "ses-warm-1", "brief-v1", "gemini-4-neon[fast=true]")
	if r2.Status != "completed" || r2.SessionID != "ses-warm-1" {
		t.Fatalf("Turn 2 result = %+v", r2)
	}
	if !strings.Contains(r2.Output, "turn=2 token=mat_t2 task=task-2 static=EMPTY/EMPTY") {
		t.Fatalf("Turn 2 output = %q (expected same process turn=2 with fresh token)", r2.Output)
	}
	if u := r2.Usage["gemini-4-neon[fast=true]"]; u.InputTokens != 100 || u.OutputTokens != 10 {
		t.Fatalf("Turn 2 usage = %+v, want this turn's own frame input=100 output=10", u)
	}
	if n := countFileLines(t, spawnLog); n != 1 {
		t.Fatalf("expected 1 agy spawn across Turn 1 & 2, got %d", n)
	}

	// Turn 3: a different brief hash evicts the parked process and spawns a
	// fresh one; the turn counter restarts at 1.
	r3 := runTurn("task-3", "mat_t3", "ses-warm-1", "brief-v2", "")
	if r3.Status != "completed" {
		t.Fatalf("Turn 3 result = %+v", r3)
	}
	if !strings.Contains(r3.Output, "turn=1 token=mat_t3 task=task-3") {
		t.Fatalf("Turn 3 output = %q (expected a fresh process restarting at turn=1)", r3.Output)
	}
	if n := countFileLines(t, spawnLog); n != 2 {
		t.Fatalf("expected 2 agy spawns after the fingerprint change, got %d", n)
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

func TestAntigravityWarmModelSelectionRunsOncePerProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir, spawnLog, pool := newAntigravityWarmTestEnv(t)
	cwd := filepath.Join(dir, "work")
	requests := filepath.Join(dir, "requests.log")
	antigravityFakePath = filepath.Join(dir, "agy")
	t.Cleanup(func() { antigravityFakePath = "" })

	runTurn := func(taskID, token, resumeID string) Result {
		b := newAntigravityWarmBackend(t, pool, map[string]string{
			"ANTIGRAVITY_FLAGS_DIR":     flagsPath(t, dir),
			"ANTIGRAVITY_REQUESTS_FILE": requests,
		}, taskID, token)
		return runAntigravityWarmTurn(t, b, cwd, taskID, resumeID, "brief-v1", "gemini-4-neon[fast=true]")
	}

	r1 := runTurn("task-1", "mat_t1", "")
	r2 := runTurn("task-2", "mat_t2", "ses-warm-1")
	if r1.Status != "completed" || r2.Status != "completed" {
		t.Fatalf("results = %+v / %+v, want both completed", r1, r2)
	}
	if n := countFileLines(t, spawnLog); n != 1 {
		t.Fatalf("expected 1 spawn, got %d", n)
	}
	selections := 0
	data, err := os.ReadFile(requests)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "set_config_option" {
			selections++
		}
	}
	if selections != 1 {
		t.Fatalf("model selection ran %d times across two turns on one process, want 1", selections)
	}
}

func TestAntigravityWarmFailedTurnNotParked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir, spawnLog, pool := newAntigravityWarmTestEnv(t)
	cwd := filepath.Join(dir, "work")
	flags := filepath.Join(dir, "flags")
	if err := os.WriteFile(filepath.Join(flags, "prompt-fail"), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	antigravityFakePath = filepath.Join(dir, "agy")
	t.Cleanup(func() { antigravityFakePath = "" })

	b := newAntigravityWarmBackend(t, pool, map[string]string{"ANTIGRAVITY_FLAGS_DIR": flags}, "task-1", "mat_t1")
	res := runAntigravityWarmTurn(t, b, cwd, "task-1", "", "brief-v1", "")
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

func TestAntigravityWarmSessionLoadRejectedFallsBackCold(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir, spawnLog, pool := newAntigravityWarmTestEnv(t)
	cwd := filepath.Join(dir, "work")
	flags := filepath.Join(dir, "flags")
	if err := os.WriteFile(filepath.Join(flags, "load-fail"), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	antigravityFakePath = filepath.Join(dir, "agy")
	t.Cleanup(func() { antigravityFakePath = "" })

	b := newAntigravityWarmBackend(t, pool, map[string]string{"ANTIGRAVITY_FLAGS_DIR": flags}, "task-1", "mat_t1")
	res := runAntigravityWarmTurn(t, b, cwd, "task-1", "ses-gone", "brief-v1", "")
	if res.Status != "failed" || !res.ResumeRejected || !strings.Contains(strings.ToLower(res.Error), "session not found") {
		t.Fatalf("rejected-resume result = %+v, want failed + ResumeRejected", res)
	}
	if pool.Len() != 0 {
		t.Fatalf("pool.Len() after a rejected resume = %d, want 0", pool.Len())
	}
	if n := countFileLines(t, spawnLog); n != 1 {
		t.Fatalf("expected 1 spawn, got %d", n)
	}
}

func TestAntigravityWarmModelOptionMissingFailsTheTurn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}

	dir, _, pool := newAntigravityWarmTestEnv(t)
	cwd := filepath.Join(dir, "work")
	flags := filepath.Join(dir, "flags")
	if err := os.WriteFile(filepath.Join(flags, "no-model-option"), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	antigravityFakePath = filepath.Join(dir, "agy")
	t.Cleanup(func() { antigravityFakePath = "" })

	b := newAntigravityWarmBackend(t, pool, map[string]string{"ANTIGRAVITY_FLAGS_DIR": flags}, "task-1", "mat_t1")
	res := runAntigravityWarmTurn(t, b, cwd, "task-1", "", "brief-v1", "gemini-4-neon[fast=true]")
	if res.Status != "failed" || !strings.Contains(res.Error, "did not advertise the requested model option") {
		t.Fatalf("result = %+v, want failed with the missing model option", res)
	}
	// A fresh session without a prompt has no transcript: the id is withheld
	// so the daemon's cold retry opens a new session instead of resuming a
	// half-configured one.
	if res.SessionID != "" {
		t.Fatalf("session id = %q, want it withheld after a half-configured setup", res.SessionID)
	}
	if pool.Len() != 0 {
		t.Fatalf("pool.Len() = %d, want 0", pool.Len())
	}
}

// TestAntigravityWarmEligible pins the warm gate: only built-in agy 1.5.5+
// claims without per-task machinery take the warm transport.
func TestAntigravityWarmEligible(t *testing.T) {
	base := Config{BuiltinRuntime: true, CLIVersion: "1.5.5"}
	if !antigravityWarmEligible(base, ExecOptions{Cwd: "/tmp/w"}) {
		t.Fatal("a built-in agy 1.5.5 plain claim is warm-eligible")
	}
	if !antigravityWarmEligible(base, ExecOptions{Cwd: "/tmp/w", ResumeSessionID: "ses-1", WarmPoolBriefHash: "h", Model: "gemini-4-neon", Timeout: time.Minute}) {
		t.Fatal("turn-scoped claim fields must not gate the warm path")
	}

	notEligible := []struct {
		name string
		cfg  Config
		opts ExecOptions
	}{
		{"custom profile", Config{BuiltinRuntime: false, CLIVersion: "1.5.5"}, ExecOptions{}},
		{"launch prefix", Config{BuiltinRuntime: true, CLIVersion: "1.5.5", LaunchPrefix: []string{"ccms", "start"}}, ExecOptions{}},
		{"older CLI", Config{BuiltinRuntime: true, CLIVersion: "1.5.4"}, ExecOptions{}},
		{"unknown version", Config{BuiltinRuntime: true, CLIVersion: ""}, ExecOptions{}},
		{"garbage version", Config{BuiltinRuntime: true, CLIVersion: "latest"}, ExecOptions{}},
		{"skill invocation", base, ExecOptions{SkillInvocation: &SkillInvocation{Prompt: "review this"}}},
		{"steering supplement", base, ExecOptions{EnableTaskSupplement: true}},
		{"custom args", base, ExecOptions{CustomArgs: []string{"--flag"}}},
		{"extra args", base, ExecOptions{ExtraArgs: []string{"--flag"}}},
	}
	for _, tc := range notEligible {
		if antigravityWarmEligible(tc.cfg, tc.opts) {
			t.Errorf("%s: antigravityWarmEligible = true, want false (the claim stays on the one-shot path)", tc.name)
		}
	}
}

func flagsPath(t *testing.T, dir string) string {
	t.Helper()
	return filepath.Join(dir, "flags")
}
