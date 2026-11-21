package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"
)

func drainContext(t *testing.T, ch <-chan Message) []ContextInfo {
	t.Helper()
	var out []ContextInfo
	for {
		select {
		case m := <-ch:
			if m.Type == MessageContext {
				if m.Context == nil {
					t.Fatal("a context message must carry its payload")
				}
				out = append(out, *m.Context)
			}
		default:
			return out
		}
	}
}

func TestClaudeAssistantReportsContextOccupancy(t *testing.T) {
	t.Parallel()

	b := &claudeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 10)

	msg := claudeSDKMessage{
		Type: "assistant",
		Message: mustMarshal(t, claudeMessageContent{
			ID:      "msg-1",
			Role:    "assistant",
			Model:   "claude-opus-5",
			Content: []claudeContentBlock{{Type: "text", Text: "hi"}},
			Usage: &claudeUsage{
				InputTokens:              1_000,
				OutputTokens:             50,
				CacheReadInputTokens:     100_000,
				CacheCreationInputTokens: 2_000,
			},
		}),
	}

	b.handleAssistant(msg, ch, make(map[string]TokenUsage), make(map[string]struct{}), 0)

	events := drainContext(t, ch)
	if len(events) != 1 {
		t.Fatalf("expected one context event, got %d", len(events))
	}
	// Occupancy is the prompt that was sent — input plus both cache classes —
	// and excludes the response's own output tokens.
	if events[0].UsedTokens != 103_000 {
		t.Fatalf("used = %d, want 103000", events[0].UsedTokens)
	}
	// No configured model means no established window. The stream's base
	// model id must not stand in for it: it drops a `[1m]` modifier.
	if events[0].LimitTokens != 0 {
		t.Fatalf("limit = %d, want none without a configured model", events[0].LimitTokens)
	}
	if events[0].Phase != ContextPhaseUsage {
		t.Fatalf("phase = %q, want a plain usage observation", events[0].Phase)
	}
}

func TestClaudeAssistantConfiguredWindowWinsOverStreamModel(t *testing.T) {
	t.Parallel()

	b := &claudeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 10)

	// The stream reports the base model, which has lost the `[1m]` modifier
	// the run was started with. Measuring against 200K here would overstate
	// occupancy fivefold.
	msg := claudeSDKMessage{
		Type: "assistant",
		Message: mustMarshal(t, claudeMessageContent{
			ID:      "msg-1",
			Role:    "assistant",
			Model:   "claude-opus-5",
			Content: []claudeContentBlock{{Type: "text", Text: "hi"}},
			Usage:   &claudeUsage{InputTokens: 300_000},
		}),
	}

	b.handleAssistant(msg, ch, make(map[string]TokenUsage), make(map[string]struct{}), 1_000_000)

	events := drainContext(t, ch)
	if len(events) != 1 || events[0].LimitTokens != 1_000_000 {
		t.Fatalf("expected the configured 1M window, got %+v", events)
	}
}

func TestClaudeAssistantReportsOccupancyOncePerResponse(t *testing.T) {
	t.Parallel()

	b := &claudeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 20)
	usage := make(map[string]TokenUsage)
	seen := make(map[string]struct{})

	// Two blocks of one response share the prompt that produced them.
	for range 2 {
		msg := claudeSDKMessage{
			Type: "assistant",
			Message: mustMarshal(t, claudeMessageContent{
				ID:      "msg-1",
				Role:    "assistant",
				Model:   "claude-opus-5",
				Content: []claudeContentBlock{{Type: "text", Text: "hi"}},
				Usage:   &claudeUsage{InputTokens: 10_000},
			}),
		}
		b.handleAssistant(msg, ch, usage, seen, 0)
	}

	if events := drainContext(t, ch); len(events) != 1 {
		t.Fatalf("expected one context event per response, got %d", len(events))
	}
}

func TestClaudeAssistantIgnoresSubagentContext(t *testing.T) {
	t.Parallel()

	b := &claudeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 10)

	// A subagent's prompt is a different conversation; its size says nothing
	// about the main session's window.
	msg := claudeSDKMessage{
		Type:            "assistant",
		ParentToolUseID: "call-1",
		Message: mustMarshal(t, claudeMessageContent{
			ID:      "msg-1",
			Role:    "assistant",
			Model:   "claude-opus-5",
			Content: []claudeContentBlock{{Type: "text", Text: "hi"}},
			Usage:   &claudeUsage{InputTokens: 150_000},
		}),
	}

	b.handleAssistant(msg, ch, make(map[string]TokenUsage), make(map[string]struct{}), 0)

	if events := drainContext(t, ch); len(events) != 0 {
		t.Fatalf("expected no context event for a subagent turn, got %+v", events)
	}
}

func TestClaudeExecuteReportsCompactBoundary(t *testing.T) {
	t.Parallel()

	events := []json.RawMessage{
		json.RawMessage(`{"type":"system","subtype":"compact_boundary","session_id":"s-1","compact_metadata":{"trigger":"auto","pre_tokens":184000}}`),
	}
	terminal := json.RawMessage(`{"type":"result","subtype":"success","result":"done"}`)
	backend := claudeUsageFixtureBackend(t, events, terminal, false)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "fixture", ExecOptions{
		Model:   "claude-opus-5[1m]",
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	var got []ContextInfo
	for message := range session.Messages {
		if message.Type == MessageContext && message.Context != nil {
			got = append(got, *message.Context)
		}
	}
	<-session.Result

	if len(got) != 1 {
		t.Fatalf("expected one context event, got %+v", got)
	}
	want := ContextInfo{
		Phase:       ContextPhaseCompacted,
		PreTokens:   184_000,
		Trigger:     "auto",
		LimitTokens: 1_000_000,
		Model:       "claude-opus-5[1m]",
	}
	if got[0] != want {
		t.Fatalf("context event = %+v, want %+v", got[0], want)
	}
}

func TestClaudeContextWindowTokens(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		model string
		want  int64
	}{
		{model: "claude-opus-5", want: 200_000},
		{model: "claude-opus-5[1m]", want: 1_000_000},
		{model: "claude-sonnet-5[500k]", want: 500_000},
		// An alias and a non-Claude id establish no window, and a guessed one
		// would be presented to the reader as a measurement.
		{model: "opus", want: 0},
		{model: "gpt-5", want: 0},
		{model: "", want: 0},
	} {
		if got := ClaudeContextWindowTokens(tc.model); got != tc.want {
			t.Fatalf("ClaudeContextWindowTokens(%q) = %d, want %d", tc.model, got, tc.want)
		}
	}
}
