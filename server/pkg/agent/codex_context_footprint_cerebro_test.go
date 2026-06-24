package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// FIR-1856: a Codex session JSONL carries a cumulative total_token_usage (used
// for cost) and a per-turn last_token_usage. parseCodexSessionFile must keep the
// cumulative non-cached/cache split in InputTokens/CacheReadTokens AND expose the
// final turn's full prompt footprint in ContextInputTokens/ContextCacheReadTokens.
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
	if got.usage.InputTokens != 15000 {
		t.Errorf("cumulative InputTokens = %d, want 15000 non-cached tokens", got.usage.InputTokens)
	}
	if got.usage.CacheReadTokens != 170000 {
		t.Errorf("cumulative CacheReadTokens = %d, want 170000", got.usage.CacheReadTokens)
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

// CEREBRO-PATCH(agent-codex-cache-accounting-test): FIR-1113 prove Codex session fallback scans the date path and normalizes cached input.
func TestScanCodexSessionUsageUsesYearMonthDayPath(t *testing.T) {
	start := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)

	dir := filepath.Join(codexHome, "sessions", "2026", "06", "23")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "session.jsonl")
	lines := `{"type":"event","payload":{"type":"turn_context","model":"gpt-5.5"}}
{"type":"event","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1000,"cached_input_tokens":700,"output_tokens":50},"last_token_usage":{"input_tokens":1000,"cached_input_tokens":700},"model":"gpt-5.5"}}}
`
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	modTime := start.Add(time.Minute)
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}

	got := scanCodexSessionUsage(start)
	if got == nil {
		t.Fatal("scanCodexSessionUsage returned nil")
	}
	if got.model != "gpt-5.5" {
		t.Fatalf("model = %q, want gpt-5.5", got.model)
	}
	if got.usage.InputTokens != 300 || got.usage.CacheReadTokens != 700 || got.usage.OutputTokens != 50 {
		t.Fatalf("usage = %+v, want non-cached input 300, cache read 700, output 50", got.usage)
	}
}
