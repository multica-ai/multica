package execenv

import (
	"strings"
	"testing"
)

func TestCerebroToolsBriefEmptyRendersNothing(t *testing.T) {
	if got := cerebroToolsBrief(nil); got != "" {
		t.Fatalf("expected empty string for no entries, got %q", got)
	}
	if got := cerebroToolsBrief([]ToolBriefEntry{}); got != "" {
		t.Fatalf("expected empty string for zero entries, got %q", got)
	}
}

func TestCerebroToolsBriefGroupsAndOrders(t *testing.T) {
	entries := []ToolBriefEntry{
		{Family: "Connections", Name: "customer-service / lookup_order", Description: "Look up an order", Verdict: "allow"},
		{Family: "MCP tools", Name: "schedule_wakeup", Description: "Schedule a wakeup", Verdict: "allow"},
		{Family: "Connections", Name: "customer-service / draft_reply", Description: "Draft a reply", Verdict: "ask"},
	}
	out := cerebroToolsBrief(entries)

	for _, want := range []string{
		"### Connections & MCP tools (resolved live from your permissions)",
		"**MCP tools**",
		"**Connections**",
		"`schedule_wakeup`",
		"`customer-service / lookup_order`",
		"`customer-service / draft_reply`",
		"Look up an order",
		// FIR-2441: the first prompt must state the api-connection argument shape
		// (path top-level, query params inside `query`, body inside `body`) so an
		// agent never has to call-and-read-the-error to discover the `query` object.
		"inside a `query` object",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected rendered brief to contain %q\n---\n%s", want, out)
		}
	}

	// MCP tools heading must come before Connections heading (familyOrder).
	if strings.Index(out, "**MCP tools**") > strings.Index(out, "**Connections**") {
		t.Errorf("expected MCP tools family before Connections family:\n%s", out)
	}

	// Ask tools are tagged; Allow tools are not.
	if !strings.Contains(out, "`customer-service / draft_reply` _(ask)_") {
		t.Errorf("expected ask tag on draft_reply:\n%s", out)
	}
	if strings.Contains(out, "`schedule_wakeup` _(ask)_") {
		t.Errorf("did not expect ask tag on an allow tool:\n%s", out)
	}
}

func TestCerebroToolsBriefIsStable(t *testing.T) {
	entries := []ToolBriefEntry{
		{Family: "Connections", Name: "b-conn / z_tool", Description: "z", Verdict: "allow"},
		{Family: "Connections", Name: "a-conn / a_tool", Description: "a", Verdict: "allow"},
		{Family: "MCP tools", Name: "list_wakeups", Description: "list", Verdict: "allow"},
	}
	first := cerebroToolsBrief(entries)
	second := cerebroToolsBrief(entries)
	if first != second {
		t.Fatalf("rendered brief is not stable across identical inputs:\n%q\n!=\n%q", first, second)
	}
}

func TestBriefIncludesToolsSectionWhenEntriesPresent(t *testing.T) {
	ctx := TaskContextForEnv{
		IssueID: "00000000-0000-0000-0000-000000000001",
		EffectiveTools: []ToolBriefEntry{
			{Family: "Connections", Name: "data-registry / query_run", Description: "Run a query", Verdict: "allow"},
		},
	}
	out := buildMetaSkillContent("claude", ctx)
	if !strings.Contains(out, "### Connections & MCP tools (resolved live from your permissions)") {
		t.Fatalf("expected tools section in brief when EffectiveTools present")
	}
	if !strings.Contains(out, "`data-registry / query_run`") {
		t.Fatalf("expected the connection tool to appear in the brief")
	}
}

func TestBriefOmitsToolsSectionWhenNoEntries(t *testing.T) {
	ctx := TaskContextForEnv{IssueID: "00000000-0000-0000-0000-000000000001"}
	out := buildMetaSkillContent("claude", ctx)
	if strings.Contains(out, "### Connections & MCP tools (resolved live from your permissions)") {
		t.Fatalf("did not expect tools section when EffectiveTools is empty")
	}
}
