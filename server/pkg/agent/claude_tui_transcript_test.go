//go:build !windows

package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadTranscriptUsage_MissingFile(t *testing.T) {
	if got := readTranscriptUsage(""); len(got) != 0 {
		t.Fatalf("expected empty map for empty path, got %v", got)
	}
	if got := readTranscriptUsage("/no/such/file.jsonl"); len(got) != 0 {
		t.Fatalf("expected empty map for missing file, got %v", got)
	}
}

func TestReadTranscriptUsage_DedupAndAggregate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.jsonl")
	// Two rows with the SAME message.id — claude writes the row again as
	// content blocks accumulate, but the usage counters are shared. The
	// parser must count them once. Then a second message with different id.
	content := `{"type":"attachment"}
{"type":"assistant","message":{"id":"msg_A","model":"opus","usage":{"input_tokens":10,"output_tokens":20,"cache_creation_input_tokens":1000,"cache_read_input_tokens":500}}}
{"type":"assistant","message":{"id":"msg_A","model":"opus","usage":{"input_tokens":10,"output_tokens":20,"cache_creation_input_tokens":1000,"cache_read_input_tokens":500}}}
{"type":"user","message":{"id":"u1"}}
{"type":"assistant","message":{"id":"msg_B","model":"opus","usage":{"input_tokens":5,"output_tokens":7,"cache_creation_input_tokens":0,"cache_read_input_tokens":2000}}}
{"type":"assistant","message":{"id":"msg_C","model":"haiku","usage":{"input_tokens":3,"output_tokens":4,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got := readTranscriptUsage(path)
	if len(got) != 2 {
		t.Fatalf("expected 2 models, got %d: %v", len(got), got)
	}
	opus := got["opus"]
	if opus.InputTokens != 15 || opus.OutputTokens != 27 || opus.CacheWriteTokens != 1000 || opus.CacheReadTokens != 2500 {
		t.Errorf("opus aggregation wrong: %+v", opus)
	}
	haiku := got["haiku"]
	if haiku.InputTokens != 3 || haiku.OutputTokens != 4 {
		t.Errorf("haiku aggregation wrong: %+v", haiku)
	}
}

func TestReadTranscriptUsage_RealFixture(t *testing.T) {
	// Smoke test against a transcript shape from the field. Uses the same
	// JSON keys claude actually writes. Verifies that an assistant row
	// missing message.id (shouldn't happen, but parser must not crash) is
	// silently skipped instead of double-counting.
	dir := t.TempDir()
	path := filepath.Join(dir, "real.jsonl")
	content := `{"type":"assistant","message":{"id":"","model":"opus","usage":{"input_tokens":1,"output_tokens":1}}}
{"type":"assistant","message":{"id":"msg_X","model":"opus","usage":{"input_tokens":6,"cache_creation_input_tokens":24662,"cache_read_input_tokens":18411,"output_tokens":99}}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := readTranscriptUsage(path)
	if len(got) != 1 {
		t.Fatalf("expected 1 model, got %d: %v", len(got), got)
	}
	u := got["opus"]
	if u.InputTokens != 6 || u.OutputTokens != 99 || u.CacheWriteTokens != 24662 || u.CacheReadTokens != 18411 {
		t.Errorf("real fixture wrong: %+v", u)
	}
}
