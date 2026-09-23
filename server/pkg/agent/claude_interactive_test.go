package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// The test binary is a stream-json peer, not an actual Claude invocation.
func init() {
	if os.Getenv("MULTICA_TEST_CLAUDE_INTERACTIVE") != "1" {
		return
	}
	input := bufio.NewScanner(os.Stdin)
	output := json.NewEncoder(os.Stdout)
	turn := 0
	for input.Scan() {
		var frame struct {
			Type      string `json:"type"`
			RequestID string `json:"request_id"`
			Request   struct {
				Subtype string `json:"subtype"`
			} `json:"request"`
		}
		if json.Unmarshal(input.Bytes(), &frame) != nil {
			os.Exit(2)
		}
		switch frame.Type {
		case "user":
			turn++
			output.Encode(map[string]any{"type": "system", "session_id": "saved"})
			if os.Getenv("MULTICA_TEST_CLAUDE_INTERACTIVE_CONTEXT_OVERFLOW") == "1" {
				output.Encode(map[string]any{"type": "result", "session_id": "saved", "is_error": true, "terminal_reason": "prompt_too_long", "result": ""})
				continue
			}
			output.Encode(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []map[string]string{{"type": "text", "text": "reply"}}}})
			if os.Getenv("MULTICA_TEST_CLAUDE_INTERACTIVE_QUEUE") == "1" && turn == 2 {
				output.Encode(map[string]any{"type": "result", "session_id": "saved", "result": "first reply"})
				output.Encode(map[string]any{"type": "result", "session_id": "saved", "result": "second reply"})
			} else if turn != 2 && os.Getenv("MULTICA_TEST_CLAUDE_INTERACTIVE_QUEUE") != "1" {
				output.Encode(map[string]any{"type": "result", "session_id": "saved", "result": "reply"})
			}
		case "control_request":
			if frame.Request.Subtype != "interrupt" {
				os.Exit(3)
			}
			output.Encode(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": frame.RequestID, "response": map[string]any{}}})
			output.Encode(map[string]any{"type": "result", "session_id": "saved", "is_error": true, "result": "interrupted"})
		default:
			os.Exit(4)
		}
	}
	os.Exit(0)
}

func TestClaudeInteractiveKeepsProcessAcrossTurnsAndInterrupt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b := &claudeBackend{cfg: Config{
		ExecutablePath: os.Args[0],
		Env:            map[string]string{"MULTICA_TEST_CLAUDE_INTERACTIVE": "1"},
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}}
	s, err := b.ExecuteInteractive(ctx, "first", ExecOptions{Cwd: t.TempDir(), KeepInteractiveOpen: true})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range s.Messages {
		}
	}()
	wait := func(state InteractionState, activity uint64) {
		t.Helper()
		for {
			snapshot := s.Control.Snapshot()
			if snapshot.State == state && snapshot.Activity == activity {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatalf("waiting for %s/%d; got %+v", state, activity, snapshot)
			case <-time.After(time.Millisecond):
			}
		}
	}
	wait(InteractionAwaitingInput, 1)
	if receipt, err := s.Control.Submit(ctx, InteractionCommand{ID: "next", Kind: "input", Activity: 1, Text: "second"}); err != nil || receipt.Outcome != "applied" {
		t.Fatalf("second message: %+v %v", receipt, err)
	}
	wait(InteractionWorking, 2)
	if receipt, err := s.Control.Submit(ctx, InteractionCommand{ID: "interrupt", Kind: "interrupt", Activity: 2}); err != nil || receipt.Snapshot.State != InteractionAwaitingInput {
		t.Fatalf("interrupt: %+v %v", receipt, err)
	}
	if receipt, err := s.Control.Submit(ctx, InteractionCommand{ID: "third", Kind: "input", Activity: 2, Text: "third"}); err != nil || receipt.Outcome != "applied" {
		t.Fatalf("third message: %+v %v", receipt, err)
	}
	wait(InteractionAwaitingInput, 3)
	if _, err := s.Control.Submit(ctx, InteractionCommand{ID: "finish", Kind: "finish", Activity: 3}); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-s.Result:
		if result.Status != "completed" || result.SessionID != "saved" || result.Output != "reply" || !s.TerminalObserved() {
			t.Fatalf("result: %+v, terminal=%v", result, s.TerminalObserved())
		}
	case <-ctx.Done():
		t.Fatal("Claude did not exit after finish")
	}
}

func TestClaudeInteractiveRefusesWrongResumedSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b := &claudeBackend{cfg: Config{
		ExecutablePath: os.Args[0],
		Env:            map[string]string{"MULTICA_TEST_CLAUDE_INTERACTIVE": "1"},
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}}
	s, err := b.ExecuteInteractive(ctx, "first", ExecOptions{Cwd: t.TempDir(), ResumeSessionID: "other", RequireSessionResume: true, KeepInteractiveOpen: true})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range s.Messages {
		}
	}()
	select {
	case result := <-s.Result:
		if result.Status != "failed" || !strings.Contains(result.Error, "did not resume") {
			t.Fatalf("result: %+v", result)
		}
	case <-ctx.Done():
		t.Fatal("session mismatch was not rejected")
	}
}

func TestClaudeInteractiveClassifiesContextOverflowWithoutResultText(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b := &claudeBackend{cfg: Config{
		ExecutablePath: os.Args[0],
		Env: map[string]string{
			"MULTICA_TEST_CLAUDE_INTERACTIVE":                  "1",
			"MULTICA_TEST_CLAUDE_INTERACTIVE_CONTEXT_OVERFLOW": "1",
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}}
	s, err := b.ExecuteInteractive(ctx, "first", ExecOptions{Cwd: t.TempDir(), KeepInteractiveOpen: true})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range s.Messages {
		}
	}()
	select {
	case result := <-s.Result:
		if result.Status != "failed" || taskfailure.Classify(result.Error) != taskfailure.ReasonAgentContextOverflow {
			t.Fatalf("context overflow result: %+v", result)
		}
	case <-ctx.Done():
		t.Fatal("Claude did not report context overflow")
	}
}

func TestClaudeInteractiveCancelPausedRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deadline, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	b := &claudeBackend{cfg: Config{
		ExecutablePath: os.Args[0],
		Env:            map[string]string{"MULTICA_TEST_CLAUDE_INTERACTIVE": "1"},
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}}
	s, err := b.ExecuteInteractive(ctx, "first", ExecOptions{Cwd: t.TempDir(), KeepInteractiveOpen: true})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range s.Messages {
		}
	}()
	for s.Control.Snapshot().State != InteractionAwaitingInput {
		select {
		case <-deadline.Done():
			t.Fatal("Claude did not reach awaiting input")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	select {
	case result := <-s.Result:
		if result.Status != "aborted" || s.Control.Snapshot().State != InteractionFinished {
			t.Fatalf("cancel result: %+v, state=%+v", result, s.Control.Snapshot())
		}
	case <-deadline.Done():
		t.Fatal("cancelled Claude process did not exit")
	}
}

func TestClaudeInteractiveQueuesBusyInputForNextTurn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b := &claudeBackend{cfg: Config{
		ExecutablePath: os.Args[0],
		Env:            map[string]string{"MULTICA_TEST_CLAUDE_INTERACTIVE": "1", "MULTICA_TEST_CLAUDE_INTERACTIVE_QUEUE": "1"},
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}}
	s, err := b.ExecuteInteractive(ctx, "first", ExecOptions{Cwd: t.TempDir(), KeepInteractiveOpen: true})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range s.Messages {
		}
	}()
	for s.Control.Snapshot().State == InteractionStarting {
		select {
		case <-ctx.Done():
			t.Fatal("Claude did not start")
		case <-time.After(time.Millisecond):
		}
	}
	receipt, err := s.Control.Submit(ctx, InteractionCommand{ID: "queued", Kind: "input", Activity: 1, Text: "second"})
	if err != nil || receipt.Outcome != "queued" {
		t.Fatalf("busy input: %+v %v", receipt, err)
	}
	for s.Control.Snapshot().State != InteractionAwaitingInput || s.Control.Snapshot().Activity != 2 {
		select {
		case <-ctx.Done():
			t.Fatalf("queued reply did not settle: %+v", s.Control.Snapshot())
		case <-time.After(time.Millisecond):
		}
	}
	if _, err := s.Control.Submit(ctx, InteractionCommand{ID: "finish", Kind: "finish", Activity: 2}); err != nil {
		t.Fatal(err)
	}
	if result := <-s.Result; result.Status != "completed" || result.Output != "second reply" {
		t.Fatalf("result: %+v", result)
	}
}
