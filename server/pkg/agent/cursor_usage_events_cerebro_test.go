package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func TestCursorExecuteEmitsCallLevelUsageEvents(t *testing.T) {
	fakePath := filepath.Join(t.TempDir(), "cursor-agent")
	script := `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"cursor-session"}'
printf '%s\n' '{"type":"step_finish","session_id":"cursor-session","model":"gpt-5-test","part":{"tokens":{"input":100,"output":20,"cache":{"read":70}},"cost":0.01}}'
printf '%s\n' '{"type":"step_finish","session_id":"cursor-session","model":"sonnet-test","part":{"tokens":{"input":40,"output":8,"cache":{"read":30}},"cost":0.005}}'
printf '%s\n' '{"type":"result","session_id":"cursor-session","model":"gpt-5-test","result":"done","usage":{"input_tokens":140,"output_tokens":28,"cached_input_tokens":100}}'
`
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("cursor", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new cursor backend: %v", err)
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
		t.Fatalf("UsageEvents = %+v, want two native Cursor call events", result.UsageEvents)
	}
	first := result.UsageEvents[0]
	if first.ProviderSessionID != "cursor-session" || first.CallID != "" || first.Sequence != 1 {
		t.Fatalf("first event identity = %+v", first)
	}
	if first.Provider != "cursor" || first.Model != "gpt-5-test" {
		t.Fatalf("first event model attribution = %+v", first)
	}
	if first.InputTokens != 100 || first.OutputTokens != 20 || first.CacheReadTokens != 70 || first.CacheWriteTokens != 0 {
		t.Fatalf("first event tokens = %+v", first)
	}
	if first.ContextTokens != 170 || first.ContextWindowTokens != 0 {
		t.Fatalf("first event context = %+v", first)
	}
	if first.Source != ModelUsageSourceStream || first.Completeness != ModelUsageTokensOnly || first.CounterSemantics != ModelUsageCounterDelta {
		t.Fatalf("first event semantics = %+v", first)
	}
	if first.ObservedAt.IsZero() {
		t.Fatal("first event ObservedAt is zero")
	}

	second := result.UsageEvents[1]
	if second.Sequence != 2 || second.Provider != "cursor" || second.Model != "sonnet-test" {
		t.Fatalf("second event identity = %+v", second)
	}
	if second.InputTokens != 40 || second.OutputTokens != 8 || second.CacheReadTokens != 30 || second.ContextTokens != 70 {
		t.Fatalf("second event tokens = %+v", second)
	}
}

func TestCursorExecuteReconcilesAuthoritativeResultUsage(t *testing.T) {
	fakePath := filepath.Join(t.TempDir(), "cursor-agent")
	script := `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"cursor-session"}'
printf '%s\n' '{"type":"step_finish","session_id":"cursor-session","model":"gpt-5-test","part":{"tokens":{"input":100,"output":20,"cache":{"read":70,"write":5}},"cost":0.01}}'
printf '%s\n' '{"type":"result","session_id":"cursor-session","model":"gpt-5-test","result":"done","usage":{"input_tokens":140,"output_tokens":30,"cached_input_tokens":90,"cache_creation_input_tokens":7}}'
`
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("cursor", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new cursor backend: %v", err)
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
		t.Fatalf("UsageEvents = %+v, want step event plus result reconciliation", result.UsageEvents)
	}
	step, reconciliation := result.UsageEvents[0], result.UsageEvents[1]
	if step.CacheWriteTokens != 5 || step.ContextTokens != 175 {
		t.Fatalf("step cache write/context = %+v, want cache_write=5 context=175", step)
	}
	if reconciliation.Source != "reconciliation" {
		t.Fatalf("reconciliation source = %q, want reconciliation", reconciliation.Source)
	}
	if reconciliation.InputTokens != 40 || reconciliation.OutputTokens != 10 ||
		reconciliation.CacheReadTokens != 20 || reconciliation.CacheWriteTokens != 2 {
		t.Fatalf("reconciliation tokens = %+v, want 40/10/20/2", reconciliation)
	}
	if reconciliation.Sequence != 2 || reconciliation.CounterSemantics != ModelUsageCounterDelta {
		t.Fatalf("reconciliation identity = %+v", reconciliation)
	}
}
