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
		// JEH-1182: Bash maps to the extracted binary, not the raw
		// command, so grant pattern="curl" matches reliably even when
		// the command contains URL slashes.
		{"Bash → binary, not command", "Bash", map[string]any{"command": "git status"}, "git"},
		{"Bash with URL → binary only", "Bash", map[string]any{"command": "curl https://example.com"}, "curl"},
		{"Bash with abs path → basename", "Bash", map[string]any{"command": "/usr/bin/kubectl get pods"}, "kubectl"},
		{"Bash with sh -c wrapper → inner binary", "Bash", map[string]any{"command": `sh -c "curl https://x"`}, "curl"},
		{"Bash unparseable → falls back to raw command", "Bash", map[string]any{"command": "FOO=bar"}, "FOO=bar"},
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

// CEREBRO-PATCH(main-test-shell-binary): JEH-1182 — shell_binary parser tests.
//
// TestShellBinaryFromCommand locks in the parsing rules that drive the
// sandbox-policy attr. The acceptance criteria on JEH-1182 ("marketing
// agent cannot call curl; dev agent can") collapse to: persona must
// receive a clean binary name, and the parser must not let a wrapper
// like `sh -c "curl ..."` slip through.
func TestShellBinaryFromCommand(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		want string
	}{
		{"plain binary", "curl https://example.com", "curl"},
		{"absolute path stripped", "/usr/bin/curl -fsSL https://x", "curl"},
		{"relative path stripped", "./scripts/deploy.sh", "deploy.sh"},
		{"leading env vars skipped", "FOO=bar BAZ=qux curl https://x", "curl"},
		{"only env vars → empty", "FOO=bar", ""},
		{"empty string", "", ""},
		{"whitespace only", "   \t\n  ", ""},
		{"sh -c unwraps to inner binary", `sh -c "curl https://x"`, "curl"},
		{"bash -c unwraps", `bash -c 'gh pr create'`, "gh"},
		{"sh without -c stays as sh", "sh script.sh", "sh"},
		{"single-quoted command", `curl 'https://x with spaces'`, "curl"},
		{"double-quoted command", `curl "https://x"`, "curl"},
		{"bash -c with env-vars inside", `bash -c "FOO=bar curl https://x"`, "curl"},
		{"plain shell builtin", "echo hello", "echo"},
		{"hyphenated binary", "git-lfs pull", "git-lfs"},
		{"binary with trailing slash path", "/usr/local/bin/kubectl get pods", "kubectl"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shellBinaryFromCommand(tc.cmd)
			if got != tc.want {
				t.Errorf("shellBinaryFromCommand(%q) = %q, want %q", tc.cmd, got, tc.want)
			}
		})
	}
}

// TestSplitShellWords covers the cases that matter for binary
// extraction: matched quotes group a token, mismatched / unbalanced
// quotes don't crash. Backslash escapes are intentionally not modelled.
func TestSplitShellWords(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"plain", "a b c", []string{"a", "b", "c"}},
		{"double quotes group", `a "b c" d`, []string{"a", "b c", "d"}},
		{"single quotes group", `a 'b c' d`, []string{"a", "b c", "d"}},
		{"empty input", "", nil},
		{"multiple whitespace collapsed", "a\t b  c", []string{"a", "b", "c"}},
		{"trailing whitespace", "a b ", []string{"a", "b"}},
		{"unterminated quote keeps reading", `a "b c`, []string{"a", "b c"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitShellWords(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("splitShellWords(%q) = %v, want %v", tc.in, got, tc.want)
			}
			for i, s := range got {
				if s != tc.want[i] {
					t.Errorf("splitShellWords(%q)[%d] = %q, want %q", tc.in, i, s, tc.want[i])
				}
			}
		})
	}
}

// TestIsEnvAssignment locks the NAME=VALUE pattern: NAME must match
// [A-Za-z_][A-Za-z0-9_]*. A leading digit, hyphen, or empty NAME means
// "not an env assignment" and the parser should treat the token as a
// regular argument.
func TestIsEnvAssignment(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"FOO=bar", true},
		{"foo=", true},
		{"_HIDDEN=1", true},
		{"FOO_BAR=baz", true},
		{"FOO9=baz", true},
		{"9FOO=bar", false},
		{"foo-bar=x", false},
		{"=bar", false},
		{"bar", false},
		{"", false},
		{"--flag=value", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := isEnvAssignment(tc.in); got != tc.want {
				t.Errorf("isEnvAssignment(%q) = %v, want %v", tc.in, got, tc.want)
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
