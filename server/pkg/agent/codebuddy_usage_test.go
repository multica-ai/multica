package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodebuddyUsageTracksMessageTotals(t *testing.T) {
	var tracker codebuddyUsageTracker
	first := json.RawMessage(`{"update":{"sessionUpdate":"usage_update","_meta":{"codebuddy.ai/messageId":"one","usage":{"prompt_tokens":38646,"completion_tokens":4,"prompt_cache_miss_tokens":4524,"cache_read_input_tokens":0,"cache_creation_input_tokens":34122,"credit":9.44}}}}`)
	tracker.observe("session/update", first)
	tracker.observe("session/update", first)
	tracker.observe("session/update", json.RawMessage(`{"update":{"sessionUpdate":"usage_update","used":38646,"_meta":{"codebuddy.ai/messageId":"one"}}}`))
	tracker.observe("session/update", json.RawMessage(`{"update":{"_meta":{"codebuddy.ai/messageId":"two","usage":{"prompt_tokens":100,"completion_tokens":10,"cache_read_input_tokens":60,"cache_creation_input_tokens":20}}}}`))
	want := TokenUsage{InputTokens: 4544, OutputTokens: 14, CacheReadTokens: 60, CacheWriteTokens: 34142}
	if got := tracker.total(); got != want {
		t.Fatalf("usage=%+v want %+v", got, want)
	}
}

// Preserve the upstream accounting invariant across the transport migration:
// duplicate blocks do not double-count, and a new execution has a new ledger.
func TestCodebuddyACPUsageIsScopedToExecution(t *testing.T) {
	event := json.RawMessage(`{"update":{"_meta":{"codebuddy.ai/messageId":"same-id","usage":{"prompt_tokens":100,"completion_tokens":10,"cache_read_input_tokens":60,"cache_creation_input_tokens":20}}}}`)
	for i := 0; i < 2; i++ {
		var tracker codebuddyUsageTracker
		tracker.observe("session/update", event)
		tracker.observe("session/update", event)
		want := TokenUsage{InputTokens: 20, OutputTokens: 10, CacheReadTokens: 60, CacheWriteTokens: 20}
		if got := tracker.total(); got != want {
			t.Fatalf("execution %d usage=%+v want %+v", i, got, want)
		}
	}
}

func TestCodebuddyACPUsageIgnoresIncompleteSnapshots(t *testing.T) {
	var tracker codebuddyUsageTracker
	tracker.observe("session/update", json.RawMessage(`{"update":{"_meta":{"codebuddy.ai/messageId":"one"}}}`))
	if got := tracker.total(); got != (TokenUsage{}) {
		t.Fatalf("context-only update became usage: %+v", got)
	}
	event := json.RawMessage(`{"update":{"_meta":{"codebuddy.ai/messageId":"one","usage":{"prompt_tokens":100,"completion_tokens":10}}}}`)
	tracker.observe("session/update", event)
	tracker.observe("session/update", json.RawMessage(`{"update":{"_meta":{"codebuddy.ai/messageId":"one","usage":{"prompt_tokens":0}}}}`))
	if got := tracker.total(); got != (TokenUsage{InputTokens: 100, OutputTokens: 10}) {
		t.Fatalf("incomplete update erased counters: %+v", got)
	}
}

func TestCodebuddyTranscriptUsageIsBoundToSessionAndRequest(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "projects", "vendor-specific-project-key")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	const current = `{"sessionId":"session-one","providerData":{"conversationRequestId":"request-new","messageId":"message-new","rawUsage":{"prompt_tokens":100,"completion_tokens":10,"cache_read_input_tokens":60,"cache_creation_input_tokens":20}}}`
	rows := []string{
		`{"sessionId":"session-one","providerData":{"conversationRequestId":"request-old","messageId":"old","rawUsage":{"prompt_tokens":99999,"completion_tokens":99999}}}`,
		`{"sessionId":"session-other","providerData":{"conversationRequestId":"request-new","messageId":"other","rawUsage":{"prompt_tokens":99999,"completion_tokens":99999}}}`,
		current, current,
	}
	path := filepath.Join(dir, "session-one.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(rows, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var tracker codebuddyUsageTracker
	tracker.record("message-new", map[string]json.RawMessage{"prompt_tokens": json.RawMessage(`100`), "completion_tokens": json.RawMessage(`10`), "cache_read_input_tokens": json.RawMessage(`60`), "cache_creation_input_tokens": json.RawMessage(`20`)})
	if err := tracker.reconcileTranscript([]string{"CODEBUDDY_CONFIG_DIR=" + root}, "", "session-one", "request-new"); err != nil {
		t.Fatal(err)
	}
	want := TokenUsage{InputTokens: 20, OutputTokens: 10, CacheReadTokens: 60, CacheWriteTokens: 20}
	if got := tracker.total(); got != want {
		t.Fatalf("history or duplicate was counted: %+v want %+v", got, want)
	}
	// Unknown request identities never borrow another turn's counters.
	var missing codebuddyUsageTracker
	if err := missing.reconcileTranscript([]string{"CODEBUDDY_CONFIG_DIR=" + root}, "", "session-one", "absent"); err != nil {
		t.Fatal(err)
	}
	if got := missing.total(); got != (TokenUsage{}) {
		t.Fatalf("unknown request received usage: %+v", got)
	}
	// A session id is a filename, never a path supplied by the runtime.
	if err := tracker.reconcileTranscript([]string{"CODEBUDDY_CONFIG_DIR=" + root}, "", "../session-one", "request-new"); err == nil {
		t.Fatal("accepted path traversal")
	}
}
