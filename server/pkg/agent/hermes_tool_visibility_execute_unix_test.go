//go:build !windows

package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The `hermes` provider covers two kinds of binary: the real Hermes Agent, and
// any custom runtime profile declaring `protocol_family: hermes` (jcode is the
// documented example). Only the former is known to put a call's whole input on
// the tool_call start frame, so toolStartCarriesFinalInput must be scoped to
// Config.BuiltinRuntime rather than switched on for every hermes-family
// implementation.
//
// These tests drive hermesBackend.Execute — the real construction path — rather
// than a hand-built client, because the scope leak lives in how the client is
// constructed, not in how it handles frames.

// hermesToolVisibilityStub answers one turn that runs a single tool whose start
// frame carries display text and no rawInput, and whose completion frame
// carries the real rawInput. That is the shape where deferring matters: a
// dialect wrongly marked "final input at start" records Input=nil and drops the
// real input when the completion frame arrives.
const hermesToolVisibilityStub = `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_tv"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_tv","update":{"sessionUpdate":"tool_call","toolCallId":"tc-1","title":"write: /tmp/a.txt","kind":"edit","content":[{"type":"content","content":{"type":"text","text":"Preparing write to /tmp/a.txt. Approval prompt shows the diff."}}]}}}\n'
      printf '{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ses_tv","update":{"sessionUpdate":"tool_call_update","toolCallId":"tc-1","status":"completed","title":"write: /tmp/a.txt","kind":"edit","rawInput":{"path":"/tmp/a.txt","content":"hi"},"content":[{"type":"content","content":{"type":"text","text":"wrote 2 bytes"}}]}}}\n'
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`

func runHermesToolVisibilityTurn(t *testing.T, builtin bool) []Message {
	t.Helper()

	dir := t.TempDir()
	fakePath := filepath.Join(dir, "hermes")
	writeTestExecutable(t, fakePath, []byte(hermesToolVisibilityStub))

	backend, err := New("hermes", Config{
		ExecutablePath: fakePath,
		Logger:         slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		BuiltinRuntime: builtin,
	})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "ping", ExecOptions{Timeout: 15 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var tools []Message
	for msg := range session.Messages {
		switch msg.Type {
		case MessageToolUse, MessageToolResult:
			tools = append(tools, msg)
		}
	}
	if result := <-session.Result; result.Status != "completed" {
		t.Fatalf("result = %+v, want a completed turn", result)
	}
	return tools
}

// A custom hermes-family runtime (BuiltinRuntime=false) must keep deferring, so
// the rawInput its completion frame carries is what gets recorded.
func TestHermesCustomRuntimeDefersToolUseAndKeepsRealInput(t *testing.T) {
	t.Parallel()

	tools := runHermesToolVisibilityTurn(t, false)

	if len(tools) != 2 {
		t.Fatalf("expected [ToolUse, ToolResult], got %d: %+v", len(tools), tools)
	}
	if tools[0].Type != MessageToolUse || tools[1].Type != MessageToolResult {
		t.Fatalf("unexpected message order: %+v", tools)
	}
	// Emitted from the completion frame, so it carries the real input.
	if path, _ := tools[0].Input["path"].(string); path != "/tmp/a.txt" {
		t.Errorf("custom runtime must keep the completion frame's rawInput, got Input = %v", tools[0].Input)
	}
	if text, ok := tools[0].Input["text"].(string); ok {
		t.Errorf("display text must not be recorded as input, got %q", text)
	}
}

// The built-in Hermes binary emits at the start frame, so the call is visible
// (and counted in flight) while it runs. Its display text is still not treated
// as input.
func TestHermesBuiltinRuntimeEmitsToolUseAtStart(t *testing.T) {
	t.Parallel()

	tools := runHermesToolVisibilityTurn(t, true)

	if len(tools) != 2 {
		t.Fatalf("expected [ToolUse, ToolResult], got %d: %+v", len(tools), tools)
	}
	if tools[0].Type != MessageToolUse || tools[1].Type != MessageToolResult {
		t.Fatalf("unexpected message order: %+v", tools)
	}
	if tools[0].Tool != "write_file" {
		t.Errorf("tool: got %q, want %q", tools[0].Tool, "write_file")
	}
	// Emitted from the start frame, which has no rawInput — and its display
	// prose must not be substituted for one.
	if tools[0].Input != nil {
		t.Errorf("start-frame emit must report no input, got %v", tools[0].Input)
	}
	if tools[1].Output != "wrote 2 bytes" {
		t.Errorf("result output: got %q", tools[1].Output)
	}
}
