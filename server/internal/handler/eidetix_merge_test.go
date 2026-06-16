package handler

import (
	"encoding/json"
	"testing"
)

func TestMergeEidetixServer_EmptyConfig(t *testing.T) {
	merged, added, err := mergeEidetixServer(nil, "https://eidetix.example/mcp/sse", "tok-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !added {
		t.Fatalf("expected added=true on empty config")
	}

	var got struct {
		McpServers map[string]struct {
			Type      string            `json:"type"`
			URL       string            `json:"url"`
			Transport string            `json:"transport"`
			Headers   map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(merged, &got); err != nil {
		t.Fatalf("merged is not valid JSON: %v", err)
	}
	e, ok := got.McpServers["eidetix"]
	if !ok {
		t.Fatalf("eidetix server not present, got %s", merged)
	}
	if e.URL != "https://eidetix.example/mcp/sse" {
		t.Errorf("url = %q, want the endpoint", e.URL)
	}
	// type:"http" is what Claude Code's --mcp-config parser requires to load a
	// remote server; without it the eidetix tools never connect.
	if e.Type != "http" {
		t.Errorf("type = %q, want http (Claude Code remote-MCP requirement)", e.Type)
	}
	if e.Transport != "streamable-http" {
		t.Errorf("transport = %q, want streamable-http (OpenClaw)", e.Transport)
	}
	if e.Headers["Authorization"] != "Bearer tok-abc" {
		t.Errorf("Authorization = %q, want Bearer tok-abc", e.Headers["Authorization"])
	}
}

func TestMergeEidetixServer_PreservesExistingServers(t *testing.T) {
	existing := json.RawMessage(`{"mcpServers":{"github":{"command":"gh-mcp"}}}`)
	merged, added, err := mergeEidetixServer(existing, "https://e/mcp/sse", "tok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !added {
		t.Fatalf("expected added=true")
	}
	var got struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(merged, &got); err != nil {
		t.Fatalf("merged not valid JSON: %v", err)
	}
	if _, ok := got.McpServers["github"]; !ok {
		t.Errorf("existing github server was dropped: %s", merged)
	}
	if _, ok := got.McpServers["eidetix"]; !ok {
		t.Errorf("eidetix server not added: %s", merged)
	}
}

func TestMergeEidetixServer_DoesNotClobberUserDefined(t *testing.T) {
	existing := json.RawMessage(`{"mcpServers":{"eidetix":{"url":"https://user/sse","transport":"streamable-http"}}}`)
	merged, added, err := mergeEidetixServer(existing, "https://managed/mcp/sse", "managed-tok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if added {
		t.Fatalf("expected added=false when a user-defined eidetix server exists")
	}
	if string(merged) != string(existing) {
		t.Errorf("user config was mutated:\n got %s\nwant %s", merged, existing)
	}
}

func TestMergeEidetixServer_MalformedExistingReturnsError(t *testing.T) {
	_, _, err := mergeEidetixServer(json.RawMessage(`{not json`), "https://e/sse", "tok")
	if err == nil {
		t.Fatalf("expected an error on malformed existing config")
	}
}
