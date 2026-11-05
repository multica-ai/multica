package daemon

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCodeGraphMCPConfig(t *testing.T) {
	raw, err := codeGraphMCPConfig("http://localhost:8081/", "mat_test")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Servers map[string]struct {
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	server := cfg.Servers["multica-codegraph"]
	if server.URL != "http://localhost:8081/api/codegraph/mcp" || server.Headers["Authorization"] != "Bearer mat_test" {
		t.Fatalf("unexpected configuration")
	}
	if strings.Contains(server.URL, "mat_test") {
		t.Fatal("credential in URL")
	}
	if _, err := codeGraphMCPConfig("http://localhost:8081", "mul_personal"); err == nil {
		t.Fatal("accepted non-task credential")
	}
}
