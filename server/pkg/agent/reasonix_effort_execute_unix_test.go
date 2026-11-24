//go:build unix

package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// reasonixEffortExecStub speaks just enough ACP to run one turn, and appends
// every request it receives to $REASONIX_RPC_LOG so the test can assert what
// actually went over the wire. set_config_option echoes the refreshed option
// the way reasonix does, which is what the read-back confirmation reads.
func reasonixEffortExecStub(sessionResult string) string {
	return `#!/bin/sh
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$REASONIX_RPC_LOG"
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":` + sessionResult + `}\n' "$id"
      ;;
    *'"method":"session/set_config_option"'*)
      requested=$(printf '%s' "$line" | sed -n 's/.*"value":"\([^"]*\)".*/\1/p')
      printf '{"jsonrpc":"2.0","id":%s,"result":{"configOptions":[{"id":"effort","currentValue":"%s","options":[{"value":"auto"},{"value":"disabled"},{"value":"low"},{"value":"high"},{"value":"max"}]}]}}\n' "$id" "$requested"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses-reasonix","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"pong"}}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func runReasonixEffortTurn(t *testing.T, sessionResult, level string) (Result, string) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "rpc.log")
	t.Setenv("REASONIX_RPC_LOG", logPath)

	fakePath := filepath.Join(dir, "reasonix")
	writeTestExecutable(t, fakePath, []byte(reasonixEffortExecStub(sessionResult)))

	backend, err := New("reasonix", Config{ExecutablePath: fakePath, Logger: discardLogger()})
	if err != nil {
		t.Fatalf("New(reasonix): %v", err)
	}
	session, err := backend.Execute(context.Background(), "ping", ExecOptions{
		ThinkingLevel: level,
		Timeout:       20 * time.Second,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read rpc log: %v", err)
	}
	return result, string(raw)
}

// TestReasonixExecuteAppliesThinkingLevel is the acceptance criterion from
// GitHub #6720 in test form: a persisted level reaches the live session through
// the option id the session itself advertised, and the turn still completes.
func TestReasonixExecuteAppliesThinkingLevel(t *testing.T) {
	result, rpcLog := runReasonixEffortTurn(t, reasonixEffortSessionResult, "high")

	if result.Status != "completed" {
		t.Fatalf("result = %+v, want a completed turn", result)
	}
	if !strings.Contains(rpcLog, `"method":"session/set_config_option"`) {
		t.Fatalf("no set_config_option reached the runtime; log:\n%s", rpcLog)
	}
	if !strings.Contains(rpcLog, `"configId":"effort"`) {
		t.Errorf("configId was not the advertised `effort`; log:\n%s", rpcLog)
	}
	if !strings.Contains(rpcLog, `"value":"high"`) {
		t.Errorf("the requested level did not reach the runtime; log:\n%s", rpcLog)
	}
	// Ordering matters: an effort applied after the prompt would not affect it.
	if strings.Index(rpcLog, "set_config_option") > strings.Index(rpcLog, "session/prompt") {
		t.Errorf("effort was applied after the prompt; log:\n%s", rpcLog)
	}
}

// TestReasonixExecuteWithoutEffortOption: a runtime build that advertises no
// effort selector must still run the turn, and must not send a config call it
// has no id for.
func TestReasonixExecuteWithoutEffortOption(t *testing.T) {
	const noEffort = `{"sessionId":"ses-reasonix","models":{"currentModelId":"m","availableModels":[{"modelId":"m"}]}}`
	result, rpcLog := runReasonixEffortTurn(t, noEffort, "high")

	if result.Status != "completed" {
		t.Fatalf("result = %+v, want the turn to run at the runtime default", result)
	}
	if strings.Contains(rpcLog, "set_config_option") {
		t.Errorf("sent a config call with no advertised option; log:\n%s", rpcLog)
	}
}

// TestReasonixExecuteWithoutThinkingLevel keeps the default path silent: an
// agent with no persisted effort must not touch the session configuration.
func TestReasonixExecuteWithoutThinkingLevel(t *testing.T) {
	result, rpcLog := runReasonixEffortTurn(t, reasonixEffortSessionResult, "")

	if result.Status != "completed" {
		t.Fatalf("result = %+v", result)
	}
	if strings.Contains(rpcLog, "set_config_option") {
		t.Errorf("an agent with no effort override must not reconfigure the session; log:\n%s", rpcLog)
	}
}
