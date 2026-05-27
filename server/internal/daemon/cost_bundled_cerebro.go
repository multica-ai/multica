package daemon

// CEREBRO-PATCH(daemon-bundled-prompt): the "bundled_read" cost saving
// (FIR-2384 step 2). When a workspace turns the saving "on", the server sets
// Task.BundleContextHint at claim time and this file rewrites the start prompt
// to steer the agent at a single `multica issue context` call instead of the
// separate `multica issue get` + `multica issue comment list` reads it makes
// today. The one bundled call also returns workspace members + issue labels, so
// follow-up `workspace members` / `issue label list` reads are unnecessary too.
//
// Precedence: snapshot_prompt wins. When a snapshot was inlined this run the
// reads are already gone, so the caller never reaches this hint (the server
// also clears BundleContextHint in that case). "shadow"/"off" never set the
// hint, so the default separate-reads instructions are emitted unchanged.

import "fmt"

// bundleContextInstruction returns the issue-context section to inline when the
// server set BundleContextHint (saving "on"), and true. When the hint is unset
// it returns "", false and the caller emits the normal separate-reads
// instructions.
func bundleContextInstruction(task Task) (string, bool) {
	if !task.BundleContextHint {
		return "", false
	}
	return fmt.Sprintf(
		"Start by running `multica issue context %s --output json` — it returns the issue, its recent comment thread, the workspace members, and the issue's labels in ONE call. Use it instead of separate `multica issue get` / `multica issue comment list` / `multica workspace members` / `multica issue label list` reads. Fetch older history with `multica issue comment list %s` only if you need threads beyond what it returns.\n\n",
		task.IssueID, task.IssueID,
	), true
}
