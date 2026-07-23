package handler

// CEREBRO-PATCH(capability-register-test): FIR-2129 registry normalization coverage.

import (
	"encoding/json"
	"testing"
)

func TestCapabilityItemsFromSnapshot(t *testing.T) {
	raw := map[string]json.RawMessage{
		"tools":            json.RawMessage(`["Bash","Read",""]`),
		"mcp_servers":      json.RawMessage(`["github"]`),
		"discovery_method": json.RawMessage(`"static"`),
		"providers":        json.RawMessage(`["claude"]`),
	}
	got := capabilityItemsFromSnapshot(raw)
	want := []capabilityReportItem{
		{Name: "github", Kind: "mcp_servers"},
		{Name: "Bash", Kind: "tools"},
		{Name: "Read", Kind: "tools"},
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%#v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Name != want[i].Name || got[i].Kind != want[i].Kind {
			t.Fatalf("item %d = %s/%s, want %s/%s", i, got[i].Kind, got[i].Name, want[i].Kind, want[i].Name)
		}
	}
}

func TestDirectMCPToolParts(t *testing.T) {
	serverName, toolName, ok := directMCPToolParts("mcp__customer-service__lookup_order")
	if !ok || serverName != "customer-service" || toolName != "lookup_order" {
		t.Fatalf("directMCPToolParts() = %q, %q, %v", serverName, toolName, ok)
	}
	for _, invalid := range []string{"Read", "mcp____lookup_order", "mcp__customer-service__"} {
		if _, _, ok := directMCPToolParts(invalid); ok {
			t.Fatalf("directMCPToolParts(%q) unexpectedly succeeded", invalid)
		}
	}
}

func TestRuntimeCapabilityReportKeepsDirectMCPCallableName(t *testing.T) {
	reporter := CapabilitySubject{Type: "runtime", ID: "runtime-id"}
	got := runtimeCapabilityReport(capabilityReportItem{
		Name: "mcp__customer-service__lookup_order",
		Kind: "tools",
		Metadata: map[string]any{
			"snapshot_key": "tools",
		},
	}, reporter)
	if got.Key != "mcp__customer-service__lookup_order" || got.Title != "lookup_order" || got.Category != "customer-service" {
		t.Fatalf("runtimeCapabilityReport() = %+v", got)
	}
	if got.Metadata["server_name"] != "customer-service" || got.Source != "runtime_report" {
		t.Fatalf("runtimeCapabilityReport() metadata/source = %+v", got)
	}
}
