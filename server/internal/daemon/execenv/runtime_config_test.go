package execenv

import (
	"strings"
	"testing"
)

// Parent/Sub-issue Protocol — the brief teaches every issue-bound agent two
// things: when finishing a child issue, tell the parent; and when creating
// sub-issues, pick `--status todo` (start now) vs `--status backlog` (wait)
// deliberately. The protocol is runtime-only (no server-side state sync) and
// the section is identical for assignment- and comment-triggered runs — the
// comment-triggered workflow rule "Do NOT change the issue status unless the
// comment explicitly asks for it" naturally short-circuits the parent
// notification, so the protocol stays a single description.

func TestParentSubIssueProtocolPresentForIssueRuns(t *testing.T) {
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

			if !strings.Contains(out, "## Parent / Sub-issue Protocol") {
				t.Fatalf("expected Parent / Sub-issue Protocol section in %s brief", tc.name)
			}
			for _, want := range []string{
				// Mechanism framing — describe the data model, not a state
				// machine. The same text must apply to every issue-bound run.
				"parent/child tree via `parent_issue_id`",
				"does NOT auto-sync",
				"best-effort",
				// Rule 1 — finish a child, tell the parent.
				"**Tell the parent when you finish a child.**",
				"`parent_issue_id`",
				"top-level",
				"NO `--parent`",
				"`@mention` the parent's assignee",
				"`mention://agent/<id>`",
				"`mention://member/<id>`",
				"`mention://squad/<id>`",
				"no assignee",
				// The comment-triggered escape hatch must live in the same
				// unified paragraph so both runs read it.
				"NOT changing this issue's status",
				"not closing out the child",
				"skip the parent notification",
				// Rule 2 — sub-issue creation semantics.
				"**Choosing `--status` when creating sub-issues.**",
				"`--status todo` = **start now**",
				"`--status backlog` = **wait**",
				"`multica issue status <child-id> todo`",
				"all `--status todo`",
				"`--status backlog` from the start",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("[%s] protocol missing %q", tc.name, want)
				}
			}
			// Earlier revisions split Step A by trigger type, used per-rule
			// gating tables, or ### A/### B subheadings. The unified
			// revision must not regress into any of those.
			for _, banned := range []string{
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
			} {
				if strings.Contains(out, banned) {
					t.Errorf("[%s] expected %q to be removed", tc.name, banned)
				}
			}
		})
	}
}

// Lock in the "compact convention, not a spec" framing: the Parent /
// Sub-issue Protocol section must stay short. The unified two-rule revision
// runs around 6 lines; this guard prevents future edits from silently
// re-inflating it back into a state-machine.
func TestParentSubIssueProtocolIsCompact(t *testing.T) {
	t.Parallel()
	ctx := TaskContextForEnv{
		IssueID: "12345678-1234-1234-1234-123456789012",
	}
	out := buildMetaSkillContent("claude", ctx)

	const header = "## Parent / Sub-issue Protocol"
	start := strings.Index(out, header)
	if start == -1 {
		t.Fatalf("protocol section missing")
	}
	rest := out[start+len(header):]
	end := strings.Index(rest, "\n## ")
	var section string
	if end == -1 {
		section = out[start:]
	} else {
		section = out[start : start+len(header)+end]
	}
	if got := strings.Count(section, "\n"); got > 10 {
		t.Errorf("Parent / Sub-issue Protocol should stay ≤10 lines (best-effort convention, not a spec); got %d:\n%s", got, section)
	}
}

// Comment-triggered briefs must NOT carry any unconditional status-flip
// command targeting the current issue. The previous revision had a
// dedicated Step A that wrote `multica issue status <this-issue-id> in_review`
// into the protocol; the unified revision removes that command from the
// protocol entirely and leans on the comment-triggered workflow rule
// "Do NOT change the issue status unless the comment explicitly asks for it"
// to keep the agent honest (Elon's blocking review on PR #2918).
func TestCommentTriggeredProtocolDoesNotForceInReview(t *testing.T) {
	t.Parallel()
	ctx := TaskContextForEnv{
		IssueID:          "55555555-6666-7777-8888-999999999999",
		TriggerCommentID: "66666666-7777-8888-9999-aaaaaaaaaaaa",
	}
	out := buildMetaSkillContent("claude", ctx)

	// The placeholder `<this-issue-id>` only ever lived inside the protocol
	// section; the workflow above substitutes the real id. So the literal
	// substring is the right canary for "protocol is trying to flip status
	// behind the workflow's back".
	if strings.Contains(out, "`multica issue status <this-issue-id> in_review`") {
		t.Errorf("comment-triggered brief must not contain a placeholder `<this-issue-id> in_review` flip — that conflicts with the comment-triggered \"do not change status unless asked\" rule")
	}

	// The comment-triggered workflow guardrail must still be present so the
	// protocol's unified instruction has something to defer to.
	const guardrail = "Do NOT change the issue status unless the comment explicitly asks for it"
	if !strings.Contains(out, guardrail) {
		t.Errorf("expected the comment-triggered workflow guardrail %q to be present", guardrail)
	}

	// The unified protocol paragraph must still teach the agent that a
	// comment-triggered run without a status flip means "not closing out
	// the child" → skip the parent notification.
	for _, want := range []string{
		"NOT changing this issue's status",
		"not closing out the child",
		"skip the parent notification",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("comment-triggered protocol missing required short-circuit phrasing %q", want)
		}
	}
}

// Assignment-triggered briefs are the inverse boundary: when the agent owns
// the issue lifecycle, the brief AS A WHOLE must still tell it to flip to
// in_review on completion. After unification the flip lives in the
// assignment-triggered workflow above (with the real id substituted), not
// in the protocol section, so we assert against the actual id.
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

// Rule 2 (creating sub-issues) must apply to any issue-bound run, including
// a top-level parent issue that has no `parent_issue_id` of its own. The
// unified preamble must not globally gate the protocol on the current issue
// being a child, and rule 2 must not carry any `parent_issue_id` reference.
func TestSubIssueCreationRuleIsUnconditional(t *testing.T) {
	t.Parallel()
	ctx := TaskContextForEnv{
		IssueID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
	}
	out := buildMetaSkillContent("claude", ctx)

	const header = "## Parent / Sub-issue Protocol"
	start := strings.Index(out, header)
	if start == -1 {
		t.Fatalf("protocol section missing")
	}
	rest := out[start:]
	end := strings.Index(rest[len(header):], "\n## ")
	var section string
	if end == -1 {
		section = rest
	} else {
		section = rest[:len(header)+end]
	}

	// Preamble must not globally gate on `parent_issue_id`.
	for _, banned := range []string{
		"When this issue has `parent_issue_id`:",
		"For parent/child work, use these best-effort rules",
	} {
		if strings.Contains(section, banned) {
			t.Errorf("preamble must not globally gate the protocol on `parent_issue_id` — rule 2 needs to reach top-level parents too; found %q", banned)
		}
	}

	// Find rule 2 and check it does NOT reference `parent_issue_id` at all
	// (the only mention of `parent_issue_id` in the section belongs to
	// rule 1's "if this issue has a `parent_issue_id`" gate).
	rule2Idx := strings.Index(section, "2. **Choosing `--status` when creating sub-issues.**")
	if rule2Idx == -1 {
		t.Fatalf("rule 2 (Choosing `--status` when creating sub-issues) missing from protocol section")
	}
	rule2 := section[rule2Idx:]
	if strings.Contains(rule2, "parent_issue_id") {
		t.Errorf("rule 2 (Choosing `--status` when creating sub-issues) must not be gated by `parent_issue_id`; it applies to any issue-bound run:\n%s", rule2)
	}

	// Rule 1 must still carry the gate — without it the agent might post on
	// a parent that doesn't exist.
	if !strings.Contains(section, "**Tell the parent when you finish a child.** If this issue has a `parent_issue_id`") {
		t.Errorf("rule 1 missing per-rule `parent_issue_id` gate")
	}
}

func TestParentSubIssueProtocolSkippedForNonIssueModes(t *testing.T) {
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
			if strings.Contains(out, "## Parent / Sub-issue Protocol") {
				t.Errorf("%s mode must NOT emit the Parent / Sub-issue Protocol section", tc.name)
			}
		})
	}
}

// Guardrails for things Elon's review explicitly flagged: no reference to a
// non-existent `multica issue list --parent` command, and no claim that the
// protocol is a stable / guaranteed handshake.
func TestParentSubIssueProtocolHasNoForbiddenClaims(t *testing.T) {
	t.Parallel()
	ctx := TaskContextForEnv{
		IssueID: "44444444-5555-6666-7777-888888888888",
	}
	out := buildMetaSkillContent("claude", ctx)

	for _, banned := range []string{
		"issue list --parent",
		"is a guaranteed handshake",
		"is a reliable handshake",
		"guarantees parent sync",
		"reliable parent sync",
	} {
		if strings.Contains(out, banned) {
			t.Errorf("brief must not contain %q (best-effort only, no inexistent CLI)", banned)
		}
	}
	// The brief must explicitly frame the signal as best-effort so the
	// agent does not assume the parent always sees it.
	if !strings.Contains(out, "best-effort") {
		t.Errorf("brief must explicitly call the parent notification best-effort")
	}
}
