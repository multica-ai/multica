package execenv

// CEREBRO-PATCH(runtime-config-snapshot): FIR-2384 — when a "fewer calls" cost
// saving rewrites the agent's start-of-run reads, the standing runtime workflow
// that buildMetaSkillContent writes into CLAUDE.md / AGENTS.md must NOT also
// tell the agent to run `multica issue get` + `multica issue comment list`
// separately. Those instructions were exactly what kept the savings dead: the
// agent obeyed the brief and re-fetched what the saving had already provided
// (and, for bundled_read, never called the endpoint whose per-task measurement
// the saving depends on). These helpers emit the read steps for the comment-
// and assignment-triggered workflows:
//   - snapshot_prompt on (snapshotInlined): point the agent at the inlined
//     issue+thread already in its start prompt.
//   - bundled_read on (bundleHint): point the agent at the single
//     `multica issue context` call.
//   - both off: reproduce the original fetch instructions verbatim, so the
//     default brief is byte-for-byte unchanged.
// snapshot_prompt takes precedence over bundled_read (matches the claim-handler
// precedence: the server clears BundleContextHint whenever a snapshot fires),
// so snapshotInlined is checked first.

import (
	"fmt"
	"strings"
)

// snapshotInlinedReadNote replaces the "fetch it yourself" read instruction
// when the issue + thread are already inlined into the start prompt.
const snapshotInlinedReadNote = "The issue and its recent thread are already included in your task prompt — they have been fetched for you. Do NOT run `multica issue get` or `multica issue comment list` to start; read the inlined context. Fetch additional history only if you need threads or replies older than what is shown."

// bundledIssueContextNote points the agent at the single `multica issue context`
// call. `purpose` is the trailing clause ("understand the issue context" /
// "understand your task").
func bundledIssueContextNote(issueID, purpose string) string {
	return fmt.Sprintf("Run `multica issue context %s --output json` to %s — ONE call that returns the issue, its recent thread, the workspace members, and the labels. Use it instead of `multica issue get` (and instead of the separate `multica issue comment list` / `multica workspace members` / `multica issue label list` reads).", issueID, purpose)
}

// commentIssueGetStep returns step 1 of the comment-triggered workflow.
func commentIssueGetStep(issueID string, snapshotInlined, bundleHint bool) string {
	switch {
	case snapshotInlined:
		return "1. " + snapshotInlinedReadNote + "\n"
	case bundleHint:
		return "1. " + bundledIssueContextNote(issueID, "understand the issue context") + "\n"
	}
	return fmt.Sprintf("1. Run `multica issue get %s --output json` to understand the issue context\n", issueID)
}

// commentThreadStep returns step 3 (and, when no saving is active, its paging
// sub-bullets) of the comment-triggered workflow.
//
// CEREBRO-PATCH(runtime-config-comment-hints): integrate upstream's
// BuildNewCommentsHint / BuildResumedCommentsHint / BuildColdCommentsHint
// (MUL-2785/2809/2805) so the brief surface matches the per-turn prompt while
// the FIR-2384 bundling rules still win when a snapshot or bundle saving is
// active. Without this integration, the upstream tests
// TestCommentTriggeredBriefCarriesNewCommentsHint and
// TestCommentTriggeredBriefResumedNoDeltaSkipsDefaultThreadRead fail because
// the brief uses the old generic step 3 instead of the situational hint.
func commentThreadStep(issueID, triggerCommentID, triggerThreadID string, newCommentCount int, newCommentsSince string, priorSessionResumed, snapshotInlined, bundleHint bool) string {
	switch {
	case snapshotInlined:
		return fmt.Sprintf("3. The triggering thread is in the inlined context above. Only if you need replies older than what is shown, run `multica issue comment list %s --thread %s --tail 30 --output json` and page older replies with the stderr `Next reply cursor: --before <ts> --before-id <reply-id>` line.\n", issueID, triggerCommentID)
	case bundleHint:
		return fmt.Sprintf("3. The triggering thread came back with your `multica issue context` call in step 1 — read it there; do NOT run a separate `multica issue comment list` for it. Only if you need replies older than what it returned, run `multica issue comment list %s --thread %s --tail 30 --output json` and page older replies with the stderr `Next reply cursor: --before <ts> --before-id <reply-id>` line.\n", issueID, triggerCommentID)
	}
	// Pick the comment-reading pointer that matches the run's state. Mirrors
	// the per-turn prompt path in daemon.buildCommentPrompt so the two
	// surfaces cannot drift (PR #2816 single-source rule).
	if hint := BuildNewCommentsHint(issueID, triggerCommentID, triggerThreadID, newCommentsSince, newCommentCount); hint != "" {
		return "3. " + hint
	}
	if priorSessionResumed {
		if hint := BuildResumedCommentsHint(issueID, triggerCommentID, triggerThreadID); hint != "" {
			return "3. " + hint
		}
	}
	if cold := BuildColdCommentsHint(issueID, triggerCommentID, triggerThreadID); cold != "" {
		return "3. " + cold
	}
	// Final defensive fallback when there is no triggering comment id (should
	// not happen on the comment-triggered path, but keep the prior wording so
	// the brief still emits a usable step 3).
	var b strings.Builder
	fmt.Fprintf(&b, "3. Read the triggering conversation first — that is what this comment is actually about. Default to the 30 most recent replies in that thread: `multica issue comment list %s --thread %s --tail 30 --output json` returns the root + the 30 newest replies (root is always included, even at `--tail 0`).\n", issueID, triggerCommentID)
	b.WriteString("   - If 30 replies aren't enough, walk older replies in the same thread one page at a time using the stderr `Next reply cursor: --before <ts> --before-id <reply-id>` line — pass the same pair back as `--before <ts> --before-id <reply-id>` on the next call. Under `--thread --tail` the cursor walks older *replies*, not older threads.\n")
	fmt.Fprintf(&b, "   - If you also need cross-thread background, pull the most recently active threads on the issue: `multica issue comment list %s --recent 20 --output json`. Under `--recent` the same `--before` / `--before-id` flags walk older *threads* instead of older replies, and the stderr line is `Next thread cursor: --before <ts> --before-id <root-id>`. Pass the pair back to scroll to older threads when 20 still isn't enough.\n", issueID)
	b.WriteString("   - Avoid the unfiltered `multica issue comment list <issue-id> --output json` form on long-running issues — it dumps the entire flat timeline (cap 2000) and wastes context on chatter unrelated to the trigger. `--since <RFC3339-timestamp>` is still available for incremental polling against a known cursor and may combine with `--thread --tail` or `--recent`.\n")
	return b.String()
}

// assignmentIssueGetStep returns step 1 of the assignment-triggered workflow.
func assignmentIssueGetStep(issueID string, snapshotInlined, bundleHint bool) string {
	switch {
	case snapshotInlined:
		return "1. " + snapshotInlinedReadNote + "\n"
	case bundleHint:
		return "1. " + bundledIssueContextNote(issueID, "understand your task") + "\n"
	}
	return fmt.Sprintf("1. Run `multica issue get %s --output json` to understand your task\n", issueID)
}

// assignmentCommentListStep returns step 3 of the assignment-triggered
// workflow. With no saving the full-history read is mandatory; with a saving on
// the inlined / bundled thread satisfies it and only older history needs paging.
func assignmentCommentListStep(issueID string, snapshotInlined, bundleHint bool) string {
	switch {
	case snapshotInlined:
		return fmt.Sprintf("3. The issue's recent thread is in the inlined context above — that satisfies the mandatory history read. Only if you need history older than what is shown, page it with `multica issue comment list %s --recent 20 --output json` plus the stderr `Next thread cursor:` line.\n", issueID)
	case bundleHint:
		return fmt.Sprintf("3. The issue's recent thread came back with your `multica issue context` call in step 1 — that satisfies the mandatory history read; do NOT run a separate `multica issue comment list`. Only if you need history older than what it returned, page it with `multica issue comment list %s --recent 20 --output json` plus the stderr `Next thread cursor:` line.\n", issueID)
	}
	return fmt.Sprintf("3. Run `multica issue comment list %s --output json` to read the full comment history (returns all comments, capped server-side at 2000) — this is mandatory, not optional. Earlier comments often carry context the issue body lacks (e.g. which repo to work in, the prior agent's findings, the reason the issue was reassigned to you). Skipping this step is the most common cause of agents acting on stale or incomplete instructions. When the flat dump is too large to ingest in one shot, treat `--recent 20 --output json` plus the `--before` / `--before-id` cursor (from the stderr `Next thread cursor:` line) as a paging strategy: keep walking older threads until you have read enough history to satisfy this mandatory step. `--recent` is a way to read the full history page-by-page, not a shortcut that replaces it.\n", issueID)
}
