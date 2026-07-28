package localtoolpolicy

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

func TestDecideAlwaysEnforces(t *testing.T) {
	for _, tc := range []struct {
		setting toolpolicy.Setting
		kind    DecisionKind
		allowed bool
	}{
		{toolpolicy.SettingAllow, KindAllow, true},
		{toolpolicy.SettingAsk, KindAsk, false},
		{toolpolicy.SettingDeny, KindDeny, false},
		{"", KindDeny, false},
		{toolpolicy.SettingInherit, KindDeny, false},
	} {
		got := Decide(toolpolicy.Effective{Setting: tc.setting, Reason: "test"})
		if got.Kind != tc.kind || got.Allowed != tc.allowed || !got.Enforced {
			t.Fatalf("Decide(%q) = %+v", tc.setting, got)
		}
	}
}

func TestAskNeedsApproval(t *testing.T) {
	if !Decide(toolpolicy.Effective{Setting: toolpolicy.SettingAsk}).NeedsApproval() {
		t.Fatal("Ask must route to approval")
	}
}

func TestProviderPolicyToolKeyNormalizesCursorHookNames(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		tool string
		want string
	}{
		{tool: "Shell", want: "tools:run_terminal_cmd"},
		{tool: "Read", want: "tools:read_file"},
		{tool: "Write", want: "tools:edit_file"},
		{tool: "StrReplace", want: "tools:edit_file"},
		{tool: "Delete", want: "tools:edit_file"},
		{tool: "Grep", want: "tools:grep_search"},
		{tool: "Glob", want: "tools:file_search"},
		{tool: "WebSearch", want: "tools:web_search"},
		{tool: "Task", want: "tools:Task"},
		{tool: "mcp__atlas__search", want: "mcp__atlas__search"},
	} {
		if got := ProviderPolicyToolKey("cursor", tc.tool); got != tc.want {
			t.Errorf("ProviderPolicyToolKey(cursor, %q) = %q, want %q", tc.tool, got, tc.want)
		}
	}
}

func TestProviderPolicyToolKeyPreservesOtherProviders(t *testing.T) {
	t.Parallel()

	if got := ProviderPolicyToolKey("claude", "Read"); got != "tools:Read" {
		t.Fatalf("ProviderPolicyToolKey(claude, Read) = %q, want tools:Read", got)
	}
	if got := ProviderPolicyToolKey("gemini", "read_file"); got != "tools:read_file" {
		t.Fatalf("ProviderPolicyToolKey(gemini, read_file) = %q, want tools:read_file", got)
	}
}

func TestProviderMandateToolKeyNormalizesWorkspaceConnectionDispatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tool string
		want string
	}{
		{
			name: "api connection endpoint",
			tool: "mcp__multica__infisical_admin__get_api_v3_secrets_raw",
			want: "infisical_admin__get_api_v3_secrets_raw",
		},
		{
			name: "workspace mcp management tool",
			tool: "mcp__multica__create_connection",
			want: "mcp__multica__create_connection",
		},
		{
			name: "ordinary mcp tool",
			tool: "mcp__company_brain__search",
			want: "mcp__company_brain__search",
		},
		{
			name: "built in tool",
			tool: "tools:Bash",
			want: "tools:Bash",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ProviderMandateToolKey(tc.tool); got != tc.want {
				t.Fatalf("ProviderMandateToolKey(%q) = %q, want %q", tc.tool, got, tc.want)
			}
		})
	}
}

func TestProviderPolicyToolKeyNormalizesCodexObservedNames(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		tool string
		want string
	}{
		{tool: "exec_command", want: "tools:bash"},
		{tool: "shell", want: "tools:bash"},
		{tool: "patch_apply", want: "tools:apply_patch"},
		{tool: "apply-patch", want: "tools:apply_patch"},
	} {
		if got := ProviderPolicyToolKey("codex", tc.tool); got != tc.want {
			t.Errorf("ProviderPolicyToolKey(codex, %q) = %q, want %q", tc.tool, got, tc.want)
		}
	}
}

func TestProviderResourcePatternNormalizesCursorHookNames(t *testing.T) {
	t.Parallel()

	if got := ProviderResourcePattern("cursor", "Shell", "", map[string]any{"command": "curl https://example.com"}); got != "curl" {
		t.Fatalf("Cursor Shell resource = %q, want curl", got)
	}
	if got := ProviderResourcePattern("cursor", "StrReplace", "", map[string]any{"file_path": "/tmp/a.go"}); got != "/tmp/a.go" {
		t.Fatalf("Cursor StrReplace resource = %q, want /tmp/a.go", got)
	}
	if got := ProviderResourcePattern("cursor", "Shell", "provided", map[string]any{"command": "curl https://example.com"}); got != "provided" {
		t.Fatalf("provided resource = %q, want provided", got)
	}
}
