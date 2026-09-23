package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Re-exec only this test binary, never an installed Pi or a provider account.
func init() {
	if os.Getenv("MULTICA_TEST_PI_RPC") != "1" {
		return
	}
	encoder := json.NewEncoder(os.Stdout)
	var outputMu sync.Mutex
	emit := func(v any) { outputMu.Lock(); defer outputMu.Unlock(); _ = encoder.Encode(v) }
	scanner := bufio.NewScanner(os.Stdin)
	prompts := 0
	cleared := false
	noAssistantAbort := false
	for scanner.Scan() {
		var command map[string]any
		if json.Unmarshal(scanner.Bytes(), &command) != nil {
			os.Exit(2)
		}
		response := map[string]any{"type": "response", "id": command["id"], "success": true}
		switch command["type"] {
		case "get_state":
			response["data"] = map[string]any{"isStreaming": false}
		case "prompt":
			prompts++
			if command["message"] == "dialog" {
				emit(map[string]any{"type": "extension_ui_request", "id": "approval", "method": "confirm"})
			}
			noAssistantAbort = command["message"] == "no-assistant-abort"
			emit(map[string]any{"type": "agent_start"})
			emit(map[string]any{"type": "message_update", "assistantMessageEvent": map[string]any{"type": "text_delta", "delta": fmt.Sprintf("pid=%d prompt=%d", os.Getpid(), prompts)}})
			// Low-level end can precede retries/compaction; it is not terminal.
			emit(map[string]any{"type": "agent_end"})
			if gate := os.Getenv("MULTICA_TEST_SETTLE_GATE"); gate != "" && prompts == 1 {
				go func() {
					waitTestSettleGate(gate)
					emit(map[string]any{"type": "message_update", "assistantMessageEvent": map[string]any{"type": "text_delta", "delta": "final reply preserved"}})
					emit(map[string]any{"type": "turn_end", "message": map[string]any{"stopReason": "stop"}})
					emit(map[string]any{"type": "agent_settled"})
				}()
			}
			if prompts > 1 || command["message"] == "finish" {
				emit(map[string]any{"type": "turn_end", "message": map[string]any{"stopReason": "stop", "model": "fake", "usage": map[string]int{"input": 2, "output": 3}}})
				emit(map[string]any{"type": "agent_settled"})
			}
		case "steer":
			emit(map[string]any{"type": "message_update", "assistantMessageEvent": map[string]any{"type": "text_delta", "delta": "steered"}})
		case "clear_queue":
			cleared = true
			response["data"] = map[string]any{"steering": []string{"queued instruction"}, "followUp": []string{}}
		case "abort":
			if !cleared {
				os.Exit(3)
			}
			if !noAssistantAbort {
				emit(map[string]any{"type": "turn_end", "message": map[string]any{"stopReason": "aborted"}})
			}
			emit(map[string]any{"type": "agent_settled"})
		default:
			os.Exit(4)
		}
		emit(response)
	}
	os.Exit(0)
}

func testPiInteractive(t *testing.T, prompt string) (*Session, context.Context, <-chan string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	b := &piBackend{cfg: Config{ExecutablePath: os.Args[0], CLIVersion: "0.87.0", Env: map[string]string{"MULTICA_TEST_PI_RPC": "1"}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte("{\"type\":\"session\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := b.ExecuteInteractive(ctx, prompt, ExecOptions{ResumeSessionID: path})
	if err != nil {
		t.Fatal(err)
	}
	output := make(chan string, 20)
	go func() {
		defer close(output)
		for message := range s.Messages {
			if message.Type == MessageText {
				output <- message.Content
			}
		}
	}()
	for s.Control.Snapshot().State == InteractionStarting {
		select {
		case <-ctx.Done():
			t.Fatal("startup timeout")
		case <-time.After(time.Millisecond):
		}
	}
	return s, ctx, output
}

func TestPiInteractiveInterruptContinueSameProcess(t *testing.T) {
	s, ctx, output := testPiInteractive(t, "work")
	first := <-output
	r, err := s.Control.Submit(ctx, InteractionCommand{ID: "steer", Kind: "input", Activity: 1, Text: "change direction"})
	if err != nil || r.Outcome != "applied" {
		t.Fatalf("steer: %+v %v", r, err)
	}
	r, err = s.Control.Submit(ctx, InteractionCommand{ID: "stop", Kind: "interrupt", Activity: 1})
	if err != nil || r.Snapshot.State != InteractionAwaitingInput {
		t.Fatalf("interrupt: %+v %v", r, err)
	}
	if len(r.ClearedSteering) != 1 {
		t.Fatal("lost cleared input")
	}
	select {
	case <-s.Result:
		t.Fatal("interrupt ended run")
	default:
	}
	r, err = s.Control.Submit(ctx, InteractionCommand{ID: "continue", Kind: "input", Activity: 1, Text: "finish"})
	if err != nil || r.Snapshot.Activity != 2 {
		t.Fatalf("continue: %+v %v", r, err)
	}
	var result Result
	select {
	case result = <-s.Result:
	case <-ctx.Done():
		t.Fatal("run did not finish")
	}
	if result.Status != "completed" {
		t.Fatalf("result: %+v", result)
	}
	if !strings.Contains(result.Output, strings.Fields(first)[0]+" prompt=2") {
		t.Fatalf("process changed: %q -> %q", first, result.Output)
	}
	if result.Usage["fake"].OutputTokens != 3 {
		t.Fatalf("usage: %+v", result.Usage)
	}
	if s.Control.Snapshot().State != InteractionFinished {
		t.Fatal("control left open")
	}
}

func TestPiInteractiveNaturalCompletion(t *testing.T) {
	s, ctx, _ := testPiInteractive(t, "finish")
	select {
	case result := <-s.Result:
		if result.Status != "completed" {
			t.Fatalf("%+v", result)
		}
	case <-ctx.Done():
		t.Fatal("completion timeout")
	}
}

func TestPiRPCArgsKeepWrapperAndProtocolSeparate(t *testing.T) {
	args := buildPiModeArgs("/tmp/session", ExecOptions{CustomArgs: []string{"--mode", "json", "-p", "--tools", "read"}}, slog.Default(), true)
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "-p") || strings.Contains(joined, "json") || !strings.HasPrefix(joined, "--mode rpc --session") || !strings.Contains(joined, "--tools read") {
		t.Fatalf("%v", args)
	}
}

func TestPiInteractiveAbortWithoutAssistantMessage(t *testing.T) {
	s, ctx, _ := testPiInteractive(t, "no-assistant-abort")
	receipt, err := s.Control.Submit(ctx, InteractionCommand{ID: "stop", Kind: "interrupt", Activity: 1})
	if err != nil || receipt.Snapshot.State != InteractionAwaitingInput {
		t.Fatalf("%+v %v", receipt, err)
	}
	select {
	case <-s.Result:
		t.Fatal("abort or agent_end ended the run")
	default:
	}
	if _, err := s.Control.Submit(ctx, InteractionCommand{ID: "next", Kind: "input", Activity: 1, Text: "finish"}); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-s.Result:
		if result.Status != "completed" {
			t.Fatalf("%+v", result)
		}
	case <-ctx.Done():
		t.Fatal("completion timeout")
	}
}

func TestPiInteractiveRejectsUnknownProtocolAndMissingHistory(t *testing.T) {
	for _, version := range []string{"", "0.86.0"} {
		b := &piBackend{cfg: Config{CLIVersion: version, ExecutablePath: "/definitely-missing/pi"}}
		if _, err := b.ExecuteInteractive(context.Background(), "work", ExecOptions{}); err == nil || !strings.Contains(err.Error(), "0.87.0") {
			t.Fatalf("version %q: %v", version, err)
		}
	}
	b := &piBackend{cfg: Config{CLIVersion: "0.87.0", ExecutablePath: os.Args[0]}}
	if _, err := b.ExecuteInteractive(context.Background(), "work", ExecOptions{ResumeSessionID: filepath.Join(t.TempDir(), "missing")}); err == nil || !strings.Contains(err.Error(), "saved Pi session") {
		t.Fatalf("missing history: %v", err)
	}
}

func TestPiInteractiveDialogFailsClosed(t *testing.T) {
	s, ctx, _ := testPiInteractive(t, "dialog")
	select {
	case result := <-s.Result:
		if result.Status != "failed" || !strings.Contains(result.Error, "no approval was granted") {
			t.Fatalf("%+v", result)
		}
	case <-ctx.Done():
		t.Fatal("invisible dialog left the run hanging")
	}
}
