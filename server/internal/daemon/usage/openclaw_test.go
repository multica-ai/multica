package usage

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestParseOpenClawFile(t *testing.T) {
	tmp := t.TempDir()

	// Real OpenClaw session JSONL with session header, model_change, and assistant messages
	content := `{"type":"session","version":3,"id":"multica-test","timestamp":"2026-04-11T13:53:05.847Z"}
{"type":"model_change","id":"03c18aae","timestamp":"2026-04-11T13:53:05.855Z","provider":"deepseek","modelId":"deepseek-chat"}
{"type":"message","id":"162ce1b7","parentId":"c90ecabe","timestamp":"2026-04-11T13:53:09.986Z","message":{"role":"assistant","content":[{"type":"text","text":"I'll start by getting the issue details."}],"api":"openai-completions","provider":"deepseek","model":"deepseek-chat","usage":{"input":133,"output":81,"cacheRead":16448,"cacheWrite":0,"totalTokens":16662}}}
{"type":"message","id":"3c063300","parentId":"50e4feb6","timestamp":"2026-04-11T13:53:14.750Z","message":{"role":"assistant","content":[{"type":"text","text":"Let me check the workspace."}],"provider":"deepseek","model":"deepseek-chat","usage":{"input":286,"output":94,"cacheRead":16448,"cacheWrite":0}}}
{"type":"message","id":"user001","timestamp":"2026-04-11T13:54:00.000Z","message":{"role":"user","content":[{"type":"text","text":"hello"}]}}
`

	filePath := filepath.Join(tmp, "session.jsonl")
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewScanner(slog.Default())
	records := s.parseOpenClawFile(filePath)

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	r := records[0]
	if r.Provider != "openclaw" {
		t.Errorf("provider = %q, want %q", r.Provider, "openclaw")
	}
	if r.Model != "deepseek/deepseek-chat" {
		t.Errorf("model = %q, want %q", r.Model, "deepseek/deepseek-chat")
	}
	if r.InputTokens != 133 {
		t.Errorf("input_tokens = %d, want %d", r.InputTokens, 133)
	}
	if r.OutputTokens != 81 {
		t.Errorf("output_tokens = %d, want %d", r.OutputTokens, 81)
	}
	if r.CacheReadTokens != 16448 {
		t.Errorf("cache_read_tokens = %d, want %d", r.CacheReadTokens, 16448)
	}
	if r.Date != "2026-04-11" {
		t.Errorf("date = %q, want %q", r.Date, "2026-04-11")
	}
}

func TestParseOpenClawFile_NoUsage(t *testing.T) {
	tmp := t.TempDir()

	// Session with no assistant messages containing usage
	content := `{"type":"session","version":3,"id":"empty-session","timestamp":"2026-04-11T13:53:05.847Z"}
{"type":"message","id":"user001","timestamp":"2026-04-11T13:54:00.000Z","message":{"role":"user","content":[{"type":"text","text":"hello"}]}}
`

	filePath := filepath.Join(tmp, "session.jsonl")
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewScanner(slog.Default())
	records := s.parseOpenClawFile(filePath)

	if len(records) != 0 {
		t.Errorf("expected 0 records, got %d", len(records))
	}
}

func TestNormalizeOpenClawModel(t *testing.T) {
	tests := []struct {
		provider string
		model    string
		want     string
	}{
		{"deepseek", "deepseek-chat", "deepseek/deepseek-chat"},
		{"anthropic", "claude-sonnet-4-5", "anthropic/claude-sonnet-4-5"},
		{"", "gpt-4o", "gpt-4o"},
		{"openai", "openai/gpt-4o", "openai/gpt-4o"}, // already has /
	}

	for _, tt := range tests {
		got := normalizeOpenClawModel(tt.provider, tt.model)
		if got != tt.want {
			t.Errorf("normalizeOpenClawModel(%q, %q) = %q, want %q", tt.provider, tt.model, got, tt.want)
		}
	}
}
