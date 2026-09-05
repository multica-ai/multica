package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

// TestMain intercepts when the test binary is re-executed as a fake
// child process by the agent backend. The fake's behavior is selected via
// CLAUDE_FAKE_MODE; absent that env var, this is a normal `go test` run.
//
// Dispatching here rather than from a Test function is what lets a fake be
// invoked with a real agent CLI's argv: TestMain runs before the testing
// package parses flags, so arguments like `run --format json` never reach it.
func TestMain(m *testing.M) {
	if os.Getenv(codeartsModelHelperEnv) == "1" {
		runFakeCodeArtsModelHelper()
		os.Exit(0)
	}
	if os.Getenv(opencodeStdinHelperEnv) == "1" {
		runFakeOpencodeStdinHelper()
		os.Exit(0)
	}
	switch mode := os.Getenv("CLAUDE_FAKE_MODE"); mode {
	case "":
		os.Exit(m.Run())
	case "startup_stdout_burst":
		runFakeClaudeStartupStdoutBurst()
		os.Exit(0)
	case "control_request":
		runFakeClaudeControlRequest()
		os.Exit(0)
	case "background_control_request":
		runFakeClaudeBackgroundControlRequest()
		os.Exit(0)
	case "async_launched_tool_result":
		runFakeClaudeAsyncLaunchedToolResult()
		os.Exit(0)
	case "scheduled_wakeup_exit", "scheduled_wakeup_eof", "scheduled_wakeup_no_result":
		runFakeClaudeScheduledWakeup(mode)
		os.Exit(0)
	case "native_loop", "native_loop_exit", "native_loop_incomplete", "native_loop_init_incomplete", "native_loop_cancel", "native_cron":
		runFakeClaudeNativeLoop(mode)
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "unknown CLAUDE_FAKE_MODE: %q\n", mode)
		os.Exit(2)
	}
}

// runFakeClaudeStartupStdoutBurst writes ~256 KiB to stdout BEFORE
// reading any byte from stdin, then reads the first stdin frame and emits a
// stream-json result. Reproduces the stdio deadlock: if the daemon writes
// the prompt to stdin before a stdout reader is running, the child blocks
// writing stdout and the daemon blocks writing stdin — neither side can
// progress until the per-task context times out and the child is killed.
func runFakeClaudeStartupStdoutBurst() {
	line := strings.Repeat("x", 1020)
	bw := bufio.NewWriter(os.Stdout)
	for i := 0; i < 256; i++ {
		if _, err := fmt.Fprintf(bw, `{"type":"log","log":{"level":"info","message":"%s"}}`+"\n", line); err != nil {
			os.Exit(11)
		}
	}
	if err := bw.Flush(); err != nil {
		os.Exit(12)
	}
	if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		os.Exit(13)
	}
	fmt.Println(`{"type":"result","subtype":"success","is_error":false,"session_id":"sess-deadlock","result":"done"}`)
}

func runFakeClaudeControlRequest() {
	reader := bufio.NewReader(os.Stdin)
	if _, err := reader.ReadString('\n'); err != nil {
		fmt.Fprintf(os.Stderr, "read prompt: %v\n", err)
		os.Exit(21)
	}
	fmt.Println(`{"type":"system","session_id":"sess-control"}`)
	fmt.Println(`{"type":"control_request","request_id":"req-42","request":{"subtype":"tool_use","tool_name":"Bash","input":{"command":"pwd"}}}`)

	line, err := reader.ReadString('\n')
	if err != nil {
		fmt.Fprintf(os.Stderr, "read control response: %v\n", err)
		os.Exit(22)
	}
	var resp struct {
		Type     string `json:"type"`
		Response struct {
			RequestID string `json:"request_id"`
		} `json:"response"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &resp); err != nil {
		fmt.Fprintf(os.Stderr, "decode control response: %v\n", err)
		os.Exit(23)
	}
	if resp.Type != "control_response" || resp.Response.RequestID != "req-42" {
		fmt.Fprintf(os.Stderr, "unexpected control response: %s\n", line)
		os.Exit(24)
	}
	fmt.Println(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"approved"}]}}`)
	fmt.Println(`{"type":"result","subtype":"success","is_error":false,"session_id":"sess-control","result":"done after control"}`)
}

func runFakeClaudeBackgroundControlRequest() {
	reader := bufio.NewReader(os.Stdin)
	if _, err := reader.ReadString('\n'); err != nil {
		fmt.Fprintf(os.Stderr, "read prompt: %v\n", err)
		os.Exit(31)
	}
	fmt.Println(`{"type":"system","session_id":"sess-background-control"}`)
	fmt.Println(`{"type":"control_request","request_id":"req-bg","request":{"subtype":"tool_use","tool_name":"Bash","input":{"command":"sleep 60","run_in_background":true}}}`)

	line, err := reader.ReadString('\n')
	if err != nil {
		fmt.Fprintf(os.Stderr, "read control response: %v\n", err)
		os.Exit(32)
	}
	var resp struct {
		Type     string `json:"type"`
		Response struct {
			RequestID string `json:"request_id"`
			Response  struct {
				UpdatedInput map[string]any `json:"updatedInput"`
			} `json:"response"`
		} `json:"response"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &resp); err != nil {
		fmt.Fprintf(os.Stderr, "decode control response: %v\n", err)
		os.Exit(33)
	}
	if resp.Type != "control_response" || resp.Response.RequestID != "req-bg" {
		fmt.Fprintf(os.Stderr, "unexpected control response: %s\n", line)
		os.Exit(34)
	}
	if runInBackground, ok := resp.Response.Response.UpdatedInput["run_in_background"].(bool); !ok || runInBackground {
		fmt.Fprintf(os.Stderr, "expected foreground updatedInput, got: %s\n", line)
		os.Exit(35)
	}

	fmt.Println(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-bg","content":"foreground completed"}]}}`)
	fmt.Println(`{"type":"result","subtype":"success","is_error":false,"session_id":"sess-background-control","result":"done after foreground rewrite"}`)
}

func runFakeClaudeAsyncLaunchedToolResult() {
	reader := bufio.NewReader(os.Stdin)
	if _, err := reader.ReadString('\n'); err != nil {
		fmt.Fprintf(os.Stderr, "read prompt: %v\n", err)
		os.Exit(41)
	}
	fmt.Println(`{"type":"system","session_id":"sess-async-launched"}`)
	fmt.Println(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-async","content":{"status":"async_launched","message":"background task launched"}}]}}`)
	fmt.Println(`{"type":"result","subtype":"success","is_error":false,"session_id":"sess-async-launched","result":"parent turn completed early"}`)
}

// runFakeClaudeScheduledWakeup reproduces a headless turn that arms a wakeup
// and then exits. A successful tool result does not promise another CLI turn.
func runFakeClaudeScheduledWakeup(mode string) {
	reader := bufio.NewReader(os.Stdin)
	if _, err := reader.ReadString('\n'); err != nil {
		fmt.Fprintf(os.Stderr, "read prompt: %v\n", err)
		os.Exit(51)
	}

	fmt.Fprintln(os.Stderr, `[claude-code:unrecognized_model] {"model":"k3-256k","query_source":"sdk"}`)
	fmt.Println(`{"type":"system","session_id":"sess-wakeup"}`)
	fmt.Println(`{"type":"assistant","message":{"role":"assistant","model":"k3-256k","content":[{"type":"tool_use","id":"call-arm","name":"ScheduleWakeup","input":{"delaySeconds":1400,"prompt":"repeat"}}]}}`)
	fmt.Println(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-arm","content":"Next wakeup scheduled"}]}}`)
	fmt.Println(`{"type":"assistant","message":{"role":"assistant","model":"k3-256k","content":[{"type":"text","text":"assistant fallback"}],"usage":{"input_tokens":4,"output_tokens":1}}}`)
	if mode == "scheduled_wakeup_no_result" {
		return
	}
	fmt.Println(`{"type":"result","subtype":"success","is_error":false,"session_id":"sess-wakeup","result":"current turn finished","modelUsage":{"k3-256k":{"inputTokens":10,"outputTokens":2,"cacheReadInputTokens":3,"cacheCreationInputTokens":4}}}`)

	if mode == "scheduled_wakeup_eof" {
		// Also enforce the turn boundary for a child that waits for stdin EOF.
		// Merely retaining sawResult while keeping stdin open would still hang.
		if _, err := reader.ReadByte(); err != io.EOF {
			fmt.Fprintf(os.Stderr, "expected stdin EOF after result, got %v\n", err)
			os.Exit(52)
		}
	}
}

// TestClaudeExecuteDoesNotDeadlockOnStartupStdoutBurst verifies that the
// claude backend drains stdout concurrently with writing the prompt to
// stdin. The buggy path serialises the two: writeClaudeInput runs before
// the reader goroutine starts, so a child that emits startup output
// before its first stdin read deadlocks both directions. Field evidence
// in the daemon log shows tasks failing exactly at the 2 h per-task
// timeout with "write |1: The pipe has been ended.", produced when
// runCtx fires, the child is killed, and the blocked stdin Write
// finally unwinds.
//
// The fake child writes 256 KiB to stdout then 128 KiB of prompt is
// pushed at stdin — both well past any plausible OS pipe buffer
// (Linux ~64 KiB, Windows 4-64 KiB) — so a regression here hangs until
// the test deadline rather than passing slowly.
func TestClaudeExecuteDoesNotDeadlockOnStartupStdoutBurst(t *testing.T) {
	t.Parallel()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	backend, err := New("claude", Config{
		ExecutablePath: self,
		Env:            map[string]string{"CLAUDE_FAKE_MODE": "startup_stdout_burst", "IS_SANDBOX": "1"},
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new claude backend: %v", err)
	}

	// 128 KiB prompt forces writeClaudeInput to block until the child
	// drains stdin, which the buggy code cannot reach because the reader
	// goroutine hasn't started yet.
	prompt := strings.Repeat("p", 128*1024)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, prompt, ExecOptions{Timeout: 20 * time.Second})
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "completed" {
			t.Fatalf("expected status=completed, got %q (error=%q)", result.Status, result.Error)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timeout waiting for result — claude backend is deadlocked on writeClaudeInput because stdout is not being drained concurrently")
	}
}

func TestClaudeExecuteRespondsToControlRequest(t *testing.T) {
	t.Parallel()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	backend, err := New("claude", Config{
		ExecutablePath: self,
		Env:            map[string]string{"CLAUDE_FAKE_MODE": "control_request", "IS_SANDBOX": "1"},
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new claude backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "run a command", ExecOptions{Timeout: 8 * time.Second})
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "completed" {
			t.Fatalf("expected status=completed, got %q (error=%q)", result.Status, result.Error)
		}
		if result.Output != "done after control" {
			t.Fatalf("expected result output from fake claude, got %q", result.Output)
		}
		if result.SessionID != "sess-control" {
			t.Fatalf("expected session id sess-control, got %q", result.SessionID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for result — claude backend did not answer control_request")
	}
}

func TestClaudeExecuteForcesBackgroundControlRequestForeground(t *testing.T) {
	t.Parallel()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	backend, err := New("claude", Config{
		ExecutablePath: self,
		Env:            map[string]string{"CLAUDE_FAKE_MODE": "background_control_request", "IS_SANDBOX": "1"},
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new claude backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "run a background command", ExecOptions{Timeout: 8 * time.Second})
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "completed" {
			t.Fatalf("expected status=completed, got %q (error=%q)", result.Status, result.Error)
		}
		if result.Output != "done after foreground rewrite" {
			t.Fatalf("expected foreground rewrite result, got %q", result.Output)
		}
		if result.SessionID != "sess-background-control" {
			t.Fatalf("expected session id sess-background-control, got %q", result.SessionID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for result — claude backend did not foreground background control_request")
	}
}

func TestClaudeExecuteFailsLoudlyOnAsyncLaunchedToolResult(t *testing.T) {
	t.Parallel()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	backend, err := New("claude", Config{
		ExecutablePath: self,
		Env:            map[string]string{"CLAUDE_FAKE_MODE": "async_launched_tool_result", "IS_SANDBOX": "1"},
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new claude backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "launch async work", ExecOptions{Timeout: 8 * time.Second})
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "failed" {
			t.Fatalf("expected status=failed, got %q (error=%q)", result.Status, result.Error)
		}
		if !strings.Contains(result.Error, "async background task") {
			t.Fatalf("expected async background task error, got %q", result.Error)
		}
		if result.SessionID != "sess-async-launched" {
			t.Fatalf("expected session id sess-async-launched, got %q", result.SessionID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for result — claude backend did not fail async_launched tool result")
	}
}

func TestClaudeExecuteScheduledWakeupPreservesTerminalResult(t *testing.T) {
	t.Parallel()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	for _, mode := range []string{"scheduled_wakeup_exit", "scheduled_wakeup_eof", "scheduled_wakeup_no_result"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			backend, err := New("claude", Config{
				ExecutablePath: self,
				Env:            map[string]string{"CLAUDE_FAKE_MODE": mode, "IS_SANDBOX": "1"},
				Logger:         slog.Default(),
			})
			if err != nil {
				t.Fatalf("new claude backend: %v", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			session, err := backend.Execute(ctx, "/loop continue", ExecOptions{Model: "k3-256k", Timeout: 3 * time.Second})
			if err != nil {
				t.Fatalf("execute returned error: %v", err)
			}
			for range session.Messages {
			}
			result, ok := <-session.Result
			if !ok {
				t.Fatal("result channel closed without a value")
			}
			if result.SessionID != "sess-wakeup" {
				t.Fatalf("expected resumable session id sess-wakeup, got %q", result.SessionID)
			}
			if mode == "scheduled_wakeup_no_result" {
				if result.Status != "failed" || !strings.Contains(result.Error, "stream ended without terminal result") {
					t.Fatalf("expected missing terminal result failure, got status=%q error=%q", result.Status, result.Error)
				}
				if !strings.Contains(result.Error, "[claude-code:unrecognized_model]") {
					t.Fatalf("expected stderr preserved for a real failure, got %q", result.Error)
				}
				return
			}
			if result.Status != "completed" || result.Error != "" {
				t.Fatalf("expected successful completion despite stderr warning, got status=%q error=%q", result.Status, result.Error)
			}
			if result.Output != "current turn finished" {
				t.Fatalf("expected terminal result text, got %q", result.Output)
			}
			wantUsage := TokenUsage{InputTokens: 10, OutputTokens: 2, CacheReadTokens: 3, CacheWriteTokens: 4}
			if got := result.Usage["k3-256k"]; got != wantUsage {
				t.Fatalf("result usage = %+v, want %+v", got, wantUsage)
			}
		})
	}
}
