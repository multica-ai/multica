package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeQoderACPScript() string {
	return `#!/bin/sh
# Fake qodercli — exercises argv (--yolo --acp), blocked custom_args, set_model failure, and prompt success.
if [ -n "$QODER_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$QODER_ARGS_FILE"
  done
fi
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_fake"}}\n' "$id"
      ;;
    *'"method":"session/set_model"'*)
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"model not available: bogus-model"}}\n' "$id"
      exit 0
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_fake","update":{"type":"AgentMessageChunk","content":{"type":"text","text":"ok"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":1,"outputTokens":2}}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func fakeQoderACPScriptWithLeakedStdout() string {
	return `#!/bin/sh
# Fake qodercli that returns session/prompt but leaves stdout open via a child
# process, matching qodercli ACP staying alive after the turn completes.
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_fake"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/notification","params":{"sessionId":"ses_fake","update":{"type":"AgentMessageChunk","content":{"type":"text","text":"ok"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn","usage":{"inputTokens":1,"outputTokens":2}}}\n' "$id"
      sleep 30 &
      wait
      ;;
  esac
done
`
}

func TestQoderBackendSetModelFailureFailsTask(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "qodercli")
	writeTestExecutable(t, fakePath, []byte(fakeQoderACPScript()))

	backend, err := New("qoder", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new qoder backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Model:   "bogus-model",
		Timeout: 30 * time.Second,
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
		if !strings.Contains(result.Error, "model not available") {
			t.Errorf("expected error to surface upstream message, got %q", result.Error)
		}
		if result.SessionID != "ses_fake" {
			t.Errorf("expected session id to be preserved on failure, got %q", result.SessionID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestQoderBackendInvokesACPFlagAndFiltersBlockedArgs(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "qodercli")
	writeTestExecutable(t, fakePath, []byte(fakeQoderACPScript()))

	backend, err := New("qoder", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"QODER_ARGS_FILE": argsFile},
	})
	if err != nil {
		t.Fatalf("new qoder backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Model:      "bogus-model",
		Timeout:    30 * time.Second,
		CustomArgs: []string{"--acp", "acp", "--yolo", "--model", "extra"},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	<-session.Result

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 argv entries, got %d: %q", len(lines), lines)
	}
	if lines[0] != "--yolo" || lines[1] != "--acp" {
		t.Fatalf("arg[0], arg[1] = %q, %q, want --yolo and --acp (full: %q)", lines[0], lines[1], lines)
	}
	for _, blocked := range []string{"acp"} {
		for _, got := range lines[2:] {
			if got == blocked {
				t.Errorf("custom_args must not inject standalone %q after daemon argv: %q", blocked, lines)
			}
		}
	}
	yoloCount := 0
	for _, got := range lines {
		if got == "--yolo" {
			yoloCount++
		}
	}
	if yoloCount != 1 {
		t.Fatalf("expected exactly one daemon --yolo, got count=%d argv=%q", yoloCount, lines)
	}
	want := []string{"--yolo", "--acp", "--model", "extra"}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("unexpected argv after filtering: %q, want %q", lines, want)
	}
}

func TestQoderBackendHappyPath(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "qodercli")
	writeTestExecutable(t, fakePath, []byte(fakeQoderACPScript()))

	backend, err := New("qoder", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new qoder backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "hi", ExecOptions{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("status=%q err=%q", result.Status, result.Error)
	}
	if result.Output != "ok" {
		t.Fatalf("output=%q want ok", result.Output)
	}
	if result.SessionID != "ses_fake" {
		t.Fatalf("session=%q", result.SessionID)
	}
	if u := result.Usage["unknown"]; u.InputTokens != 1 || u.OutputTokens != 2 {
		t.Fatalf("usage=%+v", u)
	}
}

func TestQoderBackendDoesNotWaitForeverForReaderAfterPromptDone(t *testing.T) {
	oldGrace := qoderReaderDrainGrace
	qoderReaderDrainGrace = 25 * time.Millisecond
	t.Cleanup(func() { qoderReaderDrainGrace = oldGrace })

	fakePath := filepath.Join(t.TempDir(), "qodercli")
	writeTestExecutable(t, fakePath, []byte(fakeQoderACPScriptWithLeakedStdout()))

	backend, err := New("qoder", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new qoder backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "hi", ExecOptions{Timeout: 30 * time.Second})
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
			t.Fatalf("status=%q err=%q", result.Status, result.Error)
		}
		if result.Output != "ok" {
			t.Fatalf("output=%q want ok", result.Output)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("qoder result blocked waiting for reader shutdown")
	}
}
