package runtime

import (
	"strings"
	"testing"
)

// Two completed tool rounds: the first round's results are superseded once the
// second round runs, so pruning must drop round 1 and keep round 2 verbatim.
func TestPruneSupersededGatewayToolResults_KeepsLatestRound(t *testing.T) {
	history := []GatewayMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "go"},
		{Role: "assistant", ToolCalls: []GatewayToolCall{{ID: "a"}}},
		{Role: "tool", ToolCallID: "a", Content: "round-1 result body"},
		{Role: "assistant", ToolCalls: []GatewayToolCall{{ID: "b"}}},
		{Role: "tool", ToolCallID: "b", Content: "round-2 result body"},
	}

	removed := pruneSupersededGatewayToolResults(history)
	if removed != int64(len("round-1 result body")) {
		t.Fatalf("removed = %d, want %d", removed, len("round-1 result body"))
	}
	if !isPrunedToolResult(history[3].Content) {
		t.Errorf("round-1 tool result should be pruned, got %q", history[3].Content)
	}
	if history[5].Content != "round-2 result body" {
		t.Errorf("latest round must be kept verbatim, got %q", history[5].Content)
	}

	// Idempotent: a second pass removes nothing more.
	if again := pruneSupersededGatewayToolResults(history); again != 0 {
		t.Errorf("second prune removed %d, want 0 (idempotent)", again)
	}
}

// Only one round of results exists, so nothing is superseded yet.
func TestPruneSupersededGatewayToolResults_SingleRoundNoOp(t *testing.T) {
	history := []GatewayMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "go"},
		{Role: "assistant", ToolCalls: []GatewayToolCall{{ID: "a"}}},
		{Role: "tool", ToolCallID: "a", Content: "only result"},
	}
	if removed := pruneSupersededGatewayToolResults(history); removed != 0 {
		t.Fatalf("removed = %d, want 0 for a single round", removed)
	}
	if history[3].Content != "only result" {
		t.Errorf("single round must be untouched, got %q", history[3].Content)
	}
}

func TestPruneSupersededAnthropicToolResults_KeepsLatestRound(t *testing.T) {
	toolResultUser := func(text string) AnthropicMessage {
		return AnthropicMessage{Role: "user", Content: []AnthropicContentBlock{
			{Type: "tool_result", ToolUseID: "x", Content: []AnthropicContentBlock{{Type: "text", Text: text}}},
		}}
	}
	history := []AnthropicMessage{
		{Role: "user", Content: "go"},
		{Role: "assistant", Content: []AnthropicContentBlock{{Type: "tool_use", ID: "a"}}},
		toolResultUser("round-1 body"),
		{Role: "assistant", Content: []AnthropicContentBlock{{Type: "tool_use", ID: "b"}}},
		toolResultUser("round-2 body"),
	}

	removed := pruneSupersededAnthropicToolResults(history)
	if removed != int64(len("round-1 body")) {
		t.Fatalf("removed = %d, want %d", removed, len("round-1 body"))
	}
	got1 := history[2].Content.([]AnthropicContentBlock)[0].Content[0].Text
	if !isPrunedToolResult(got1) {
		t.Errorf("round-1 tool result should be pruned, got %q", got1)
	}
	got2 := history[4].Content.([]AnthropicContentBlock)[0].Content[0].Text
	if got2 != "round-2 body" {
		t.Errorf("latest round must be kept verbatim, got %q", got2)
	}
}

func TestPrunedToolResultPlaceholder_IsRecognised(t *testing.T) {
	p := prunedToolResultPlaceholder(42)
	if !strings.Contains(p, "42") || !isPrunedToolResult(p) {
		t.Errorf("placeholder %q not well-formed / not recognised", p)
	}
}

// When prune_tool_results is "on" (applied), the measurement must report the
// chars ACTUALLY pruned, not the full tool-result surface.
func TestMeasureRun_PruneOnUsesActuallyPrunedChars(t *testing.T) {
	got := measureRun(map[string]string{savingPruneToolResults: savingModeOn},
		CostSavingRunFacts{ToolResultChars: 4000, PrunedToolResultChars: 800}, "")
	m, ok := findMeasurement(got, savingPruneToolResults)
	if !ok {
		t.Fatalf("prune measurement missing: %+v", got)
	}
	if !m.Applied {
		t.Errorf("on mode must be applied")
	}
	// 800 chars / 4 chars-per-token = 200 tokens (the measured saving), not the
	// 1000 tokens the full 4000-char surface would imply.
	if m.Baseline != 200 || m.Effective != 0 {
		t.Errorf("prune-on measurement = %+v, want baseline=200 effective=0", m)
	}
}
