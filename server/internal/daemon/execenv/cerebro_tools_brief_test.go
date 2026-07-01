package execenv

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/connmeta"
)

// FIR-2441 (DoD 4): with no resolved tools the brief no longer renders nothing
// — it renders the compact "0 connection tools" explanation so silence can never
// be mistaken for a docs/tools load failure. It stays compact: header + the one
// note, no tool list and no argument-shape guidance (there is nothing to call).
func TestCerebroToolsBriefEmptyRendersZeroConnectionsNote(t *testing.T) {
	for _, entries := range [][]ToolBriefEntry{nil, {}} {
		got := cerebroToolsBrief(entries)
		if !strings.Contains(got, "### Connections & MCP tools (resolved live from your permissions)") {
			t.Fatalf("expected section header for empty entries, got %q", got)
		}
		if !strings.Contains(got, "You currently have 0 connection tools") {
			t.Fatalf("expected zero-connection-tools note for empty entries, got %q", got)
		}
		if !strings.Contains(got, "multica_connection_tools_status") {
			t.Fatalf("expected diagnostic pointer for empty entries, got %q", got)
		}
		// Compact: no tool-list scaffolding or arg-shape guidance when empty.
		if strings.Contains(got, connmeta.APIConnectionArgHint) {
			t.Fatalf("did not expect arg-shape guidance when there are no tools:\n%s", got)
		}
	}
}

// When the agent HAS tools but none of them are connection tools, the note still
// appears (alongside the normal section) so the empty Connections family is never
// ambiguous.
func TestCerebroToolsBriefMCPOnlyStillNotesZeroConnections(t *testing.T) {
	out := cerebroToolsBrief([]ToolBriefEntry{
		{Family: "MCP tools", Name: "schedule_wakeup", Description: "Schedule a wakeup", Verdict: "allow"},
	})
	if !strings.Contains(out, "You currently have 0 connection tools") {
		t.Fatalf("expected zero-connection-tools note when only MCP tools are present:\n%s", out)
	}
	if !strings.Contains(out, "`schedule_wakeup`") {
		t.Fatalf("expected the MCP tool to still be listed:\n%s", out)
	}
}

// With at least one connection tool present, the zero note must NOT appear.
func TestCerebroToolsBriefWithConnectionOmitsZeroNote(t *testing.T) {
	out := cerebroToolsBrief([]ToolBriefEntry{
		{Family: "Connections", Name: "data-registry / query_run", Description: "Run a query", Verdict: "allow"},
	})
	if strings.Contains(out, "You currently have 0 connection tools") {
		t.Fatalf("did not expect zero-connection-tools note when a connection tool is present:\n%s", out)
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
		// Assert the exact shared connmeta constant so the local brief and the cloud
		// gateway system prompt render byte-for-byte the same rule (single source).
		connmeta.APIConnectionArgHint,
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

// FIR-2441 (DoD 4): with no resolved tools the brief now carries the compact
// "0 connection tools" explanation instead of omitting the section entirely.
func TestBriefNotesZeroConnectionsWhenNoEntries(t *testing.T) {
	ctx := TaskContextForEnv{IssueID: "00000000-0000-0000-0000-000000000001"}
	out := buildMetaSkillContent("claude", ctx)
	if !strings.Contains(out, "### Connections & MCP tools (resolved live from your permissions)") {
		t.Fatalf("expected tools section header when EffectiveTools is empty")
	}
	if !strings.Contains(out, "You currently have 0 connection tools") {
		t.Fatalf("expected zero-connection-tools note when EffectiveTools is empty")
	}
}
