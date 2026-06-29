package execenv

import (
	"fmt"
	"os"
	"path/filepath"
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
				// Stage guidance must reach the always-on brief so agents
				// reach for stages instead of only the manual backlog chain
				// (MUL-3508 follow-up).
				"**Ordering with stages.**",
				"`--stage <N>`",
				"`multica issue children <id>`",
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

// The CLAUDE.md workflow surface must carry the same issue-wide since-delta
// new-comment hint as the per-turn prompt. PR #2816 requires the two surfaces
// stay in sync.
func TestCommentTriggeredBriefCarriesNewCommentsHint(t *testing.T) {
	t.Parallel()
	const (
		issueID = "55555555-6666-7777-8888-999999999999"
		since   = "2026-05-28T11:00:00Z"
	)
	ctx := TaskContextForEnv{
		IssueID:          issueID,
		TriggerCommentID: "reply-abc",
		NewCommentCount:  4,
		NewCommentsSince: since,
	}
	out := buildMetaSkillContent("claude", ctx)

	// Issue-wide count.
	if !strings.Contains(out, "4 new comment(s) on this issue since your last run") {
		t.Errorf("comment brief must report the issue-wide new-comment count, got:\n%s", out)
	}
	if !strings.Contains(out, "blindly") {
		t.Errorf("comment brief must discourage blindly reading every new comment, got:\n%s", out)
	}
	// Parent thread first.
	if !strings.Contains(out, "--thread reply-abc --since "+since+" --output json") {
		t.Errorf("comment brief must point at the triggering (parent) thread --since read first, got:\n%s", out)
	}
	if !strings.Contains(out, "--tail 30") {
		t.Errorf("comment brief must offer the full-thread (--tail 30) option, got:\n%s", out)
	}
	// Issue-wide catch-up demoted to an only-if-needed fallback.
	if !strings.Contains(out, "multica issue comment list "+issueID+" --since "+since+" --output json") {
		t.Errorf("comment brief must keep the issue-wide --since catch-up fallback, got:\n%s", out)
	}
	// The removed resolve step must not reappear.
	if strings.Contains(out, "multica comment resolve") {
		t.Errorf("comment brief must not carry the dropped resolve step, got:\n%s", out)
	}
}

// Cold start (no prior run → no since anchor) must point the agent at the
// triggering CONVERSATION (--thread <trigger> --tail 30) instead of the flat
// timeline dump or the since-delta hint.
func TestCommentTriggeredBriefColdStartThreadRead(t *testing.T) {
	t.Parallel()
	const issueID = "55555555-6666-7777-8888-999999999999"
	ctx := TaskContextForEnv{
		IssueID:          issueID,
		TriggerCommentID: "trigger-1",
		TriggerThreadID:  "thread-root-1",
		NewCommentCount:  0,
		NewCommentsSince: "",
	}
	out := buildMetaSkillContent("claude", ctx)
	if strings.Contains(out, "new comment(s) since your last run") {
		t.Errorf("no since-delta hint should render on cold start, got:\n%s", out)
	}
	if !strings.Contains(out, "multica issue comment list "+issueID+" --thread thread-root-1 --tail 30 --output json") {
		t.Errorf("cold start must point at the triggering thread read, got:\n%s", out)
	}
}

// A resumed comment session with no since-delta should not fall back to the
// cold-start "read the triggering conversation first" instruction. The trigger
// body is already embedded in the per-turn prompt and the resumed session should
// carry prior thread context, so the thread read is only a fallback.
func TestCommentTriggeredBriefResumedNoDeltaSkipsDefaultThreadRead(t *testing.T) {
	t.Parallel()
	const issueID = "55555555-6666-7777-8888-999999999999"
	ctx := TaskContextForEnv{
		IssueID:             issueID,
		TriggerCommentID:    "trigger-1",
		TriggerThreadID:     "thread-root-1",
		PriorSessionResumed: true,
		NewCommentCount:     0,
		NewCommentsSince:    "",
	}
	out := buildMetaSkillContent("claude", ctx)

	for _, want := range []string{
		"triggering comment is already included above",
		"No other new comments on this issue since your last run",
		"active thread anchor `thread-root-1` and triggering comment ID `trigger-1`",
		"If your reply depends on thread context",
		"do not rely only on resumed session memory",
		"multica issue comment list " + issueID + " --thread thread-root-1 --tail 30 --output json",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("resumed/no-delta brief missing %q\n--- output ---\n%s", want, out)
		}
	}
	if strings.Contains(out, "scoped to the triggering thread") {
		t.Errorf("resumed/no-delta brief must not claim the delta is thread-scoped, got:\n%s", out)
	}
	if strings.Contains(out, "Read the triggering conversation first") {
		t.Errorf("resumed/no-delta brief must not use the cold-start forced-read wording, got:\n%s", out)
	}
}

// Assignment-triggered briefs are the high-risk path for role conflicts:
// non-executor agents still need issue context, but the runtime workflow must
// not turn status changes, investigation, implementation, or delegation into
// permissions that override Agent Identity.
func TestAssignmentTriggeredProtocolHonorsAgentIdentity(t *testing.T) {
	t.Parallel()
	const issueID = "77777777-8888-9999-aaaa-bbbbbbbbbbbb"
	ctx := TaskContextForEnv{IssueID: issueID}
	out := buildMetaSkillContent("claude", ctx)

	for _, want := range []string{
		"## Instruction Precedence",
		"Agent Identity instructions have priority over the assignment workflow below.",
		"If a workflow step conflicts with Agent Identity, skip the conflicting action",
		"Never treat this runtime workflow as permission to change issue status, investigate, implement",
		"Run `multica issue status " + issueID + " in_progress` unless your Agent Identity forbids issue status changes; if it does, skip this step.",
		"Complete the task within your Agent Identity boundaries.",
		"Do not investigate, implement, create issues, update issues, or delegate if your Agent Identity forbids that action",
		"When done, run `multica issue status " + issueID + " in_review` unless your Agent Identity forbids issue status changes; if it does, skip this step.",
		"If blocked, run `multica issue status " + issueID + " blocked` unless your Agent Identity forbids issue status changes.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("assignment-triggered brief missing identity-bound workflow text %q\n---\n%s", want, out)
		}
	}

	for _, banned := range []string{
		"4. Run `multica issue status " + issueID + " in_progress`\n",
		"5. Follow your Skills and Agent Identity to complete the task (write code, investigate, etc.)",
		"8. When done, run `multica issue status " + issueID + " in_review`\n",
	} {
		if strings.Contains(out, banned) {
			t.Errorf("assignment-triggered brief still contains unconditional legacy workflow text %q\n---\n%s", banned, out)
		}
	}
}

func TestInstructionPrecedenceOnlyAppliesToAssignmentWorkflow(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		ctx  TaskContextForEnv
	}{
		{
			name: "comment-triggered",
			ctx: TaskContextForEnv{
				IssueID:          "11111111-2222-3333-4444-555555555555",
				TriggerCommentID: "22222222-3333-4444-5555-666666666666",
			},
		},
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
			for _, banned := range []string{
				"## Instruction Precedence",
				"assignment workflow below",
				"Never treat this runtime workflow as permission to change issue status",
			} {
				if strings.Contains(out, banned) {
					t.Errorf("%s brief must not inherit assignment-only precedence text %q\n---\n%s", tc.name, banned, out)
				}
			}
		})
	}
}

func TestChatOutputDoesNotRequireIssueComment(t *testing.T) {
	t.Parallel()

	out := buildMetaSkillContent("claude", TaskContextForEnv{ChatSessionID: "chat-1"})

	for _, want := range []string{
		"This is a chat session",
		"Your reply is delivered directly to the chat window the user is reading",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("chat brief missing chat output guidance %q\n---\n%s", want, out)
		}
	}

	for _, banned := range []string{
		"Final results MUST be delivered via `multica issue comment add`",
		"The user does NOT see your terminal output",
		"do not call `multica issue comment add`",
		"unless the user explicitly asks",
	} {
		if strings.Contains(out, banned) {
			t.Errorf("chat brief must not inherit issue-comment output warning %q\n---\n%s", banned, out)
		}
	}
}

// The Output section for issue tasks must forbid mid-run progress
// comments and require the single final result comment. Guards the
// MUL-3605 regression where a review agent surfaced its progress
// narration as the result instead of posting a conclusion. (The
// pre-existing "Final results MUST be delivered … invisible without it"
// and "state the outcome, not the process" lines already carry the
// mandatory-comment and no-process-dump halves.) Chat / quick-create /
// autopilot kinds keep their own delivery channels and must NOT inherit
// this rule. Runs both the legacy and slim paths.
func TestOutputForbidsMidRunProgressComments(t *testing.T) {
	wantPhrases := []string{
		"Post exactly ONE comment per run",
		"Do NOT post progress updates",
	}
	issueCtxs := map[string]TaskContextForEnv{
		"assignment": {IssueID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"},
		"comment":    {IssueID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", TriggerCommentID: "tc-1"},
	}

	run := func(t *testing.T, label string) {
		for name, ctx := range issueCtxs {
			out := buildMetaSkillContent("claude", ctx)
			for _, want := range wantPhrases {
				if !strings.Contains(out, want) {
					t.Errorf("%s/%s brief missing output rule %q\n---\n%s", label, name, want, out)
				}
			}
		}
		// Chat keeps its own delivery channel; it must not inherit the
		// issue-task "post a final comment" rules.
		chat := buildMetaSkillContent("claude", TaskContextForEnv{ChatSessionID: "chat-1"})
		for _, banned := range wantPhrases {
			if strings.Contains(chat, banned) {
				t.Errorf("%s chat brief must not inherit issue output rule %q", label, banned)
			}
		}
	}

	// Not parallel: the slim subtest toggles a process-wide feature flag.
	t.Run("legacy", func(t *testing.T) { run(t, "legacy") })
	t.Run("slim", func(t *testing.T) {
		withSlimBrief(t)
		run(t, "slim")
	})
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

// writeRuntimeConfigFile is the safe replacement for the previous
// unconditional os.WriteFile of CLAUDE.md / AGENTS.md. The two
// states it must handle correctly are: file missing, file present without
// markers (user-authored content already there — the regression case from
// MUL-2753), and file present with markers (idempotent second-run replace).

func TestWriteRuntimeConfigFileCreatesMissingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	const brief = "# Multica Agent Runtime\n\nbrief body line"

	if err := writeRuntimeConfigFile(path, brief); err != nil {
		t.Fatalf("writeRuntimeConfigFile returned error: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back file: %v", err)
	}
	s := string(got)
	if !strings.HasPrefix(s, runtimeMarkerBegin+"\n") {
		t.Errorf("output should start with begin marker, got:\n%s", s)
	}
	if !strings.Contains(s, brief) {
		t.Errorf("output should contain brief body, got:\n%s", s)
	}
	if !strings.Contains(s, "\n"+runtimeMarkerEnd+"\n") {
		t.Errorf("output should contain end marker followed by newline, got:\n%s", s)
	}
}

func TestWriteRuntimeConfigFilePreservesUserContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	const userContent = "# User repo CLAUDE.md\n\n- rule one\n- rule two\n"
	if err := os.WriteFile(path, []byte(userContent), 0o644); err != nil {
		t.Fatalf("seed user file: %v", err)
	}

	const brief = "## Multica brief\n\ninjected body"
	if err := writeRuntimeConfigFile(path, brief); err != nil {
		t.Fatalf("writeRuntimeConfigFile returned error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back file: %v", err)
	}
	s := string(got)
	// The user's original content must be untouched and appear before the
	// injected marker block; this is the core regression case from MUL-2753.
	if !strings.HasPrefix(s, userContent) {
		t.Errorf("user content must be preserved verbatim at the top of the file, got:\n%s", s)
	}
	beginIdx := strings.Index(s, runtimeMarkerBegin)
	endIdx := strings.Index(s, runtimeMarkerEnd)
	if beginIdx < 0 || endIdx <= beginIdx {
		t.Fatalf("expected a well-formed marker block in:\n%s", s)
	}
	if beginIdx < len(userContent) {
		t.Errorf("begin marker must appear after user content, beginIdx=%d userLen=%d", beginIdx, len(userContent))
	}
	if !strings.Contains(s, brief) {
		t.Errorf("brief body missing from output:\n%s", s)
	}
}

func TestWriteRuntimeConfigFileReplacesExistingBlock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	const userBefore = "# User AGENTS.md\n\nuser line above\n"
	const userAfter = "\nuser line below the block\n"
	original := userBefore +
		runtimeMarkerBegin + "\n" +
		"OLD BRIEF CONTENT THAT MUST GO AWAY\n" +
		runtimeMarkerEnd + "\n" +
		userAfter
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	const newBrief = "## New Multica brief\n\nfresh body"
	if err := writeRuntimeConfigFile(path, newBrief); err != nil {
		t.Fatalf("writeRuntimeConfigFile returned error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back file: %v", err)
	}
	s := string(got)
	if !strings.HasPrefix(s, userBefore) {
		t.Errorf("content above the marker block must be preserved, got:\n%s", s)
	}
	if !strings.HasSuffix(s, userAfter) {
		t.Errorf("content below the marker block must be preserved, got:\n%s", s)
	}
	if strings.Contains(s, "OLD BRIEF CONTENT THAT MUST GO AWAY") {
		t.Errorf("previous block body must be replaced, got:\n%s", s)
	}
	if !strings.Contains(s, newBrief) {
		t.Errorf("new brief body missing from output:\n%s", s)
	}
	if strings.Count(s, runtimeMarkerBegin) != 1 || strings.Count(s, runtimeMarkerEnd) != 1 {
		t.Errorf("there must be exactly one begin/end marker pair, got:\n%s", s)
	}
}

func TestWriteRuntimeConfigFileIsIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	const userContent = "# User CLAUDE.md\n\nimportant rules\n"
	if err := os.WriteFile(path, []byte(userContent), 0o644); err != nil {
		t.Fatalf("seed user file: %v", err)
	}

	const brief = "## Multica brief\n\nbody"
	for i := 0; i < 5; i++ {
		if err := writeRuntimeConfigFile(path, brief); err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back file: %v", err)
	}
	s := string(got)
	if strings.Count(s, runtimeMarkerBegin) != 1 {
		t.Errorf("repeated runs must not duplicate the begin marker, count=%d, file:\n%s", strings.Count(s, runtimeMarkerBegin), s)
	}
	if strings.Count(s, runtimeMarkerEnd) != 1 {
		t.Errorf("repeated runs must not duplicate the end marker, count=%d, file:\n%s", strings.Count(s, runtimeMarkerEnd), s)
	}
	if strings.Count(s, brief) != 1 {
		t.Errorf("repeated runs must not duplicate the brief body, count=%d, file:\n%s", strings.Count(s, brief), s)
	}
	if !strings.HasPrefix(s, userContent) {
		t.Errorf("user content must remain intact at the top of the file, got:\n%s", s)
	}
}

// InjectRuntimeConfig is the production entry point — verify the marker
// semantics propagate through it for each provider's target filename.
func TestInjectRuntimeConfigPreservesUserContent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		provider string
		filename string
	}{
		{"claude", "CLAUDE.md"},
		{"codex", "AGENTS.md"},
		{"copilot", "AGENTS.md"},
		{"opencode", "AGENTS.md"},
		{"openclaw", "AGENTS.md"},
		{"hermes", "AGENTS.md"},
		{"pi", "AGENTS.md"},
		{"cursor", "AGENTS.md"},
		{"kimi", "AGENTS.md"},
		{"kiro", "AGENTS.md"},
		{"antigravity", "AGENTS.md"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.provider, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, tc.filename)
			const userContent = "# User-authored file\n\ndon't touch this\n"
			if err := os.WriteFile(path, []byte(userContent), 0o644); err != nil {
				t.Fatalf("seed: %v", err)
			}

			content, err := InjectRuntimeConfig(dir, tc.provider, TaskContextForEnv{
				IssueID: "11111111-2222-3333-4444-555555555555",
			})
			if err != nil {
				t.Fatalf("InjectRuntimeConfig: %v", err)
			}
			if content == "" {
				t.Fatalf("returned brief content must be non-empty")
			}

			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			s := string(got)
			if !strings.HasPrefix(s, userContent) {
				t.Errorf("[%s] user content must be preserved verbatim at the top of %s, got:\n%s", tc.provider, tc.filename, s)
			}
			if !strings.Contains(s, runtimeMarkerBegin) || !strings.Contains(s, runtimeMarkerEnd) {
				t.Errorf("[%s] %s must contain the runtime marker block, got:\n%s", tc.provider, tc.filename, s)
			}
		})
	}
}

func TestInjectRuntimeConfigUnknownProviderSkipsWrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Seed all three candidate filenames so we can verify none of them get
	// written when the provider is unknown.
	for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("untouched\n"), 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}

	if _, err := InjectRuntimeConfig(dir, "totally-unknown-provider", TaskContextForEnv{
		IssueID: "11111111-2222-3333-4444-555555555555",
	}); err != nil {
		t.Fatalf("InjectRuntimeConfig: %v", err)
	}
	for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(got) != "untouched\n" {
			t.Errorf("unknown provider must not write %s; got:\n%s", name, string(got))
		}
	}
}

// Parser hardening: the end marker must be found strictly after the begin
// marker so a stray end marker that appears earlier in user content (e.g.
// a documentation snippet showing what the wire format looks like) doesn't
// trick writeRuntimeConfigFile into thinking the file is malformed and
// appending another block on every run.
func TestWriteRuntimeConfigFileIgnoresStrayEndMarkerBeforeBegin(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	// Seed a file whose user-authored portion documents the marker format
	// (so the *end* marker appears before any *begin* marker), then has a
	// real block authored by an earlier Multica run below.
	const userDoc = "# Repo CLAUDE.md\n\nExample of what Multica writes:\n" +
		runtimeMarkerEnd + "\n\n# Real config below\n"
	original := userDoc +
		runtimeMarkerBegin + "\nFIRST BRIEF\n" + runtimeMarkerEnd + "\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	const newBrief = "SECOND BRIEF"
	if err := writeRuntimeConfigFile(path, newBrief); err != nil {
		t.Fatalf("writeRuntimeConfigFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	s := string(got)

	// The user's stray end marker line plus surrounding doc text must still
	// be present, and the file must contain exactly one begin marker and
	// one *additional* end marker (so two end markers total — the stray
	// one and the one closing our block).
	if !strings.Contains(s, userDoc) {
		t.Errorf("user doc with stray end marker must be preserved verbatim, got:\n%s", s)
	}
	if got, want := strings.Count(s, runtimeMarkerBegin), 1; got != want {
		t.Errorf("expected exactly %d begin markers, got %d:\n%s", want, got, s)
	}
	if got, want := strings.Count(s, runtimeMarkerEnd), 2; got != want {
		t.Errorf("expected exactly %d end markers (1 user stray + 1 closing our block), got %d:\n%s", want, got, s)
	}
	if strings.Contains(s, "FIRST BRIEF") {
		t.Errorf("previous brief body must be replaced, got:\n%s", s)
	}
	if !strings.Contains(s, newBrief) {
		t.Errorf("new brief body missing from output:\n%s", s)
	}

	// Idempotency under the stray-end pattern: a second write must not
	// stack another block.
	if err := writeRuntimeConfigFile(path, newBrief); err != nil {
		t.Fatalf("second writeRuntimeConfigFile: %v", err)
	}
	got2, _ := os.ReadFile(path)
	s2 := string(got2)
	if got, want := strings.Count(s2, runtimeMarkerBegin), 1; got != want {
		t.Errorf("repeat write must not grow begin markers, got %d, want %d:\n%s", got, want, s2)
	}
}

// Parser hardening: a file containing only a begin marker (e.g. a previous
// run that crashed mid-write) must not cause every subsequent run to stack
// another block beneath the half-block.
func TestWriteRuntimeConfigFileReplacesMalformedHalfBlock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")

	const userTop = "# Repo AGENTS.md\n\nrules above\n"
	const halfBlock = "leftover from crashed write\nsecond line\n"
	original := userTop + runtimeMarkerBegin + "\n" + halfBlock
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	const newBrief = "recovered brief"
	if err := writeRuntimeConfigFile(path, newBrief); err != nil {
		t.Fatalf("writeRuntimeConfigFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	s := string(got)
	if !strings.HasPrefix(s, userTop) {
		t.Errorf("user content above the half-block must be preserved, got:\n%s", s)
	}
	if strings.Contains(s, "leftover from crashed write") {
		t.Errorf("half-block contents must be replaced, got:\n%s", s)
	}
	if got, want := strings.Count(s, runtimeMarkerBegin), 1; got != want {
		t.Errorf("expected exactly %d begin marker, got %d:\n%s", want, got, s)
	}
	if got, want := strings.Count(s, runtimeMarkerEnd), 1; got != want {
		t.Errorf("expected exactly %d end marker after recovery, got %d:\n%s", want, got, s)
	}
	if !strings.Contains(s, newBrief) {
		t.Errorf("new brief body missing from output:\n%s", s)
	}
}

// Cleanup excises the marker block, preserving every byte of surrounding
// user content. This is the local_directory invariant: a `claude` /
// `codex` run started by the user after a Multica task must see the same
// file the user wrote.
func TestCleanupRuntimeConfigPreservesUserContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	const userBefore = "# Repo CLAUDE.md\n\nuser line above\n"
	const userAfter = "\nuser line below the block\n"
	const userExpected = "# Repo CLAUDE.md\n\nuser line above\n\nuser line below the block\n"
	// Inject via the production write path so we exercise the actual
	// marker block format, not a hand-rolled approximation.
	if err := os.WriteFile(path, []byte(userBefore+userAfter), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := writeRuntimeConfigFile(path, "brief body"); err != nil {
		t.Fatalf("seed brief: %v", err)
	}

	if err := CleanupRuntimeConfig(dir, "claude"); err != nil {
		t.Fatalf("CleanupRuntimeConfig: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	s := string(got)
	if strings.Contains(s, runtimeMarkerBegin) || strings.Contains(s, runtimeMarkerEnd) {
		t.Errorf("marker block must be removed, got:\n%s", s)
	}
	if strings.Contains(s, "brief body") {
		t.Errorf("brief body must be removed, got:\n%s", s)
	}
	if s != userExpected {
		t.Errorf("user content must be preserved byte-for-byte\n got:\n%q\nwant:\n%q", s, userExpected)
	}
}

// Cleanup removes the file entirely when the marker block was the only
// content — i.e. we created the file from scratch in a directory that had
// no pre-existing CLAUDE.md / AGENTS.md.
func TestCleanupRuntimeConfigRemovesFileWhenOnlyBlockRemained(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	// No seed — writeRuntimeConfigFile creates the file with only the
	// marker block inside.
	if err := writeRuntimeConfigFile(path, "brief body"); err != nil {
		t.Fatalf("seed brief: %v", err)
	}

	if err := CleanupRuntimeConfig(dir, "claude"); err != nil {
		t.Fatalf("CleanupRuntimeConfig: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected file to be removed, stat err=%v", err)
	}
}

// Cleanup is a no-op when no marker block exists or when the file is
// missing — Cleanup is safe to call defensively from the daemon's defer.
func TestCleanupRuntimeConfigNoOpCases(t *testing.T) {
	t.Parallel()

	t.Run("missing file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := CleanupRuntimeConfig(dir, "claude"); err != nil {
			t.Errorf("missing file must be no-op, got: %v", err)
		}
		// And the directory must remain untouched.
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("readdir: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("expected dir to remain empty, got: %v", entries)
		}
	})

	t.Run("file without marker block", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "CLAUDE.md")
		const userContent = "# Repo CLAUDE.md\n\nrules\n"
		if err := os.WriteFile(path, []byte(userContent), 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := CleanupRuntimeConfig(dir, "claude"); err != nil {
			t.Errorf("no-marker-block file must be no-op, got: %v", err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		if string(got) != userContent {
			t.Errorf("file must be untouched\n got:\n%q\nwant:\n%q", string(got), userContent)
		}
	})

	t.Run("unknown provider", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		// Seed every candidate filename to verify none of them get touched.
		for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("untouched\n"), 0o644); err != nil {
				t.Fatalf("seed %s: %v", name, err)
			}
		}
		if err := CleanupRuntimeConfig(dir, "totally-unknown-provider"); err != nil {
			t.Errorf("unknown provider must be no-op, got: %v", err)
		}
		for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
			got, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			if string(got) != "untouched\n" {
				t.Errorf("unknown provider must not touch %s; got:\n%s", name, string(got))
			}
		}
	})
}

// Cleanup must handle a half-block left by a previous crashed run: begin
// marker present but no end. Otherwise the half-block would survive
// cleanup and pollute the next manual CLI invocation in the same dir.
func TestCleanupRuntimeConfigRemovesMalformedHalfBlock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")

	const userTop = "# Repo AGENTS.md\n\nrules\n"
	original := userTop + runtimeMarkerBegin + "\nhalf-written brief no end\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := CleanupRuntimeConfig(dir, "codex"); err != nil {
		t.Fatalf("CleanupRuntimeConfig: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	s := string(got)
	if strings.Contains(s, runtimeMarkerBegin) {
		t.Errorf("half-block begin marker must be excised, got:\n%s", s)
	}
	if strings.Contains(s, "half-written brief no end") {
		t.Errorf("half-block body must be excised, got:\n%s", s)
	}
	if !strings.HasPrefix(s, userTop) {
		t.Errorf("user content above the half-block must remain, got:\n%s", s)
	}
}

// Cleanup must remove the marker block for every provider's target file,
// using the same provider→filename mapping as InjectRuntimeConfig — so a
// new provider added to one side cannot drift past the other.
func TestCleanupRuntimeConfigByProvider(t *testing.T) {
	t.Parallel()
	cases := []struct {
		provider string
		filename string
	}{
		{"claude", "CLAUDE.md"},
		{"codex", "AGENTS.md"},
		{"copilot", "AGENTS.md"},
		{"opencode", "AGENTS.md"},
		{"openclaw", "AGENTS.md"},
		{"hermes", "AGENTS.md"},
		{"pi", "AGENTS.md"},
		{"cursor", "AGENTS.md"},
		{"kimi", "AGENTS.md"},
		{"kiro", "AGENTS.md"},
		{"antigravity", "AGENTS.md"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.provider, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, tc.filename)
			const userContent = "# User file\n\ndon't touch this\n"
			if err := os.WriteFile(path, []byte(userContent), 0o644); err != nil {
				t.Fatalf("seed: %v", err)
			}

			// Inject through the production path so cleanup runs against
			// the same wire format the agent saw.
			if _, err := InjectRuntimeConfig(dir, tc.provider, TaskContextForEnv{
				IssueID: "11111111-2222-3333-4444-555555555555",
			}); err != nil {
				t.Fatalf("InjectRuntimeConfig: %v", err)
			}
			if err := CleanupRuntimeConfig(dir, tc.provider); err != nil {
				t.Fatalf("CleanupRuntimeConfig: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			s := string(got)
			if strings.Contains(s, runtimeMarkerBegin) || strings.Contains(s, runtimeMarkerEnd) {
				t.Errorf("[%s] marker block must be removed from %s, got:\n%s", tc.provider, tc.filename, s)
			}
			if s != userContent {
				t.Errorf("[%s] user content in %s must be preserved byte-for-byte\n got:\n%q\nwant:\n%q", tc.provider, tc.filename, s, userContent)
			}
		})
	}
}

// Inject → Cleanup → manual edit → Inject must converge back to the
// pre-injection state on the next Cleanup. This is the end-to-end
// regression that locks in: the user's repo is byte-identical to what
// they had before the task, every task cycle.
func TestInjectThenCleanupRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	const userContent = "# User-authored CLAUDE.md\n\n- rule A\n- rule B\n"
	if err := os.WriteFile(path, []byte(userContent), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Two full inject→cleanup cycles — covers both the "first task on a
	// fresh user file" path and the "subsequent task hits a clean file
	// again" path.
	for i := 0; i < 2; i++ {
		if _, err := InjectRuntimeConfig(dir, "claude", TaskContextForEnv{
			IssueID: "11111111-2222-3333-4444-555555555555",
		}); err != nil {
			t.Fatalf("iter %d inject: %v", i, err)
		}
		if err := CleanupRuntimeConfig(dir, "claude"); err != nil {
			t.Fatalf("iter %d cleanup: %v", i, err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("iter %d read back: %v", i, err)
		}
		if string(got) != userContent {
			t.Errorf("iter %d: user file must be byte-identical to pre-injection state\n got:\n%q\nwant:\n%q", i, string(got), userContent)
		}
	}
}

// Byte-exact boundary coverage flagged in PR #3438 review (Elon): the
// previous cleanup used TrimRight + "\n" and TrimSpace-based file removal,
// which created a real diff in three boundary cases. The table walks each
// one through a full inject→cleanup cycle and asserts the file ends up
// byte-identical (or, for missing-file, that it stays missing).
func TestInjectThenCleanupRoundTripByteExactBoundaries(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		// seed describes the pre-inject filesystem state. When seedExists
		// is false the file is absent; when true the file is created with
		// seedContent (which may be empty / whitespace-only / arbitrary
		// bytes).
		seedExists  bool
		seedContent string
	}{
		{
			name:        "file missing — Inject creates, Cleanup removes",
			seedExists:  false,
			seedContent: "",
		},
		{
			name:        "pre-existing empty file (zero bytes)",
			seedExists:  true,
			seedContent: "",
		},
		{
			name:        "pre-existing whitespace-only file",
			seedExists:  true,
			seedContent: "   \n",
		},
		{
			name:        "no trailing newline",
			seedExists:  true,
			seedContent: "rules",
		},
		{
			name:        "one trailing newline (the common markdown shape)",
			seedExists:  true,
			seedContent: "# Rules\n\nbody\n",
		},
		{
			name:        "two trailing newlines",
			seedExists:  true,
			seedContent: "rules\n\n",
		},
		{
			name:        "many trailing newlines",
			seedExists:  true,
			seedContent: "rules\n\n\n\n",
		},
		{
			name:        "CRLF line endings",
			seedExists:  true,
			seedContent: "rule A\r\nrule B\r\n",
		},
		{
			name:        "no final newline AND embedded blank lines",
			seedExists:  true,
			seedContent: "para 1\n\npara 2\n\npara 3",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "CLAUDE.md")

			if tc.seedExists {
				if err := os.WriteFile(path, []byte(tc.seedContent), 0o644); err != nil {
					t.Fatalf("seed: %v", err)
				}
			}

			// Two cycles to cover both "first inject hits user file" and
			// "subsequent inject hits a cleaned file" paths.
			for i := 0; i < 2; i++ {
				if _, err := InjectRuntimeConfig(dir, "claude", TaskContextForEnv{
					IssueID: "11111111-2222-3333-4444-555555555555",
				}); err != nil {
					t.Fatalf("iter %d inject: %v", i, err)
				}
				if err := CleanupRuntimeConfig(dir, "claude"); err != nil {
					t.Fatalf("iter %d cleanup: %v", i, err)
				}

				if !tc.seedExists {
					// Missing file must remain missing after the cycle so
					// the user's directory listing is also byte-identical
					// (no zero-byte stub left behind).
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Errorf("iter %d: file must remain missing, stat err=%v", i, err)
					}
					continue
				}
				got, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("iter %d read back: %v", i, err)
				}
				if string(got) != tc.seedContent {
					t.Errorf("iter %d: file must be byte-identical to seed\n got:  %q\n want: %q", i, string(got), tc.seedContent)
				}
			}
		})
	}
}

// Idempotency across the byte-exact boundaries: when a second Inject runs
// against a file that already carries a marker block (the "replace in
// place" branch), the surrounding bytes must stay untouched and the
// subsequent Cleanup must still restore the user's original file
// byte-exactly. This guards against a regression where the replace path
// would re-normalise pre/post bytes the way the old cleanup did.
func TestInjectReplaceThenCleanupRestoresByteExact(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		seedContent string
	}{
		{name: "no trailing newline", seedContent: "rules"},
		{name: "two trailing newlines", seedContent: "rules\n\n"},
		{name: "empty file", seedContent: ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "CLAUDE.md")
			if err := os.WriteFile(path, []byte(tc.seedContent), 0o644); err != nil {
				t.Fatalf("seed: %v", err)
			}

			// First inject — append path.
			if _, err := InjectRuntimeConfig(dir, "claude", TaskContextForEnv{
				IssueID: "11111111-2222-3333-4444-555555555555",
			}); err != nil {
				t.Fatalf("first inject: %v", err)
			}
			// Second inject — replace-in-place path.
			if _, err := InjectRuntimeConfig(dir, "claude", TaskContextForEnv{
				IssueID: "11111111-2222-3333-4444-555555555555",
			}); err != nil {
				t.Fatalf("second inject: %v", err)
			}
			if err := CleanupRuntimeConfig(dir, "claude"); err != nil {
				t.Fatalf("cleanup: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if string(got) != tc.seedContent {
				t.Errorf("file must be byte-identical to seed after replace+cleanup\n got:  %q\n want: %q", string(got), tc.seedContent)
			}
		})
	}
}

// The fixed managed separator is the invariant that makes byte-exact
// cleanup possible. This test pins it: writeRuntimeConfigFile must
// produce exactly `<user-bytes><\n\n><marker-block>` for ANY non-empty
// or empty pre-existing file, with no trailing-newline normalisation.
func TestWriteRuntimeConfigFileAlwaysInsertsFixedManagedSeparator(t *testing.T) {
	t.Parallel()
	for _, seed := range []string{"", "rules", "rules\n", "rules\n\n", "rules\n\n\n\n"} {
		seed := seed
		t.Run(fmt.Sprintf("seed=%q", seed), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "CLAUDE.md")
			if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
				t.Fatalf("seed: %v", err)
			}
			if err := writeRuntimeConfigFile(path, "brief body"); err != nil {
				t.Fatalf("write: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			s := string(got)
			// The seed must appear verbatim at the start of the file —
			// no extra newline appended, no trailing newline trimmed.
			if !strings.HasPrefix(s, seed) {
				t.Errorf("seed bytes must survive verbatim at the start of the file\n got: %q\n seed: %q", s, seed)
			}
			// Immediately after the seed we must see the fixed managed
			// separator, then the begin marker.
			markerStart := len(seed) + len(runtimeManagedSeparator)
			if len(s) < markerStart+len(runtimeMarkerBegin) {
				t.Fatalf("file shorter than expected layout\n got: %q", s)
			}
			if got, want := s[len(seed):markerStart], runtimeManagedSeparator; got != want {
				t.Errorf("expected managed separator %q immediately after seed, got %q", want, got)
			}
			if got, want := s[markerStart:markerStart+len(runtimeMarkerBegin)], runtimeMarkerBegin; got != want {
				t.Errorf("expected begin marker after managed separator, got %q", got)
			}
		})
	}
}
