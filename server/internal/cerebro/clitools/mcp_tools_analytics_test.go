package clitools

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/mcp"
)

func TestRegisterAnalyticsToolsExposesCatalogAndQuery(t *testing.T) {
	server := mcp.NewServer("test", "test")
	registerAnalyticsTools(server, nil)
	names := map[string]bool{}
	for _, tool := range server.Tools() {
		names[tool.Name] = true
	}
	if !names["analytics_catalog"] || !names["analytics_query"] {
		t.Fatalf("tools = %#v", server.Tools())
	}
}
