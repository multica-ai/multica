//go:build !windows

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

// The `hermes` provider covers two binaries with opposite answers on reasoning
// effort, and this PR ships no real-jcode verification. These tests are the
// substitute evidence for the main execution chain: they drive
// hermesBackend.Execute against ACP stubs shaped like each binary and assert
// what reaches the wire.

// hermesEffortExecStub answers a full turn and appends every request it receives
// to $HERMES_RPC_LOG. sessionResult is returned from BOTH session/new and
// session/resume so one stub covers either entry path.
func hermesEffortExecStub(sessionResult string) string {
	return `#!/bin/sh
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$HERMES_RPC_LOG"
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"session/new"'*|*'"method":"session/resume"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":` + sessionResult + `}\n' "$id"
      ;;
    *'"method":"session/set_config_option"'*)
      requested=$(printf '%s' "$line" | sed -n 's/.*"value":"\([^"]*\)".*/\1/p')
      printf '{"jsonrpc":"2.0","id":%s,"result":{"configOptions":[{"id":"reasoning_effort","currentValue":"%s","options":[{"value":"low"},{"value":"medium"},{"value":"high"}]}]}}\n' "$id" "$requested"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func runHermesEffortTurn(t *testing.T, sessionResult, level string, opts ExecOptions) string {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "rpc.log")
	fakePath := filepath.Join(dir, "hermes")
	writeTestExecutable(t, fakePath, []byte(hermesEffortExecStub(sessionResult)))

	backend, err := New("hermes", Config{
		ExecutablePath: fakePath,
		Logger:         slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		Env:            map[string]string{"HERMES_RPC_LOG": logPath},
	})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	opts.ThinkingLevel = level
	opts.Timeout = 15 * time.Second
	session, err := backend.Execute(ctx, "ping", opts)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("result = %+v, want a completed turn", result)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read rpc log: %v", err)
	}
	return string(raw)
}

// TestHermesExecuteAppliesJcodeEffort covers both entry paths: a fresh session
// and a resumed one must each push the level under jcode's own advertised
// option id, before the prompt.
func TestHermesExecuteAppliesJcodeEffort(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts ExecOptions
	}{
		{name: "session/new"},
		{name: "session/resume", opts: ExecOptions{ResumeSessionID: "ses-jcode", ResumeExpected: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rpcLog := runHermesEffortTurn(t, jcodeEffortSessionResult, "high", tc.opts)

			if !strings.Contains(rpcLog, `"method":"session/set_config_option"`) {
				t.Fatalf("no set_config_option reached the runtime; log:\n%s", rpcLog)
			}
			if !strings.Contains(rpcLog, `"configId":"reasoning_effort"`) {
				t.Errorf("configId was not jcode's advertised id; log:\n%s", rpcLog)
			}
			if !strings.Contains(rpcLog, `"value":"high"`) {
				t.Errorf("the requested level did not reach the runtime; log:\n%s", rpcLog)
			}
			// An effort applied after the prompt would not affect it.
			if strings.Index(rpcLog, "set_config_option") > strings.Index(rpcLog, "session/prompt") {
				t.Errorf("effort was applied after the prompt; log:\n%s", rpcLog)
			}
		})
	}
}

// TestHermesExecuteSendsNothingForHermesAgent is the other binary under the same
// provider: no advertised option means no config call at all, and the turn still
// runs at the runtime's own default.
func TestHermesExecuteSendsNothingForHermesAgent(t *testing.T) {
	rpcLog := runHermesEffortTurn(t, hermesAgentSessionResult, "high", ExecOptions{})
	if strings.Contains(rpcLog, "set_config_option") {
		t.Errorf("sent a config call to a runtime advertising no option; log:\n%s", rpcLog)
	}
	if !strings.Contains(rpcLog, `"method":"session/prompt"`) {
		t.Errorf("the turn should still have prompted; log:\n%s", rpcLog)
	}
}
