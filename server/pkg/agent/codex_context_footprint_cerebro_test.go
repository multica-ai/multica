package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// FIR-1856: a Codex session JSONL carries a cumulative total_token_usage (used
// for cost) and a per-turn last_token_usage. parseCodexSessionFile must keep the
// cumulative in InputTokens/CacheReadTokens AND expose the final turn's footprint
// in ContextInputTokens/ContextCacheReadTokens for the context-window indicator.
func TestParseCodexSessionFile_CapturesLastTurnFootprint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	// Two turns. The lifetime sum grows to 185k; the final turn's prompt is 125k
	// (120k of it cached). The bug pre-fix used the 185k cumulative for the gauge.
	lines := `{"type":"event","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":60000,"cached_input_tokens":50000,"output_tokens":1000},"last_token_usage":{"input_tokens":60000,"cached_input_tokens":50000},"model":"gpt-5.5"}}}
{"type":"event","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":185000,"cached_input_tokens":170000,"output_tokens":3000},"last_token_usage":{"input_tokens":125000,"cached_input_tokens":120000},"model":"gpt-5.5"}}}
`
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}

	got := parseCodexSessionFile(path)
	if got == nil {
		t.Fatal("parseCodexSessionFile returned nil")
	}
	if got.usage.InputTokens != 185000 {
		t.Errorf("cumulative InputTokens = %d, want 185000 (kept for cost)", got.usage.InputTokens)
	}
	if got.usage.ContextInputTokens != 125000 {
		t.Errorf("footprint ContextInputTokens = %d, want 125000 (last turn)", got.usage.ContextInputTokens)
	}
	if got.usage.ContextCacheReadTokens != 120000 {
		t.Errorf("footprint ContextCacheReadTokens = %d, want 120000 (last turn cached)", got.usage.ContextCacheReadTokens)
	}
	if got.model != "gpt-5.5" {
		t.Errorf("model = %q, want gpt-5.5", got.model)
	}
}
