package execenv

import (
	"strings"
	"testing"
)

// Project workflow section of the issue brief (MUL-7420).

func TestBriefProjectWorkflowAbsentLeavesBriefUnchanged(t *testing.T) {
	t.Parallel()
	base := TaskContextForEnv{IssueID: "issue-1", AgentID: "a-1", AgentName: "Eve"}
	out := buildMetaSkillContent("claude", base)
	if strings.Contains(out, "Project Workflow") || strings.Contains(out, "uses its own workflow") {
		t.Fatalf("a Default-workflow issue must not mention a project workflow\n---\n%s", out)
	}
	empty := base
	empty.ProjectWorkflow = &ProjectWorkflowForEnv{Name: "Delivery"}
	if got := buildMetaSkillContent("claude", empty); strings.Contains(got, "### Project Workflow") {
		t.Errorf("a workflow without steps must not render a section")
	}
}

func TestBriefProjectWorkflowRendersStepsAndExits(t *testing.T) {
	t.Parallel()
	ctx := TaskContextForEnv{
		IssueID: "issue-1", AgentID: "a-1", AgentName: "Forge",
		ProjectWorkflow: &ProjectWorkflowForEnv{
			Name:             "Delivery",
			CurrentStatusKey: "implement",
			Steps: []ProjectWorkflowStepForEnv{
				{Key: "todo", Name: "Todo"},
				{Key: "implement", Name: "Implement", Handler: "Forge", Instructions: "Open a PR.\nLink it here.", NextStatusKey: "code_review"},
				{Key: "code_review", Name: "Code review", Handler: "Sentinel", NextStatusKey: "done", BackStatusKey: "implement"},
				{Key: "done", Name: "Done"},
			},
		},
	}
	out := buildMetaSkillContent("claude", ctx)
	for _, want := range []string{
		"finish your step by moving to the step's next status instead",
		"### Project Workflow: Delivery\n",
		"- `todo` Todo\n",
		"- `implement` Implement — hands off to Forge (current)\n",
		"- `code_review` Code review — hands off to Sentinel\n",
		"> Open a PR.\n> Link it here.\n",
		"- Step done → `multica issue status <id> code_review`\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("brief is missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, "Needs changes →") {
		t.Errorf("the current step has no back status, so no needs-changes exit may render")
	}
}

func TestBriefProjectWorkflowSanitizesUserText(t *testing.T) {
	t.Parallel()
	ctx := TaskContextForEnv{
		IssueID: "issue-1", AgentID: "a-1", AgentName: "Eve",
		ProjectWorkflow: &ProjectWorkflowForEnv{
			Name:             "Flow\n## Injected",
			CurrentStatusKey: "a",
			Steps: []ProjectWorkflowStepForEnv{
				{Key: "a", Name: "Step\n# Heading"},
				{Key: "bad key`", Name: "Dropped"},
			},
		},
	}
	out := buildMetaSkillContent("claude", ctx)
	if strings.Contains(out, "\n## Injected") || strings.Contains(out, "\n# Heading") {
		t.Fatalf("user-authored names must not inject headings\n---\n%s", out)
	}
	if strings.Contains(out, "Dropped") {
		t.Errorf("a step whose key is not a code token must be dropped")
	}
}
