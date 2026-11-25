package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"slices"
	"testing"
	"time"
)

// Resuming a session whose last turn was cut off (the daemon restarted mid
// task) can make Claude Code re-run that turn before the queued prompt. The
// re-run's result must not end the new turn, or the new prompt runs with no
// turn attached and every later reply lands one turn late (GH #11506).
func TestClaudeResumeIgnoresResultOfReRunInterruptedTurn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI fixture is POSIX-oriented")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))

	run := func(pool *WarmSessionPool, prompt, resumeID string) Result {
		t.Helper()
		cfg := Config{
			ExecutablePath: self,
			Logger:         discard,
			Env:            map[string]string{"CLAUDE_FAKE_MODE": "resume_interrupted_turn", "IS_SANDBOX": "1"},
		}
		if pool != nil {
			cfg.WarmPool = pool
		}
		b, err := New("claude", cfg)
		if err != nil {
			t.Fatalf("New(claude): %v", err)
		}
		sess, err := b.Execute(context.Background(), prompt, ExecOptions{
			Cwd:                  t.TempDir(),
			Model:                "claude-opus-5-5",
			ResumeSessionID:      resumeID,
			Timeout:              10 * time.Second,
			EnableTaskSupplement: true,
			HandshakeTimeout:     2 * time.Second,
		})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		for range sess.Messages {
		}
		return <-sess.Result
	}

	t.Run("warm pool", func(t *testing.T) {
		pool := NewWarmSessionPool(WarmSessionPoolConfig{IdleTTL: 30 * time.Second, MaxIdle: 4, Logger: discard})
		defer pool.Close()

		r1 := run(pool, "first message", "claude-resumed")
		if r1.Status != "completed" || r1.Output != "reply to first message" {
			t.Fatalf("resumed turn result = %+v, want the reply to its own prompt", r1)
		}
		if pool.Len() != 1 {
			t.Fatalf("pool.Len() = %d, want the resumed process parked", pool.Len())
		}
		r2 := run(pool, "second message", r1.SessionID)
		if r2.Status != "completed" || r2.Output != "reply to second message" {
			t.Fatalf("warm turn result = %+v, want the reply to its own prompt", r2)
		}
	})

	t.Run("one-shot", func(t *testing.T) {
		r := run(nil, "first message", "claude-resumed")
		if r.Status != "completed" || r.Output != "reply to first message" {
			t.Fatalf("resumed turn result = %+v, want the reply to its own prompt", r)
		}
	})
}

// runFakeClaudeResumeInterruptedTurn mirrors the frames Claude Code 2.1.325
// emits on --resume of a cut-off session: the re-run of the old turn, as its
// own command, before any queued prompt. Every hook waits for a reply the way
// the CLI does; an unanswered UserPromptSubmit drops the prompt.
func runFakeClaudeResumeInterruptedTurn() {
	frames := make(chan map[string]any, 16)
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Buffer(make([]byte, 1<<20), 1<<20)
		for scanner.Scan() {
			var frame map[string]any
			if json.Unmarshal(scanner.Bytes(), &frame) == nil {
				frames <- frame
			}
		}
		close(frames)
	}()
	out := json.NewEncoder(os.Stdout)
	emit := func(frame map[string]any) {
		frame["session_id"] = "claude-resumed"
		_ = out.Encode(frame)
	}
	var queued []map[string]any
	next := func(match func(map[string]any) bool, timeout time.Duration) map[string]any {
		deadline := time.After(timeout)
		for {
			select {
			case frame, ok := <-frames:
				if !ok {
					os.Exit(0)
				}
				if match(frame) {
					return frame
				}
				if frame["type"] == "user" {
					queued = append(queued, frame)
				}
			case <-deadline:
				return nil
			}
		}
	}
	hook := func(requestID, event string) bool {
		msg := claudeSupplementHook(event, "")
		msg.RequestID = requestID
		_ = out.Encode(msg)
		return next(func(f map[string]any) bool {
			resp, _ := f["response"].(map[string]any)
			return f["type"] == "control_response" && resp["request_id"] == requestID
		}, 3*time.Second) != nil
	}
	command := func(id, prompt string, n int) {
		emit(map[string]any{"type": "command_lifecycle", "command_uuid": id, "state": "started"})
		var reply string
		if hook(fmt.Sprintf("ups-%d", n), "UserPromptSubmit") {
			emit(map[string]any{"type": "system", "subtype": "init"})
			reply = "reply to " + prompt
			emit(map[string]any{"type": "assistant", "message": map[string]any{
				"id": fmt.Sprintf("msg-%d", n), "role": "assistant", "model": "claude-opus-5-5",
				"content": []any{map[string]any{"type": "text", "text": reply}},
			}})
			hook(fmt.Sprintf("stop-%d", n), "Stop")
		} else {
			reply = "UserPromptSubmit operation blocked by hook"
		}
		emit(map[string]any{"type": "result", "subtype": "success", "result": reply, "is_error": false})
		emit(map[string]any{"type": "command_lifecycle", "command_uuid": id, "state": "completed"})
	}

	first := next(func(map[string]any) bool { return true }, 5*time.Second)
	if first == nil {
		os.Exit(10)
	}
	if first["type"] == "control_request" {
		_ = out.Encode(map[string]any{"type": "control_response", "response": map[string]any{
			"subtype": "success", "request_id": first["request_id"], "response": map[string]any{},
		}})
	} else {
		queued = append(queued, first)
	}
	if slices.Contains(os.Args, "--resume") {
		command("re-run-of-cut-off-turn", "Continue from where you left off.", 0)
	}
	for n := 1; ; n++ {
		var prompt map[string]any
		if len(queued) > 0 {
			prompt, queued = queued[0], queued[1:]
		} else {
			prompt = next(func(f map[string]any) bool { return f["type"] == "user" }, time.Hour)
		}
		id, _ := prompt["uuid"].(string)
		message, _ := prompt["message"].(map[string]any)
		content, _ := message["content"].([]any)
		block, _ := content[0].(map[string]any)
		text, _ := block["text"].(string)
		if id != "" {
			emit(map[string]any{"type": "command_lifecycle", "command_uuid": id, "state": "queued"})
		}
		command(id, text, n)
	}
}
