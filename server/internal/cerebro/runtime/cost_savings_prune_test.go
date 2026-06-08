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
// COMPOUNDING chars pruned (counted once per subsequent model call), not the
// full tool-result surface.
func TestMeasureRun_PruneOnUsesActuallyPrunedChars(t *testing.T) {
	got := measureRun(map[string]string{savingPruneToolResults: savingModeOn},
		CostSavingRunFacts{ToolResultChars: 4000, PrunedContextCharsCompounded: 800}, "")
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

// FIR-2404: a run can legitimately call tools without producing any prunable
// round (the agent fired one tool call and then answered, so nothing was ever
// superseded). When prune is "on" for that run we still write a measurement
// row — Baseline=0/Effective=0/SavedCents=0 — because the run is part of the
// "on" arm's denominator. Dropping the row would bias the dashboard's
// treatment-vs-control comparison: only runs that happened to save chars would
// count as treatment runs, and the holdout A/B average would be inflated.
func TestMeasureRun_PruneOnWritesZeroRowWhenNothingPruned(t *testing.T) {
	got := measureRun(map[string]string{savingPruneToolResults: savingModeOn},
		CostSavingRunFacts{ToolResultChars: 400, PrunedToolResultChars: 0}, "")
	m, ok := findMeasurement(got, savingPruneToolResults)
	if !ok {
		t.Fatalf("prune measurement missing — on-mode runs must always record a row: %+v", got)
	}
	if !m.Applied {
		t.Errorf("on mode must be applied even when nothing was pruned")
	}
	if m.Metric != metricContextTokens || m.Baseline != 0 || m.Effective != 0 || m.SavedCents != 0 {
		t.Errorf("zero-prune on measurement = %+v, want metric=context_tokens baseline=0 effective=0 saved_cents=0", m)
	}
}

// FIR-2404: prune_tool_results does not overlap with snapshot_prompt or
// bundled_read. snapshot/bundled measure inlined context (context_tokens),
// prune measures
// PrunedToolResultChars/ToolResultChars (transcript characters dropped
// mid-run). Different facts, different metrics, different lifecycle stage. A
// single gateway run can be measured by all three simultaneously without any
// double counting, and prune's value is unaffected by InlinedContextReads.
//
// This is the explicit "no overlap" guarantee from the issue's precedence
// question — the equivalent of the (bundled × snapshot) table from step 2.
func TestMeasureRun_PruneIsIndependentOfSnapshotAndBundled(t *testing.T) {
	modes := map[string]string{
		savingSnapshotPrompt:   savingModeOn,
		savingBundledRead:      savingModeOn,
		savingPruneToolResults: savingModeOn,
	}
	facts := CostSavingRunFacts{
		InlinedContextReads:          3,
		InlinedContextChars:          12_000,
		ToolResultChars:              400,
		PrunedContextCharsCompounded: 200,
	}
	got := measureRun(modes, facts, "")

	snap, snapOK := findMeasurement(got, savingSnapshotPrompt)
	bun, bunOK := findMeasurement(got, savingBundledRead)
	prune, pruneOK := findMeasurement(got, savingPruneToolResults)
	if !snapOK || !bunOK || !pruneOK {
		t.Fatalf("expected snapshot+bundled+prune all measured, got %+v", got)
	}

	// All three score saved context in context_tokens on the dashboard (FIR-2572).
	if snap.Metric != metricContextTokens || bun.Metric != metricContextTokens {
		t.Errorf("snapshot/bundled must measure context_tokens; got snap=%q bundled=%q", snap.Metric, bun.Metric)
	}
	if prune.Metric != metricContextTokens {
		t.Errorf("prune must measure context_tokens, got %q (would imply overlap with snapshot/bundled)", prune.Metric)
	}

	// Prune's measurement is driven only by its own facts
	// (PrunedContextCharsCompounded when applied). It must NOT see
	// InlinedContextReads — 200/4 = 50 tokens,
	// nothing influenced by the 3 inlined reads above.
	if prune.Baseline != 50 || prune.Effective != 0 {
		t.Errorf("prune baseline/effective = %d/%d, want 50/0 (no contamination from InlinedContextReads)", prune.Baseline, prune.Effective)
	}

	// And snapshot/bundled must NOT see ToolResultChars.
	wantSnap := snapshotSavedContextTokens(facts)
	wantBun := bundledSavedContextTokens(facts.InlinedContextChars, facts)
	if snap.Baseline != wantSnap || snap.Effective != 0 {
		t.Errorf("snapshot baseline/effective = %d/%d, want %d/0", snap.Baseline, snap.Effective, wantSnap)
	}
	if bun.Baseline != wantBun || bun.Effective != 0 {
		t.Errorf("bundled baseline/effective = %d/%d, want %d/0", bun.Baseline, bun.Effective, wantBun)
	}
}
