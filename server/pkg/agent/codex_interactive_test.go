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
	"sync"
	"testing"
	"time"
)

func init() {
	if os.Getenv("MULTICA_TEST_CODEX_INTERACTIVE") != "1" {
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("codex-cli 0.147.0")
		os.Exit(0)
	}
	encoder := json.NewEncoder(os.Stdout)
	var outputMu sync.Mutex
	emit := func(v any) { outputMu.Lock(); defer outputMu.Unlock(); _ = encoder.Encode(v) }
	event := func(method string, params any) { emit(map[string]any{"method": method, "params": params}) }
	scanner := bufio.NewScanner(os.Stdin)
	turn := 0
	for scanner.Scan() {
		var command struct {
			ID     *int           `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &command) != nil {
			os.Exit(2)
		}
		if command.ID == nil {
			continue
		}
		result := map[string]any{}
		switch command.Method {
		case "initialize":
		case "thread/resume":
			if command.Params["threadId"] == "missing" {
				emit(map[string]any{"id": *command.ID, "error": map[string]any{"code": -32000, "message": "thread not found"}})
				continue
			}
			result["thread"] = map[string]any{"id": "thread-live"}
		case "thread/start":
			result["thread"] = map[string]any{"id": "thread-live"}
		case "turn/start":
			turn++
			turnID := fmt.Sprintf("turn-%d", turn)
			result["turn"] = map[string]any{"id": turnID}
			event("turn/started", map[string]any{"threadId": "thread-live", "turn": map[string]any{"id": turnID}})
			event("item/agentMessage/delta", map[string]any{"threadId": "thread-live", "turnId": turnID, "itemId": fmt.Sprintf("item-%d", turn), "delta": fmt.Sprintf("pid=%d turn=%d", os.Getpid(), turn)})
			if gate := os.Getenv("MULTICA_TEST_SETTLE_GATE"); gate != "" && turn == 1 {
				go func() {
					waitTestSettleGate(gate)
					event("item/completed", map[string]any{"threadId": "thread-live", "turnId": turnID, "item": map[string]any{"id": "final", "type": "agentMessage", "phase": "final_answer", "text": "final reply preserved"}})
					event("turn/completed", map[string]any{"threadId": "thread-live", "turn": map[string]any{"id": turnID, "status": "completed"}})
				}()
			}
			if turn > 1 {
				event("item/completed", map[string]any{"threadId": "thread-live", "turnId": turnID, "item": map[string]any{"id": fmt.Sprintf("item-%d", turn), "type": "agentMessage", "phase": "final_answer", "text": fmt.Sprintf("pid=%d turn=%d", os.Getpid(), turn)}})
				event("turn/completed", map[string]any{"threadId": "thread-live", "turn": map[string]any{"id": turnID, "status": "completed"}})
			}
		case "turn/steer":
			if command.Params["expectedTurnId"] != fmt.Sprintf("turn-%d", turn) {
				os.Exit(3)
			}
			result["turnId"] = command.Params["expectedTurnId"]
		case "turn/interrupt":
			event("turn/completed", map[string]any{"threadId": "thread-live", "turn": map[string]any{"id": fmt.Sprintf("turn-%d", turn), "status": "interrupted"}})
		default:
			os.Exit(4)
		}
		emit(map[string]any{"id": *command.ID, "result": result})
	}
	os.Exit(0)
}

func TestCodexInteractiveMissingResumeDoesNotStartFresh(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b := &codexBackend{cfg: Config{ExecutablePath: os.Args[0], Env: map[string]string{"MULTICA_TEST_CODEX_INTERACTIVE": "1"}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	s, err := b.ExecuteInteractive(ctx, "work", ExecOptions{Cwd: t.TempDir(), ResumeSessionID: "missing"})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range s.Messages {
		}
	}()
	select {
	case result := <-s.Result:
		if result.Status != "failed" || !strings.Contains(result.Error, "refusing a fresh thread") {
			t.Fatalf("%+v", result)
		}
	case <-ctx.Done():
		t.Fatal("started a fresh thread instead of failing")
	}
}

func TestCodexInteractiveToolAndReasoningProgress(t *testing.T) {
	var messages []Message
	c := &codexClient{interactiveProgress: true, onMessage: func(m Message) { messages = append(messages, m) }}
	c.handleItemNotification("item/commandExecution/outputDelta", map[string]any{"itemId": "cmd", "delta": "first "})
	c.handleItemNotification("item/commandExecution/outputDelta", map[string]any{"itemId": "cmd", "delta": "second"})
	c.handleItemNotification("item/reasoning/summaryTextDelta", map[string]any{"itemId": "thought", "delta": "checking"})
	if len(messages) != 3 || messages[1].Output != "first second" || messages[1].Type != MessageToolProgress || messages[2].Type != MessageThinking {
		t.Fatalf("%+v", messages)
	}
}

func TestCodexInteractiveInterruptContinueSameProcess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b := &codexBackend{cfg: Config{ExecutablePath: os.Args[0], Env: map[string]string{"MULTICA_TEST_CODEX_INTERACTIVE": "1"}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	s, err := b.ExecuteInteractive(ctx, "work", ExecOptions{Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	output := make(chan string, 16)
	go func() {
		for message := range s.Messages {
			if message.Type == MessageText {
				output <- message.Content
			}
		}
		close(output)
	}()
	var first string
	select {
	case first = <-output:
	case <-ctx.Done():
		t.Fatal("startup timeout")
	}
	for s.Control.Snapshot().State == InteractionStarting {
		select {
		case <-ctx.Done():
			t.Fatal("control not ready")
		case <-time.After(time.Millisecond):
		}
	}
	_, err = s.Control.Submit(ctx, InteractionCommand{ID: "steer", Kind: "input", Activity: 1, Text: "focus"})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := s.Control.Submit(ctx, InteractionCommand{ID: "stop", Kind: "interrupt", Activity: 1})
	if err != nil || receipt.Snapshot.State != InteractionAwaitingInput {
		t.Fatalf("interrupt: %+v %v", receipt, err)
	}
	select {
	case <-s.Result:
		t.Fatal("interrupt ended execution")
	default:
	}
	_, err = s.Control.Submit(ctx, InteractionCommand{ID: "continue", Kind: "input", Activity: 1, Text: "finish"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-s.Result:
		if result.Status != "completed" || result.SessionID != "thread-live" || !strings.Contains(result.Output, strings.Fields(first)[0]+" turn=2") {
			t.Fatalf("first=%q result=%+v", first, result)
		}
	case <-ctx.Done():
		t.Fatal("completion timeout")
	}
}
