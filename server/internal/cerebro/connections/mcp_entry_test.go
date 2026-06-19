package connections

import "testing"

// stubRewriter relays every connection to a fixed URL + bearer.
type stubRewriter struct {
	url    string
	bearer string
}

func (s stubRewriter) RelayEntry(_, _, _ string, _ bool) (string, string, bool) {
	return s.url, s.bearer, true
}

// FIR-1563: every HTTP MCP entry the daemon injects MUST carry "type":"http",
// otherwise Claude Code silently ignores the server and a local runtime sees
// zero tools. These tests fail closed if the discriminator is ever dropped.

func TestMCPServerEntryDirectHasHTTPType(t *testing.T) {
	c := Connection{
		Name: "customer-service-mcp",
		Type: TypeMCPHTTP,
		URL:  "http://customer-service-mcp.internal:3000/mcp",
		AuthConfig: AuthConfig{
			BearerToken: "fc_mcp_secret",
		},
	}

	entry := mcpServerEntry(c, nil)

	if entry["type"] != "http" {
		t.Fatalf("direct entry type = %v, want \"http\"", entry["type"])
	}
	if entry["url"] != c.URL {
		t.Fatalf("direct entry url = %v, want %q", entry["url"], c.URL)
	}
	headers, ok := entry["headers"].(map[string]string)
	if !ok || headers["Authorization"] != "Bearer fc_mcp_secret" {
		t.Fatalf("direct entry headers = %v, want bearer set", entry["headers"])
	}
}

func TestMCPServerEntryRelayedHasHTTPType(t *testing.T) {
	c := Connection{
		Name:     "customer-service-mcp",
		Type:     TypeMCPHTTP,
		URL:      "http://customer-service-mcp.internal:3000/mcp",
		Internal: true,
	}
	rw := stubRewriter{
		url:    "https://multica-api.firtal.com/mcp-relay/customer-service-mcp",
		bearer: "relay_token",
	}

	entry := mcpServerEntry(c, rw)

	if entry["type"] != "http" {
		t.Fatalf("relayed entry type = %v, want \"http\"", entry["type"])
	}
	if entry["url"] != rw.url {
		t.Fatalf("relayed entry url = %v, want relay url %q", entry["url"], rw.url)
	}
	// The relayed entry must NOT leak the connection's internal URL.
	if entry["url"] == c.URL {
		t.Fatalf("relayed entry leaked internal url %q", c.URL)
	}
	headers, ok := entry["headers"].(map[string]string)
	if !ok || headers["Authorization"] != "Bearer relay_token" {
		t.Fatalf("relayed entry headers = %v, want relay bearer", entry["headers"])
	}
}
