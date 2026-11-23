//go:build !windows

package agent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Background work whose result the turn reads is ordinary tool use: the run
// completes and the process is parked. The notice is read because another
// tool result follows it, and a launch the subagent made is the subagent's to
// collect. Event shapes follow Claude Code 2.1.280's stream-json output for a
// background Agent.
func TestClaudeWarmPoolParksAfterCollectedBackgroundWork(t *testing.T) {
	dir := t.TempDir()
	fakeClaude := filepath.Join(dir, "claude")
	script := `#!/bin/sh
while IFS= read -r line; do
  case "$line" in
    *'"type":"user"'*)
      printf '{"type":"system","subtype":"init","session_id":"claude-bg-ok"}\n'
      printf '{"type":"system","subtype":"task_started","task_id":"agent-1","tool_use_id":"toolu_1","is_backgrounded":true,"task_type":"local_agent","session_id":"claude-bg-ok"}\n'
      printf '{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_1","type":"tool_result","content":[{"type":"text","text":"Async agent launched successfully."}]}]},"tool_use_result":{"isAsync":true,"status":"async_launched","agentId":"agent-1"}}\n'
      printf '{"type":"system","subtype":"task_started","task_id":"sub-bash-1","tool_use_id":"toolu_s1","is_backgrounded":true,"owned_by_subagent":true,"task_type":"local_bash","session_id":"claude-bg-ok"}\n'
      printf '{"type":"user","parent_tool_use_id":"toolu_1","message":{"role":"user","content":[{"tool_use_id":"toolu_s1","type":"tool_result","content":"Command running in background with ID: sub-bash-1."}]},"tool_use_result":{"stdout":"","backgroundTaskId":"sub-bash-1"}}\n'
      printf '{"type":"system","subtype":"task_notification","task_id":"agent-1","tool_use_id":"toolu_1","status":"completed","session_id":"claude-bg-ok"}\n'
      printf '{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_2","type":"tool_result","content":"ok"}]}}\n'
      printf '{"type":"assistant","message":{"id":"msg-1","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"collected"}]}}\n'
      printf '{"type":"result","subtype":"success","session_id":"claude-bg-ok","result":"collected","is_error":false}\n'
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

	b, err := New("claude", Config{
		ExecutablePath: fakeClaude,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		TaskID:         "task-bg-ok",
		WarmPool:       pool,
		Env:            map[string]string{"MULTICA_TASK_ID": "task-bg-ok"},
	})
	if err != nil {
		t.Fatalf("New(claude): %v", err)
	}
	sess, err := b.Execute(context.Background(), "go", ExecOptions{Cwd: dir, Model: "claude-opus-5-5", Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range sess.Messages {
	}
	res := <-sess.Result
	if res.Status != "completed" || res.Output != "collected" {
		t.Fatalf("result = %+v, want completed with collected output", res)
	}
	if pool.Len() != 1 {
		t.Fatalf("pool.Len() = %d, want the clean process parked", pool.Len())
	}
}

// Claude Code ends its turn with a result while background work keeps running,
// and does not exit on stdin EOF until that work finishes. A pooled turn that
// ends this way must fail, must not park the process, and must kill the
// process group rather than leave the work running unowned.
func TestClaudeWarmPoolKillsBackgroundWorkLeftRunning(t *testing.T) {
	dir := t.TempDir()
	fakeClaude := filepath.Join(dir, "claude")
	pidFile := filepath.Join(dir, "background.pid")
	script := `#!/bin/sh
while IFS= read -r line; do
  case "$line" in
    *'"type":"user"'*)
      sleep 60 &
      echo $! > "` + pidFile + `"
      printf '{"type":"system","subtype":"init","session_id":"claude-bg-left"}\n'
      printf '{"type":"system","subtype":"task_started","task_id":"bash-1","tool_use_id":"toolu_1","is_backgrounded":true,"task_type":"local_bash","session_id":"claude-bg-left"}\n'
      printf '{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_1","type":"tool_result","content":"Command running in background with ID: bash-1."}]},"tool_use_result":{"stdout":"","stderr":"","interrupted":false,"backgroundTaskId":"bash-1"}}\n'
      printf '{"type":"assistant","message":{"id":"msg-1","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"Standing by"}]}}\n'
      printf '{"type":"result","subtype":"success","session_id":"claude-bg-left","result":"Standing by","is_error":false}\n'
      ;;
  esac
done
wait
`
	writeTestExecutable(t, fakeClaude, []byte(script))

	pool := NewWarmSessionPool(WarmSessionPoolConfig{
		IdleTTL: 30 * time.Second,
		MaxIdle: 4,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	defer pool.Close()

	b, err := New("claude", Config{
		ExecutablePath: fakeClaude,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		TaskID:         "task-bg-left",
		WarmPool:       pool,
		Env:            map[string]string{"MULTICA_TASK_ID": "task-bg-left"},
	})
	if err != nil {
		t.Fatalf("New(claude): %v", err)
	}
	start := time.Now()
	sess, err := b.Execute(context.Background(), "go", ExecOptions{Cwd: dir, Model: "claude-opus-5-5", Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range sess.Messages {
	}
	res := <-sess.Result
	elapsed := time.Since(start)

	if res.Status != "failed" || !strings.Contains(res.Error, "1 background task(s) whose results it never read") {
		t.Fatalf("result = %+v, want failed for background work left running", res)
	}
	if res.SessionID != "claude-bg-left" {
		t.Fatalf("session id = %q, want claude-bg-left", res.SessionID)
	}
	if pool.Len() != 0 {
		t.Fatalf("pool.Len() = %d, want the process with live background work closed", pool.Len())
	}
	if elapsed >= 15*time.Second {
		t.Fatalf("run took %v; the background work should be killed, not waited for", elapsed)
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read background pid: %v", err)
	}
	var pid int
	if _, err := fmt.Sscan(strings.TrimSpace(string(raw)), &pid); err != nil {
		t.Fatalf("parse background pid %q: %v", raw, err)
	}
	if processAlive(pid) {
		t.Fatalf("background process %d still running after the turn ended", pid)
	}
}

// Captured from Claude Code 2.1.280: the notice lands while the request for the
// turn's final message is already in flight, so the model replies "Waiting"
// without the result, and Claude delivers it in a turn of its own after the
// result. A pooled run ends at that first result, so the result was never read.
func TestClaudeWarmPoolFailsWhenNoticeArrivesAfterLastToolResult(t *testing.T) {
	dir := t.TempDir()
	fakeClaude := filepath.Join(dir, "claude")
	script := `#!/bin/sh
while IFS= read -r line; do
  case "$line" in
    *'"type":"user"'*)
      printf '{"type":"system","subtype":"init","session_id":"claude-bg-unread"}\n'
      printf '{"type":"system","subtype":"task_started","task_id":"agent-1","tool_use_id":"toolu_1","is_backgrounded":true,"task_type":"local_agent","session_id":"claude-bg-unread"}\n'
      printf '{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_1","type":"tool_result","content":[{"type":"text","text":"Async agent launched successfully."}]}]},"tool_use_result":{"isAsync":true,"status":"async_launched","agentId":"agent-1"}}\n'
      printf '{"type":"assistant","parent_tool_use_id":"toolu_1","message":{"id":"msg-sub","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"PONG"}]}}\n'
      printf '{"type":"system","subtype":"task_notification","task_id":"agent-1","tool_use_id":"toolu_1","status":"completed","session_id":"claude-bg-unread"}\n'
      printf '{"type":"assistant","message":{"id":"msg-1","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"Waiting for the agent to complete..."}]}}\n'
      printf '{"type":"result","subtype":"success","session_id":"claude-bg-unread","result":"Waiting for the agent to complete...","is_error":false}\n'
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

	b, err := New("claude", Config{
		ExecutablePath: fakeClaude,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		TaskID:         "task-bg-unread",
		WarmPool:       pool,
		Env:            map[string]string{"MULTICA_TASK_ID": "task-bg-unread"},
	})
	if err != nil {
		t.Fatalf("New(claude): %v", err)
	}
	sess, err := b.Execute(context.Background(), "go", ExecOptions{Cwd: dir, Model: "claude-opus-5-5", Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range sess.Messages {
	}
	res := <-sess.Result
	if res.Status != "failed" || !strings.Contains(res.Error, "1 background task(s) whose results it never read") {
		t.Fatalf("result = %+v, want failed for a notice the turn never read", res)
	}
	if pool.Len() != 0 {
		t.Fatalf("pool.Len() = %d, want the process closed so the pending delivery turn cannot run unowned", pool.Len())
	}
}

// Without the warm pool the process keeps streaming after its result: Claude
// delivers the late notice in a turn of its own, and the run lasts until that
// turn ends and the process exits.
func TestClaudeOneShotReadsBackgroundResultAfterResult(t *testing.T) {
	dir := t.TempDir()
	fakeClaude := filepath.Join(dir, "claude")
	script := `#!/bin/sh
IFS= read -r line
printf '{"type":"system","subtype":"init","session_id":"claude-bg-oneshot"}\n'
printf '{"type":"system","subtype":"task_started","task_id":"agent-1","tool_use_id":"toolu_1","is_backgrounded":true,"task_type":"local_agent","session_id":"claude-bg-oneshot"}\n'
printf '{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_1","type":"tool_result","content":[{"type":"text","text":"Async agent launched successfully."}]}]},"tool_use_result":{"isAsync":true,"status":"async_launched","agentId":"agent-1"}}\n'
printf '{"type":"assistant","message":{"id":"msg-1","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"Waiting"}]}}\n'
printf '{"type":"result","subtype":"success","session_id":"claude-bg-oneshot","result":"Waiting","is_error":false}\n'
printf '{"type":"system","subtype":"task_notification","task_id":"agent-1","tool_use_id":"toolu_1","status":"completed","session_id":"claude-bg-oneshot"}\n'
printf '{"type":"assistant","message":{"id":"msg-2","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"PONG"}]}}\n'
printf '{"type":"result","subtype":"success","session_id":"claude-bg-oneshot","result":"PONG","is_error":false}\n'
`
	writeTestExecutable(t, fakeClaude, []byte(script))

	b, err := New("claude", Config{
		ExecutablePath: fakeClaude,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		Env:            map[string]string{"MULTICA_TASK_ID": "task-bg-oneshot"},
	})
	if err != nil {
		t.Fatalf("New(claude): %v", err)
	}
	sess, err := b.Execute(context.Background(), "go", ExecOptions{Cwd: dir, Model: "claude-opus-5-5", Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range sess.Messages {
	}
	res := <-sess.Result
	if res.Status != "completed" || res.Output != "PONG" {
		t.Fatalf("result = %+v, want completed with the delivered result", res)
	}
}
