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

	turn := b.handleAssistant(msg, ch, make(map[string]TokenUsage), make(map[string]struct{}))

	// Occupancy is the prompt that was sent — input plus both cache classes —
	// and excludes the response's own output tokens.
	if turn.promptTokens != 103_000 || turn.model != "claude-opus-5" {
		t.Fatalf("turn = %+v, want 103000 measured by claude-opus-5", turn)
	}
	// The figure is not stated here: the window it should be measured against
	// is not knowable until the CLI answers, which it cannot do before this
	// very response exists. The caller holds it until then.
	if events := drainContext(t, ch); len(events) != 0 {
		t.Fatalf("handleAssistant must not state the figure, got %+v", events)
	}
}

// A run whose window is never established reports nothing: an absolute token
// count is not a figure a reader can act on. Here the CLI answers nothing and
// the model it names establishes no window either.
func TestClaudeReportsNothingWhenNoWindowIsEverEstablished(t *testing.T) {
	t.Parallel()

	events := []json.RawMessage{
		json.RawMessage(`{"type":"system","subtype":"init","session_id":"s-1","model":"some-other-model"}`),
		json.RawMessage(`{"type":"assistant","message":{"id":"m-1","role":"assistant","model":"some-other-model",` +
			`"content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":103000}}}`),
	}
	terminal := json.RawMessage(`{"type":"result","subtype":"success","result":"done"}`)
	backend := claudeUsageFixtureBackend(t, events, terminal, false)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "fixture", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if got := drainSessionContext(t, session); len(got) != 0 {
		t.Fatalf("expected no observation without a window, got %+v", got)
	}
}

func TestClaudeAssistantConfiguredWindowWinsOverStreamModel(t *testing.T) {
	t.Parallel()

	// The stream reports the base model, which has lost the `[1m]` modifier
	// the run was started with. Measuring against 200K would overstate
	// occupancy fivefold, so the configured string is what counts.
	events := []json.RawMessage{
		json.RawMessage(`{"type":"system","subtype":"init","session_id":"s-1","model":"claude-opus-5-5"}`),
		json.RawMessage(`{"type":"assistant","message":{"id":"m-1","role":"assistant","model":"claude-opus-5-5",` +
			`"content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":300000}}}`),
	}
	terminal := json.RawMessage(`{"type":"result","subtype":"success","result":"done"}`)
	backend := claudeUsageFixtureBackend(t, events, terminal, false)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "fixture", ExecOptions{
		Model:   "claude-opus-5-5[1m]",
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := drainSessionContext(t, session)
	if len(got) != 1 || got[0].LimitTokens != 1_000_000 {
		t.Fatalf("expected the configured 1M window, got %+v", got)
	}
}

func TestClaudeAssistantReportsOccupancyOncePerResponse(t *testing.T) {
	t.Parallel()

	b := &claudeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 20)
	usage := make(map[string]TokenUsage)
	seen := make(map[string]struct{})
	var turns []assistantTurn

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
		turns = append(turns, b.handleAssistant(msg, ch, usage, seen))
	}

	if turns[0].promptTokens != 10_000 {
		t.Fatalf("first block measured %d, want the response's prompt", turns[0].promptTokens)
	}
	// The second block of the same response shares that prompt; measuring it
	// again would report the same occupancy twice.
	if turns[1].promptTokens != 0 {
		t.Fatalf("second block measured %d, want nothing", turns[1].promptTokens)
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

	turn := b.handleAssistant(msg, ch, make(map[string]TokenUsage), make(map[string]struct{}))

	if turn.promptTokens != 0 {
		t.Fatalf("subagent turn measured %d, want nothing", turn.promptTokens)
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

// The window is not in the stream anywhere, but init names the model the CLI
// resolved — including for a run that asked for none, which is how most agents
// are configured. Verified against Claude Code 2.1.330: `claude -p` with no
// --model emits `"subtype":"init","model":"claude-opus-5-5"`.
func TestClaudeResolvesTheWindowFromInitWhenNoModelWasConfigured(t *testing.T) {
	t.Parallel()

	events := []json.RawMessage{
		json.RawMessage(`{"type":"system","subtype":"init","session_id":"s-1","model":"claude-opus-5-5"}`),
		json.RawMessage(`{"type":"assistant","message":{"id":"m-1","role":"assistant","model":"claude-opus-5-5",` +
			`"content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":50000}}}`),
	}
	terminal := json.RawMessage(`{"type":"result","subtype":"success","result":"done"}`)
	backend := claudeUsageFixtureBackend(t, events, terminal, false)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "fixture", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	got := drainSessionContext(t, session)
	if len(got) != 1 {
		t.Fatalf("expected one context event, got %+v", got)
	}
	if got[0].LimitTokens != 200_000 {
		t.Fatalf("limit = %d, want the family default resolved from init", got[0].LimitTokens)
	}
}

// The CLI strips `[1m]` before it reports a model: with --model
// 'claude-opus-5-5[1m]', both init and the assistant event say
// "claude-opus-5-5" (verified against 2.1.330). The configured string is the
// only surviving evidence of the request, so it must win.
func TestClaudeKeepsTheConfiguredWindowOverInitsStrippedModel(t *testing.T) {
	t.Parallel()

	events := []json.RawMessage{
		json.RawMessage(`{"type":"system","subtype":"init","session_id":"s-1","model":"claude-opus-5-5"}`),
		json.RawMessage(`{"type":"assistant","message":{"id":"m-1","role":"assistant","model":"claude-opus-5-5",` +
			`"content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":50000}}}`),
	}
	terminal := json.RawMessage(`{"type":"result","subtype":"success","result":"done"}`)
	backend := claudeUsageFixtureBackend(t, events, terminal, false)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "fixture", ExecOptions{
		Model:   "claude-opus-5-5[1m]",
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := drainSessionContext(t, session)
	if len(got) != 1 || got[0].LimitTokens != 1_000_000 {
		t.Fatalf("context events = %+v, want the configured 1M window", got)
	}
}

// A CLI default carrying `[1m]` with nothing configured resolves to the 200K
// family default. A prompt larger than that disproves it for the whole run:
// 200K was never this run's window, so no measurement may be stated against
// it — not the later smaller ones, and not the held first one either. With
// the CLI never answering, the run therefore reports nothing at all.
func TestClaudeStatesNothingAgainstADisprovedWindow(t *testing.T) {
	t.Parallel()

	events := []json.RawMessage{
		json.RawMessage(`{"type":"system","subtype":"init","session_id":"s-1","model":"claude-opus-5-5"}`),
		json.RawMessage(`{"type":"assistant","message":{"id":"m-1","role":"assistant","model":"claude-opus-5-5",` +
			`"content":[{"type":"text","text":"a"}],"usage":{"input_tokens":150000}}}`),
		json.RawMessage(`{"type":"assistant","message":{"id":"m-2","role":"assistant","model":"claude-opus-5-5",` +
			`"content":[{"type":"text","text":"b"}],"usage":{"input_tokens":250000}}}`),
		json.RawMessage(`{"type":"system","subtype":"init","session_id":"s-1","model":"claude-opus-5-5"}`),
		json.RawMessage(`{"type":"assistant","message":{"id":"m-3","role":"assistant","model":"claude-opus-5-5",` +
			`"content":[{"type":"text","text":"c"}],"usage":{"input_tokens":100000}}}`),
	}
	terminal := json.RawMessage(`{"type":"result","subtype":"success","result":"done"}`)
	backend := claudeUsageFixtureBackend(t, events, terminal, false)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "fixture", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if usages := drainSessionContext(t, session); len(usages) != 0 {
		t.Fatalf("expected nothing stated against a disproved window, got %+v", usages)
	}
}

func TestContextMessageNeedsAWindowTheMeasurementFitsIn(t *testing.T) {
	t.Parallel()

	// 250K of prompt proves the window is not 200K. Nothing true is left to
	// measure against, so there is no observation — a figure pinned at 100%
	// would read as "out of room" when the run may be nowhere near it.
	if _, ok := contextMessage("claude-opus-5-5", 250_000, 200_000); ok {
		t.Fatal("a prompt larger than the window must not be reported")
	}

	// No window at all is the same answer: an absolute token count is not an
	// actionable figure, and the runtimes that could only produce one no
	// longer report at all.
	if _, ok := contextMessage("claude-opus-5-5", 150_000, 0); ok {
		t.Fatal("occupancy with no window must not be reported")
	}

	// A window the prompt fits in is what gets reported.
	msg, ok := contextMessage("claude-opus-5-5", 150_000, 200_000)
	if !ok || msg.Context == nil {
		t.Fatalf("expected an observation, got %+v", msg)
	}
	if msg.Context.UsedTokens != 150_000 || msg.Context.LimitTokens != 200_000 {
		t.Fatalf("context = %+v, want 150000 of 200000", *msg.Context)
	}
}

// drainSessionContext collects the context observations of a finished session.
func drainSessionContext(t *testing.T, session *Session) []ContextInfo {
	t.Helper()
	var got []ContextInfo
	for message := range session.Messages {
		if message.Type == MessageContext && message.Context != nil {
			got = append(got, *message.Context)
		}
	}
	<-session.Result
	return got
}
