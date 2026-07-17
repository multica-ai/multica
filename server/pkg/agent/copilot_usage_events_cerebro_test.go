package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func TestCopilotExecuteEmitsAssistantCallUsageEvents(t *testing.T) {
	fakePath := filepath.Join(t.TempDir(), "copilot")
	script := `#!/bin/sh
printf '%s\n' '{"type":"session.start","data":{"sessionId":"copilot-session","selectedModel":"claude-sonnet-4"}}'
printf '%s\n' '{"type":"assistant.message","data":{"messageId":"call-1","content":"using tool","outputTokens":12}}'
printf '%s\n' '{"type":"tool.execution_complete","data":{"toolCallId":"tool-1","model":"claude-opus-4.6","success":true,"result":{"content":"ok"}}}'
printf '%s\n' '{"type":"assistant.message","data":{"messageId":"call-2","content":"done","outputTokens":7}}'
printf '%s\n' '{"type":"result","sessionId":"copilot-session","exitCode":0}'
`
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("copilot", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new copilot backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for range session.Messages {
	}
	result := <-session.Result

	if len(result.UsageEvents) != 2 {
		t.Fatalf("UsageEvents = %+v, want two native Copilot assistant-call events", result.UsageEvents)
	}
	first := result.UsageEvents[0]
	if first.EventID != "copilot:copilot-session:call:call-1" || first.CallID != "call-1" || first.Sequence != 1 {
		t.Fatalf("first identity = %+v", first)
	}
	if first.Provider != "copilot" || first.Model != "claude-sonnet-4" || first.OutputTokens != 12 {
		t.Fatalf("first attribution = %+v", first)
	}
	if first.InputTokens != 0 || first.ContextTokens != 0 || first.Source != ModelUsageSourceStream || first.Completeness != ModelUsageTokensOnly || first.CounterSemantics != ModelUsageCounterDelta {
		t.Fatalf("first semantics = %+v", first)
	}
	second := result.UsageEvents[1]
	if second.CallID != "call-2" || second.Sequence != 2 || second.Model != "claude-opus-4.6" || second.OutputTokens != 7 {
		t.Fatalf("second event = %+v", second)
	}
}
