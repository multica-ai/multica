package execenv

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/runtimeapps"
)

// Sub-issue Creation section — after MUL-2538 the platform posts the
// child-done parent notification itself, so the brief no longer carries
// any parent-notification rule (per Bohan's call on PR #3055: delete the
// guidance entirely, do not replace it with a "do not post one" sentence
// — the agent should not be thinking about parent comments at all). All
// that remains is the `--status todo` vs `--status backlog` rule for
// creating sub-issues, which is unrelated to the notification path.

// platformSkillFixture is the built-in every issue brief expects to be able to
// point at. The pointer is resolved from the task's actual skills, so a brief
// built without it deliberately carries no pointer at all.
func platformSkillFixture() SkillContextForEnv {
	return SkillContextForEnv{
		Name:    "multica-platform",
		Content: "---\nname: multica-platform\n---\n\nbody",
	}
}

func TestSubIssueCreationSectionPresentForIssueRuns(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		ctx  TaskContextForEnv
	}{
		{
			name: "assignment-triggered",
			ctx: TaskContextForEnv{
				IssueID:     "11111111-2222-3333-4444-555555555555",
				AgentSkills: []SkillContextForEnv{platformSkillFixture()},
			},
		},
		{
			name: "comment-triggered",
			ctx: TaskContextForEnv{
				IssueID:          "22222222-3333-4444-5555-666666666666",
				TriggerCommentID: "33333333-4444-5555-6666-777777777777",
				AgentSkills:      []SkillContextForEnv{platformSkillFixture()},
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
				// MUL-5442 demotes the full todo/backlog/stage playbook to the
				// multica-platform skill. The brief keeps a one-line map (all
				// three flags stay discoverable, MUL-3508 follow-up) plus the
				// skill pointer; the skill side of the contract is asserted in
				// internal/service (TestPlatformSkillCoversPlatformContracts).
				"`--status todo` starts an agent-assigned child immediately",
				"`--status backlog` parks it",
				"`--stage <N>` groups children into ordered stages",
				"read `references/issues.md` in the `multica-platform` skill",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("[%s] section missing %q", tc.name, want)
				}
			}
		})
	}
}

func TestIssueWorkflowCarriesSourceContextPrecedenceOnce(t *testing.T) {
	t.Parallel()
	out := buildMetaSkillContent("claude", TaskContextForEnv{IssueID: "issue-1"})
	const rule = "If the issue JSON contains `source_context`"
	if count := strings.Count(out, rule); count != 1 {
		t.Fatalf("source-context precedence rule count = %d, want 1", count)
	}
	if !strings.Contains(out, "current issue title, description, and comments are authoritative task instructions") {
		t.Fatal("source-context rule does not identify the current issue as authoritative")
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

// The status rule is a fact judgment with two write moments (MUL-6417): no
// trigger-type modes, no assignee gate. A turn that advances the issue's own
// ask records in_progress when it STARTS — judged only at turn end, a fresh
// assignment sat in todo for the whole first work turn (Bohan's post-merge
// report) — and the end of the turn records the state the work reached. The
// two invariants MUL-6300 pinned as gates (PR #205, reinforced by Elon's
// blocking review on PR #2918) survive as consequences of the fact anchor and
// stay pinned here:
//
//   - a purely conversational turn changes nothing about the issue's state,
//     so it writes nothing — at either moment;
//   - concurrently triggered agents judging the same fact write the same
//     value or nothing, so the board cannot flap.
//
// The brief must also still carry no unconditional placeholder flip: a bare
// `multica issue status <this-issue-id> in_review` command would fire on every
// turn regardless of whether the turn delivered anything.
func TestStatusRuleIsFactJudgmentAtBothMoments(t *testing.T) {
	t.Parallel()
	ctx := TaskContextForEnv{
		IssueID:          "55555555-6666-7777-8888-999999999999",
		TriggerCommentID: "66666666-7777-8888-9999-aaaaaaaaaaaa",
	}
	out := buildMetaSkillContent("claude", ctx)

	if strings.Contains(out, "`multica issue status <this-issue-id> in_review`") {
		t.Errorf("brief must not contain a placeholder `<this-issue-id> in_review` flip — status is judged from what the turn delivered")
	}

	for _, want := range []string{
		// The anchor: issue state, not run lifecycle, written when it changes.
		"**Issue status — write the state the issue is in, whenever it changes**",
		"Status reflects the state the ISSUE is in, not your run's lifecycle",
		// The start moment: INSIDE step 3, at the read→work boundary. A run
		// on MUL-6460 proved a detached status-block bullet does not fire —
		// the model is walking the numbered list when the condition triggers.
		"3. If any part of what this turn will produce is what the issue itself asks for",
		// Category-scoped skip so a custom in_progress-category status (e.g.
		// Planning, MUL-6460) already counts as "recorded" once agents can
		// see the catalog.
		"already in an `in_progress`-category status",
		"the board should show the issue being worked while you work, not only after",
		// No assignee gate: the judgment applies to whoever is running.
		"whoever the assignee is",
		// Delivery lands in in_review and the ceiling keeps `done` human.
		"`done` stays human",
		// Assigned deliverables must not be misread as status-neutral
		// research: stage barriers and parent notifications key off the
		// delivery write.
		"stage barriers and parent notifications depend on that signal",
		// Invariant 1: conversation does not move the board. Ancillary is
		// defined by OUTPUT (no part of the issue's own deliverable), not by
		// activity words like "research" that also describe real work.
		"questions, discussion, and acknowledgements never touch status",
		"Your turn produced none of the issue's own deliverable",
		// Invariant 2: concurrent agents converge instead of flapping.
		"This no-write default is what keeps concurrent runs from flapping the board",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status rule missing %q\n---\n%s", want, out)
		}
	}

	// The retired gates must not come back: no trigger-type modes, no
	// assignee-scoped arc, no UNCONDITIONAL opening flip (the start moment is
	// conditional on the turn advancing the issue's ask), and no end-only
	// timing that hides a long first work turn in todo.
	for _, banned := range []string{
		"Turn mode",
		"Ownership mode",
		"Reply mode",
		"when this issue is assigned to you and this turn does substantive work on it",
		"set `in_progress` when you start",
		"Before step 3, run `multica issue status",
		"judge once, at the end of the turn",
		"do not open with a status write",
		// The example that misled the MUL-6460 run: research that IS the
		// issue's ask pattern-matched an example meant for consult pull-ins.
		"asked to research stays",
		// Activity-word lists must not come back in EITHER direction — the
		// negative lists are the failure class behind the research and
		// review cases, and the positive form-list ("in whatever form:
		// code, research, ...") is the same shape read as exhaustive
		// (J's review on #7295).
		"A turn that only answers, reviews, or consults",
		"you only answered a question, reviewed, or discussed",
		"in whatever form: code, research",
	} {
		if strings.Contains(out, banned) {
			t.Errorf("brief still carries retired status gate %q (MUL-6417)\n---\n%s", banned, out)
		}
	}

	// Position is load-bearing, not style (J's review on #7295): a
	// presence-pin cannot tell WHERE a sentence lives, and both field
	// incidents came from correct sentences sitting in positions that do
	// not fire. The two anchors must live INSIDE their numbered steps.
	var step3, step5 string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "3. ") {
			step3 = line
		}
		if strings.HasPrefix(line, "5. ") {
			step5 = line
		}
	}
	if !strings.Contains(step3, "set `in_progress` FIRST") {
		t.Errorf("the start write must be anchored inside step 3\n---\n%s", step3)
	}
	if !strings.Contains(step3, "never decides") {
		t.Errorf("the never-decides guard must live inside step 3, the position that fires\n---\n%s", step3)
	}
	if !strings.Contains(step5, "confirm the status still matches") {
		t.Errorf("the exit-side status check must be anchored inside step 5\n---\n%s", step5)
	}

	// The squad-leader bullet must not leak into the ordinary path.
	if strings.Contains(out, "dispatching members is not delivery") {
		t.Errorf("ordinary-agent brief must not carry the squad-leader status bullet:\n%s", out)
	}
}

// TestPerRunCommentContextStaysOutOfBrief pins MUL-5377: no per-run comment
// routing value may be rendered into the runtime brief. The brief lands in
// messages[0], ahead of the whole conversation, so any change there throws away
// the prompt cache for the entire history on resume. The helpers are unchanged
// and still feed the per-turn user message (daemon.buildCommentPrompt).
func TestPerRunCommentContextStaysOutOfBrief(t *testing.T) {
	t.Parallel()
	const (
		issueID = "55555555-6666-7777-8888-999999999999"
		since   = "2026-05-28T11:00:00Z"
	)
	out := buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID:          issueID,
		TriggerCommentID: "reply-abc",
		TriggerThreadID:  "thread-abc",
		NewCommentCount:  4,
		NewCommentsSince: since,
		CommentReplyTargets: []ThreadReplyTarget{
			{ThreadID: "thread-abc", ParentID: "reply-abc"},
			{ThreadID: "thread-def", ParentID: "reply-def"},
		},
	})

	for _, banned := range []string{
		"reply-abc", "thread-abc", "reply-def", "thread-def", since,
		"4 new comment(s) on this issue since your last run",
		"DISTINCT threads",
	} {
		if strings.Contains(out, banned) {
			t.Errorf("brief must not carry per-run comment value %q (MUL-5377)\n---\n%s", banned, out)
		}
	}

	// The helper that now feeds the per-turn prompt is unchanged.
	hint := BuildNewCommentsHint(issueID, "reply-abc", "thread-abc", since, 4)
	for _, want := range []string{
		"4 new comment(s) on this issue since your last run",
		"across all threads",
		"--thread thread-abc --since " + since + " --compact --output json",
		"--tail 30",
	} {
		if !strings.Contains(hint, want) {
			t.Errorf("BuildNewCommentsHint missing %q\n---\n%s", want, hint)
		}
	}
}

// TestCommentHintsCarryNoModality pins MUL-6984: the per-turn comment hints
// carry this turn's facts and exact commands and never decide whether the
// wide read happens — workflow step 2 owns that. Each hint hands the scan over
// (or, on the resumed no-delta path, reports the server-computed answer to
// it); none of them makes it conditional on the agent's own guess.
func TestCommentHintsCarryNoModality(t *testing.T) {
	t.Parallel()
	const issueID = "55555555-6666-7777-8888-999999999999"
	hints := map[string]string{
		"cold":    BuildColdCommentsHint(issueID, "trigger-1", "thread-root-1"),
		"warm":    BuildNewCommentsHint(issueID, "trigger-1", "thread-root-1", "2026-05-28T11:00:00Z", 4),
		"resumed": BuildResumedCommentsHint(issueID, "trigger-1", "thread-root-1"),
	}
	for name, hint := range hints {
		if hint == "" {
			t.Fatalf("%s hint rendered empty", name)
		}
		for _, banned := range []string{
			"Only if you need",
			"Need cross-thread background",
			"If your reply depends on thread context",
			"read them all blindly",
			"only if needed",
		} {
			if strings.Contains(hint, banned) {
				t.Errorf("%s hint must not make the wide read optional (%q):\n%s", name, banned, hint)
			}
		}
	}
	for _, name := range []string{"cold", "warm"} {
		if !strings.Contains(hints[name], "--roots-only --summary") {
			t.Errorf("%s hint must hand over the scan step 2 requires:\n%s", name, hints[name])
		}
	}
	if !strings.Contains(hints["resumed"], "issue-wide delta is empty") {
		t.Errorf("resumed hint must report the empty delta as the scan's answer:\n%s", hints["resumed"])
	}
}

// Cold-start thread routing moved to the per-turn prompt (MUL-5377); the
// helper behaviour it relies on is pinned here directly.
func TestColdCommentsHintPointsAtTriggeringThread(t *testing.T) {
	t.Parallel()
	const issueID = "55555555-6666-7777-8888-999999999999"
	hint := BuildColdCommentsHint(issueID, "trigger-1", "thread-root-1")
	if strings.Contains(hint, "new comment(s) since your last run") {
		t.Errorf("no since-delta hint should render on cold start, got:\n%s", hint)
	}
	if !strings.Contains(hint, "multica issue comment list "+issueID+" --thread thread-root-1 --tail 30 --compact --output json") {
		t.Errorf("cold start must point at the triggering thread read, got:\n%s", hint)
	}
	if strings.Contains(buildMetaSkillContent("claude", TaskContextForEnv{IssueID: issueID, TriggerCommentID: "trigger-1", TriggerThreadID: "thread-root-1"}), "thread-root-1") {
		t.Error("brief must not carry the per-run thread id (MUL-5377)")
	}
}

// Resumed/no-delta routing moved to the per-turn prompt (MUL-5377).
func TestResumedCommentsHintSkipsDefaultThreadRead(t *testing.T) {
	t.Parallel()
	const issueID = "55555555-6666-7777-8888-999999999999"
	hint := BuildResumedCommentsHint(issueID, "trigger-1", "thread-root-1")

	for _, want := range []string{
		"triggering comment is already included above",
		"No other new comments on this issue since your last run",
		"issue-wide delta is empty",
		"if resumed memory is not enough",
		"multica issue comment list " + issueID + " --thread thread-root-1 --tail 30 --compact --output json",
	} {
		if !strings.Contains(hint, want) {
			t.Errorf("resumed/no-delta hint missing %q\n--- output ---\n%s", want, hint)
		}
	}
	// The anchor-restating sentence is gone (MUL-5721 OPT-1): the read command
	// carries the thread anchor and the reply cookbook carries the trigger id.
	if strings.Contains(hint, "active thread anchor") {
		t.Errorf("resumed/no-delta hint must not restate anchors outside the commands, got:\n%s", hint)
	}
	if strings.Contains(hint, "scoped to the triggering thread") {
		t.Errorf("resumed/no-delta hint must not claim the delta is thread-scoped, got:\n%s", hint)
	}
	if strings.Contains(hint, "in place of `--thread ... --tail 30`") {
		t.Errorf("resumed/no-delta hint must not render the reconstruction (cold) hint, got:\n%s", hint)
	}
}

// The continuity notice moved out of the brief and into the per-turn prompt
// (MUL-5377) because it is true of one run and false of the next.
func TestSessionContinuityNoticeLivesOutsideBrief(t *testing.T) {
	t.Parallel()
	for _, want := range []string{
		"## Session Continuity Notice",
		"could NOT be restored",
		"tell the user up front",
	} {
		if !strings.Contains(SessionContinuityNoticeUnrecoverable, want) {
			t.Errorf("SessionContinuityNoticeUnrecoverable missing %q", want)
		}
	}

	// MUL-5722: the issue variant carries the same heading and the same
	// "do not assume continuity" job, but must NOT order an announcement. An
	// issue's discussion lives in its comments, which the agent re-reads every
	// turn, so telling the user it was lost describes a loss that did not
	// happen — they hear "the discussion is gone" when every word survives.
	if !strings.Contains(SessionContinuityNoticeIssue, "## Session Continuity Notice") {
		t.Error("SessionContinuityNoticeIssue must keep the section heading")
	}
	if strings.Contains(SessionContinuityNoticeIssue, "tell the user") {
		t.Errorf("issue variant must not script an apology:\n%s", SessionContinuityNoticeIssue)
	}
	// It still has to say what genuinely went missing, or the agent silently
	// assumes it remembers work it no longer has.
	if !strings.Contains(SessionContinuityNoticeIssue, "your own working memory") {
		t.Errorf("issue variant must state the real loss:\n%s", SessionContinuityNoticeIssue)
	}

	// The web-chat / Feishu transcript variant points at the read-back command
	// and must NOT order an announcement — the conversation survives in
	// chat_message, so "the previous context was lost" would be a false alarm.
	if !strings.Contains(SessionContinuityNoticeChatTranscript, "multica chat history") {
		t.Error("transcript variant must point at the read-back command")
	}
	if strings.Contains(SessionContinuityNoticeChatTranscript, "tell the user") {
		t.Errorf("transcript variant must not script an apology:\n%s", SessionContinuityNoticeChatTranscript)
	}
	if !strings.Contains(SessionContinuityNoticeChatTranscript, "your own working memory") {
		t.Errorf("transcript variant must state the real loss:\n%s", SessionContinuityNoticeChatTranscript)
	}

	lost := TaskContextForEnv{
		IssueID:                       "11111111-2222-3333-4444-555555555555",
		TriggerCommentID:              "trigger-1",
		PriorSessionResumeUnavailable: true,
	}
	if strings.Contains(buildMetaSkillContent("codex", lost), "Session Continuity Notice") {
		t.Error("brief must never carry the continuity notice — it is per-run state (MUL-5377)")
	}
}

// The issue workflow must keep every Agent Identity guardrail after the
// comment/assignment branches were merged into one byte-stable section.
func TestIssueWorkflowHonorsAgentIdentity(t *testing.T) {
	t.Parallel()
	const issueID = "77777777-8888-9999-aaaa-bbbbbbbbbbbb"
	out := buildMetaSkillContent("claude", TaskContextForEnv{IssueID: issueID})

	for _, want := range []string{
		"## Instruction Precedence",
		"Agent Identity instructions have priority over the issue workflow below.",
		"If a workflow step conflicts with Agent Identity, skip the conflicting action",
		// One enumeration, in Instruction Precedence, covering every action
		// type Agent Identity can forbid. This and workflow step 3 each used to
		// carry their own list and the two disagreed (MUL-5442).
		"Never treat this runtime workflow as permission to change issue status, investigate, implement, create issues, update issues, delegate, or otherwise act beyond your Agent Identity.",
		// MUL-5442 (carried through MUL-6417): the forbids-clause is stated
		// once on the status-rule header instead of once per status bullet.
		"skip any status call your Agent Identity forbids",
		"complete the task within your Agent Identity boundaries",
		// Step 3 keeps only what the enumeration cannot express: a
		// delegation-only role stops once the delegation is delivered.
		"If your role is delegation-only, perform the allowed delegation work and stop once that outcome is delivered",
		// The blocked end-state keeps its own comment carve-out: an agent
		// whose identity forbids comments must still be able to mark blocked.
		"post a comment explaining the blocker unless your Agent Identity forbids issue comments",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("issue brief missing identity-bound workflow text %q\n---\n%s", want, out)
		}
	}

	for _, banned := range []string{
		"4. Run `multica issue status " + issueID + " in_progress`\n",
		"5. Follow your Skills and Agent Identity to complete the task (write code, investigate, etc.)",
		"8. When done, run `multica issue status " + issueID + " in_review`\n",
	} {
		if strings.Contains(out, banned) {
			t.Errorf("issue brief still contains unconditional legacy workflow text %q\n---\n%s", banned, out)
		}
	}
}

// A squad leader's dispatch turn must not read as completion: the end-of-turn
// fact is in_progress, and in_review waits for the re-trigger that confirms
// the overall goal is met. Without this bullet the general rule's "delivered →
// in_review" reading would let a leader close out the parent on the very turn
// it hands work to members.
func TestSquadLeaderIssueWorkflowKeepsParentInProgress(t *testing.T) {
	t.Parallel()
	const issueID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	out := buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID:       issueID,
		IsSquadLeader: true,
	})

	for _, want := range []string{
		"dispatching members is not delivery",
		"a dispatch turn leaves the parent `in_progress`",
		"where you confirm the overall goal is met",
		// The shared no-write default still governs leader conversation turns.
		"questions, discussion, and acknowledgements never touch status",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("squad-leader issue brief missing %q\n---\n%s", want, out)
		}
	}
}

// TestProtocolHeadingInInstructionsGetsNoLeaderBrief is the brief-side half of
// the MUL-5811 negative regression. IsSquadLeader now comes from the claim's
// is_leader_task / squad_id, so an ordinary agent that documents a
// "## Squad Operating Protocol" section in its own instructions must get the
// ordinary brief — its instructions rendered verbatim under Agent Identity,
// and not one leader-only branch.
func TestProtocolHeadingInInstructionsGetsNoLeaderBrief(t *testing.T) {
	t.Parallel()

	const instructions = "I write docs about squads.\n\n## Squad Operating Protocol\n\nHow leaders dispatch work..."
	out := buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID:           "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		TriggerCommentID:  "bbbbbbbb-cccc-dddd-eeee-ffffffffffff",
		AgentName:         "Docs writer",
		AgentInstructions: instructions,
		IsSquadLeader:     false,
	})

	if !strings.Contains(out, instructions) {
		t.Fatalf("agent instructions must reach the brief verbatim\n---\n%s", out)
	}
	for _, banned := range []string{
		"### Squad maintenance",
		"multica squad member set-role",
		"multica squad activity",
		"unless your outcome is `no_action`",
		"dispatching members is not delivery",
	} {
		if strings.Contains(out, banned) {
			t.Fatalf("ordinary-agent brief leaked leader-only content %q\n---\n%s", banned, out)
		}
	}
	if !strings.Contains(out, "**Post your final results as a comment — this step is mandatory**") {
		t.Fatalf("ordinary-agent brief lost the unconditional reply obligation\n---\n%s", out)
	}
}

// Instruction Precedence belongs to the issue workflow only; the issue-less
// kinds must not inherit it. After MUL-5377 it applies to every issue run,
// comment-triggered or not, because there is a single issue workflow.
func TestInstructionPrecedenceOnlyAppliesToIssueWorkflow(t *testing.T) {
	t.Parallel()
	if out := buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID:          "11111111-2222-3333-4444-555555555555",
		TriggerCommentID: "22222222-3333-4444-5555-666666666666",
	}); !strings.Contains(out, "## Instruction Precedence") {
		t.Errorf("comment-triggered issue brief must carry Instruction Precedence\n---\n%s", out)
	}

	cases := []struct {
		name string
		ctx  TaskContextForEnv
	}{
		{"chat", TaskContextForEnv{ChatSessionID: "chat-1"}},
		{"quick-create", TaskContextForEnv{QuickCreatePrompt: "create me an issue"}},
		{"autopilot run-only", TaskContextForEnv{AutopilotRunID: "run-1"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := buildMetaSkillContent("claude", tc.ctx)
			for _, banned := range []string{
				"## Instruction Precedence",
				"issue workflow below",
				"Never treat this runtime workflow as permission to change issue status",
			} {
				if strings.Contains(out, banned) {
					t.Errorf("%s brief must not inherit issue-only precedence text %q\n---\n%s", tc.name, banned, out)
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

	// The `runtime_brief_slim` flag was retired (MUL-4297); there is now a
	// single brief.
	run(t, "brief")
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

// Connected Apps moved to the per-turn prompt (MUL-5377): the app set is
// resolved per run from the runtime MCP overlay.
func TestConnectedAppsBlockLivesOutsideBrief(t *testing.T) {
	t.Parallel()
	apps := []runtimeapps.ConnectedApp{{
		Provider:    "composio",
		ServerName:  "composio",
		ToolkitSlug: "notion",
		ToolkitName: "Notion",
	}}

	block := BuildConnectedAppsBlock(apps)
	for _, want := range []string{
		"## Connected Apps",
		"- Notion (`notion`) via MCP server `composio`",
		"Use the listed MCP server when the task asks to read or act in one of these apps.",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("connected-apps block missing %q\n---\n%s", want, block)
		}
	}
	if BuildConnectedAppsBlock(nil) != "" {
		t.Error("empty app list must render nothing")
	}

	out := buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID:          "11111111-2222-3333-4444-555555555555",
		WorkspaceContext: "Prefer source-of-truth systems.",
		ConnectedApps:    apps,
	})
	if strings.Contains(out, "## Connected Apps") {
		t.Errorf("brief must not carry Connected Apps — it is per-run state (MUL-5377)\n---\n%s", out)
	}
}

func TestConnectedAppsHeadingSkippedWhenEmpty(t *testing.T) {
	t.Parallel()
	out := buildMetaSkillContent("claude", TaskContextForEnv{IssueID: "11111111-2222-3333-4444-555555555555"})
	if strings.Contains(out, "## Connected Apps") {
		t.Fatalf("empty connected apps must not emit the heading")
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
		{"codebuddy", "CODEBUDDY.md"},
		{"codex", "AGENTS.md"},
		{"copilot", "AGENTS.md"},
		{"opencode", "AGENTS.md"},
		{"codearts", "AGENTS.md"},
		{"openclaw", "AGENTS.md"},
		{"hermes", "AGENTS.md"},
		{"pi", "AGENTS.md"},
		{"omp", "AGENTS.md"},
		{"cursor", "AGENTS.md"},
		{"kimi", "AGENTS.md"},
		{"reasonix", "AGENTS.md"},
		{"dsh", "AGENTS.md"},
		{"dim", "AGENTS.md"},
		{"zeroclaw", "AGENTS.md"},
		{"kiro", "AGENTS.md"},
		{"antigravity", "AGENTS.md"},
		{"qwen", "QWEN.md"},
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

// CodeBuddy is a Claude Code fork but ships its own native config
// directory (~/.codebuddy, .codebuddy/) rather than reusing Claude's
// ~/.claude / CLAUDE.md paths (see
// https://www.codebuddy.ai/docs/cli/codebuddy-dir). This pins the two
// providers to different target filenames so a future edit can't
// silently re-merge them.
func TestRuntimeConfigPathDistinguishesCodebuddyFromClaude(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	claudePath := runtimeConfigPath(dir, "claude")
	codebuddyPath := runtimeConfigPath(dir, "codebuddy")

	if claudePath != filepath.Join(dir, "CLAUDE.md") {
		t.Errorf("claude runtime config path = %q, want CLAUDE.md", claudePath)
	}
	if codebuddyPath != filepath.Join(dir, "CODEBUDDY.md") {
		t.Errorf("codebuddy runtime config path = %q, want CODEBUDDY.md", codebuddyPath)
	}
	if claudePath == codebuddyPath {
		t.Fatal("claude and codebuddy must not share a runtime config path")
	}
}

func TestInjectRuntimeConfigUnknownProviderSkipsWrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Seed all candidate filenames so we can verify none of them get
	// written when the provider is unknown.
	for _, name := range []string{"CLAUDE.md", "CODEBUDDY.md", "AGENTS.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("untouched\n"), 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}

	if _, err := InjectRuntimeConfig(dir, "totally-unknown-provider", TaskContextForEnv{
		IssueID: "11111111-2222-3333-4444-555555555555",
	}); err != nil {
		t.Fatalf("InjectRuntimeConfig: %v", err)
	}
	for _, name := range []string{"CLAUDE.md", "CODEBUDDY.md", "AGENTS.md"} {
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
		for _, name := range []string{"CLAUDE.md", "CODEBUDDY.md", "AGENTS.md"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("untouched\n"), 0o644); err != nil {
				t.Fatalf("seed %s: %v", name, err)
			}
		}
		if err := CleanupRuntimeConfig(dir, "totally-unknown-provider"); err != nil {
			t.Errorf("unknown provider must be no-op, got: %v", err)
		}
		for _, name := range []string{"CLAUDE.md", "CODEBUDDY.md", "AGENTS.md"} {
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
		{"codebuddy", "CODEBUDDY.md"},
		{"codex", "AGENTS.md"},
		{"copilot", "AGENTS.md"},
		{"opencode", "AGENTS.md"},
		{"codearts", "AGENTS.md"},
		{"openclaw", "AGENTS.md"},
		{"hermes", "AGENTS.md"},
		{"pi", "AGENTS.md"},
		{"omp", "AGENTS.md"},
		{"cursor", "AGENTS.md"},
		{"kimi", "AGENTS.md"},
		{"reasonix", "AGENTS.md"},
		{"dsh", "AGENTS.md"},
		{"dim", "AGENTS.md"},
		{"zeroclaw", "AGENTS.md"},
		{"kiro", "AGENTS.md"},
		{"antigravity", "AGENTS.md"},
		{"qwen", "QWEN.md"},
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

// Cross-thread fan-out moved to the per-turn prompt (MUL-5377).
func TestMultiThreadReplyInstructionsFanOut(t *testing.T) {
	t.Parallel()
	out := BuildMultiThreadCommentReplyInstructions("55555555-6666-7777-8888-999999999999", []ThreadReplyTarget{
		{ThreadID: "c1", ParentID: "c1"},
		{ThreadID: "c2", ParentID: "c2"},
		{ThreadID: "c3", ParentID: "c3"},
	}, false)

	for _, want := range []string{
		"3 DISTINCT threads",
		"Post ONE reply per thread",
		"OVERRIDES",
		"--parent c1", "--parent c2", "--parent c3",
		"OLDEST thread first",
		// MUL-5825: the posting mechanism is a pointer at the brief's
		// canonical section plus the one multi-thread-specific delta.
		"`## Comment Formatting`",
		"DISTINCT body file per thread",
		"never reuse a `--parent` from an earlier turn",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("cross-thread instructions must contain %q, got:\n%s", want, out)
		}
	}

	// Pin ledger (MUL-5825): the embedded file-operations cookbook was
	// retired in favour of the `## Comment Formatting` pointer above — it
	// triple-wrote the mechanism already carried by the brief and the
	// single-thread cookbook (~1KB per multi-thread turn). These strings are
	// the retired machinery; none may reappear in the fan-out block. The
	// `--content-file` / inline `--content` anchors and the semantic
	// `\n`-escape anchor (replacing the phrasing-fragile "Do NOT write
	// literal") were added on Elon's #6517 review: without them, prose-only
	// restatements of the flag mechanics could regrow under green tests.
	for _, banned := range []string{
		"For EACH thread above",                // old cookbook opener
		"UTF-8 file with your file-write tool", // restated mechanism
		"multica issue comment add",            // embedded example commands
		"--content-file",                       // restated posting flag (#6517 review)
		"inline `--content`",                   // restated inline ban (#6517 review)
		"--content-stdin",                      // restated HEREDOC ban
		"rm ./reply-",                          // unix cleanup example
		"Remove-Item",                          // windows cleanup example
		"`\\n` escape",                         // restated \n-escape rule, any phrasing
	} {
		if strings.Contains(out, banned) {
			t.Errorf("fan-out block re-grew retired cookbook text %q (mechanism lives in ## Comment Formatting — MUL-5825), got:\n%s", banned, out)
		}
	}
}

// TestMultiThreadReplyInstructionsOSInvariant pins that the fan-out block is
// byte-identical across host OSes (MUL-5825). The only OS-dependent text was
// the embedded cleanup command pair (`rm` vs `Remove-Item`), which left with
// the cookbook; the OS split now lives solely in the brief's
// `## Comment Formatting`. If this fails, OS-specific mechanism text crept
// back into the block — move it to the brief instead.
//
// Not parallel: mutates the package-level runtimeGOOS.
func TestMultiThreadReplyInstructionsOSInvariant(t *testing.T) {
	saved := runtimeGOOS
	t.Cleanup(func() { runtimeGOOS = saved })

	targets := []ThreadReplyTarget{
		{ThreadID: "c1", ParentID: "c1"},
		{ThreadID: "c2", ParentID: "c2"},
	}
	for _, leader := range []bool{false, true} {
		runtimeGOOS = "linux"
		linux := BuildMultiThreadCommentReplyInstructions("55555555-6666-7777-8888-999999999999", targets, leader)
		runtimeGOOS = "windows"
		windows := BuildMultiThreadCommentReplyInstructions("55555555-6666-7777-8888-999999999999", targets, leader)
		if linux != windows {
			t.Errorf("fan-out block (leader=%v) must be OS-invariant\nlinux:\n%s\nwindows:\n%s", leader, linux, windows)
		}
	}
}

// Single-thread reply cookbook moved to the per-turn prompt (MUL-5377).
func TestSingleThreadReplyInstructionsKeepSingleParent(t *testing.T) {
	t.Parallel()
	out := BuildCommentReplyInstructions("claude", "55555555-6666-7777-8888-999999999999", "c3", false)

	if strings.Contains(out, "DISTINCT threads") {
		t.Errorf("single/same-thread instructions must not emit the multi-thread fan-out block, got:\n%s", out)
	}
	if !strings.Contains(out, "--parent c3 --content-file ./reply.md") {
		t.Errorf("single/same-thread instructions must keep the single --parent=trigger cookbook, got:\n%s", out)
	}
}

// TestInjectRuntimeConfigByteIdenticalAcrossTriggers is the regression guard
// for MUL-5377.
//
// Claude Code loads the runtime brief into messages[0], ahead of the entire
// conversation. A cache breakpoint is all-or-nothing, so a single differing
// byte in this file invalidates the prompt cache for the whole history: on a
// resumed session the measured cost was ~426k re-created tokens per run, with
// only tools[]+system[] surviving.
//
// Therefore the rendered managed block must be byte-identical for the same
// (agent, issue, provider) no matter what triggered the run. Every field
// varied below is per-run state that used to be interpolated into the brief.
//
// If this test fails, do NOT relax it — move the offending value into the
// per-turn user message (daemon.BuildPrompt) instead. A "skip the write when
// the block is unchanged" guard does not help here: when a volatile field
// creeps back in the block is no longer identical, so the guard never fires
// and the cache breaks anyway.
func TestInjectRuntimeConfigByteIdenticalAcrossTriggers(t *testing.T) {
	t.Parallel()

	const issueID = "11111111-2222-3333-4444-555555555555"
	base := TaskContextForEnv{
		IssueID:   issueID,
		AgentID:   "agent-1",
		AgentName: "Eve",
	}

	variants := []struct {
		name   string
		mutate func(c *TaskContextForEnv)
	}{
		{"assignment-triggered", func(c *TaskContextForEnv) {}},
		{"comment-triggered", func(c *TaskContextForEnv) {
			c.TriggerCommentID = "comment-1"
			c.TriggerThreadID = "thread-1"
		}},
		{"comment-triggered-other-comment", func(c *TaskContextForEnv) {
			c.TriggerCommentID = "comment-2"
			c.TriggerThreadID = "thread-2"
		}},
		{"resumed-with-delta", func(c *TaskContextForEnv) {
			c.TriggerCommentID = "comment-3"
			c.PriorSessionResumed = true
			c.NewCommentCount = 7
			c.NewCommentsSince = "2026-05-28T11:00:00Z"
		}},
		{"resume-unavailable", func(c *TaskContextForEnv) {
			c.TriggerCommentID = "comment-4"
			c.PriorSessionResumeUnavailable = true
		}},
		{"cross-thread-fan-out", func(c *TaskContextForEnv) {
			c.TriggerCommentID = "comment-5"
			c.CommentReplyTargets = []ThreadReplyTarget{
				{ThreadID: "t1", ParentID: "t1"},
				{ThreadID: "t2", ParentID: "t2"},
			}
		}},
		{"member-initiator", func(c *TaskContextForEnv) {
			c.InitiatorType = "member"
			c.InitiatorID = "user-1"
			c.InitiatorName = "Bohan"
			c.InitiatorEmail = "bohan@example.com"
		}},
		{"agent-initiator", func(c *TaskContextForEnv) {
			c.InitiatorType = "agent"
			c.InitiatorID = "agent-9"
			c.InitiatorName = "GPT-Boy"
		}},
		{"connected-apps", func(c *TaskContextForEnv) {
			c.ConnectedApps = []runtimeapps.ConnectedApp{{
				Provider:    "composio",
				ServerName:  "composio",
				ToolkitSlug: "notion",
				ToolkitName: "Notion",
			}}
		}},
	}

	// Non-vacuity guard: the brief must still depend on its stable inputs, or
	// this whole test would pass on a function that ignores ctx entirely.
	// Since MUL-5442's cross-channel dedup the brief is deliberately
	// issue-id-independent (the per-turn message carries the ids), so the
	// guard now varies a different stable input: the agent identity.
	otherAgent := base
	otherAgent.AgentName = "Someone Else"
	if buildMetaSkillContent("claude", base) == buildMetaSkillContent("claude", otherAgent) {
		t.Fatal("brief does not vary with agent identity — byte-identity assertions below would be vacuous")
	}

	// The stronger MUL-5442 invariant this PR claims as a design benefit:
	// with identical stable inputs, two DIFFERENT issue ids must render the
	// byte-identical brief — this is what makes a cross-issue shared cache
	// prefix possible. Asserted directly, per provider, so a truncated,
	// transformed, or id-conditional use of the issue id cannot slip past
	// the Contains-based negative check.
	for _, provider := range []string{"claude", "codex"} {
		otherIssue := base
		otherIssue.IssueID = "99999999-8888-7777-6666-555555555555"
		if buildMetaSkillContent(provider, base) != buildMetaSkillContent(provider, otherIssue) {
			t.Fatalf("%s brief differs across issue ids — the cross-issue cache invariant is broken", provider)
		}
	}

	for _, provider := range []string{"claude", "codex"} {
		provider := provider
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			var want string
			for i, v := range variants {
				ctx := base
				v.mutate(&ctx)
				got := buildMetaSkillContent(provider, ctx)
				if i == 0 {
					want = got
					continue
				}
				if got != want {
					t.Errorf("brief differs for variant %q — per-run state leaked into messages[0] (MUL-5377).\n%s",
						v.name, firstBriefDiff(want, got))
				}
			}
		})
	}
}

// firstBriefDiff reports the first differing byte with surrounding context so a
// failure names the offending section instead of dumping two whole briefs.
func firstBriefDiff(want, got string) string {
	n := len(want)
	if len(got) < n {
		n = len(got)
	}
	i := 0
	for i < n && want[i] == got[i] {
		i++
	}
	lo := i - 120
	if lo < 0 {
		lo = 0
	}
	hiW, hiG := i+120, i+120
	if hiW > len(want) {
		hiW = len(want)
	}
	if hiG > len(got) {
		hiG = len(got)
	}
	return "first difference at byte " + strconv.Itoa(i) +
		"\n--- baseline ---\n" + want[lo:hiW] +
		"\n--- variant ---\n" + got[lo:hiG]
}

// TestAutopilotBriefByteIdenticalAcrossRunScopedFields is the invariant
// MUL-6984 actually moved, and the one TestBriefByteIdenticalAcrossRunsForEveryKind
// cannot see: its autopilot row pins run-1 / ap-1 and its variants only mutate
// the generic resume / initiator / connected-app fields, so reinserting the
// autopilot title, source, payload or description into the brief would leave
// it green.
//
// Every field below identifies ONE run. The brief lands in messages[0], ahead
// of the whole conversation, so a per-run value here throws away the prompt
// cache for the entire history on resume (MUL-5377) — and it also gives a
// second hand-maintained copy somewhere to drift from the per-turn one, which
// is how MUL-5696 happened. Two runs of the same autopilot, and two runs of
// different autopilots, must all produce the same bytes.
func TestAutopilotBriefByteIdenticalAcrossRunScopedFields(t *testing.T) {
	t.Parallel()

	base := TaskContextForEnv{AgentID: "a-1", AgentName: "Eve", AutopilotRunID: "run-1", AutopilotID: "ap-1"}

	runs := []struct {
		name string
		ctx  TaskContextForEnv
	}{
		{"baseline", base},
		{"second run of the same autopilot", func() TaskContextForEnv {
			c := base
			c.AutopilotRunID = "run-2"
			c.AutopilotTitle = "Nightly dependency sweep"
			c.AutopilotSource = "schedule"
			c.AutopilotDescription = "Check dependencies and report outdated packages."
			c.AutopilotTriggerPayload = `{"schedule":"0 3 * * *"}`
			return c
		}()},
		{"run of a different autopilot", func() TaskContextForEnv {
			c := base
			c.AutopilotRunID = "run-3"
			c.AutopilotID = "ap-2"
			c.AutopilotTitle = "Triage inbound issues"
			c.AutopilotSource = "webhook"
			c.AutopilotDescription = "Read the payload and file one issue per report."
			c.AutopilotTriggerPayload = `{"action":"opened","issue":{"number":7,"title":"crash on start"}}`
			return c
		}()},
	}

	want := buildMetaSkillContent("claude", runs[0].ctx)
	for _, r := range runs[1:] {
		if got := buildMetaSkillContent("claude", r.ctx); got != want {
			t.Errorf("autopilot brief changed for %q — a per-run value reached the cache prefix\n%s",
				r.name, firstBriefDiff(want, got))
		}
	}

	// Byte-identity alone would also hold if the brief rendered none of these
	// AND the values never reached the agent at all. Pin the other half here:
	// the brief must not carry them, and daemon.TestBuildPromptAutopilotRunOnly
	// pins the per-turn message as the surface that does.
	for _, banned := range []string{
		"run-1", "ap-1", "Nightly dependency sweep", "Triage inbound issues",
		"schedule", "webhook", "Check dependencies", "0 3 * * *", "crash on start",
	} {
		for _, r := range runs {
			if strings.Contains(buildMetaSkillContent("claude", r.ctx), banned) {
				t.Errorf("autopilot brief (%s) carries the run-scoped value %q; the per-turn message owns it", r.name, banned)
			}
		}
	}
}

// TestBriefByteIdenticalAcrossRunsForEveryKind extends the MUL-5377 guarantee
// past issue runs.
//
// Chat sessions resume too — handler/daemon.go:2172 hands the daemon a
// PriorSessionID from the chat_session row, with the same PriorWorkDir and
// PriorSessionResumeUnavailable plumbing as an issue task. So a chat brief that
// varied per turn would lose the prompt cache exactly the same way, and a long
// chat is precisely where that hurts most. Autopilot and quick-create are
// single-shot today, but the invariant is free to hold for them too and stops a
// future resume path from silently reintroducing the bug.
func TestBriefByteIdenticalAcrossRunsForEveryKind(t *testing.T) {
	t.Parallel()

	kinds := map[string]TaskContextForEnv{
		"chat":         {ChatSessionID: "chat-1", ChatChannelType: ChannelTypeSlack, AgentID: "a-1", AgentName: "Eve"},
		"quick-create": {QuickCreatePrompt: "make an issue", AgentID: "a-1", AgentName: "Eve"},
		"autopilot":    {AutopilotRunID: "run-1", AutopilotID: "ap-1", AgentID: "a-1", AgentName: "Eve"},
		// WeCom is the channel a real deployment flips the file-delivery
		// verdict on. The Slack row above catches the same leak today, but only
		// because the brief's copy is channel-agnostic; scope that copy to
		// WeCom alone and this is the row still holding the line.
		"chat-wecom": {ChatSessionID: "chat-1", ChatChannelType: ChannelTypeWecom, AgentID: "a-1", AgentName: "Eve"},
	}

	// Per-run state that changes between turns of one resumed session.
	variants := []struct {
		name   string
		mutate func(c *TaskContextForEnv)
	}{
		{"baseline", func(c *TaskContextForEnv) {}},
		{"resumed", func(c *TaskContextForEnv) { c.PriorSessionResumed = true }},
		{"resume-unavailable", func(c *TaskContextForEnv) { c.PriorSessionResumeUnavailable = true }},
		{"member-initiator", func(c *TaskContextForEnv) {
			c.InitiatorType, c.InitiatorID = "member", "u-1"
			c.InitiatorName, c.InitiatorEmail = "Bohan", "bohan@example.com"
		}},
		{"other-initiator", func(c *TaskContextForEnv) {
			// A Slack channel lets a different person trigger each turn.
			c.InitiatorType, c.InitiatorID = "member", "u-2"
			c.InitiatorName, c.InitiatorEmail = "Someone Else", "else@example.com"
		}},
		{"agent-initiator", func(c *TaskContextForEnv) {
			c.InitiatorType, c.InitiatorID = "agent", "a-9"
			c.InitiatorName = "GPT-Boy"
		}},
		{"connected-apps", func(c *TaskContextForEnv) {
			c.ConnectedApps = []runtimeapps.ConnectedApp{{
				Provider: "composio", ServerName: "composio",
				ToolkitSlug: "notion", ToolkitName: "Notion",
			}}
		}},
		{"channel-delivers-files", func(c *TaskContextForEnv) {
			// The server's file-delivery verdict arrives on every claim and is
			// a deployment fact, not a session one: an upgrade that starts
			// sending the field, or object storage being turned on or off,
			// flips it under a session already running. Both halves of that
			// flip must render the same brief, which is why the verdict is
			// stated by the per-turn chat prompt and never here.
			c.ChatChannelDeliversFiles = true
		}},
	}

	for kindName, baseCtx := range kinds {
		kindName, baseCtx := kindName, baseCtx
		t.Run(kindName, func(t *testing.T) {
			t.Parallel()
			var want string
			for i, v := range variants {
				ctx := baseCtx
				v.mutate(&ctx)
				got := buildMetaSkillContent("claude", ctx)
				if i == 0 {
					want = got
					continue
				}
				if got != want {
					t.Errorf("%s brief differs for variant %q — per-run state leaked into the cached prefix (MUL-5377).\n%s",
						kindName, v.name, firstBriefDiff(want, got))
				}
			}
		})
	}
}

// TestBriefSkillsListIsNamesOnly pins the shape of the `## Skills` section: an
// index of invocable names, with no descriptions and no per-provider branch.
//
// Descriptions were removed because every runtime CLI already builds its own
// listing from the SKILL.md frontmatter the daemon writes, so the brief's copy
// was the same routing signal paid for twice — ~3,100 tokens per brief on a
// real task, 40% of the whole brief (MUL-5529).
//
// The provider branch was removed because its fallback was wrong: it told
// providers outside a hardcoded list to look in `.agent_context/skills/`, but
// the only providers that ever reached it — grok and traecli — have their files
// written to `.grok/skills` and `.traecli/skills` and discover them natively.
func TestBriefSkillsListIsNamesOnly(t *testing.T) {
	t.Parallel()

	ctx := TaskContextForEnv{
		IssueID:   "issue-1",
		AgentName: "Eve",
		AgentID:   "eve-1",
		AgentSkills: []SkillContextForEnv{
			{
				Name:        "PR Review",
				Description: "Use when reviewing a pull request for the Multica project.",
				Content:     "---\nname: pr-review\n---\n\nbody",
			},
		},
	}

	// grok and traecli are the providers that used to take the removed branch;
	// the rest are a spread across the native-discovery list.
	for _, provider := range []string{"claude", "codex", "opencode", "hermes", "grok", "traecli", "some-unknown-provider"} {
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			out := buildMetaSkillContent(provider, ctx)

			if !strings.Contains(out, "- **pr-review**\n") {
				t.Errorf("brief does not list the skill by slug:\n%s", out)
			}
			if strings.Contains(out, "Use when reviewing a pull request") {
				t.Errorf("brief still carries the skill description; the CLI's own listing already has it:\n%s", out)
			}
			if strings.Contains(out, ".agent_context/skills/") {
				t.Errorf("brief still points at the removed fallback path:\n%s", out)
			}
			if !strings.Contains(out, "discovered automatically") {
				t.Errorf("brief lost the native-discovery framing:\n%s", out)
			}
		})
	}
}

// TestBriefIssuePointerFollowsTheInstalledSkill covers the compatibility
// direction the server cannot reach (MUL-6986).
//
// The brief is assembled here, in the daemon, from a binary the user installs
// on their own schedule. A backend deploy does not rewrite it, and an app
// update does not wait for a deploy, so both skews happen:
//
//   - old daemon, new backend — the server ships a redirect stub under the old
//     name, because this code is already frozen on that machine;
//   - new daemon, old backend — the server has no idea the merge happened, so
//     THIS code has to cope, which is why the pointer is resolved from the
//     skills the task actually received rather than hardcoded.
//
// The third case is the one that matters most: when neither skill is installed
// the brief says nothing. Naming a skill the agent does not have is worse than
// omitting the pointer — it sends the agent hunting, and on a miss it may skip
// the contract altogether.
func TestBriefIssuePointerFollowsTheInstalledSkill(t *testing.T) {
	t.Parallel()

	skill := func(name string) SkillContextForEnv {
		return SkillContextForEnv{Name: name, Content: "---\nname: " + name + "\n---\n\nbody"}
	}

	cases := []struct {
		name   string
		skills []SkillContextForEnv
		want   string // exact pointer text; "" = no pointer at all
	}{
		{
			name:   "current backend",
			skills: []SkillContextForEnv{skill("multica-platform")},
			want:   "`references/issues.md` in the `multica-platform` skill",
		},
		{
			// New daemon against a backend that has not been deployed yet.
			name:   "pre-merge backend",
			skills: []SkillContextForEnv{skill("multica-working-on-issues")},
			want:   "the `multica-working-on-issues` skill",
		},
		{
			// Mid-transition: the redirect stub rides along with the merged
			// skill. The merged skill wins — the stub is only a signpost.
			name:   "merged skill wins over the redirect stub",
			skills: []SkillContextForEnv{skill("multica-working-on-issues"), skill("multica-platform")},
			want:   "`references/issues.md` in the `multica-platform` skill",
		},
		{
			name:   "neither installed",
			skills: []SkillContextForEnv{skill("pr-review")},
			want:   "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := buildMetaSkillContent("claude", TaskContextForEnv{
				IssueID:     "issue-1",
				AgentSkills: tc.skills,
			})

			// The flags themselves are unconditional: they stay discoverable
			// with or without a skill to point at.
			for _, always := range []string{
				"`--status todo` starts an agent-assigned child immediately",
				"`--stage <N>` groups children into ordered stages",
				"never secrets or long content",
			} {
				if !strings.Contains(out, always) {
					t.Errorf("brief lost unconditional content %q", always)
				}
			}

			if tc.want == "" {
				for _, banned := range []string{"Full write discipline:", "Before creating sub-issues, read"} {
					if strings.Contains(out, banned) {
						t.Errorf("brief points at a skill with none installed (%q):\n%s", banned, out)
					}
				}
				return
			}
			if !strings.Contains(out, "Full write discipline: "+tc.want+".") {
				t.Errorf("metadata pointer does not name %q:\n%s", tc.want, out)
			}
			if !strings.Contains(out, "Before creating sub-issues, read "+tc.want+" —") {
				t.Errorf("sub-issue pointer does not name %q:\n%s", tc.want, out)
			}
		})
	}
}

// TestBriefPointsAtThePlatformSkill pins the one recall hint the Skills section
// carries (MUL-6986).
//
// Eight domain skills advertised eight descriptions in the always-loaded
// listing; they are now one skill with one description, which is cheaper but
// gives an agent one name to guess instead of eight. This line is what pays
// that back, so it must appear whenever the skill does — an agent that cannot
// find the platform contracts is strictly worse off than before the merge.
//
// Naming is by bare name, on the stated assumption that no workspace skill
// shares a built-in's name (see builtinSlug).
func TestBriefPointsAtThePlatformSkill(t *testing.T) {
	t.Parallel()

	skill := func(name string) SkillContextForEnv {
		return SkillContextForEnv{Name: name, Content: "---\nname: " + name + "\n---\n\nbody"}
	}

	cases := []struct {
		name   string
		skills []SkillContextForEnv
		want   string // the slug the pointer must name; "" = no pointer
	}{
		{
			name:   "platform skill present",
			skills: []SkillContextForEnv{skill("multica-platform")},
			want:   "multica-platform",
		},
		{
			name:   "alongside workspace skills",
			skills: []SkillContextForEnv{skill("pr-review"), skill("multica-platform")},
			want:   "multica-platform",
		},
		{
			name:   "platform skill absent",
			skills: []SkillContextForEnv{skill("pr-review")},
			want:   "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := buildMetaSkillContent("claude", TaskContextForEnv{
				IssueID:     "issue-1",
				AgentName:   "Eve",
				AgentID:     "eve-1",
				AgentSkills: tc.skills,
			})
			if tc.want == "" {
				if strings.Contains(out, "skill and open the reference") {
					t.Errorf("brief emitted a platform pointer with no built-in platform skill present:\n%s", out)
				}
				return
			}
			want := "load the `" + tc.want + "` skill"
			if !strings.Contains(out, want) {
				t.Errorf("brief does not point at %q:\n%s", tc.want, out)
			}
			// Pinned as "the domains your task touches", never a count: a task
			// that spans squads + issues + mentions needs all three, and
			// wording that implies one would make the agent act on contracts
			// it has not read.
			if !strings.Contains(out, "for the domains your task touches") {
				t.Errorf("pointer does not route by domain:\n%s", out)
			}
			if strings.Contains(out, "the one reference") {
				t.Errorf("pointer narrows on-demand reading to a single reference:\n%s", out)
			}
		})
	}
}

// Every brief that teaches `--output json` also says not to merge stderr into
// it, because the two facts are only useful together. The CLI is right:
// confirmations go to stderr, JSON goes to stdout, and `--output json | jq` has
// always worked. It stays right only while the caller keeps the streams apart,
// and `2>&1` is ordinary shell habit. The cost of merging them is not a cosmetic
// parse error: a confirmation line inside the JSON makes the parse fail, so a
// write that SUCCEEDED reads as one that failed, and the retry posts the comment
// or sends the file a second time.
//
// The assertions are on the rule's wording, not on loose substrings, because
// `2>&1` and "look like it failed" both survive a brief that says to merge the
// streams. Each builder must carry the prohibition verbatim, exactly once, with
// the consequence attached.
//
// Both brief builders are checked, not one. The quick-create brief is a
// separate function with its own copy of the `--output json` line, so guidance
// added to the full brief alone would be missing from exactly the runs that are
// given the least context to work it out for themselves.
func TestEveryBriefThatTeachesJSONOutputAlsoWarnsAgainstMergingStderr(t *testing.T) {
	t.Parallel()
	const (
		wantFlag = "--output json"
		// The premise the rule rests on. "Do not merge them" says nothing about
		// WHICH stream carries what, so a brief that swapped the two would pass
		// every other assertion here while telling an agent the opposite of the
		// truth — the same defect one clause to the left.
		wantPremise = "writes JSON to stdout; confirmations and warnings go to stderr"
		// The prohibition itself, not just the operator it names: "Always merge
		// them (`2>&1`)" contains `2>&1` and would pass a bare-operator check.
		wantRule = "Do not merge them (`2>&1`)"
		// The consequence, in the direction that makes the rule worth obeying;
		// the inverse claim ("failed write looks like it succeeded") is a
		// different bug and must not satisfy this.
		wantWhy = "a write that SUCCEEDED look like it failed"
	)
	briefs := map[string]string{
		"full":         buildMetaSkillContent("claude", TaskContextForEnv{IssueID: "11111111-2222-3333-4444-555555555555"}),
		"quick-create": buildMetaSkillContent("claude", TaskContextForEnv{QuickCreatePrompt: "make an issue"}),
	}
	for name, brief := range briefs {
		if !strings.Contains(brief, wantFlag) {
			t.Fatalf("%s brief does not mention %s at all; this test's premise is gone", name, wantFlag)
		}
		if !strings.Contains(brief, wantPremise) {
			t.Errorf("%s brief teaches %s without saying %q — the rule below it is only correct while the streams carry what this says they carry", name, wantFlag, wantPremise)
		}
		switch got := strings.Count(brief, wantRule); got {
		case 1:
		case 0:
			t.Errorf("%s brief teaches %s without saying %q — the habit it has to displace is the one thing an agent will not infer", name, wantFlag, wantRule)
			continue // the reason check below would report a rule that is not there
		default:
			t.Errorf("%s brief repeats %q %d times; one rule, one place, or the next edit fixes only one of them", name, wantRule, got)
		}
		if !strings.Contains(brief, wantWhy) {
			t.Errorf("%s brief states %q without %q; a rule with no reason is the first one dropped under pressure", name, wantRule, wantWhy)
		}
	}
}
