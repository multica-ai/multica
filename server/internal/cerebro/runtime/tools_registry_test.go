package runtime

import (
	"encoding/json"
	"fmt"
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

// CEREBRO-PATCH(firtal-gateway-anthropic-tool-schema): guard Anthropic schema compatibility for built-in runtime tools.
func TestAnthropicToolSchemasDefineArrayItems(t *testing.T) {
	reg := NewDefaultRegistry(nil, nil, ToolContext{})
	for name, tool := range reg.tools {
		assertArraySchemasHaveItems(t, name, tool.InputSchema(), name)
	}
}

// CEREBRO-PATCH(firtal-gateway-anthropic-tool-schema): guard provider-safe normalized tool schemas, not only raw registry schemas.
func TestAnthropicToolSchemasOmitEmptyRequiredLists(t *testing.T) {
	reg := NewDefaultRegistry(nil, nil, ToolContext{})
	tools := make([]Tool, 0, len(reg.tools))
	for _, tool := range reg.tools {
		tools = append(tools, tool)
	}

	for _, tool := range reg.ToAnthropicTools(tools) {
		assertNoEmptyRequiredLists(t, tool.Name, tool.InputSchema, tool.Name)
	}
}

func assertArraySchemasHaveItems(t *testing.T, toolName string, node any, path string) {
	t.Helper()

	m, ok := node.(map[string]any)
	if !ok {
		return
	}
	if typ, _ := m["type"].(string); typ == "array" {
		if _, ok := m["items"]; !ok {
			t.Fatalf("tool %q schema path %s has type=array without items; Anthropic rejects malformed array schemas", toolName, path)
		}
	}
	for key, child := range m {
		switch v := child.(type) {
		case map[string]any:
			assertArraySchemasHaveItems(t, toolName, v, path+"."+key)
		case []any:
			for i, item := range v {
				assertArraySchemasHaveItems(t, toolName, item, fmt.Sprintf("%s.%s[%d]", path, key, i))
			}
		}
	}
	if props, ok := m["properties"].(map[string]any); ok {
		for key, child := range props {
			assertArraySchemasHaveItems(t, toolName, child, path+".properties."+key)
		}
	}
}

func assertNoEmptyRequiredLists(t *testing.T, toolName string, node any, path string) {
	t.Helper()

	m, ok := node.(map[string]any)
	if !ok {
		return
	}
	if required, ok := m["required"]; ok {
		switch v := required.(type) {
		case []any:
			if len(v) == 0 {
				t.Fatalf("tool %q schema path %s has empty required list; omit required when no fields are mandatory", toolName, path)
			}
		case []string:
			if len(v) == 0 {
				t.Fatalf("tool %q schema path %s has empty required list; omit required when no fields are mandatory", toolName, path)
			}
		}
	}
	for key, child := range m {
		switch v := child.(type) {
		case map[string]any:
			assertNoEmptyRequiredLists(t, toolName, v, path+"."+key)
		case []any:
			for i, item := range v {
				assertNoEmptyRequiredLists(t, toolName, item, fmt.Sprintf("%s.%s[%d]", path, key, i))
			}
		}
	}
}
