package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"testing"
	"time"
)

func TestClaudeProcessRepliesToHookWhenIdle(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	proc := &claudeProcess{
		inputWriter: &claudeInputWriter{w: &buf},
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		procDone:    make(chan struct{}),
	}

	hook := claudeSupplementHook("UserPromptSubmit", "")
	hook.RequestID = "idle-hook-1"
	proc.dispatchMessage(hook)

	resp := waitClaudeControlResponse(t, &buf, time.Second)
	if resp["type"] != "control_response" {
		t.Fatalf("type = %v", resp["type"])
	}
	inner := resp["response"].(map[string]any)
	if inner["subtype"] != "success" || inner["request_id"] != "idle-hook-1" {
		t.Fatalf("response = %#v", inner)
	}
	payload, _ := inner["response"].(map[string]any)
	if len(payload) != 0 {
		t.Fatalf("idle hook reply must be empty, got %#v", payload)
	}
	if _, ok := payload["behavior"]; ok {
		t.Fatal("idle hook reply must not use the permission payload")
	}

	before := buf.Len()
	tool, _ := json.Marshal(map[string]any{"subtype": "can_use_tool", "tool_name": "Bash", "input": map[string]any{}})
	proc.dispatchMessage(claudeSDKMessage{Type: "control_request", RequestID: "late-tool", Request: tool})
	proc.dispatchMessage(claudeSDKMessage{Type: "log"})
	time.Sleep(50 * time.Millisecond)
	if buf.Len() != before {
		t.Fatalf("idle process answered a non-hook event: %s", buf.Bytes()[before:])
	}
}

func TestClaudeHandleControlRequestRepliesToHookCallback(t *testing.T) {
	t.Parallel()

	b := &claudeBackend{cfg: Config{Logger: slog.Default()}}
	var written bytes.Buffer
	hook := claudeSupplementHook("Stop", "")
	hook.RequestID = "hook-99"
	b.handleControlRequest(hook, &written)

	resp := waitClaudeControlResponse(t, &written, time.Second)
	inner := resp["response"].(map[string]any)
	if inner["subtype"] != "success" || inner["request_id"] != "hook-99" {
		t.Fatalf("response = %#v", inner)
	}
	payload, _ := inner["response"].(map[string]any)
	if payload["behavior"] == "allow" {
		t.Fatalf("hook_callback must not get a permission-shaped reply: %#v", payload)
	}
}

// After a pooled turn's result the process stays up with no active turn.
// The CLI still emits hook_callback (Stop / late UserPromptSubmit). If the
// host stays silent, the next prompt is blocked and then dropped (GH #11506).
func TestClaudeWarmPoolAnswersHookAfterResult(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI fixture is POSIX-oriented")
	}

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pool := NewWarmSessionPool(WarmSessionPoolConfig{
		IdleTTL: 30 * time.Second,
		MaxIdle: 4,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	defer pool.Close()

	runTurn := func(taskID, resumeID string) Result {
		t.Helper()
		b, err := New("claude", Config{
			ExecutablePath: self,
			Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
			TaskID:         taskID,
			WarmPool:       pool,
			Env: map[string]string{
				"CLAUDE_FAKE_MODE": "idle_hook_after_result",
				"IS_SANDBOX":       "1",
				"MULTICA_TASK_ID":  taskID,
			},
		})
		if err != nil {
			t.Fatalf("New(claude): %v", err)
		}
		sess, err := b.Execute(context.Background(), "go", ExecOptions{
			Cwd:                  dir,
			Model:                "claude-opus-5-5",
			ResumeSessionID:      resumeID,
			Timeout:              8 * time.Second,
			EnableTaskSupplement: true,
			HandshakeTimeout:     2 * time.Second,
		})
		if err != nil {
			t.Fatalf("Execute(%s): %v", taskID, err)
		}
		for range sess.Messages {
		}
		return <-sess.Result
	}

	r1 := runTurn("task-idle-hook-1", "")
	if r1.Status != "completed" || r1.SessionID != "claude-idle-hook" || r1.Output != "turn 1" {
		t.Fatalf("turn 1 result = %+v", r1)
	}
	if pool.Len() != 1 {
		t.Fatalf("pool.Len() after turn 1 = %d, want 1", pool.Len())
	}

	r2 := runTurn("task-idle-hook-2", r1.SessionID)
	if r2.Status != "completed" || r2.SessionID != "claude-idle-hook" || r2.Output != "turn 2" {
		t.Fatalf("turn 2 result = %+v (idle hook was probably unanswered)", r2)
	}
	if pool.Len() != 1 {
		t.Fatalf("pool.Len() after turn 2 = %d, want 1", pool.Len())
	}
}

func waitClaudeControlResponse(t *testing.T, buf *bytes.Buffer, timeout time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		line := bytes.TrimSpace(buf.Bytes())
		if len(line) > 0 {
			var resp map[string]any
			if err := json.Unmarshal(line, &resp); err != nil {
				t.Fatalf("unmarshal control response %q: %v", line, err)
			}
			return resp
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for control_response")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// runFakeClaudeIdleHookAfterResult answers one initialize handshake, then for
// each user prompt emits a result and a hook_callback and waits for the hook
// reply before reading the next prompt. This is the warm-pool idle window.
func runFakeClaudeIdleHookAfterResult() {
	reader := bufio.NewReader(os.Stdin)
	read := func() map[string]any {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(10)
		}
		var frame map[string]any
		if json.Unmarshal(line, &frame) != nil {
			os.Exit(11)
		}
		return frame
	}
	first := read()
	if typ, _ := first["type"].(string); typ == "control_request" {
		requestID, _ := first["request_id"].(string)
		_ = writeClaudeFrame(os.Stdout, map[string]any{
			"type": "control_response",
			"response": map[string]any{
				"subtype":    "success",
				"request_id": requestID,
				"response":   map[string]any{},
			},
		})
		first = read()
	}
	if typ, _ := first["type"].(string); typ != "user" {
		os.Exit(14)
	}
	for turn := 1; ; turn++ {
		fmt.Printf("{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"claude-idle-hook\"}\n")
		fmt.Printf("{\"type\":\"result\",\"subtype\":\"success\",\"session_id\":\"claude-idle-hook\",\"result\":\"turn %d\",\"is_error\":false}\n", turn)
		hook := claudeSupplementHook("UserPromptSubmit", "")
		hook.RequestID = fmt.Sprintf("idle-hook-%d", turn)
		if err := json.NewEncoder(os.Stdout).Encode(hook); err != nil {
			os.Exit(23)
		}
		frame := read()
		if typ, _ := frame["type"].(string); typ != "control_response" {
			os.Exit(15)
		}
		response, _ := frame["response"].(map[string]any)
		if response["request_id"] != hook.RequestID {
			os.Exit(26)
		}
		next := read()
		if typ, _ := next["type"].(string); typ != "user" {
			os.Exit(16)
		}
	}
}
