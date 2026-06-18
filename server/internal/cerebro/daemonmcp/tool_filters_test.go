package daemonmcp

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestAddExcludedToolsKeepsServerAndAddsToolFilters(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"mcpServers":{"shopify":{"url":"https://mcp.example","excludeTools":["read_secret"]},"slack":{"url":"https://slack.example"}}}`)
	out := AddExcludedTools(raw, []string{
		"mcp__shopify__delete_order",
		"mcp__shopify__refund_order",
		"mcp__shopify__delete_order",
		"mcp__missing__noop",
		"bad-token",
	})

	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("parse output: %v", err)
	}
	servers := doc["mcpServers"].(map[string]any)
	if _, ok := servers["shopify"]; !ok {
		t.Fatalf("shopify server should still be present: %v", servers)
	}
	shopify := servers["shopify"].(map[string]any)
	got := stringsFromAny(shopify["excludeTools"])
	want := []string{"delete_order", "read_secret", "refund_order"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("excludeTools = %v, want %v", got, want)
	}
	slack := servers["slack"].(map[string]any)
	if _, ok := slack["excludeTools"]; ok {
		t.Fatalf("unmatched server should not get excludeTools: %v", slack)
	}
}

func TestAddExcludedToolsNoopCases(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"mcpServers":{"shopify":{"url":"https://mcp.example"}}}`)
	if got := AddExcludedTools(raw, nil); string(got) != string(raw) {
		t.Fatalf("nil tokens should be no-op, got %s", got)
	}
	if got := AddExcludedTools(raw, []string{"mcp__missing__tool"}); string(got) != string(raw) {
		t.Fatalf("unmatched tokens should be no-op, got %s", got)
	}
	if got := AddExcludedTools(json.RawMessage(`not json`), []string{"mcp__x__y"}); string(got) != `not json` {
		t.Fatalf("malformed input should be returned unchanged, got %s", got)
	}
}

func stringsFromAny(v any) []string {
	raw, _ := v.([]any)
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
