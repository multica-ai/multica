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
