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
	catalogBridgeBullet,
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

// A status-map [Project workflow] block lists no commands (MUL-9913): the
// brief states how to choose from the map instead, and keeps every invariant
// of the command form — no built-in moves, no competing status list.
func TestBriefWithStatusMapStatesTheMapRules(t *testing.T) {
	t.Parallel()
	catalog := []IssueStatusForEnv{{Key: "wont_fix", Name: "Won't Fix", Category: "closed"}}
	for name, ctx := range map[string]TaskContextForEnv{
		"agent":          {IssueID: "issue-1", AgentID: "a-1", AgentName: "Eve", HasProjectWorkflow: true, ProjectWorkflowStatusMap: true},
		"squad leader":   {IssueID: "issue-1", AgentID: "a-1", AgentName: "Eve", HasProjectWorkflow: true, ProjectWorkflowStatusMap: true, IsSquadLeader: true},
		"custom catalog": {IssueID: "issue-1", AgentID: "a-1", AgentName: "Eve", HasProjectWorkflow: true, ProjectWorkflowStatusMap: true, IssueStatuses: catalog},
	} {
		out := buildMetaSkillContent("claude", ctx)
		for _, rule := range builtInStatusRules {
			if strings.Contains(out, rule) {
				t.Errorf("%s: status-map brief still carries built-in rule %q", name, rule)
			}
		}
		for _, want := range []string{
			"3. Complete the task within your Agent Identity boundaries",
			"5. Before exiting, confirm the issue's status in `[Project workflow]` matches where things actually stand.",
			"`[Project workflow]` is this issue's status map.",
			"Entering a status that hands off reassigns the issue and starts its handler's run",
			"A status without a handler hands nothing off: the work stays yours",
			"At a step you handle, follow its Routes line.",
			"leave done-category statuses to a human unless Routes leads there",
			"a status in the closed category that fits (`cancelled` when there are no custom statuses); comment why first",
			"add no wakeup for that",
			// A stuck step goes back to whoever can supply what is missing when
			// it cannot park the issue: at a terminal status, whose author
			// routed only the send-back, or without a blocked column.
			"at a done- or closed-category status, its send-back route if it has one; elsewhere `blocked` when the map has it, else your step's send-back route when it has one",
			"This issue's status map is in `[Project workflow]`",
			"--duplicate-of",
			"questions, discussion, and acknowledgements never touch status",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: status-map brief missing %q\n---\n%s", name, want, out)
			}
		}
		// The command form's wording would point at lines the map lacks.
		// The map is the only status list, and no built-in key is named.
		for _, stale := range []string{
			"Step done", "Needs changes", "Can't proceed  →", "Move this issue only with its commands", "statuses and commands are in",
			"`wont_fix`", "`in_progress`", "`in_review`",
		} {
			if strings.Contains(out, stale) {
				t.Errorf("%s: status-map brief carries %q", name, stale)
			}
		}
		if got := strings.Count(out, "**Issue status"); got != 1 {
			t.Errorf("%s: brief has %d issue-status sections, want 1", name, got)
		}
	}
	leader := buildMetaSkillContent("claude", TaskContextForEnv{IssueID: "issue-1", HasProjectWorkflow: true, ProjectWorkflowStatusMap: true, IsSquadLeader: true})
	if !strings.Contains(leader, "dispatching members is not delivery — move the parent on only on a later turn") {
		t.Errorf("leader status-map brief must keep dispatch-is-not-delivery\n---\n%s", leader)
	}
}

// The status map flag only applies with a workflow block, and an issue
// without one picks closed statuses by the same rule.
func TestStatusMapFlagNeedsAWorkflowAndSharesTheClosedRule(t *testing.T) {
	t.Parallel()
	plain := buildMetaSkillContent("claude", TaskContextForEnv{IssueID: "issue-1", ProjectWorkflowStatusMap: true})
	if plain != buildMetaSkillContent("claude", TaskContextForEnv{IssueID: "issue-1"}) {
		t.Fatal("the status map flag must not change a brief without a project workflow")
	}
	if !strings.Contains(plain, issueStatusClosedRule) {
		t.Errorf("brief without a workflow must carry the closed-status rule\n---\n%s", plain)
	}
}
