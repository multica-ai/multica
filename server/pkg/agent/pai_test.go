package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakePaiACPScript returns a POSIX-sh script that impersonates `p-ai acp`
// for a single short ACP session: it acks initialize / session/new / set_model
// and then either fails set_model (exit) or completes a full prompt turn.
// Writes every received frame to $PAI_REQUESTS_FILE when set, and the full
// argv to $PAI_ARGS_FILE when set.
func fakePaiACPScript() string {
	return `#!/bin/sh
if [ -n "$PAI_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$PAI_ARGS_FILE"
  done
fi
while IFS= read -r line; do
  if [ -n "$PAI_REQUESTS_FILE" ]; then
    printf '%s\n' "$line" >> "$PAI_REQUESTS_FILE"
  fi
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentInfo":{"name":"p-ai","version":"0.97.11"},"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_pai","models":{"availableModels":[{"modelId":"cfg-a","name":"Config A"}]}}}\n' "$id"
      ;;
    *'"method":"session/resume"'*)
      sid=$(printf '%s' "$line" | sed -n 's/.*"sessionId":"\([^"]*\)".*/\1/p')
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"%s"}}\n' "$id" "$sid"
      ;;
    *'"method":"session/set_model"'*)
      if [ "$PAI_SET_MODEL_ERROR" = "1" ]; then
        printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"model not available: bogus-model"}}\n' "$id"
        exit 0
      fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_pai","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"pong"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

// TestPaiBackendInvokesACPSubcommand pins the argv for `p-ai`: the daemon
// must launch the ACP transport through the `acp` subcommand.
func TestPaiBackendInvokesACPSubcommand(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture; validated on unix")
	}

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "p-ai")
	writeTestExecutable(t, fakePath, []byte(fakePaiACPScript()))

	backend, err := New("pai", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"PAI_ARGS_FILE": argsFile},
	})
	if err != nil {
		t.Fatalf("new pai backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "task", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("status = %q, error = %q", result.Status, result.Error)
	}

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) < 1 || lines[0] != "acp" {
		t.Fatalf("expected first arg to be acp, got %q", lines)
	}
}

// TestPaiBackendFullTurn pins the happy path: initialize → session/new →
// set_model → session/prompt, with the streamed chunk and session id carried
// into the Result.
func TestPaiBackendFullTurn(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture; validated on unix")
	}

	recordPath := filepath.Join(t.TempDir(), "requests.jsonl")
	fakePath := filepath.Join(t.TempDir(), "p-ai")
	writeTestExecutable(t, fakePath, []byte(fakePaiACPScript()))

	backend, err := New("pai", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"PAI_REQUESTS_FILE": recordPath},
	})
	if err != nil {
		t.Fatalf("new pai backend: %v", err)
	}

	var messages []Message
	done := make(chan struct{})
	session, err := backend.Execute(context.Background(), "reply with pong", ExecOptions{
		Model:        "cfg-a",
		SystemPrompt: "runtime-brief: you are on Multica duty",
		Timeout:      5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		defer close(done)
		for m := range session.Messages {
			messages = append(messages, m)
		}
	}()

	select {
	case result := <-session.Result:
		if result.Status != "completed" {
			t.Fatalf("status = %q, error = %q", result.Status, result.Error)
		}
		if result.SessionID != "ses_pai" {
			t.Errorf("session id = %q, want ses_pai", result.SessionID)
		}
		if !strings.Contains(result.Output, "pong") {
			t.Errorf("output = %q, want it to contain pong", result.Output)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
	<-done

	wantOrder := []string{"initialize", "session/new", "session/set_model", "session/prompt"}
	var methods []string
	raw, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read recorded requests: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var frame map[string]any
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatalf("decode recorded request: %v", err)
		}
		method, _ := frame["method"].(string)
		methods = append(methods, method)
	}
	if !reflect.DeepEqual(methods, wantOrder) {
		t.Fatalf("ACP method order = %v, want %v", methods, wantOrder)
	}

	// System prompt must ride inline (providerNeedsInlineSystemPrompt).
	// The fake binary records frames; find the prompt text.
	var promptText string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if !strings.Contains(line, `"method":"session/prompt"`) {
			continue
		}
		var frame struct {
			Params struct {
				Prompt []struct {
					Text string `json:"text"`
				} `json:"prompt"`
			} `json:"params"`
		}
		if err := json.Unmarshal([]byte(line), &frame); err == nil && len(frame.Params.Prompt) > 0 {
			promptText = frame.Params.Prompt[0].Text
		}
	}
	if !strings.Contains(promptText, "runtime-brief") || !strings.Contains(promptText, "reply with pong") {
		t.Errorf("inline system prompt missing from turn text: %q", promptText)
	}
}

// TestPaiBackendSetModelFailureFailsTask mirrors the kimi contract: a model
// override P-ai refuses must fail the task, never silently run on the
// default model.
func TestPaiBackendSetModelFailureFailsTask(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture; validated on unix")
	}

	fakePath := filepath.Join(t.TempDir(), "p-ai")
	writeTestExecutable(t, fakePath, []byte(fakePaiACPScript()))

	backend, err := New("pai", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"PAI_SET_MODEL_ERROR": "1"},
	})
	if err != nil {
		t.Fatalf("new pai backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Model:   "bogus-model",
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
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
		if !strings.Contains(result.Error, `could not switch to model "bogus-model"`) {
			t.Errorf("expected error to name the requested model, got %q", result.Error)
		}
		if result.SessionID != "" {
			t.Errorf("expected the never-prompted session id to be withheld, got %q", result.SessionID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

// TestPaiBackendResumeSkipsSetModelWithoutOverride: a resumed session without
// a model override must go straight to session/prompt, keeping the model the
// previous turn left in place.
func TestPaiBackendResumeSkipsSetModelWithoutOverride(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture; validated on unix")
	}

	recordPath := filepath.Join(t.TempDir(), "requests.jsonl")
	fakePath := filepath.Join(t.TempDir(), "p-ai")
	writeTestExecutable(t, fakePath, []byte(fakePaiACPScript()))

	backend, err := New("pai", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"PAI_REQUESTS_FILE": recordPath},
	})
	if err != nil {
		t.Fatalf("new pai backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "task", ExecOptions{
		Timeout:         5 * time.Second,
		ResumeSessionID: "ses_pai",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result := <-session.Result:
		if result.Status != "completed" {
			t.Fatalf("status = %q, error = %q", result.Status, result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	var methods []string
	raw, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read recorded requests: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var frame map[string]any
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatalf("decode recorded request: %v", err)
		}
		method, _ := frame["method"].(string)
		methods = append(methods, method)
	}
	wantOrder := []string{"initialize", "session/resume", "session/prompt"}
	if !reflect.DeepEqual(methods, wantOrder) {
		t.Fatalf("ACP method order = %v, want %v", methods, wantOrder)
	}
}
