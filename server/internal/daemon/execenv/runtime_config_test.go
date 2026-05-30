package execenv

import (
	"strings"
	"testing"
)

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

// FIR-2384 step 1: when the snapshot_prompt cost saving inlines the issue +
// thread into the start prompt (IssueSnapshotInlined), the standing runtime
// workflow brief must NOT also instruct the agent to run `multica issue get`
// + `multica issue comment list`. Tine's QA on the first cut failed precisely
// because buildMetaSkillContent still emitted those mandatory read steps, so
// the agent re-fetched what was already inlined and the saving stayed dead.
func TestSnapshotInlinedSuppressesRuntimeReadSteps(t *testing.T) {
	t.Parallel()
	const issueID = "11111111-2222-3333-4444-555555555555"
	const triggerID = "33333333-4444-5555-6666-777777777777"

	cases := []struct {
		name string
		ctx  TaskContextForEnv
		// stepInstructions are the numbered workflow steps that must vanish
		// when the snapshot is inlined (and must be present when it is not).
		stepInstructions []string
	}{
		{
			name: "comment-triggered",
			ctx:  TaskContextForEnv{IssueID: issueID, TriggerCommentID: triggerID},
			stepInstructions: []string{
				"1. Run `multica issue get",
				"3. Read the triggering conversation first",
			},
		},
		{
			name: "assignment-triggered",
			ctx:  TaskContextForEnv{IssueID: issueID},
			stepInstructions: []string{
				"1. Run `multica issue get",
				"3. Run `multica issue comment list",
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Saving off (default): the mandatory read steps must be present —
			// behaviour is unchanged when the workspace has not enabled snapshot.
			off := buildMetaSkillContent("claude", tc.ctx)
			for _, want := range tc.stepInstructions {
				if !strings.Contains(off, want) {
					t.Errorf("[%s] snapshot OFF: brief must still contain read step %q", tc.name, want)
				}
			}

			// Saving on: the same read steps must be gone, replaced by a note
			// pointing at the already-inlined context.
			onCtx := tc.ctx
			onCtx.IssueSnapshotInlined = true
			on := buildMetaSkillContent("claude", onCtx)
			for _, gone := range tc.stepInstructions {
				if strings.Contains(on, gone) {
					t.Errorf("[%s] snapshot ON: brief must NOT instruct read step %q (agent would re-fetch what was inlined)", tc.name, gone)
				}
			}
			if !strings.Contains(on, "they have been fetched for you") {
				t.Errorf("[%s] snapshot ON: brief must point the agent at the inlined context", tc.name)
			}
		})
	}
}

// The issue_context.md Quick Start must mirror the brief: no "run multica issue
// get" nudge once the snapshot inlined the issue into the start prompt.
func TestSnapshotInlinedSuppressesIssueContextQuickStart(t *testing.T) {
	t.Parallel()
	const issueID = "11111111-2222-3333-4444-555555555555"

	off := renderIssueContext("claude", TaskContextForEnv{IssueID: issueID})
	if !strings.Contains(off, "Run `multica issue get") {
		t.Errorf("snapshot OFF: issue_context Quick Start must contain the fetch instruction")
	}

	on := renderIssueContext("claude", TaskContextForEnv{IssueID: issueID, IssueSnapshotInlined: true})
	if strings.Contains(on, "Run `multica issue get") {
		t.Errorf("snapshot ON: issue_context Quick Start must NOT instruct a re-fetch")
	}
	if !strings.Contains(on, "already inlined in your task prompt") {
		t.Errorf("snapshot ON: issue_context Quick Start must point at the inlined context")
	}
}

// CEREBRO-PATCH(runtime-config-bundled): FIR-2384 step 2 — when the bundled_read
// cost saving is on (BundleContextHint), the standing runtime workflow brief
// must steer the agent at the single `multica issue context` call instead of
// the separate `multica issue get` + `multica issue comment list` reads. If the
// brief still ordered the separate reads, the agent would never call the
// endpoint whose per-task measurement the saving depends on — the same class of
// bug Tine flagged for step 1's snapshot path.
func TestBundleContextHintSteersRuntimeReadStepsToIssueContext(t *testing.T) {
	t.Parallel()
	const issueID = "11111111-2222-3333-4444-555555555555"
	const triggerID = "33333333-4444-5555-6666-777777777777"

	cases := []struct {
		name string
		ctx  TaskContextForEnv
		gone []string // separate-read steps that must vanish when bundled_read is on
	}{
		{
			name: "comment-triggered",
			ctx:  TaskContextForEnv{IssueID: issueID, TriggerCommentID: triggerID},
			gone: []string{"1. Run `multica issue get", "3. Read the triggering conversation first"},
		},
		{
			name: "assignment-triggered",
			ctx:  TaskContextForEnv{IssueID: issueID},
			gone: []string{"1. Run `multica issue get", "3. Run `multica issue comment list"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Off (default): the separate reads must be present — unchanged behaviour.
			off := buildMetaSkillContent("claude", tc.ctx)
			for _, want := range tc.gone {
				if !strings.Contains(off, want) {
					t.Errorf("[%s] bundled OFF: brief must still contain read step %q", tc.name, want)
				}
			}

			// On: the separate reads must be replaced by the single bundled call.
			onCtx := tc.ctx
			onCtx.BundleContextHint = true
			on := buildMetaSkillContent("claude", onCtx)
			for _, g := range tc.gone {
				if strings.Contains(on, g) {
					t.Errorf("[%s] bundled ON: brief must NOT instruct separate read %q (the agent must call `multica issue context`)", tc.name, g)
				}
			}
			if !strings.Contains(on, "multica issue context "+issueID) {
				t.Errorf("[%s] bundled ON: brief must steer the agent at `multica issue context %s`", tc.name, issueID)
			}
		})
	}

	// snapshot_prompt takes precedence: when both are set, the inlined snapshot
	// wins and the brief must not mention the bundled `issue context` call.
	bothCtx := TaskContextForEnv{IssueID: issueID, IssueSnapshotInlined: true, BundleContextHint: true}
	both := buildMetaSkillContent("claude", bothCtx)
	if strings.Contains(both, "multica issue context "+issueID) {
		t.Errorf("snapshot precedence: brief must use the inlined snapshot, not the bundled call, when both are on")
	}
	if !strings.Contains(both, "they have been fetched for you") {
		t.Errorf("snapshot precedence: brief must point at the inlined context when both are on")
	}

	// issue_context.md Quick Start mirrors the brief for bundled_read.
	qsOff := renderIssueContext("claude", TaskContextForEnv{IssueID: issueID})
	if !strings.Contains(qsOff, "Run `multica issue get") {
		t.Errorf("bundled OFF: issue_context Quick Start must contain the fetch instruction")
	}
	qsOn := renderIssueContext("claude", TaskContextForEnv{IssueID: issueID, BundleContextHint: true})
	if strings.Contains(qsOn, "Run `multica issue get") {
		t.Errorf("bundled ON: issue_context Quick Start must NOT instruct a separate `multica issue get`")
	}
	if !strings.Contains(qsOn, "multica issue context "+issueID) {
		t.Errorf("bundled ON: issue_context Quick Start must point at `multica issue context`")
	}
}
