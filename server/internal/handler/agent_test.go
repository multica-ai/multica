package handler

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestExtractMCPServersReturnsNilForEmptyConfig(t *testing.T) {
	t.Parallel()

	if got := extractMCPServers(nil); got != nil {
		t.Errorf("expected nil for nil config, got %s", got)
	}
	if got := extractMCPServers([]byte{}); got != nil {
		t.Errorf("expected nil for empty config, got %s", got)
	}
}

func TestExtractMCPServersReturnsNilForInvalidJSON(t *testing.T) {
	t.Parallel()

	if got := extractMCPServers([]byte("not json")); got != nil {
		t.Errorf("expected nil for invalid JSON, got %s", got)
	}
}

func TestExtractMCPServersReturnsNilWhenKeyMissing(t *testing.T) {
	t.Parallel()

	cfg := []byte(`{"other_field":"value"}`)
	if got := extractMCPServers(cfg); got != nil {
		t.Errorf("expected nil when mcp_servers key missing, got %s", got)
	}
}

func TestExtractMCPServersReturnsRawJSON(t *testing.T) {
	t.Parallel()

	cfg := []byte(`{"mcp_servers":{"fs":{"command":"npx","args":["-y","@modelcontextprotocol/server-filesystem","/tmp"]}}}`)
	got := extractMCPServers(cfg)
	if got == nil {
		t.Fatal("expected non-nil result")
	}

	// The extracted value should be a JSON object with key "fs".
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(got, &servers); err != nil {
		t.Fatalf("unmarshal extracted: %v", err)
	}
	if _, ok := servers["fs"]; !ok {
		t.Errorf("expected fs key in extracted servers, got %s", got)
	}

	// Make sure we preserved the raw bytes without re-marshalling.
	if !bytes.Contains(got, []byte(`"command":"npx"`)) {
		t.Errorf("expected raw bytes to contain command, got %s", got)
	}
}
