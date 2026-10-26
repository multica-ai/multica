package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const antigravityStdinHelperEnv = "MULTICA_ANTIGRAVITY_STDIN_HELPER"

// Runs as a native child executable, including on Windows, so the test covers
// CreateProcess without a shell or an argv-sized prompt hidden in a wrapper.
func runFakeAntigravityStdinHelper(mode string) {
	fail := func(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
	argv, err := json.Marshal(os.Args[1:])
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(os.Getenv("AGY_TEST_ARGV"), argv, 0600); err != nil {
		fail(err)
	}
	if mode == "cancel" {
		// Deliberately never read stdin: cancellation must also unblock the writer
		// when a prompt is larger than the OS pipe buffer.
		fmt.Println(`{"event":"step_update","step_update":{"step_index":0,"state":"DONE","step_type":"agent_response","text_delta":"working","usage":{"input_tokens":12,"output_tokens":3}}}`)
		time.Sleep(time.Minute)
		return
	}
	data, err := io.ReadAll(os.Stdin) // No output until EOF: detects an unclosed input pipe.
	if err != nil {
		fail(err)
	}
	var frame struct {
		Event   string `json:"event"`
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(data, &frame); err != nil {
		fail(err)
	}
	if frame.Event != "user" {
		fail(fmt.Errorf("event = %q", frame.Event))
	}
	if err := os.WriteFile(os.Getenv("AGY_TEST_INPUT"), []byte(frame.Message.Content), 0600); err != nil {
		fail(err)
	}
	fmt.Println(`{"event":"init","conversation_id":"test-session"}`)
	fmt.Println(`{"event":"step_update","step_update":{"step_index":0,"state":"DONE","step_type":"agent_response","text_delta":"ok","usage":{"input_tokens":12,"output_tokens":3}}}`)
	fmt.Println(`{"event":"result","result":{"conversation_id":"test-session","status":"SUCCESS","response":"ok","usage":{"input_tokens":120,"output_tokens":30}}}`)
}

func TestAntigravityStdinTransport(t *testing.T) {
	for _, tc := range []struct{ name, prompt, mode, resume string }{
		{name: "large ASCII", prompt: strings.Repeat("x", 34000)},
		{name: "multiline Unicode resumed", prompt: strings.Repeat("中文😀\n\"quoted\"\\path\r\n", 5000), resume: "test-session"},
		{name: "cancel blocked stdin", prompt: strings.Repeat("x", 1024*1024), mode: "cancel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			argvPath, inputPath := filepath.Join(dir, "argv.json"), filepath.Join(dir, "input.txt")
			mode := tc.mode
			if mode == "" {
				mode = "complete"
			}
			backend, err := New("antigravity", Config{ExecutablePath: executable, Logger: quietAntigravityLogger(), Env: map[string]string{
				antigravityStdinHelperEnv: mode, "AGY_TEST_ARGV": argvPath, "AGY_TEST_INPUT": inputPath,
			}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			session, err := backend.Execute(ctx, tc.prompt, ExecOptions{Cwd: dir, ResumeSessionID: tc.resume})
			if err != nil {
				t.Fatal(err)
			}
			for msg := range session.Messages {
				if mode == "cancel" && msg.Type == MessageText && msg.Content == "working" {
					cancel()
				}
			}
			result := <-session.Result
			wantStatus := "completed"
			if mode == "cancel" {
				wantStatus = "aborted"
			}
			if result.Status != wantStatus {
				t.Fatalf("status=%s error=%s", result.Status, result.Error)
			}
			if got := result.Usage["unknown"]; got != (TokenUsage{InputTokens: 12, OutputTokens: 3}) {
				t.Fatalf("usage=%+v", got)
			}
			data, err := os.ReadFile(argvPath)
			if err != nil {
				t.Fatal(err)
			}
			var args []string
			if err := json.Unmarshal(data, &args); err != nil {
				t.Fatal(err)
			}
			if len(data) > 4096 || slices.Contains(args, tc.prompt) || slices.Contains(args, "-p") {
				t.Fatal("prompt leaked into argv")
			}
			if i := slices.Index(args, "--input-format"); i < 0 || i+1 >= len(args) || args[i+1] != "stream-json" {
				t.Fatalf("input flags=%v", args)
			}
			if tc.resume != "" {
				if i := slices.Index(args, "--conversation"); i < 0 || i+1 >= len(args) || args[i+1] != tc.resume {
					t.Fatalf("resume flags=%v", args)
				}
			}
			if mode == "cancel" {
				return
			}
			if result.Output != "ok" || result.SessionID != "test-session" {
				t.Fatalf("result=%+v", result)
			}
			received, err := os.ReadFile(inputPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(received) != tc.prompt {
				t.Fatalf("stdin changed: got %d bytes, want %d", len(received), len(tc.prompt))
			}
		})
	}
}
