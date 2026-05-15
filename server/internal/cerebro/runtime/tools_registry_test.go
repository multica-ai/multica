package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMulticaMCPToolMatrixMatchesInventory(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "permguard", "inventory.json"))
	if err != nil {
		t.Fatalf("read inventory: %v", err)
	}
	var inventory struct {
		MCP []struct {
			ID string `json:"id"`
		} `json:"mcp"`
	}
	if err := json.Unmarshal(raw, &inventory); err != nil {
		t.Fatalf("parse inventory: %v", err)
	}

	meta := MulticaMCPToolMatrix()
	if len(meta) != len(inventory.MCP) {
		t.Fatalf("tool matrix count = %d, inventory count = %d", len(meta), len(inventory.MCP))
	}
	seen := map[string]ToolMeta{}
	for _, item := range meta {
		if item.Name == "" {
			t.Fatal("tool matrix contains empty name")
		}
		if item.Description == "" {
			t.Fatalf("tool %q has empty description", item.Name)
		}
		if item.Status != ToolStatusImplemented && item.Status != ToolStatusNewlyImplemented && item.Status != ToolStatusExcluded {
			t.Fatalf("tool %q has invalid status %q", item.Name, item.Status)
		}
		if _, ok := seen[item.Name]; ok {
			t.Fatalf("tool %q appears more than once", item.Name)
		}
		seen[item.Name] = item
	}
	for _, item := range inventory.MCP {
		if _, ok := seen[item.ID]; !ok {
			t.Fatalf("inventory tool %q missing from tool matrix", item.ID)
		}
	}
}

func TestApprovedMulticaMCPToolNamesCoversAllNonExcludedTools(t *testing.T) {
	meta := AllBuiltinToolMeta()
	names := callableBuiltinToolNames()
	allowed := map[string]bool{}
	for _, item := range meta {
		if item.Status == ToolStatusImplemented || item.Status == ToolStatusNewlyImplemented {
			allowed[item.Name] = true
		}
	}
	if len(names) != len(allowed) {
		t.Fatalf("callable names count = %d, implemented metadata count = %d", len(names), len(allowed))
	}
	for _, name := range names {
		if !allowed[name] {
			t.Fatalf("callable name %q is not implemented/newly_implemented metadata", name)
		}
	}
}
