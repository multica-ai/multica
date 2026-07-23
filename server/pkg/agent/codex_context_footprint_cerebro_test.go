package agent

import (
	"fmt"
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

func TestScanCodexSessionUsageMatchesActiveSession(t *testing.T) {
	// CEREBRO-PATCH(codex-session-bound-usage-test): FIR-3337 regression proof
	// for two Codex tasks writing transcripts concurrently.
	start := time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC)
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)

	dir := filepath.Join(codexHome, "sessions", "2026", "07", "16")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeSession := func(name, sessionID string, input int64) {
		t.Helper()
		path := filepath.Join(dir, name)
		lines := `{"type":"session_meta","payload":{"id":"` + sessionID + `"}}
{"type":"event","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":` + fmt.Sprint(input) + `,"output_tokens":50},"last_token_usage":{"input_tokens":` + fmt.Sprint(input) + `},"model":"gpt-5.6-sol"}}}
`
		if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
			t.Fatal(err)
		}
		modTime := start.Add(time.Minute)
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatal(err)
		}
	}

	writeSession("a-active.jsonl", "thread-active", 99_000)
	writeSession("z-other.jsonl", "thread-other", 777_000)

	got := scanCodexSessionUsage(start, "thread-active")
	if got == nil {
		t.Fatal("scanCodexSessionUsage returned nil")
	}
	if got.usage.InputTokens != 99_000 {
		t.Fatalf("InputTokens = %d, want active session's 99000", got.usage.InputTokens)
	}
}

func TestScanCodexSessionUsageFallsBackWhenSessionMetadataIsMissing(t *testing.T) {
	start := time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC)
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)

	dir := filepath.Join(codexHome, "sessions", "2026", "07", "16")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "metadata-missing.jsonl")
	lines := `{"type":"event","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":55000,"output_tokens":50},"last_token_usage":{"input_tokens":55000},"model":"gpt-5.6-sol"}}}
`
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	modTime := start.Add(time.Minute)
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}

	got := scanCodexSessionUsage(start, "thread-active")
	if got == nil {
		t.Fatal("scanCodexSessionUsage returned nil, want mtime fallback for transcript without session metadata")
	}
	if got.usage.InputTokens != 55_000 {
		t.Fatalf("InputTokens = %d, want fallback transcript's 55000", got.usage.InputTokens)
	}
}

func TestScanCodexSessionUsageFallsBackToNewestTranscriptWhenSessionDoesNotMatch(t *testing.T) {
	start := time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC)
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)

	dir := filepath.Join(codexHome, "sessions", "2026", "07", "16")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeSession := func(name, sessionID string, input int64, modTime time.Time) {
		t.Helper()
		path := filepath.Join(dir, name)
		lines := `{"type":"session_meta","payload":{"id":"` + sessionID + `"}}
{"type":"event","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":` + fmt.Sprint(input) + `,"output_tokens":50},"last_token_usage":{"input_tokens":` + fmt.Sprint(input) + `},"model":"gpt-5.6-sol"}}}
`
		if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatal(err)
		}
	}

	writeSession("z-older.jsonl", "thread-old", 11_000, start.Add(time.Minute))
	writeSession("a-newer.jsonl", "thread-new", 22_000, start.Add(2*time.Minute))

	got := scanCodexSessionUsage(start, "thread-missing")
	if got == nil {
		t.Fatal("scanCodexSessionUsage returned nil, want newest mtime fallback")
	}
	if got.usage.InputTokens != 22_000 {
		t.Fatalf("InputTokens = %d, want newest fallback transcript's 22000", got.usage.InputTokens)
	}
}

// CEREBRO-PATCH(codex-call-usage-events-test): FIR-3337 proves that Codex's
// per-call deltas, real context window, and explicit compaction survive parsing.
func TestParseCodexSessionFileBuildsCallEventsAndCompaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	lines := `{"timestamp":"2026-07-16T10:00:00Z","type":"session_meta","payload":{"id":"session-1"}}
{"timestamp":"2026-07-16T10:00:01Z","type":"turn_context","payload":{"model":"gpt-5.6-sol"}}
{"timestamp":"2026-07-16T10:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100000,"output_tokens":500,"cached_input_tokens":80000},"last_token_usage":{"input_tokens":100000,"output_tokens":500,"cached_input_tokens":80000},"model_context_window":1050000,"model":"gpt-5.6-sol"}}}
{"timestamp":"2026-07-16T10:00:03Z","type":"compacted","payload":{"message":"","replacement_history":[]}}
{"timestamp":"2026-07-16T10:00:04Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":130000,"output_tokens":700,"cached_input_tokens":100000},"last_token_usage":{"input_tokens":30000,"output_tokens":200,"cached_input_tokens":20000},"model_context_window":1050000,"model":"gpt-5.6-sol"}}}
`
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}

	got := parseCodexSessionFile(path)
	if got == nil {
		t.Fatal("parseCodexSessionFile returned nil")
	}
	if len(got.events) != 3 {
		t.Fatalf("events = %+v, want two calls and one compaction", got.events)
	}
	first := got.events[0]
	if first.InputTokens != 20_000 || first.CacheReadTokens != 80_000 || first.OutputTokens != 500 {
		t.Fatalf("first call tokens = %+v", first)
	}
	if first.ContextTokens != 100_000 || first.ContextWindowTokens != 1_050_000 {
		t.Fatalf("first call context = %+v", first)
	}
	if got.events[1].CompactionKind != ModelUsageCompactionProviderExplicit {
		t.Fatalf("compaction event = %+v", got.events[1])
	}
	if got.events[2].Sequence != 3 || got.events[2].CounterSemantics != ModelUsageCounterDelta {
		t.Fatalf("second call = %+v", got.events[2])
	}
}
