package claudehook

import (
	"strings"
	"testing"
)

func TestParseAndAccessors(t *testing.T) {
	in, err := Parse(strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"ls"},"cwd":"/tmp"}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if in.Name() != "Bash" {
		t.Errorf("Name = %q, want Bash", in.Name())
	}
	if in.InputArgs()["command"] != "ls" {
		t.Errorf("InputArgs[command] = %v, want ls", in.InputArgs()["command"])
	}
	if in.Dir() != "/tmp" {
		t.Errorf("Dir = %q, want /tmp", in.Dir())
	}

	// Aliases used for direct-invocation testing.
	in2, err := Parse(strings.NewReader(`{"tool":"Write","args":{"file_path":"/x"},"workdir":"/w"}`))
	if err != nil {
		t.Fatalf("Parse alias: %v", err)
	}
	if in2.Name() != "Write" || in2.InputArgs()["file_path"] != "/x" || in2.Dir() != "/w" {
		t.Errorf("alias accessors wrong: name=%q args=%v dir=%q", in2.Name(), in2.InputArgs(), in2.Dir())
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse(strings.NewReader("not json")); err == nil {
		t.Fatal("expected error on non-JSON input")
	}
}

func TestGated(t *testing.T) {
	gated := []string{"Bash", "Write", "Edit", "NotebookEdit", "WebFetch", "WebSearch", "Task",
		"mcp__supabase__execute_sql", "mcp__github__create_pr"}
	for _, tool := range gated {
		if !Gated(tool) {
			t.Errorf("Gated(%q) = false, want true", tool)
		}
	}
	allow := []string{"Read", "Grep", "Glob", "TodoWrite", "ToolSearch", "Skill",
		"ScheduleWakeup", "Monitor", "EnterPlanMode", "ExitPlanMode", "AskUserQuestion", ""}
	for _, tool := range allow {
		if Gated(tool) {
			t.Errorf("Gated(%q) = true, want false (default-allow)", tool)
		}
	}
}

func TestPolicyToolKey(t *testing.T) {
	cases := map[string]string{
		// Built-in Claude tools get the "tools:" capability-key prefix the
		// permissions screen authors a Deny under (TECH-2563 regression).
		"Bash":     "tools:Bash",
		"WebFetch": "tools:WebFetch",
		"Edit":     "tools:Edit",
		"Write":    "tools:Write",
		// MCP tools keep their mcp__ name (connection denies enforced elsewhere).
		"mcp__supabase__execute_sql": "mcp__supabase__execute_sql",
		// Already-namespaced keys and the empty tool are passed through unchanged.
		"connection:customer-service": "connection:customer-service",
		"tools:Bash":                  "tools:Bash",
		"":                            "",
	}
	for in, want := range cases {
		if got := PolicyToolKey(in); got != want {
			t.Errorf("PolicyToolKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResourcePattern(t *testing.T) {
	cases := []struct {
		tool string
		args map[string]any
		want string
	}{
		{"Bash", map[string]any{"command": "curl https://x.com"}, "curl"},
		{"Bash", map[string]any{"command": "/usr/bin/kubectl get pods"}, "kubectl"},
		{"Bash", map[string]any{"command": "FOO=bar gh pr list"}, "gh"},
		{"Bash", map[string]any{"command": `sh -c "curl https://x"`}, "curl"},
		{"Edit", map[string]any{"file_path": "/repo/a.go"}, "/repo/a.go"},
		{"Write", map[string]any{"file_path": "/repo/b.go"}, "/repo/b.go"},
		{"NotebookEdit", map[string]any{"notebook_path": "/n.ipynb"}, "/n.ipynb"},
		{"WebFetch", map[string]any{"url": "https://api.internal"}, "https://api.internal"},
		{"WebSearch", map[string]any{"query": "secrets"}, "secrets"},
		{"Task", map[string]any{"description": "do thing"}, "do thing"},
		{"mcp__supabase__execute_sql", map[string]any{"query": "DROP TABLE x"}, "DROP TABLE x"},
		{"Bash", map[string]any{}, ""},
		{"Read", map[string]any{"file_path": "/r"}, "/r"},
	}
	for _, c := range cases {
		if got := ResourcePattern(c.tool, c.args); got != c.want {
			t.Errorf("ResourcePattern(%q, %v) = %q, want %q", c.tool, c.args, got, c.want)
		}
	}
}

func TestShellBinary(t *testing.T) {
	cases := []struct{ cmd, want string }{
		{"curl https://x", "curl"},
		{"/usr/bin/curl", "curl"},
		{"./build.sh", "build.sh"},
		{"FOO=1 BAR=2 kubectl apply", "kubectl"},
		{`bash -c "rm -rf /"`, "rm"},
		{"", ""},
		{"   ", ""},
	}
	for _, c := range cases {
		if got := ShellBinary(c.cmd); got != c.want {
			t.Errorf("ShellBinary(%q) = %q, want %q", c.cmd, got, c.want)
		}
	}
}
