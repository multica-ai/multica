//go:build unix

package agent

import (
	"context"
	"encoding/json"
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
  for arg in "$@"; do printf '%s\n' "$arg" >> "$REASONIX_ARGS_FILE"; done
fi
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"_meta":{"_reasonix.io/session/status":{"schemaVersion":1},"_reasonix.io/session/status_update":{"schemaVersion":1}}}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"reasonix-session"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"_reasonix.io/session/status_update","params":{"schemaVersion":1,"sequence":1,"status":{"model":"deepseek-reasoner","usage":{"turn":{"promptTokens":15,"completionTokens":4,"cacheHitTokens":10,"cacheMissTokens":5,"estimatedCost":0.001,"currency":"USD"}}}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"reasonix-session","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"done"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func TestReasonixBackendExecutesACPWithStateUsage(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "args")
	fakePath := filepath.Join(tempDir, "reasonix")
	writeTestExecutable(t, fakePath, []byte(fakeReasonixACPScript()))

	backend, err := New("reasonix", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"REASONIX_ARGS_FILE": argsFile},
	})
	if err != nil {
		t.Fatalf("New(reasonix): %v", err)
	}
	session, err := backend.Execute(context.Background(), "finish", ExecOptions{
		Timeout: 5 * time.Second,
		CustomArgs: []string{
			"--profile", "delivery",
			"--planner", "off",
			"--sandbox-network", "on",
			"--sandbox-bash", "enforce",
			"--workspace-only",
			"--model", "other",
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "completed" || result.Output != "done" || result.SessionID != "reasonix-session" {
		t.Fatalf("result = %+v", result)
	}
	usage := result.Usage["deepseek-reasoner"]
	if usage.InputTokens != 5 || usage.CacheReadTokens != 10 || usage.OutputTokens != 4 || usage.CostUSDTicks != 10_000_000 {
		t.Fatalf("usage = %+v", usage)
	}
	rawArgs, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	wantArgs := []string{"acp", "--profile", "balanced", "--planner", "auto", "--sandbox-network", "auto", "--sandbox-bash", "auto", "--workspace-only"}
	if got := strings.Fields(string(rawArgs)); strings.Join(got, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("args = %q, want %q", got, wantArgs)
	}
}

func TestReasonixBackendStopReasonErrorFails(t *testing.T) {
	t.Parallel()
	script := strings.Replace(fakeReasonixACPScript(), `"stopReason":"end_turn"`, `"stopReason":"error"`, 1)
	fakePath := filepath.Join(t.TempDir(), "reasonix")
	writeTestExecutable(t, fakePath, []byte(script))
	backend, err := New("reasonix", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(reasonix): %v", err)
	}
	session, err := backend.Execute(context.Background(), "finish", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "failed" || !strings.Contains(result.Error, "stopReason=error") {
		t.Fatalf("result = %+v", result)
	}
}

func TestReasonixBackendQuestionFailsWithActionableError(t *testing.T) {
	t.Parallel()
	script := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*) printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id" ;;
    *'"method":"session/new"'*) printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"question-session"}}\n' "$id" ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","id":99,"method":"session/request_permission","params":{"sessionId":"question-session","toolCall":{"toolCallId":"ask-a-q1","title":"Choose a deployment"},"options":[{"optionId":"q1:1","kind":"allow_once"},{"optionId":"q1:cancel","kind":"reject_once"}]}}\n'
      IFS= read -r permission
      case "$permission" in *'"optionId":"q1:cancel"'*) ;; *) exit 2 ;; esac
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
	fakePath := filepath.Join(t.TempDir(), "reasonix")
	writeTestExecutable(t, fakePath, []byte(script))
	backend, err := New("reasonix", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(reasonix): %v", err)
	}
	session, err := backend.Execute(context.Background(), "finish", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "failed" || !strings.Contains(result.Error, "interactive user input") || !strings.Contains(result.Error, "Choose a deployment") {
		t.Fatalf("result = %+v", result)
	}
}

func TestReasonixResumeFiltersUnsupportedSSEMCP(t *testing.T) {
	t.Parallel()
	recordPath := filepath.Join(t.TempDir(), "frames.jsonl")
	fakePath := filepath.Join(t.TempDir(), "reasonix")
	writeTestExecutable(t, fakePath, []byte(fakeACPRecordingScript(
		recordPath,
		"resumed",
		`{"mcpCapabilities":{"http":true,"sse":false}}`,
	)))
	backend, err := New("reasonix", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(reasonix): %v", err)
	}
	session, err := backend.Execute(context.Background(), "finish", ExecOptions{
		ResumeSessionID: "resumed",
		Timeout:         5 * time.Second,
		McpConfig: json.RawMessage(`{"mcpServers":{
			"local":{"command":"uvx","args":["mcp-local"]},
			"remote-http":{"type":"http","url":"https://example.test/mcp"},
			"remote-sse":{"type":"sse","url":"https://example.test/sse"}
		}}`),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "completed" || result.SessionID != "resumed" {
		t.Fatalf("result = %+v", result)
	}
	frame := findRecordedFrame(t, recordPath, "session/resume")
	servers := frame["params"].(map[string]any)["mcpServers"].([]any)
	if len(servers) != 2 {
		t.Fatalf("mcpServers = %+v, want stdio + HTTP only", servers)
	}
	for _, raw := range servers {
		if raw.(map[string]any)["name"] == "remote-sse" {
			t.Fatalf("unsupported SSE server was forwarded: %+v", servers)
		}
	}
}

func TestReasonixBackendLeaseConflictIsActionable(t *testing.T) {
	t.Parallel()
	script := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*) printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id" ;;
    *'"method":"session/resume"'*) printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32600,"message":"session is in use; close the other Reasonix window or process first"}}\n' "$id"; exit 0 ;;
  esac
done
`
	fakePath := filepath.Join(t.TempDir(), "reasonix")
	writeTestExecutable(t, fakePath, []byte(script))
	backend, err := New("reasonix", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(reasonix): %v", err)
	}
	session, err := backend.Execute(context.Background(), "finish", ExecOptions{ResumeSessionID: "busy", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "failed" || result.ResumeRejected || !strings.Contains(result.Error, "already in use") || !strings.Contains(result.Error, "close the other Reasonix") {
		t.Fatalf("result = %+v", result)
	}
}

func TestReasonixBackendCancelledStopReasonAborts(t *testing.T) {
	t.Parallel()
	script := strings.Replace(fakeReasonixACPScript(), `"stopReason":"end_turn"`, `"stopReason":"cancelled"`, 1)
	fakePath := filepath.Join(t.TempDir(), "reasonix")
	writeTestExecutable(t, fakePath, []byte(script))
	backend, err := New("reasonix", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(reasonix): %v", err)
	}
	session, err := backend.Execute(context.Background(), "finish", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "aborted" || !strings.Contains(result.Error, "cancelled") {
		t.Fatalf("result = %+v", result)
	}
}

func TestReasonixBackendTimeoutAndCancellationReapProcess(t *testing.T) {
	t.Parallel()
	const script = `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*) printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id" ;;
    *'"method":"session/new"'*) printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"hanging"}}\n' "$id" ;;
    *'"method":"session/prompt"'*) ;;
  esac
done
`
	tests := []struct {
		name       string
		timeout    time.Duration
		cancelSoon bool
		wantStatus string
	}{
		{name: "timeout", timeout: 100 * time.Millisecond, wantStatus: "timeout"},
		{name: "cancellation", timeout: 5 * time.Second, cancelSoon: true, wantStatus: "aborted"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fakePath := filepath.Join(t.TempDir(), "reasonix")
			writeTestExecutable(t, fakePath, []byte(script))
			backend, err := New("reasonix", Config{ExecutablePath: fakePath, Logger: slog.Default()})
			if err != nil {
				t.Fatalf("New(reasonix): %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			session, err := backend.Execute(ctx, "finish", ExecOptions{Timeout: tt.timeout})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			go func() {
				for range session.Messages {
				}
			}()
			if tt.cancelSoon {
				time.AfterFunc(100*time.Millisecond, cancel)
			}
			select {
			case result := <-session.Result:
				if result.Status != tt.wantStatus {
					t.Fatalf("result = %+v, want status %q", result, tt.wantStatus)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("backend did not return after context termination")
			}
			select {
			case _, ok := <-session.Result:
				if ok {
					t.Fatal("result channel produced more than one value")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("backend did not reap the process and close the result channel")
			}
		})
	}
}

func TestReasonixBackendUnknownResumeIsRetryableFresh(t *testing.T) {
	t.Parallel()
	script := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*) printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id" ;;
    *'"method":"session/resume"'*) printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"session/resume: unknown session dead"}}\n' "$id"; exit 0 ;;
  esac
done
`
	fakePath := filepath.Join(t.TempDir(), "reasonix")
	writeTestExecutable(t, fakePath, []byte(script))
	backend, err := New("reasonix", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(reasonix): %v", err)
	}
	session, err := backend.Execute(context.Background(), "finish", ExecOptions{ResumeSessionID: "dead", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "failed" || !result.ResumeRejected || result.SessionID != "" {
		t.Fatalf("result = %+v", result)
	}
}

func TestDiscoverReasonixModelsUsesEphemeralState(t *testing.T) {
	tempDir := t.TempDir()
	recordPath := filepath.Join(tempDir, "state-home")
	t.Setenv("REASONIX_DISCOVERY_RECORD", recordPath)
	t.Setenv("REASONIX_STATE_HOME", filepath.Join(tempDir, "must-not-use"))
	script := `#!/bin/sh
printf '%s' "$REASONIX_STATE_HOME" > "$REASONIX_DISCOVERY_RECORD"
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*) printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id" ;;
    *'"method":"session/new"'*) printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"discovery","models":{"currentModelId":"deepseek-reasoner","availableModels":[{"modelId":"deepseek-reasoner","name":"DeepSeek Reasoner"}]}}}\n' "$id" ;;
  esac
done
`
	fakePath := filepath.Join(tempDir, "reasonix")
	writeTestExecutable(t, fakePath, []byte(script))

	models, err := discoverReasonixModels(context.Background(), fakePath)
	if err != nil {
		t.Fatalf("discoverReasonixModels: %v", err)
	}
	if len(models) != 1 || models[0].ID != "deepseek-reasoner" || !models[0].Default {
		t.Fatalf("models = %+v", models)
	}
	raw, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read state record: %v", err)
	}
	stateHome := string(raw)
	if stateHome == "" || stateHome == filepath.Join(tempDir, "must-not-use") {
		t.Fatalf("state home was not isolated: %q", stateHome)
	}
	if _, err := os.Stat(stateHome); !os.IsNotExist(err) {
		t.Fatalf("temporary state home was not cleaned up: %q, stat err=%v", stateHome, err)
	}
}
