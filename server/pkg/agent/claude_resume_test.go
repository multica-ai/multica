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
	"strings"
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

	runVariant := func(variant string, pool *WarmSessionPool, prompt, resumeID string) Result {
		t.Helper()
		cfg := Config{
			ExecutablePath: self,
			Logger:         discard,
			Env: map[string]string{
				"CLAUDE_FAKE_MODE":           "resume_interrupted_turn",
				"CLAUDE_FAKE_RESUME_VARIANT": variant,
				"IS_SANDBOX":                 "1",
			},
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
	run := func(pool *WarmSessionPool, prompt, resumeID string) Result {
		t.Helper()
		return runVariant("", pool, prompt, resumeID)
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

	// A prompt that arrives while the re-run is still going can be folded
	// into it: the prompt's command starts and completes inside the re-run,
	// and one result answers both.
	t.Run("prompt folded into the re-run", func(t *testing.T) {
		r := runVariant("folded", nil, "first message", "claude-resumed")
		if r.Status != "completed" || r.Output != "reply to first message" {
			t.Fatalf("resumed turn result = %+v, want the folded result", r)
		}
	})

	// Once the turn's own command started, its result counts even when a
	// command folded into it ends some other way first.
	t.Run("own command fails after a folded command is cancelled", func(t *testing.T) {
		start := time.Now()
		r := runVariant("cancelled_fold", nil, "first message", "")
		if r.Status != "failed" || !strings.Contains(r.Error, "API Error") {
			t.Fatalf("turn result = %+v, want the own command's error", r)
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Fatalf("turn took %s; the error result was ignored until timeout", elapsed)
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
	variant := os.Getenv("CLAUDE_FAKE_RESUME_VARIANT")
	lifecycle := func(id, state string) {
		emit(map[string]any{"type": "command_lifecycle", "command_uuid": id, "state": state})
	}
	promptOf := func(frame map[string]any) (string, string) {
		id, _ := frame["uuid"].(string)
		message, _ := frame["message"].(map[string]any)
		content, _ := message["content"].([]any)
		block, _ := content[0].(map[string]any)
		text, _ := block["text"].(string)
		return id, text
	}
	command := func(id, prompt string, n int) {
		if variant == "cancelled_fold" {
			lifecycle(id, "started")
			if !hook(fmt.Sprintf("ups-%d", n), "UserPromptSubmit") {
				os.Exit(31)
			}
			lifecycle("folded-cron", "started")
			lifecycle("folded-cron", "cancelled")
			emit(map[string]any{"type": "result", "subtype": "error_during_execution", "result": "API Error: 500", "is_error": true})
			lifecycle(id, "completed")
			return
		}
		lifecycle(id, "started")
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
	if slices.Contains(os.Args, "--resume") && variant == "folded" {
		lifecycle("re-run-of-cut-off-turn", "started")
		if !hook("ups-0", "UserPromptSubmit") {
			os.Exit(32)
		}
		var prompt map[string]any
		if len(queued) > 0 {
			prompt, queued = queued[0], queued[1:]
		} else {
			prompt = next(func(f map[string]any) bool { return f["type"] == "user" }, 5*time.Second)
		}
		id, text := promptOf(prompt)
		lifecycle(id, "queued")
		lifecycle(id, "started")
		lifecycle(id, "completed")
		emit(map[string]any{"type": "result", "subtype": "success", "result": "reply to " + text, "is_error": false})
		lifecycle("re-run-of-cut-off-turn", "completed")
	} else if slices.Contains(os.Args, "--resume") {
		command("re-run-of-cut-off-turn", "Continue from where you left off.", 0)
	}
	for n := 1; ; n++ {
		var prompt map[string]any
		if len(queued) > 0 {
			prompt, queued = queued[0], queued[1:]
		} else {
			prompt = next(func(f map[string]any) bool { return f["type"] == "user" }, time.Hour)
		}
		id, text := promptOf(prompt)
		if id != "" {
			lifecycle(id, "queued")
		}
		command(id, text, n)
	}
}
