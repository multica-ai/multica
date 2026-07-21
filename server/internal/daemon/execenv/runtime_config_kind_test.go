package execenv

import (
	"strings"
	"testing"
)

// TestClassifyTask pins the precedence rule on classifyTask. All five
// kinds plus tiebreak cases for safety.
func TestClassifyTask(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		ctx  TaskContextForEnv
		want taskKind
	}{
		{"chat", TaskContextForEnv{ChatSessionID: "c"}, kindChat},
		{"quick-create", TaskContextForEnv{QuickCreatePrompt: "p"}, kindQuickCreate},
		{"autopilot", TaskContextForEnv{AutopilotRunID: "r"}, kindAutopilotRunOnly},
		{"comment-triggered", TaskContextForEnv{IssueID: "i", TriggerCommentID: "c"}, kindCommentTriggered},
		{"assignment-triggered", TaskContextForEnv{IssueID: "i"}, kindAssignmentTriggered},
		{"assignment-bare", TaskContextForEnv{}, kindAssignmentTriggered},
		{"tiebreak-chat-vs-quick", TaskContextForEnv{ChatSessionID: "c", QuickCreatePrompt: "p"}, kindChat},
		{"tiebreak-quick-vs-autopilot", TaskContextForEnv{QuickCreatePrompt: "p", AutopilotRunID: "r"}, kindQuickCreate},
		{"tiebreak-autopilot-vs-comment", TaskContextForEnv{AutopilotRunID: "r", IssueID: "i", TriggerCommentID: "c"}, kindAutopilotRunOnly},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyTask(tc.ctx); got != tc.want {
				t.Errorf("classifyTask: got %d, want %d", got, tc.want)
			}
		})
	}
}

// TestTaskKindHasIssueContext pins the predicate that gates Project
// Context / Issue Metadata / Sub-issue Creation in the slim dispatcher.
func TestTaskKindHasIssueContext(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind taskKind
		want bool
	}{
		{kindCommentTriggered, true},
		{kindAssignmentTriggered, true},
		{kindAutopilotRunOnly, false},
		{kindQuickCreate, false},
		{kindChat, false},
	}
	for _, tc := range cases {
		if got := tc.kind.hasIssueContext(); got != tc.want {
			t.Errorf("kind=%d hasIssueContext: got %v, want %v", tc.kind, got, tc.want)
		}
	}
}

// TestBuildMetaSkillContentBriefContent pins that buildMetaSkillContent
// renders the (now sole) brief: the `issue get` one-liner is present and
// the retired legacy verbose description is not.
func TestBuildMetaSkillContentBriefContent(t *testing.T) {
	t.Parallel()

	out := buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID:          "issue-1",
		TriggerCommentID: "comment-1",
		AgentName:        "Eve",
		AgentID:          "eve-1",
	})

	if !strings.Contains(out, "- `multica issue get <id> --output json` — full issue.\n") {
		t.Errorf("brief is missing the `issue get` one-liner\n---\n%s", out)
	}
	if strings.Contains(out, "Get full issue details.") {
		t.Errorf("brief still carries the retired legacy `issue get` description\n---\n%s", out)
	}
}

// TestBuildMetaSkillContentSlimKindMatrix locks in which sections the
// slim brief emits per task kind, machine-checking the matrix documented
// on `buildMetaSkillContentSlim`. Heading is matched as a discrete line
// (preceded by newline + followed by newline) so inline references like
// "see ## Comment Formatting" do not trip the absence assertions.
func TestBuildMetaSkillContentSlimKindMatrix(t *testing.T) {

	baseRepo := []RepoContextForEnv{{URL: "https://example.com/x.git", Description: "x"}}
	baseSkill := []SkillContextForEnv{{Name: "skill-x", Description: "x"}}

	type sectionCheck struct {
		heading  string
		mustHave map[taskKind]bool
	}
	allKinds := map[taskKind]bool{
		kindCommentTriggered: true, kindAssignmentTriggered: true,
		kindAutopilotRunOnly: true, kindQuickCreate: true, kindChat: true,
	}
	issueKinds := map[taskKind]bool{
		kindCommentTriggered: true, kindAssignmentTriggered: true,
	}
	checks := []sectionCheck{
		{"# Multica Agent Runtime", allKinds},
		{"## Background Task Safety", allKinds},
		{"## Agent Identity", allKinds},
		{"## Available Commands", allKinds},
		{"### Workflow", allKinds},
		{"## Important: Always Use the `multica` CLI", allKinds},
		{"## Output", allKinds},
		{"## Comment Formatting", issueKinds},
		{"## Repositories", map[taskKind]bool{
			kindCommentTriggered: true, kindAssignmentTriggered: true,
			kindAutopilotRunOnly: true, kindChat: true,
		}},
		{"## Issue Metadata", issueKinds},
		{"## Instruction Precedence", map[taskKind]bool{kindAssignmentTriggered: true}},
		{"## Sub-issue Creation", issueKinds},
		{"## Skills", map[taskKind]bool{
			kindCommentTriggered: true, kindAssignmentTriggered: true,
			kindAutopilotRunOnly: true, kindChat: true,
		}},
		{"## Mentions", issueKinds},
		{"## Attachments", issueKinds},
	}

	fixtures := map[taskKind]TaskContextForEnv{
		kindChat: {ChatSessionID: "c-1", AgentName: "Eve", AgentID: "eve-1",
			Repos: baseRepo, AgentSkills: baseSkill},
		kindQuickCreate: {QuickCreatePrompt: "p", AgentName: "Eve", AgentID: "eve-1",
			Repos: baseRepo, AgentSkills: baseSkill},
		kindAutopilotRunOnly: {AutopilotRunID: "r-1", AgentName: "Eve", AgentID: "eve-1",
			Repos: baseRepo, AgentSkills: baseSkill},
		kindCommentTriggered: {IssueID: "i-1", TriggerCommentID: "tc-1",
			AgentName: "Eve", AgentID: "eve-1", Repos: baseRepo, AgentSkills: baseSkill},
		kindAssignmentTriggered: {IssueID: "i-1", AgentName: "Eve", AgentID: "eve-1",
			Repos: baseRepo, AgentSkills: baseSkill},
	}

	for kind, ctx := range fixtures {
		out := buildMetaSkillContent("claude", ctx)
		for _, c := range checks {
			needle := "\n" + c.heading + "\n"
			firstLine := c.heading + "\n"
			present := strings.HasPrefix(out, firstLine) || strings.Contains(out, needle)
			want := c.mustHave[kind]
			if want && !present {
				t.Errorf("kind=%d: expected heading %q in slim brief", kind, c.heading)
			}
			if !want && present {
				t.Errorf("kind=%d: heading %q should NOT be in slim brief (matrix gating regression)", kind, c.heading)
			}
		}
	}
}

// TestSlimQuickCreateAvailableCommands locks the minimal-variant content
// for quick-create's Available Commands: `issue create` present, every
// other Core command absent (the hard guardrails forbid the call).
func TestSlimQuickCreateAvailableCommands(t *testing.T) {

	out := buildMetaSkillContent("codex", TaskContextForEnv{
		QuickCreatePrompt: "create an issue about flaky tests",
		AgentName:         "Eve", AgentID: "eve-1",
	})

	for _, want := range []string{
		"## Available Commands",
		"multica issue create --title",
		"`multica --help`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("quick_create slim Available Commands missing %q", want)
		}
	}

	for _, banned := range []string{
		"multica issue get <id>",
		"multica issue comment list <issue-id>",
		"multica issue update <id>",
		"multica issue status <id> <status>",
		"multica issue comment add <issue-id>",
		"multica issue metadata list <issue-id>",
		"multica issue metadata set <issue-id>",
		"multica issue metadata delete <issue-id>",
		"multica issue children <id>",
		"multica repo checkout <url>",
		"### Squad maintenance",
		"multica squad member set-role",
	} {
		if strings.Contains(out, banned) {
			t.Errorf("quick_create slim Available Commands should NOT advertise %q (hard guardrails forbid the call)", banned)
		}
	}
}

// TestBackgroundTaskSafetySlimHardPins asserts the slim brief carries the
// same hardened Background Task Safety pins as the legacy brief (MUL-4140).
// The verbose path is covered by
// TestInjectRuntimeConfigBackgroundTaskSafetyProviderAgnostic; this locks
// the compressed slim path so a future slim-brief trim can't quietly drop
// the no-background-and-yield / no-"standing by" guardrails that address
// the MUL-4091 mechanism.
func TestBackgroundTaskSafetySlimHardPins(t *testing.T) {

	out := buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID: "i-1", TriggerCommentID: "tc-1",
		AgentName: "Eve", AgentID: "eve-1",
	})

	for _, want := range []string{
		"## Background Task Safety",
		"Do NOT end your turn while background tasks",
		"wait for a future notification/reminder",
		"run the work synchronously instead",
		"Never background-and-yield",
		"foreground tool call that blocks",
		"gh run watch",
		"running in the background so you can keep working",
		"standing by",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("slim Background Task Safety missing hardened pin %q\n---\n%s", want, out)
		}
	}
}
