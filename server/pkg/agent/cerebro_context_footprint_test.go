package agent

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestLastTurnFootprint_LastTurnWinsNotSum(t *testing.T) {
	var lt lastTurnFootprint
	// Three turns, each re-reading a growing cached prefix. The cumulative SUM
	// (what task_usage stores) would be 150+250+350 = 750k, far over a window;
	// the footprint must be the LAST turn alone.
	lt.observe("claude-opus-4-8[1m]", 150_000, 140_000)
	lt.observe("claude-opus-4-8[1m]", 250_000, 240_000)
	lt.observe("claude-opus-4-8[1m]", 350_000, 330_000)

	var u TokenUsage
	lt.apply(&u)
	if u.ContextInputTokens != 350_000 {
		t.Fatalf("ContextInputTokens = %d, want 350000 (last turn, not the 750000 sum)", u.ContextInputTokens)
	}
	if u.ContextCacheReadTokens != 330_000 {
		t.Fatalf("ContextCacheReadTokens = %d, want 330000", u.ContextCacheReadTokens)
	}
}

func TestLastTurnFootprint_ZeroPromptIgnored(t *testing.T) {
	var lt lastTurnFootprint
	lt.observe("m", 200_000, 180_000)
	lt.observe("m", 0, 0) // a trailing usage-less event must not blank the real footprint
	var u TokenUsage
	lt.apply(&u)
	if u.ContextInputTokens != 200_000 {
		t.Fatalf("ContextInputTokens = %d, want 200000 (zero event ignored)", u.ContextInputTokens)
	}
}

func TestLastTurnFootprint_ApplyNoopWhenUnset(t *testing.T) {
	var lt lastTurnFootprint
	u := TokenUsage{ContextInputTokens: 42}
	lt.apply(&u)
	if u.ContextInputTokens != 42 {
		t.Fatalf("apply on unset footprint mutated usage: %d", u.ContextInputTokens)
	}
}

func TestLastTurnFootprint_ApplyToMap(t *testing.T) {
	var lt lastTurnFootprint
	lt.observe("claude-sonnet-4-6", 120_000, 100_000)
	usage := map[string]TokenUsage{"claude-sonnet-4-6": {InputTokens: 9}}
	lt.applyToMap(usage)
	got := usage["claude-sonnet-4-6"]
	if got.ContextInputTokens != 120_000 || got.ContextCacheReadTokens != 100_000 {
		t.Fatalf("applyToMap: got %+v", got)
	}
	if got.InputTokens != 9 {
		t.Fatalf("applyToMap clobbered cumulative InputTokens: %d", got.InputTokens)
	}
}

// FIR-1931 regression: the footprint is observed from the streamed assistant
// messages with the PLAIN model id, but the result message keys per-model usage
// with the gateway's [1m] long-context tag. The exact-key lookup misses, so
// applyToMap must fall back to the single usage entry whose base model matches —
// otherwise [1m] runs lose the footprint and the bar shows "estimate".
func TestLastTurnFootprint_ApplyToMap_TaggedKeyMismatch(t *testing.T) {
	var lt lastTurnFootprint
	lt.observe("claude-opus-4-8", 238_000, 192_000) // plain id, as streamed
	usage := map[string]TokenUsage{
		"claude-opus-4-8[1m]": {InputTokens: 10_778}, // tagged id, as keyed by result
	}
	lt.applyToMap(usage)
	got := usage["claude-opus-4-8[1m]"]
	if got.ContextInputTokens != 238_000 || got.ContextCacheReadTokens != 192_000 {
		t.Fatalf("tagged-key footprint not applied: got %+v", got)
	}
	if got.InputTokens != 10_778 {
		t.Fatalf("applyToMap clobbered cumulative InputTokens: %d", got.InputTokens)
	}
	if _, ok := usage["claude-opus-4-8"]; ok {
		t.Fatalf("applyToMap created a spurious plain-id entry instead of matching the tagged one")
	}
}

// Safety: when more than one model entry shares the base, the ambiguous match is
// skipped rather than guessing — the footprint stays on the cumulative fallback.
func TestLastTurnFootprint_ApplyToMap_AmbiguousBaseSkipped(t *testing.T) {
	var lt lastTurnFootprint
	lt.observe("claude-opus-4-8", 200_000, 150_000)
	usage := map[string]TokenUsage{
		"claude-opus-4-8[1m]":  {InputTokens: 1},
		"claude-opus-4-8[exp]": {InputTokens: 2},
	}
	lt.applyToMap(usage)
	for k, v := range usage {
		if v.ContextInputTokens != 0 {
			t.Fatalf("ambiguous base should skip apply, but %s got footprint %d", k, v.ContextInputTokens)
		}
	}
}

// stripModelCapabilityTag strips only a single trailing bracketed tag.
func TestStripModelCapabilityTag(t *testing.T) {
	cases := map[string]string{
		"claude-opus-4-8[1m]":  "claude-opus-4-8",
		"claude-opus-4-8":      "claude-opus-4-8",
		"claude-opus-4-8[exp]": "claude-opus-4-8",
		"":                     "",
	}
	for in, want := range cases {
		if got := stripModelCapabilityTag(in); got != want {
			t.Fatalf("stripModelCapabilityTag(%q) = %q, want %q", in, got, want)
		}
	}
}

// End-to-end through claude's handleAssistant: the footprint must equal the LAST
// assistant turn's whole prompt (input + cache_read + cache_creation), even though
// the per-turn cumulative usage keeps summing.
func TestClaudeHandleAssistant_FootprintIsLastTurn(t *testing.T) {
	b := &claudeBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 32)
	var output strings.Builder
	usage := make(map[string]TokenUsage)
	var lastTurn lastTurnFootprint

	turn := func(input, cacheRead, cacheCreate int64) claudeSDKMessage {
		body, _ := json.Marshal(map[string]any{
			"role":  "assistant",
			"model": "claude-opus-4-8[1m]",
			"content": []map[string]any{
				{"type": "text", "text": "hi"},
			},
			"usage": map[string]any{
				"input_tokens":                input,
				"output_tokens":               10,
				"cache_read_input_tokens":     cacheRead,
				"cache_creation_input_tokens": cacheCreate,
			},
		})
		return claudeSDKMessage{Type: "assistant", Message: body}
	}

	b.handleAssistant(turn(5_000, 100_000, 20_000), ch, &output, usage, &lastTurn)
	b.handleAssistant(turn(4_000, 380_000, 6_000), ch, &output, usage, &lastTurn)

	// Cumulative still sums (cost truth): cache_read 100k+380k = 480k.
	if got := usage["claude-opus-4-8[1m]"].CacheReadTokens; got != 480_000 {
		t.Fatalf("cumulative CacheReadTokens = %d, want 480000", got)
	}
	// Footprint = last turn only: 4000 + 380000 + 6000 = 390000.
	lastTurn.applyToMap(usage)
	got := usage["claude-opus-4-8[1m]"]
	if got.ContextInputTokens != 390_000 {
		t.Fatalf("ContextInputTokens = %d, want 390000 (last turn whole prompt)", got.ContextInputTokens)
	}
	if got.ContextCacheReadTokens != 380_000 {
		t.Fatalf("ContextCacheReadTokens = %d, want 380000", got.ContextCacheReadTokens)
	}
}
