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

func fakeReasonixACPScript() string {
	return `#!/bin/sh
if [ -n "$REASONIX_ARGS_FILE" ]; then
  for arg in "$@"; do
    printf '%s\n' "$arg" >> "$REASONIX_ARGS_FILE"
  done
fi
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":false},"agentInfo":{"name":"reasonix","version":"0.1.0"},"authMethods":[]}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_reasonix"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_reasonix","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"done"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func TestBuildReasonixArgsForNPX(t *testing.T) {
	t.Parallel()

	got := buildReasonixArgs("npx", []string{"reasonix"}, ExecOptions{
		Model:      "deepseek-chat",
		CustomArgs: []string{"reasonix", "acp", "--yolo", "--model", "ignored", "--budget", "0.5"},
	}, slog.Default())

	want := []string{"reasonix", "acp", "--model", "deepseek-chat", "--yolo", "--budget", "0.5"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("args = %q, want %q", got, want)
	}
}

func TestBuildReasonixArgsForWrapperCommand(t *testing.T) {
	t.Parallel()

	got := buildReasonixArgs("npx", []string{"dsnix@latest"}, ExecOptions{
		Model:      "deepseek-chat",
		CustomArgs: []string{"reasonix", "acp", "--yolo", "--budget", "0.5"},
	}, slog.Default())

	want := []string{"dsnix@latest", "acp", "--model", "deepseek-chat", "--yolo", "--budget", "0.5"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("args = %q, want %q", got, want)
	}
}

func TestResolveReasonixCommandDefaultsToNPXReasonix(t *testing.T) {
	t.Parallel()

	execPath, fixedArgs, err := resolveReasonixCommand("")
	if err != nil {
		t.Fatalf("resolveReasonixCommand: %v", err)
	}
	if execPath != "npx" {
		t.Fatalf("execPath = %q, want npx", execPath)
	}
	if strings.Join(fixedArgs, "\n") != "reasonix" {
		t.Fatalf("fixedArgs = %q, want [reasonix]", fixedArgs)
	}
}

func TestResolveReasonixCommandSupportsWrapperAndPackage(t *testing.T) {
	t.Parallel()

	execPath, fixedArgs, err := resolveReasonixCommand("npx dsnix@latest")
	if err != nil {
		t.Fatalf("resolveReasonixCommand: %v", err)
	}
	if execPath != "npx" {
		t.Fatalf("execPath = %q, want npx", execPath)
	}
	want := []string{"dsnix@latest"}
	if strings.Join(fixedArgs, "\n") != strings.Join(want, "\n") {
		t.Fatalf("fixedArgs = %q, want %q", fixedArgs, want)
	}
}

func TestReasonixBackendInvokesNPXACP(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "npx")
	writeTestExecutable(t, fakePath, []byte(fakeReasonixACPScript()))

	backend, err := New("reasonix", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"REASONIX_ARGS_FILE": argsFile},
	})
	if err != nil {
		t.Fatalf("new reasonix backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Model:      "deepseek-chat",
		Timeout:    5 * time.Second,
		CustomArgs: []string{"reasonix", "acp", "--yolo", "--model", "ignored", "--budget", "0.5"},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("status = %q, error=%q", result.Status, result.Error)
	}
	if result.Output != "done" {
		t.Fatalf("output = %q, want done", result.Output)
	}

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	want := []string{"reasonix", "acp", "--model", "deepseek-chat", "--yolo", "--budget", "0.5"}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("argv = %q, want %q", lines, want)
	}
}

func TestReasonixBackendInvokesWrapperWithFixedPackage(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "argv.txt")
	fakePath := filepath.Join(tempDir, "npx")
	writeTestExecutable(t, fakePath, []byte(fakeReasonixACPScript()))

	backend, err := New("reasonix", Config{
		ExecutablePath: fakePath + " dsnix@latest",
		Logger:         slog.Default(),
		Env:            map[string]string{"REASONIX_ARGS_FILE": argsFile},
	})
	if err != nil {
		t.Fatalf("new reasonix backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{
		Model:      "deepseek-chat",
		Timeout:    5 * time.Second,
		CustomArgs: []string{"reasonix", "acp", "--yolo", "--budget", "0.5"},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("status = %q, error=%q", result.Status, result.Error)
	}

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	want := []string{"dsnix@latest", "acp", "--model", "deepseek-chat", "--yolo", "--budget", "0.5"}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("argv = %q, want %q", lines, want)
	}
}
