package execenv

import (
	"strings"
	"testing"
)

// The built-in status rules a project workflow replaces (MUL-8145): each one
// names a status the workflow may not list, or moves the issue into a step
// that hands it to someone else.
var builtInStatusRules = []string{
	"set `in_progress` FIRST",
	"always lands here",
	"→ `in_review`",
	"a dispatch turn leaves the parent `in_progress`",
	"5. Before exiting, confirm the status still matches where things actually stand.",
	"The workflow rules above refer to exact built-in status keys",
}

func TestBriefWithProjectWorkflowDropsBuiltInStatusRules(t *testing.T) {
	t.Parallel()
	catalog := []IssueStatusForEnv{{Key: "code_review", Name: "Code review", Category: "in_review"}}
	for name, ctx := range map[string]TaskContextForEnv{
		"agent":                {IssueID: "issue-1", AgentID: "a-1", AgentName: "Eve", HasProjectWorkflow: true},
		"squad leader":         {IssueID: "issue-1", AgentID: "a-1", AgentName: "Eve", HasProjectWorkflow: true, IsSquadLeader: true},
		"custom catalog":       {IssueID: "issue-1", AgentID: "a-1", AgentName: "Eve", HasProjectWorkflow: true, IssueStatuses: catalog},
		"comment-triggered":    {IssueID: "issue-1", AgentID: "a-1", AgentName: "Eve", HasProjectWorkflow: true, TriggerCommentID: "c-1"},
		"leader with comments": {IssueID: "issue-1", AgentID: "a-1", AgentName: "Eve", HasProjectWorkflow: true, IsSquadLeader: true, TriggerCommentID: "c-1"},
	} {
		out := buildMetaSkillContent("claude", ctx)
		for _, rule := range builtInStatusRules {
			if strings.Contains(out, rule) {
				t.Errorf("%s: workflow brief still carries built-in rule %q", name, rule)
			}
		}
		for _, want := range []string{
			"3. Complete the task within your Agent Identity boundaries",
			"5. Before exiting, confirm the issue sits where things actually stand: you ran the matching command from `[Project workflow]`",
			"**Issue status — this issue follows its project workflow**",
			"Move this issue only with its commands.",
			"run its Step done command",
			"— flip status. This issue's statuses and commands are in `[Project workflow]`; for other issues, an invalid status errors with the full valid list.",
			// The duplicate-mark bullet still rides on the status command.
			"--duplicate-of",
			// The no-write default for question-only turns survives the swap.
			"questions, discussion, and acknowledgements never touch status",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: workflow brief missing %q\n---\n%s", name, want, out)
			}
		}
		// No status key list competes with [Project workflow]: neither the
		// built-in line nor the custom catalog.
		for _, list := range []string{"(todo / in_progress / in_review", "Available statuses by lifecycle category", "`later`", "`code_review`"} {
			if strings.Contains(out, list) {
				t.Errorf("%s: workflow brief still lists statuses (%q)", name, list)
			}
		}
		// Nothing else in the brief asks for in_progress / in_review on this
		// issue, so nothing has to forbid them either (MUL-8145).
		for _, word := range []string{"`in_progress`", "`in_review`"} {
			if strings.Contains(out, word) {
				t.Errorf("%s: workflow brief still names %s", name, word)
			}
		}
		if got := strings.Count(out, "**Issue status"); got != 1 {
			t.Errorf("%s: brief has %d issue-status sections, want 1", name, got)
		}
	}
}

func TestBriefWithProjectWorkflowKeepsSquadLeaderDispatchRule(t *testing.T) {
	t.Parallel()
	leader := buildMetaSkillContent("claude", TaskContextForEnv{IssueID: "issue-1", HasProjectWorkflow: true, IsSquadLeader: true})
	if !strings.Contains(leader, "a dispatch turn leaves the parent at its current step, and you run Step done only on the later turn") {
		t.Errorf("leader workflow brief must keep dispatch-is-not-delivery in workflow terms\n---\n%s", leader)
	}
	member := buildMetaSkillContent("claude", TaskContextForEnv{IssueID: "issue-1", HasProjectWorkflow: true})
	if strings.Contains(member, "Squad leader:") {
		t.Errorf("non-leader workflow brief must not carry the squad leader bullet")
	}
}

// The flag is the only switch: without it the brief keeps the built-in rules,
// so issues outside a handoff workflow (and old servers) see no change.
func TestBriefWithoutProjectWorkflowKeepsBuiltInStatusRules(t *testing.T) {
	t.Parallel()
	out := buildMetaSkillContent("claude", TaskContextForEnv{IssueID: "issue-1", AgentID: "a-1", AgentName: "Eve"})
	for _, want := range []string{"set `in_progress` FIRST", "always lands here", "5. Before exiting, confirm the status still matches where things actually stand."} {
		if !strings.Contains(out, want) {
			t.Errorf("brief without a workflow lost built-in rule %q", want)
		}
	}
	for _, leak := range []string{"[Project workflow]", "follows its project workflow"} {
		if strings.Contains(out, leak) {
			t.Errorf("brief without a workflow must not mention %q", leak)
		}
	}
}
