package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestNewReturnsMuseBackend(t *testing.T) {
	t.Parallel()
	b, err := New("muse", Config{ExecutablePath: "/nonexistent/muse"})
	if err != nil {
		t.Fatalf("New(muse) error: %v", err)
	}
	if _, ok := b.(*museBackend); !ok {
		t.Fatalf("expected *museBackend, got %T", b)
	}
}

func TestBuildMuseArgsKeepsProtocolManaged(t *testing.T) {
	t.Parallel()
	args := buildMuseArgs(ExecOptions{
		Model:           "muse-spark-1.2",
		ThinkingLevel:   "ultra",
		ResumeSessionID: "sess-123",
		Cwd:             "/tmp/work",
		MaxTurns:        5,
		ExtraArgs:       []string{"--model", "other", "--reasoning-effort", "low", "--workspace", "/evil", "--sandbox-network", "restricted", "--trust-workspace", "--json", "exec", "serve"},
		CustomArgs: []string{
			"--prompt-file", "evil.md", "--model", "evil", "--reasoning-effort", "low",
			"--session-id", "evil", "--workspace", "/evil2", "--max-model-steps", "999",
			"--approval-mode", "ask", "--approval-judge", "off", "--sandbox-network", "restricted",
			"--disable-sandbox", "--provider", "other", "--preset", "evil",
		},
	}, slog.Default(), "/tmp/prompt.md")
	joined := strings.Join(args, " ")
	// Prompt file is daemon-owned.
	if !strings.Contains(joined, "--prompt-file /tmp/prompt.md") {
		t.Fatalf("prompt file missing from %v", args)
	}
	// Daemon-owned flags must appear exactly once with expected values.
	for _, want := range []string{"--model muse-spark-1.2", "--reasoning-effort ultra", "--session-id sess-123", "--workspace /tmp/work", "--max-model-steps 5", "--trust-workspace", "--approval-mode never", "--sandbox-network enabled", "--json"} {
		if strings.Count(joined, want) != 1 {
			t.Fatalf("expected %q once in %v (got %d)", want, args, strings.Count(joined, want))
		}
	}
	// Injected values must not leak.
	for _, forbidden := range []string{"other", "evil", "/evil", "restricted", "ask"} {
		// "other" appears in model but already checked as "other" alone would be ambiguous; check for evil-specific leakage
		if strings.Contains(joined, "--model other") || strings.Contains(joined, "--reasoning-effort low --") || strings.Contains(joined, "evil.md") {
			t.Fatalf("managed argument %q leaked into %v", forbidden, args)
		}
	}
	if strings.Contains(joined, "evil.md") || strings.Contains(joined, "--prompt-file evil") {
		t.Fatalf("evil prompt file leaked into %v", args)
	}
	// Bare subcommand re-issuance must be blocked.
	if strings.Contains(joined, " serve ") || strings.HasSuffix(joined, " serve") {
		t.Fatalf("bare serve leaked into %v", args)
	}
}

func TestBuildMuseArgsRequiresSandboxNetworkBlocked(t *testing.T) {
	t.Parallel()
	args := buildMuseArgs(ExecOptions{ExtraArgs: []string{"--sandbox-network", "restricted"}}, slog.Default(), "/tmp/p.md")
	if strings.Contains(strings.Join(args, " "), "restricted") {
		t.Fatalf("--sandbox-network restricted leaked into %v", args)
	}
}

func TestHandleMuseEventDeltaAndTerminal(t *testing.T) {
	t.Parallel()
	state := museStreamState{usage: make(map[string]TokenUsage)}
	ch := make(chan Message, 8)

	deltaRaw := museRawEvent{PayloadType: "run.output.delta", Payload: json.RawMessage(`{"kind":"run_output_delta","text":"Hello "}`)}
	handleMuseEvent(deltaRaw, ch, &state)
	deltaRaw2 := museRawEvent{PayloadType: "run.output.delta", Payload: json.RawMessage(`{"kind":"run_output_delta","text":"world"}`)}
	handleMuseEvent(deltaRaw2, ch, &state)

	if state.lastAssistantText != "Hello world" || state.assistantEventCount != 2 {
		t.Fatalf("delta handling failed: %+v", state)
	}
	// Drain
	if len(ch) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(ch))
	}

	termRaw := museRawEvent{PayloadType: "run.terminal.completed", Payload: json.RawMessage(`{"kind":"run_terminal","terminal":"completed","text":"Hello world"}`)}
	handleMuseEvent(termRaw, ch, &state)
	if !state.sawResult || state.resultIsError || state.finalResultText != "Hello world" {
		t.Fatalf("terminal handling failed: %+v", state)
	}
}

func TestHandleMuseEventToolUseAndResultCorrelateViaTaskID(t *testing.T) {
	t.Parallel()
	state := museStreamState{usage: make(map[string]TokenUsage)}
	ch := make(chan Message, 16)

	// Side effect intent for a tool: task_id is the canonical correlation id.
	intentPayload := `{"kind":"task_lifecycle","event":{"kind":"side_effect_intent","operation":"tool:write_file","idempotency_key":"tool:call_abc123","task_id":"task-111"}}`
	raw := museRawEvent{PayloadType: "task.lifecycle.side_effect_intent", Payload: json.RawMessage(intentPayload)}
	handleMuseEvent(raw, ch, &state)

	if len(ch) != 1 {
		t.Fatalf("expected 1 ToolUse, got %d", len(ch))
	}
	use := <-ch
	if use.Type != MessageToolUse || use.Tool != "write_file" || use.CallID != "task-111" {
		t.Fatalf("ToolUse = %+v, want tool=write_file call=task-111", use)
	}
	// Mapping must be recorded
	if state.taskIDByCallID["call_abc123"] != "task-111" {
		t.Fatalf("taskIDByCallID not recorded: %+v", state.taskIDByCallID)
	}

	// tool.result arrives with provider call_id, must be remapped to task_id
	toolResultPayload := `{"kind":"tool_result","call_id":"call_abc123","text":"wrote 10 bytes"}`
	raw2 := museRawEvent{PayloadType: "tool.result", Payload: json.RawMessage(toolResultPayload)}
	handleMuseEvent(raw2, ch, &state)

	if len(ch) != 1 {
		t.Fatalf("expected 1 ToolResult, got %d", len(ch))
	}
	res := <-ch
	if res.Type != MessageToolResult || res.CallID != "task-111" || res.Tool != "write_file" || res.Output != "wrote 10 bytes" {
		t.Fatalf("ToolResult = %+v, want call=task-111 tool=write_file", res)
	}

	// task.lifecycle.output chunk for the same task must also correlate
	outputPayload := `{"kind":"task_lifecycle","event":{"kind":"output","chunk":"wrote 10 bytes to /tmp/x","task_id":"task-111"}}`
	raw3 := museRawEvent{PayloadType: "task.lifecycle.output", Payload: json.RawMessage(outputPayload)}
	handleMuseEvent(raw3, ch, &state)
	if len(ch) != 1 {
		t.Fatalf("expected 1 output ToolResult, got %d", len(ch))
	}
	out := <-ch
	if out.CallID != "task-111" || out.Output != "wrote 10 bytes to /tmp/x" {
		t.Fatalf("output ToolResult = %+v", out)
	}
}

func TestHandleMuseEventDeduplicatesToolUse(t *testing.T) {
	t.Parallel()
	state := museStreamState{usage: make(map[string]TokenUsage)}
	ch := make(chan Message, 8)
	payload := `{"kind":"task_lifecycle","event":{"kind":"side_effect_intent","operation":"tool:write_file","idempotency_key":"tool:call_dup","task_id":"task-dup"}}`
	raw := museRawEvent{PayloadType: "task.lifecycle.side_effect_intent", Payload: json.RawMessage(payload)}
	handleMuseEvent(raw, ch, &state)
	handleMuseEvent(raw, ch, &state) // duplicate
	if len(ch) != 1 {
		t.Fatalf("expected dedup to 1, got %d", len(ch))
	}
}

func TestHandleMuseEventRunModelConfigured(t *testing.T) {
	t.Parallel()
	state := museStreamState{usage: make(map[string]TokenUsage)}
	ch := make(chan Message, 2)
	raw := museRawEvent{PayloadType: "run.model.configured", Payload: json.RawMessage(`{"kind":"run_model_configured","model_id":"muse-spark-1.2"}`)}
	handleMuseEvent(raw, ch, &state)
	if state.model != "muse-spark-1.2" {
		t.Fatalf("model = %q, want muse-spark-1.2", state.model)
	}
}

func TestMuseBlockedArgsContainSandboxNetwork(t *testing.T) {
	t.Parallel()
	if _, ok := museBlockedArgs["--sandbox-network"]; !ok {
		t.Fatal("--sandbox-network must be in museBlockedArgs")
	}
	if museBlockedArgs["--sandbox-network"] != blockedWithValue {
		t.Fatalf("--sandbox-network mode wrong: %v", museBlockedArgs["--sandbox-network"])
	}
}

func TestMuseThinkingEnums(t *testing.T) {
	t.Parallel()
	if !ThinkingControlSupported("muse") {
		t.Fatal("ThinkingControlSupported(muse) should be true")
	}
	for _, v := range []string{"none", "minimal", "low", "medium", "high", "xhigh", "ultra"} {
		if !IsKnownThinkingValue("muse", v) {
			t.Fatalf("IsKnownThinkingValue(muse, %q) should be true", v)
		}
	}
	for _, v := range []string{"max", "off", "invalid"} {
		if IsKnownThinkingValue("muse", v) {
			t.Fatalf("IsKnownThinkingValue(muse, %q) should be false", v)
		}
	}
}

func TestMuseListModelsEmptyWithoutSubprocess(t *testing.T) {
	t.Parallel()
	cat, err := ListModels(nilContext(), "muse", Command{Path: "/nonexistent/muse"})
	if err != nil {
		t.Fatalf("muse ListModels should not error, got: %v", err)
	}
	if len(cat.Models) != 0 {
		t.Fatalf("muse ListModels should return empty catalog, got %d models", len(cat.Models))
	}
}

func nilContext() context.Context { return context.Background() }
