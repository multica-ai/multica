package execenv

import (
	"strings"
	"testing"
)

// TestClassifyTask pins the precedence rule documented on classifyTask:
//
//  1. chat wins
//  2. quick-create
//  3. autopilot run-only
//  4. comment-triggered
//  5. otherwise assignment-triggered
//
// The pre-refactor builder relied on the daemon never setting two of
// ChatSessionID / QuickCreatePrompt / AutopilotRunID at once, but did not
// document the tiebreak. Now that the tiebreak is a function with a fixed
// switch order, it deserves a test so any future call site that violates
// the mutex still lands on a deterministic kind instead of silently picking
// up the wrong workflow.
func TestClassifyTask(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		ctx  TaskContextForEnv
		want taskKind
	}{
		{
			name: "chat",
			ctx:  TaskContextForEnv{ChatSessionID: "chat-1"},
			want: kindChat,
		},
		{
			name: "quick-create",
			ctx:  TaskContextForEnv{QuickCreatePrompt: "draft an issue"},
			want: kindQuickCreate,
		},
		{
			name: "autopilot-run-only",
			ctx:  TaskContextForEnv{AutopilotRunID: "run-1"},
			want: kindAutopilotRunOnly,
		},
		{
			name: "comment-triggered",
			ctx: TaskContextForEnv{
				IssueID:          "issue-1",
				TriggerCommentID: "comment-1",
			},
			want: kindCommentTriggered,
		},
		{
			name: "assignment-triggered",
			ctx:  TaskContextForEnv{IssueID: "issue-1"},
			want: kindAssignmentTriggered,
		},
		{
			name: "assignment-triggered-bare",
			ctx:  TaskContextForEnv{},
			want: kindAssignmentTriggered,
		},
		// Tiebreak cases — two specific-kind flags set at once. The
		// daemon never produces these, but if a future call site
		// accidentally does, classifyTask must still return a
		// deterministic kind chosen by the documented precedence.
		{
			name: "tiebreak-chat-beats-quick-create",
			ctx: TaskContextForEnv{
				ChatSessionID:     "chat-1",
				QuickCreatePrompt: "p",
			},
			want: kindChat,
		},
		{
			name: "tiebreak-quick-create-beats-autopilot",
			ctx: TaskContextForEnv{
				QuickCreatePrompt: "p",
				AutopilotRunID:    "run-1",
			},
			want: kindQuickCreate,
		},
		{
			name: "tiebreak-autopilot-beats-comment",
			ctx: TaskContextForEnv{
				AutopilotRunID:   "run-1",
				IssueID:          "issue-1",
				TriggerCommentID: "comment-1",
			},
			want: kindAutopilotRunOnly,
		},
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

// TestTaskKindHasIssueContext pins the predicate used to gate Issue Metadata
// and Sub-issue Creation. Equivalent to the pre-refactor scattered check
// `ChatSessionID == "" && QuickCreatePrompt == "" && AutopilotRunID == ""`;
// pulling it onto taskKind means the kind matrix decides — not three
// duplicated string compares in the builder.
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

// TestBuildMetaSkillContentKindMatrix locks in which sections each kind
// emits today. This is the post-refactor structural canary for MUL-3560
// PR 0.5: any later PR that drops a section from a kind must update the
// negative expectations here in lockstep, and any PR that accidentally
// adds a section back fails this test.
//
// The fixtures here intentionally use the minimal context required to
// trigger each kind, with one repo and one skill so Repositories / Skills
// fire. They are NOT meant to exercise every conditional inside each
// section — that is covered by the dedicated per-section tests in the
// rest of this package.
func TestBuildMetaSkillContentKindMatrix(t *testing.T) {
	t.Parallel()

	baseRepo := []RepoContextForEnv{{URL: "https://example.com/x.git", Description: "x"}}
	baseSkill := []SkillContextForEnv{{Name: "skill-x", Description: "x"}}

	type sectionCheck struct {
		heading string
		// kinds that MUST contain this heading
		mustHave map[taskKind]bool
		// kinds that MUST NOT contain it (left implicit: any kind not
		// in mustHave should not have it)
	}
	checks := []sectionCheck{
		{
			heading:  "# Multica Agent Runtime",
			mustHave: allKinds(),
		},
		{
			heading:  "## Background Task Safety",
			mustHave: allKinds(),
		},
		// Identity is only rendered when AgentName/ID/Instructions are
		// present; the matrix tests below provide AgentName for every
		// kind so it should fire across the board.
		{
			heading:  "## Agent Identity",
			mustHave: allKinds(),
		},
		{
			heading:  "## Available Commands",
			mustHave: allKinds(),
		},
		{
			heading:  "## Comment Formatting",
			mustHave: allKinds(),
		},
		{
			heading:  "## Repositories",
			mustHave: allKinds(),
		},
		{
			heading: "## Issue Metadata",
			mustHave: map[taskKind]bool{
				kindCommentTriggered:    true,
				kindAssignmentTriggered: true,
			},
		},
		{
			heading: "## Instruction Precedence",
			mustHave: map[taskKind]bool{
				kindAssignmentTriggered: true,
			},
		},
		{
			heading:  "### Workflow",
			mustHave: allKinds(),
		},
		{
			heading: "## Sub-issue Creation",
			mustHave: map[taskKind]bool{
				kindCommentTriggered:    true,
				kindAssignmentTriggered: true,
			},
		},
		{
			heading:  "## Skills",
			mustHave: allKinds(),
		},
		{
			heading:  "## Mentions",
			mustHave: allKinds(),
		},
		{
			heading:  "## Attachments",
			mustHave: allKinds(),
		},
		{
			heading:  "## Important: Always Use the `multica` CLI",
			mustHave: allKinds(),
		},
		{
			heading:  "## Output",
			mustHave: allKinds(),
		},
	}

	fixtures := map[taskKind]TaskContextForEnv{
		kindChat: {
			ChatSessionID: "chat-1",
			AgentName:     "Agent X",
			AgentID:       "agent-x",
			Repos:         baseRepo,
			AgentSkills:   baseSkill,
		},
		kindQuickCreate: {
			QuickCreatePrompt: "make an issue",
			AgentName:         "Agent X",
			AgentID:           "agent-x",
			Repos:             baseRepo,
			AgentSkills:       baseSkill,
		},
		kindAutopilotRunOnly: {
			AutopilotRunID: "run-1",
			AgentName:      "Agent X",
			AgentID:        "agent-x",
			Repos:          baseRepo,
			AgentSkills:    baseSkill,
		},
		kindCommentTriggered: {
			IssueID:          "issue-1",
			TriggerCommentID: "comment-1",
			AgentName:        "Agent X",
			AgentID:          "agent-x",
			Repos:            baseRepo,
			AgentSkills:      baseSkill,
		},
		kindAssignmentTriggered: {
			IssueID:     "issue-1",
			AgentName:   "Agent X",
			AgentID:     "agent-x",
			Repos:       baseRepo,
			AgentSkills: baseSkill,
		},
	}

	for kind, ctx := range fixtures {
		out := buildMetaSkillContent("claude", ctx)
		for _, c := range checks {
			present := strings.Contains(out, c.heading)
			want := c.mustHave[kind]
			if want && !present {
				t.Errorf("kind=%d: expected heading %q in brief", kind, c.heading)
			}
			if !want && present {
				t.Errorf("kind=%d: heading %q should NOT be in brief (matrix gating regression)", kind, c.heading)
			}
		}
	}
}

func allKinds() map[taskKind]bool {
	return map[taskKind]bool{
		kindCommentTriggered:    true,
		kindAssignmentTriggered: true,
		kindAutopilotRunOnly:    true,
		kindQuickCreate:         true,
		kindChat:                true,
	}
}
