// CEREBRO-PATCH(main-test): persona integration changes.
package main

import "testing"

// TestMCPResourceKind locks in W2.5's gating convention: each MCP tool
// resolves to a per-server resource_kind so an operator can grant
// access to one MCP server (e.g. supabase) without implicitly granting
// every server. Malformed names fall back to the catch-all kind.
func TestMCPResourceKind(t *testing.T) {
	cases := []struct {
		name string
		tool string
		want string
	}{
		{"happy path: supabase execute_sql", "mcp__supabase__execute_sql", "claude.tool.mcp.supabase"},
		{"hyphenated server", "mcp__github-cli__create_issue", "claude.tool.mcp.github-cli"},
		{"no tool segment", "mcp__supabase__", "claude.tool.mcp.supabase"},
		{"missing server segment falls back to catch-all", "mcp__", "claude.tool.mcp"},
		{"single-underscore (malformed) falls back", "mcp__noseparator", "claude.tool.mcp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mcpResourceKind(tc.tool)
			if got != tc.want {
				t.Errorf("mcpResourceKind(%q) = %q, want %q", tc.tool, got, tc.want)
			}
		})
	}
}

// TestResourceIDFromInput locks in the W4.7 mapping from tool_input
// fields to resource_id. Sandbox grants with a non-* resource_pattern
// match against this string (path.Match), so getting the field choice
// right per tool is the load-bearing part — a regression here makes
// every refined grant deny everything.
func TestResourceIDFromInput(t *testing.T) {
	cases := []struct {
		name string
		tool string
		args map[string]any
		want string
	}{
		{"Bash → command", "Bash", map[string]any{"command": "git status"}, "git status"},
		{"Bash missing → empty", "Bash", map[string]any{"description": "hi"}, ""},
		{"Edit → file_path", "Edit", map[string]any{"file_path": "/safe/foo.go"}, "/safe/foo.go"},
		{"Write → file_path", "Write", map[string]any{"file_path": "/tmp/x"}, "/tmp/x"},
		{"Read → file_path", "Read", map[string]any{"file_path": "/etc/passwd"}, "/etc/passwd"},
		{"NotebookEdit → notebook_path", "NotebookEdit", map[string]any{"notebook_path": "/nb/a.ipynb"}, "/nb/a.ipynb"},
		{"WebFetch → url", "WebFetch", map[string]any{"url": "https://example.com"}, "https://example.com"},
		{"WebSearch → query", "WebSearch", map[string]any{"query": "claude code"}, "claude code"},
		{"Task → description", "Task", map[string]any{"description": "plan x"}, "plan x"},
		{"Task fallback to subagent_type", "Task", map[string]any{"subagent_type": "general-purpose"}, "general-purpose"},
		{"MCP tool: first string arg", "mcp__supabase__execute_sql", map[string]any{"sql": "SELECT 1", "extras": 5}, "SELECT 1"},
		{"MCP tool: no string args → empty", "mcp__supabase__execute_sql", map[string]any{"limit": 10}, ""},
		{"unknown tool → empty", "MysteryTool", map[string]any{"command": "x"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resourceIDFromInput(tc.tool, tc.args)
			if got != tc.want {
				t.Errorf("resourceIDFromInput(%q, %v) = %q, want %q", tc.tool, tc.args, got, tc.want)
			}
		})
	}
}

// TestToolToKind_DefaultAllowSet locks in the static map: a missing
// entry means the hook short-circuits to allow without calling persona,
// so adding a tool to the map is the explicit gating opt-in.
func TestToolToKind_DefaultAllowSet(t *testing.T) {
	wantGated := map[string]string{
		"Bash":         "claude.tool.bash",
		"Write":        "claude.tool.write",
		"Edit":         "claude.tool.edit",
		"NotebookEdit": "claude.tool.notebook_edit",
		"WebFetch":     "claude.tool.web_fetch",
		"WebSearch":    "claude.tool.web_search",
		"Task":         "claude.tool.task",
	}
	for tool, kind := range wantGated {
		got, ok := toolToKind[tool]
		if !ok {
			t.Errorf("expected %q in toolToKind, missing", tool)
			continue
		}
		if got != kind {
			t.Errorf("toolToKind[%q] = %q, want %q", tool, got, kind)
		}
	}
	// Read/Grep/Glob etc. are deliberately absent — they default-allow.
	for _, defaultAllow := range []string{"Read", "Grep", "Glob", "TodoWrite"} {
		if _, ok := toolToKind[defaultAllow]; ok {
			t.Errorf("%q should be default-allow (not in toolToKind), but is in the map", defaultAllow)
		}
	}
}
