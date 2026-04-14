package usage

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestParseHermesFile(t *testing.T) {
	tmp := t.TempDir()

	// Hermes session JSONL with usage_update entries (cumulative snapshots)
	content := `{"type":"session_start","timestamp":"2026-04-10T14:00:00.000Z","model":"claude-sonnet-4-5"}
{"type":"usage_update","timestamp":"2026-04-10T14:01:00.000Z","model":"claude-sonnet-4-5","usage":{"inputTokens":1000,"outputTokens":200,"cachedReadTokens":500,"thoughtTokens":50}}
{"type":"usage_update","timestamp":"2026-04-10T14:02:00.000Z","model":"claude-sonnet-4-5","usage":{"inputTokens":3000,"outputTokens":600,"cachedReadTokens":1500,"thoughtTokens":100}}
`

	filePath := filepath.Join(tmp, "session-001.jsonl")
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewScanner(slog.Default())
	record := s.parseHermesFile(filePath)

	if record == nil {
		t.Fatal("expected non-nil record")
	}

	if record.Provider != "hermes" {
		t.Errorf("provider = %q, want %q", record.Provider, "hermes")
	}
	if record.Model != "claude-sonnet-4-5" {
		t.Errorf("model = %q, want %q", record.Model, "claude-sonnet-4-5")
	}
	if record.Date != "2026-04-10" {
		t.Errorf("date = %q, want %q", record.Date, "2026-04-10")
	}
	// Should take the last (cumulative) snapshot
	if record.InputTokens != 3000 {
		t.Errorf("input_tokens = %d, want %d", record.InputTokens, 3000)
	}
	// output_tokens + thought_tokens
	if record.OutputTokens != 700 {
		t.Errorf("output_tokens = %d, want %d (600 + 100)", record.OutputTokens, 700)
	}
	if record.CacheReadTokens != 1500 {
		t.Errorf("cache_read_tokens = %d, want %d", record.CacheReadTokens, 1500)
	}
}

func TestParseHermesFile_NoUsage(t *testing.T) {
	tmp := t.TempDir()

	content := `{"type":"session_start","timestamp":"2026-04-10T14:00:00.000Z","model":"test-model"}
{"type":"message","timestamp":"2026-04-10T14:01:00.000Z","content":"hello"}
`

	filePath := filepath.Join(tmp, "session-empty.jsonl")
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewScanner(slog.Default())
	record := s.parseHermesFile(filePath)

	if record != nil {
		t.Errorf("expected nil record for no usage data, got %+v", record)
	}
}

func TestParseHermesFile_SingleUsage(t *testing.T) {
	tmp := t.TempDir()

	content := `{"type":"usage_update","timestamp":"2026-04-10T14:01:00.000Z","model":"hermes-3","usage":{"inputTokens":500,"outputTokens":100,"cachedReadTokens":0,"thoughtTokens":0}}
`

	filePath := filepath.Join(tmp, "session-single.jsonl")
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewScanner(slog.Default())
	record := s.parseHermesFile(filePath)

	if record == nil {
		t.Fatal("expected non-nil record")
	}
	if record.InputTokens != 500 {
		t.Errorf("input_tokens = %d, want %d", record.InputTokens, 500)
	}
	if record.OutputTokens != 100 {
		t.Errorf("output_tokens = %d, want %d", record.OutputTokens, 100)
	}
	if record.Model != "hermes-3" {
		t.Errorf("model = %q, want %q", record.Model, "hermes-3")
	}
}
