package usage

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestParseOpenCodeFile(t *testing.T) {
	tmp := t.TempDir()

	// Real OpenCode message JSON format with token usage
	content := `{
		"id": "msg_test001",
		"sessionID": "ses_test001",
		"role": "assistant",
		"time": {"created": 1768332037518, "completed": 1768332039410},
		"modelID": "claude-sonnet-4-5",
		"providerID": "anthropic",
		"tokens": {"input": 10916, "output": 5, "reasoning": 100, "cache": {"read": 448, "write": 50}}
	}`

	filePath := filepath.Join(tmp, "msg_test001.json")
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewScanner(slog.Default())
	record := s.parseOpenCodeFile(filePath)

	if record == nil {
		t.Fatal("expected non-nil record")
	}

	if record.Provider != "opencode" {
		t.Errorf("provider = %q, want %q", record.Provider, "opencode")
	}
	if record.Model != "claude-sonnet-4-5" {
		t.Errorf("model = %q, want %q", record.Model, "claude-sonnet-4-5")
	}
	if record.InputTokens != 10916 {
		t.Errorf("input_tokens = %d, want %d", record.InputTokens, 10916)
	}
	// output_tokens + reasoning
	if record.OutputTokens != 105 {
		t.Errorf("output_tokens = %d, want %d", record.OutputTokens, 105)
	}
	if record.CacheReadTokens != 448 {
		t.Errorf("cache_read_tokens = %d, want %d", record.CacheReadTokens, 448)
	}
	if record.CacheWriteTokens != 50 {
		t.Errorf("cache_write_tokens = %d, want %d", record.CacheWriteTokens, 50)
	}
}

func TestParseOpenCodeFile_UserMessage(t *testing.T) {
	tmp := t.TempDir()

	// User messages should be ignored (no token usage to report)
	content := `{
		"id": "msg_user001",
		"sessionID": "ses_test001",
		"role": "user",
		"time": {"created": 1768332037000}
	}`

	filePath := filepath.Join(tmp, "msg_user001.json")
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewScanner(slog.Default())
	record := s.parseOpenCodeFile(filePath)

	if record != nil {
		t.Errorf("expected nil record for user message, got %+v", record)
	}
}

func TestParseOpenCodeFile_NoCache(t *testing.T) {
	tmp := t.TempDir()

	// Message without cache field
	content := `{
		"id": "msg_test002",
		"sessionID": "ses_test002",
		"role": "assistant",
		"time": {"created": 1768332037518},
		"modelID": "gpt-4o",
		"providerID": "openai",
		"tokens": {"input": 500, "output": 200, "reasoning": 0}
	}`

	filePath := filepath.Join(tmp, "msg_test002.json")
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewScanner(slog.Default())
	record := s.parseOpenCodeFile(filePath)

	if record == nil {
		t.Fatal("expected non-nil record")
	}
	if record.CacheReadTokens != 0 {
		t.Errorf("cache_read_tokens = %d, want 0", record.CacheReadTokens)
	}
	if record.CacheWriteTokens != 0 {
		t.Errorf("cache_write_tokens = %d, want 0", record.CacheWriteTokens)
	}
	if record.Model != "gpt-4o" {
		t.Errorf("model = %q, want %q", record.Model, "gpt-4o")
	}
}

func TestParseOpenCodeFile_ZeroTokens(t *testing.T) {
	tmp := t.TempDir()

	// Message with zero tokens should return nil
	content := `{
		"id": "msg_test003",
		"sessionID": "ses_test003",
		"role": "assistant",
		"time": {"created": 1768332037518},
		"modelID": "test-model",
		"tokens": {"input": 0, "output": 0, "reasoning": 0}
	}`

	filePath := filepath.Join(tmp, "msg_test003.json")
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewScanner(slog.Default())
	record := s.parseOpenCodeFile(filePath)

	if record != nil {
		t.Errorf("expected nil record for zero tokens, got %+v", record)
	}
}
