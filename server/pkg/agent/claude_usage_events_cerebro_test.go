package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestClaudeExecuteEmitsCallLevelUsageEvents(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\n" +
		"IFS= read -r _\n" +
		"printf '%s\\n' '{\"type\":\"system\",\"session_id\":\"sess-call-usage\"}'\n" +
		"printf '%s\\n' '{\"type\":\"assistant\",\"session_id\":\"sess-call-usage\",\"message\":{\"id\":\"msg-opus\",\"role\":\"assistant\",\"model\":\"claude-opus-4-7\",\"content\":[{\"type\":\"text\",\"text\":\"first\"}],\"usage\":{\"input_tokens\":100,\"output_tokens\":20,\"cache_read_input_tokens\":70,\"cache_creation_input_tokens\":10}}}'\n" +
		"printf '%s\\n' '{\"type\":\"assistant\",\"session_id\":\"sess-call-usage\",\"message\":{\"id\":\"msg-sonnet\",\"role\":\"assistant\",\"model\":\"claude-sonnet-4-6\",\"content\":[{\"type\":\"text\",\"text\":\"second\"}],\"usage\":{\"input_tokens\":40,\"output_tokens\":8,\"cache_read_input_tokens\":30,\"cache_creation_input_tokens\":5}}}'\n" +
		"printf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"session_id\":\"sess-call-usage\",\"result\":\"done\"}'\n"
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("claude", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new claude backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{Timeout: 5 * time.Second})
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
		if len(result.UsageEvents) != 2 {
			t.Fatalf("UsageEvents = %+v, want two native Claude call events", result.UsageEvents)
		}

		first := result.UsageEvents[0]
		if first.ProviderSessionID != "sess-call-usage" || first.CallID != "msg-opus" || first.Sequence != 1 {
			t.Fatalf("first event identity = %+v", first)
		}
		if first.Provider != "anthropic" || first.Model != "claude-opus-4-7" {
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
		if second.CallID != "msg-sonnet" || second.Sequence != 2 || second.Model != "claude-sonnet-4-6" {
			t.Fatalf("second event identity = %+v", second)
		}
		if second.InputTokens != 40 || second.OutputTokens != 8 || second.CacheReadTokens != 30 || second.CacheWriteTokens != 5 || second.ContextTokens != 75 {
			t.Fatalf("second event tokens = %+v", second)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}
