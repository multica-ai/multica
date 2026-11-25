package execenv

import (
	"strings"
	"testing"
)

func TestBuildMetaSkillContentTaskPlanPerProvider(t *testing.T) {
	t.Parallel()
	ctx := TaskContextForEnv{IssueID: "issue-1"}

	claude := buildMetaSkillContent("claude", ctx)
	if !strings.Contains(claude, "## Task Plan") || !strings.Contains(claude, "TaskCreate") || !strings.Contains(claude, "TaskList") {
		t.Fatalf("claude brief must name the checklist tools:\n%s", claude)
	}
	codex := buildMetaSkillContent("codex", ctx)
	if !strings.Contains(codex, "## Task Plan") || !strings.Contains(codex, "update_plan") || strings.Contains(codex, "TaskCreate") {
		t.Fatalf("codex brief must name update_plan only:\n%s", codex)
	}
	if other := buildMetaSkillContent("gemini", ctx); strings.Contains(other, "## Task Plan") {
		t.Fatal("runtimes without a plan tool must not get the section")
	}
}
