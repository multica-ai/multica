package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

// runFakeClaudeSteerEcho reads the initial prompt frame, emits an assistant
// message (so the caller can prove the run is live before steering), then
// blocks reading a SECOND stdin frame — the steer message — and echoes its
// text into the final result. A backend that never delivers the steer frame
// hangs here until the test deadline instead of passing.
func runFakeClaudeSteerEcho() {
	reader := bufio.NewReader(os.Stdin)
	if _, err := reader.ReadString('\n'); err != nil {
		fmt.Fprintf(os.Stderr, "read prompt: %v\n", err)
		os.Exit(51)
	}
	fmt.Println(`{"type":"system","session_id":"sess-steer"}`)
	fmt.Println(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"working on it"}]}}`)

	line, err := reader.ReadString('\n')
	if err != nil {
		fmt.Fprintf(os.Stderr, "read steer frame: %v\n", err)
		os.Exit(52)
	}
	var frame struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &frame); err != nil {
		fmt.Fprintf(os.Stderr, "decode steer frame: %v\n", err)
		os.Exit(53)
	}
	if frame.Type != "user" || frame.Message.Role != "user" || len(frame.Message.Content) != 1 {
		fmt.Fprintf(os.Stderr, "unexpected steer frame shape: %s\n", line)
		os.Exit(54)
	}
	result := map[string]any{
		"type":       "result",
		"subtype":    "success",
		"is_error":   false,
		"session_id": "sess-steer",
		"result":     "steered: " + frame.Message.Content[0].Text,
	}
	data, err := json.Marshal(result)
	if err != nil {
		os.Exit(55)
	}
	fmt.Println(string(data))
}

// TestClaudeSteerDeliversMidRunUserMessage exercises the full Steer path
// against a fake child: the steer text must arrive as a well-formed user
// stream-json frame on the SAME stdin, after the initial prompt, while the
// run is live.
func TestClaudeSteerDeliversMidRunUserMessage(t *testing.T) {
	t.Parallel()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	backend, err := New("claude", Config{
		ExecutablePath: self,
		Env:            map[string]string{"CLAUDE_FAKE_MODE": "steer_echo", "IS_SANDBOX": "1"},
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new claude backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "original prompt", ExecOptions{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if session.Steer == nil {
		t.Fatal("claude session must advertise Steer support")
	}

	// Wait until the fake has emitted its first assistant message, proving
	// the run is genuinely mid-flight when we steer.
	sawAssistant := false
	for msg := range session.Messages {
		if msg.Type == MessageText && msg.Content == "working on it" {
			sawAssistant = true
			if err := session.Steer("switch to plan B"); err != nil {
				t.Fatalf("steer: %v", err)
			}
		}
	}
	if !sawAssistant {
		t.Fatal("never saw the fake's assistant message")
	}

	result := <-session.Result
	if result.Error != "" {
		t.Fatalf("unexpected result error: %q", result.Error)
	}
	if result.Output != "steered: switch to plan B" {
		t.Fatalf("steer text did not round-trip through the live session, got output %q", result.Output)
	}

	// The session is finished; a late steer must fail loudly so callers
	// fall back to their follow-up-task path instead of silently dropping
	// the message.
	if err := session.Steer("too late"); err == nil {
		t.Fatal("steer after completion must return an error")
	}
}

// TestClaudeSteerAfterCancelFails pins the cancelled-run behaviour: once the
// context is done and the child killed, Steer reports an error rather than
// writing into a dead pipe.
func TestClaudeSteerAfterCancelFails(t *testing.T) {
	t.Parallel()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	backend, err := New("claude", Config{
		ExecutablePath: self,
		// The fake blocks waiting for a steer frame we never send, so the
		// run is alive until cancel fires.
		Env:    map[string]string{"CLAUDE_FAKE_MODE": "steer_echo", "IS_SANDBOX": "1"},
		Logger: slog.Default(),
	})
	if err != nil {
		t.Fatalf("new claude backend: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session, err := backend.Execute(ctx, "original prompt", ExecOptions{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	for msg := range session.Messages {
		if msg.Type == MessageText && msg.Content == "working on it" {
			cancel()
		}
	}
	<-session.Result

	if err := session.Steer("into the void"); err == nil {
		t.Fatal("steer after cancellation must return an error")
	}
}
