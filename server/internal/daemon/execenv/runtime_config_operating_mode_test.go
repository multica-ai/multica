package execenv

import (
	"strings"
	"testing"
)

func TestOperatingModeRuntimeBrief(t *testing.T) {
	base := TaskContextForEnv{
		IssueID: "issue-1",
		Repos: []RepoContextForEnv{{
			URL:         "https://example.com/acme/widget.git",
			Description: "Widget repository",
		}},
	}
	missing := buildMetaSkillContent("claude", base)

	coding := base
	coding.AgentOperatingMode = "coding"
	if got := buildMetaSkillContent("claude", coding); got != missing {
		t.Fatal("explicit coding mode changed the runtime brief")
	}

	unknown := base
	unknown.AgentOperatingMode = "unknown"
	if got := buildMetaSkillContent("claude", unknown); got != missing {
		t.Fatal("unknown stored mode did not fall back to the coding runtime brief")
	}

	operational := base
	operational.AgentOperatingMode = "operational"
	opBrief := buildMetaSkillContent("claude", operational)
	if !strings.Contains(opBrief, "business-task agent in the Multica platform") {
		t.Fatalf("operational brief is missing its business-task identity:\n%s", opBrief)
	}
	if strings.Contains(opBrief, "coding agent in the Multica platform") {
		t.Fatalf("operational brief retained the coding identity:\n%s", opBrief)
	}
	if strings.Contains(opBrief, "## Repositories") || strings.Contains(opBrief, base.Repos[0].URL) {
		t.Fatalf("operational brief exposed repository context:\n%s", opBrief)
	}

	hybrid := base
	hybrid.AgentOperatingMode = "hybrid"
	hybridBrief := buildMetaSkillContent("claude", hybrid)
	if !strings.Contains(hybridBrief, "business-task agent in the Multica platform") {
		t.Fatalf("hybrid brief is missing its business-task identity:\n%s", hybridBrief)
	}
	if !strings.Contains(hybridBrief, "## Repositories") || !strings.Contains(hybridBrief, base.Repos[0].URL) {
		t.Fatalf("hybrid brief lost repository context:\n%s", hybridBrief)
	}
}
