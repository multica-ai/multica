package execenv

import (
	"strings"
  "testing"
)

func TestScopeIndexForBudget_UnderLimit(t *testing.T) {
	entries := []DocumentIndexEntry{
		{Path: "a.md"},
		{Path: "b.md"},
	}
	result, dropped := scopeIndexForBudget(entries, "MyProject", 200)
	if len(result) != 2 || dropped != 0 {
		t.Fatalf("expected 2 entries and 0 dropped, got %d and %d", len(result), dropped)
	}
}

func TestScopeIndexForBudget_OverLimitNoProject(t *testing.T) {
	entries := make([]DocumentIndexEntry, 250)
	for i := range entries {
		entries[i] = DocumentIndexEntry{Path: "doc/" + string(rune('a'+i%26)) + ".md"}
	}
	result, dropped := scopeIndexForBudget(entries, "", 200)
	if len(result) != 200 {
		t.Fatalf("expected 200 entries, got %d", len(result))
	}
	if dropped != 50 {
		t.Fatalf("expected 50 dropped, got %d", dropped)
	}
}

func TestScopeIndexForBudget_OverLimitWithProject(t *testing.T) {
	entries := make([]DocumentIndexEntry, 0, 210)
	// 10 project-scoped entries
	for i := 0; i < 10; i++ {
		entries = append(entries, DocumentIndexEntry{Path: "myproject/doc" + string(rune('a'+i)) + ".md"})
	}
	// 200 other entries
	for i := 0; i < 200; i++ {
		entries = append(entries, DocumentIndexEntry{Path: "other/doc" + string(rune('a'+i%26)) + ".md"})
	}

	result, dropped := scopeIndexForBudget(entries, "myproject", 200)

	// All 10 project entries should be included
	projectCount := 0
	for _, e := range result {
		if len(e.Path) > 10 && e.Path[:10] == "myproject/" {
			projectCount++
		}
	}
	if projectCount != 10 {
		t.Fatalf("expected 10 project entries, got %d", projectCount)
	}
	if len(result) != 200 {
		t.Fatalf("expected 200 entries total, got %d", len(result))
	}
	if dropped != 10 {
		t.Fatalf("expected 10 dropped, got %d", dropped)
	}
}

func TestScopeIndexForBudget_ResultIsSorted(t *testing.T) {
	entries := []DocumentIndexEntry{
		{Path: "zzz/doc.md"},
		{Path: "aaa/doc.md"},
		{Path: "myproj/doc.md"},
	}
	// Under limit — should return as-is (already passes through)
	result, _ := scopeIndexForBudget(entries, "myproj", 2)
	// Over limit — should be sorted
	for i := 1; i < len(result); i++ {
		if result[i].Path < result[i-1].Path {
			t.Fatalf("result not sorted: %s < %s", result[i].Path, result[i-1].Path)
		}
	}
}

// Sub-issue Creation section — after MUL-2538 the platform posts the
// child-done parent notification itself, so the brief no longer carries
// any parent-notification rule (per Bohan's call on PR #3055: delete the
// guidance entirely, do not replace it with a "do not post one" sentence
// — the agent should not be thinking about parent comments at all). All
// that remains is the `--status todo` vs `--status backlog` rule for
// creating sub-issues, which is unrelated to the notification path.

func TestSubIssueCreationSectionPresentForIssueRuns(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		ctx  TaskContextForEnv
	}{
		{
			name: "assignment-triggered",
			ctx:  TaskContextForEnv{IssueID: "11111111-2222-3333-4444-555555555555"},
		},
		{
			name: "comment-triggered",
			ctx: TaskContextForEnv{
				IssueID:          "22222222-3333-4444-5555-666666666666",
				TriggerCommentID: "33333333-4444-5555-6666-777777777777",
			},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := buildMetaSkillContent("claude", tc.ctx)

			if !strings.Contains(out, "## Sub-issue Creation") {
				t.Fatalf("expected Sub-issue Creation section in %s brief", tc.name)
			}
			for _, want := range []string{
				"**Choosing `--status` when creating sub-issues.**",
				"`--status todo` = **start now**",
				"`--status backlog` = **wait**",
				"`multica issue status <child-id> todo`",
				"all `--status todo`",
				"`--status backlog` from the start",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("[%s] section missing %q", tc.name, want)
				}
			}
		})
	}
}

// The brief must no longer carry any parent-notification guidance. PR
// #2918 added a "Tell the parent when you finish a child" rule that
// turned into noise (self-mention loops, planner ack ping-pong,
// hardcoded `MUL-` prefix). PR #3055 first downgraded it to a "do NOT
// post one" guardrail, but Bohan's product call was to remove the
// guidance entirely rather than substitute a new prohibition. These
// canaries lock that in: any wording that re-introduces the
// parent-comment concept — positive, negative, or descriptive — must
// not come back through future edits.
func TestBriefHasNoParentNotificationGuidance(t *testing.T) {
	t.Parallel()
	cases := []TaskContextForEnv{
		{IssueID: "11111111-2222-3333-4444-555555555555"},
		{
			IssueID:          "22222222-3333-4444-5555-666666666666",
			TriggerCommentID: "33333333-4444-5555-6666-777777777777",
		},
	}
	for _, ctx := range cases {
		ctx := ctx
		out := buildMetaSkillContent("claude", ctx)

		// The pre-MUL-2538 phrasing instructed the agent to compose a
		// parent comment by hand — including a hardcoded `MUL-` prefix
		// and an assignee mention. The intermediate revision (PR #3055
		// before Bohan's call) instead told the agent NOT to post one.
		// Both framings must stay out.
		for _, banned := range []string{
			// Old "do it yourself" framing (PR #2918).
			"## Parent / Sub-issue Protocol",
			"**Tell the parent when you finish a child.**",
			"multica issue comment add <parent-id>",
			"with NO `--parent`",
			"link the child as `[MUL-",
			"`@mention` the parent's assignee",
			"`mention://agent/<id>`",
			"`mention://member/<id>`",
			"`mention://squad/<id>`",
			// Intermediate "do NOT do it yourself" framing (PR #3055
			// before Bohan's call) — also out per product direction.
			"**Do NOT post your own parent-notification comment.**",
			"Do NOT post your own parent-notification comment",
			"parent-notification comment",
			"system comment on the parent fires from the status transition",
			"re-trigger the parent's assignee for nothing",
			"platform posts a top-level system comment on the parent",
			// Earlier revisions split rules by trigger type or used
			// table/subsection layouts. None of those structures should
			// come back either.
			"| Parent assignee | Parent status |",
			"The same agent as yourself",
			"| Member or squad |",
			"### A. Notify the parent",
			"### B. Choose",
			"When this issue has `parent_issue_id`:",
			"**Closing out child work** (only if this issue has `parent_issue_id`)",
			"**Notify the parent** (only if this issue has `parent_issue_id`",
			"**Creating sub-issues** (applies to any issue-bound run)",
			"For parent/child work, use these best-effort rules",
			// The protocol must no longer emit a placeholder
			// `<this-issue-id>` status flip — the workflow above owns
			// that command with the real issue id substituted.
			"`multica issue status <this-issue-id> in_review`",
			// Non-existent CLI form Elon's earlier review flagged.
			"issue list --parent",
		} {
			if strings.Contains(out, banned) {
				t.Errorf("expected %q to be removed from the brief", banned)
			}
		}
	}
}

// Comment-triggered briefs must NOT carry any unconditional status-flip
// command targeting the current issue. Previous revisions had a
// dedicated protocol step that wrote `multica issue status <this-issue-id> in_review`;
// the comment-triggered workflow rule "Do NOT change the issue status
// unless the comment explicitly asks for it" must remain the source of
// truth (Elon's blocking review on PR #2918).
func TestCommentTriggeredProtocolDoesNotForceInReview(t *testing.T) {
	t.Parallel()
	ctx := TaskContextForEnv{
		IssueID:          "55555555-6666-7777-8888-999999999999",
		TriggerCommentID: "66666666-7777-8888-9999-aaaaaaaaaaaa",
	}
	out := buildMetaSkillContent("claude", ctx)

	if strings.Contains(out, "`multica issue status <this-issue-id> in_review`") {
		t.Errorf("comment-triggered brief must not contain a placeholder `<this-issue-id> in_review` flip — that conflicts with the comment-triggered \"do not change status unless asked\" rule")
	}

	const guardrail = "Do NOT change the issue status unless the comment explicitly asks for it"
	if !strings.Contains(out, guardrail) {
		t.Errorf("expected the comment-triggered workflow guardrail %q to be present", guardrail)
	}
}

// Assignment-triggered briefs are the inverse boundary: when the agent
// owns the issue lifecycle, the brief AS A WHOLE must still tell it to
// flip to in_review on completion. The flip lives in the
// assignment-triggered workflow above (with the real id substituted).
func TestAssignmentTriggeredProtocolStillFlipsInReview(t *testing.T) {
	t.Parallel()
	const issueID = "77777777-8888-9999-aaaa-bbbbbbbbbbbb"
	ctx := TaskContextForEnv{IssueID: issueID}
	out := buildMetaSkillContent("claude", ctx)

	want := "`multica issue status " + issueID + " in_review`"
	if !strings.Contains(out, want) {
		t.Errorf("assignment-triggered brief must still flip to in_review on completion (expected %q in the workflow above)", want)
	}
}

// The sub-issue creation rule must reach top-level parents that have no
// `parent_issue_id` of their own — that is where the `todo` vs `backlog`
// decision matters most. The section must not gate on this issue being
// a child, and must not even mention `parent_issue_id`.
func TestSubIssueCreationSectionIsUnconditional(t *testing.T) {
	t.Parallel()
	ctx := TaskContextForEnv{
		IssueID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
	}
	out := buildMetaSkillContent("claude", ctx)

	const header = "## Sub-issue Creation"
	start := strings.Index(out, header)
	if start == -1 {
		t.Fatalf("sub-issue creation section missing")
	}
	rest := out[start:]
	end := strings.Index(rest[len(header):], "\n## ")
	var section string
	if end == -1 {
		section = rest
	} else {
		section = rest[:len(header)+end]
	}

	if strings.Contains(section, "parent_issue_id") {
		t.Errorf("Sub-issue Creation section must not reference `parent_issue_id` — it applies to any issue-bound run, including top-level parents:\n%s", section)
	}
}

// Workspace Context block: workspace.context (the per-workspace system prompt
// owners set in Settings → General) must reach the brief as `## Workspace
// Context` for every task kind so agents see a consistent shared system prompt
// regardless of how they were triggered. Empty content must skip the heading
// entirely — bare headings would just add noise.
func TestWorkspaceContextRenderedAcrossTaskKinds(t *testing.T) {
	t.Parallel()
	const wsContext = "All comments must be in English. Prefer concise PR descriptions."
	cases := []struct {
		name string
		ctx  TaskContextForEnv
	}{
		{
			name: "assignment-triggered",
			ctx: TaskContextForEnv{
				IssueID:          "11111111-2222-3333-4444-555555555555",
				WorkspaceContext: wsContext,
			},
		},
		{
			name: "comment-triggered",
			ctx: TaskContextForEnv{
				IssueID:          "22222222-3333-4444-5555-666666666666",
				TriggerCommentID: "33333333-4444-5555-6666-777777777777",
				WorkspaceContext: wsContext,
			},
		},
		{
			name: "chat",
			ctx: TaskContextForEnv{
				ChatSessionID:    "chat-1",
				WorkspaceContext: wsContext,
			},
		},
		{
			name: "quick-create",
			ctx: TaskContextForEnv{
				QuickCreatePrompt: "create me an issue",
				WorkspaceContext:  wsContext,
			},
		},
		{
			name: "autopilot run-only",
			ctx: TaskContextForEnv{
				AutopilotRunID:   "run-1",
				WorkspaceContext: wsContext,
			},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := buildMetaSkillContent("claude", tc.ctx)

			if !strings.Contains(out, "## Workspace Context") {
				t.Fatalf("[%s] expected `## Workspace Context` heading", tc.name)
			}
			if !strings.Contains(out, wsContext) {
				t.Errorf("[%s] brief missing workspace context body %q", tc.name, wsContext)
			}
			// The block must precede Available Commands so it acts as
			// background framing, not a footer hidden below CLI usage.
			ctxIdx := strings.Index(out, "## Workspace Context")
			cmdsIdx := strings.Index(out, "## Available Commands")
			if ctxIdx == -1 || cmdsIdx == -1 || ctxIdx > cmdsIdx {
				t.Errorf("[%s] `## Workspace Context` must appear above `## Available Commands` (ctx=%d, cmds=%d)", tc.name, ctxIdx, cmdsIdx)
			}
		})
	}
}

func TestWorkspaceContextHeadingSkippedWhenEmpty(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		ctx  TaskContextForEnv
	}{
		{
			name: "empty string",
			ctx: TaskContextForEnv{
				IssueID:          "11111111-2222-3333-4444-555555555555",
				WorkspaceContext: "",
			},
		},
		{
			name: "whitespace only",
			ctx: TaskContextForEnv{
				IssueID:          "11111111-2222-3333-4444-555555555555",
				WorkspaceContext: "   \n\t  \r\n",
			},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := buildMetaSkillContent("claude", tc.ctx)
			if strings.Contains(out, "## Workspace Context") {
				t.Errorf("[%s] empty workspace context must NOT emit the heading", tc.name)
			}
		})
	}
}

func TestSubIssueCreationSectionSkippedForNonIssueModes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		ctx  TaskContextForEnv
	}{
		{
			name: "chat",
			ctx:  TaskContextForEnv{ChatSessionID: "chat-1"},
		},
		{
			name: "quick-create",
			ctx:  TaskContextForEnv{QuickCreatePrompt: "create me an issue"},
		},
		{
			name: "autopilot run-only",
			ctx:  TaskContextForEnv{AutopilotRunID: "run-1"},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := buildMetaSkillContent("claude", tc.ctx)
			if strings.Contains(out, "## Sub-issue Creation") {
				t.Errorf("%s mode must NOT emit the Sub-issue Creation section", tc.name)
			}
		})
	}
}
