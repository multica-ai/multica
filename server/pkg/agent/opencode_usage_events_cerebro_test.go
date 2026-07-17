package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenCodeExecuteEmitsCallLevelUsageEvents(t *testing.T) {
	fakePath := filepath.Join(t.TempDir(), "opencode")
	script := `#!/bin/sh
printf '%s\n' '{"type":"step_start","sessionID":"opencode-session","part":{}}'
printf '%s\n' '{"type":"step_finish","sessionID":"opencode-session","part":{"tokens":{"input":100,"output":20,"cache":{"read":70,"write":10}}}}'
printf '%s\n' '{"type":"step_finish","sessionID":"opencode-session","part":{"tokens":{"input":40,"output":8,"cache":{"read":30,"write":5}}}}'
`
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("opencode", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new opencode backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt", ExecOptions{Timeout: 5 * time.Second, Model: "anthropic/claude-test"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for range session.Messages {
	}
	result := <-session.Result

	if len(result.UsageEvents) != 2 {
		t.Fatalf("UsageEvents = %+v, want two native OpenCode call events", result.UsageEvents)
	}
	first := result.UsageEvents[0]
	if first.ProviderSessionID != "opencode-session" || first.CallID != "" || first.Sequence != 1 {
		t.Fatalf("first event identity = %+v", first)
	}
	if first.Provider != "opencode" || first.Model != "anthropic/claude-test" {
		t.Fatalf("first event model attribution = %+v", first)
	}
	if first.InputTokens != 100 || first.OutputTokens != 20 || first.CacheReadTokens != 70 || first.CacheWriteTokens != 10 {
		t.Fatalf("first event tokens = %+v", first)
	}
	if first.ContextTokens != 180 || first.ContextWindowTokens != 0 {
		t.Fatalf("first event context = %+v", first)
	}
	if first.Source != ModelUsageSourceStream || first.Completeness != ModelUsageTokensOnly || first.CounterSemantics != ModelUsageCounterDelta {
		t.Fatalf("first event semantics = %+v", first)
	}
	if first.ObservedAt.IsZero() {
		t.Fatal("first event ObservedAt is zero")
	}

	second := result.UsageEvents[1]
	if second.Sequence != 2 || second.Model != "anthropic/claude-test" {
		t.Fatalf("second event identity = %+v", second)
	}
	if second.InputTokens != 40 || second.OutputTokens != 8 || second.CacheReadTokens != 30 || second.CacheWriteTokens != 5 || second.ContextTokens != 75 {
		t.Fatalf("second event tokens = %+v", second)
	}
}
