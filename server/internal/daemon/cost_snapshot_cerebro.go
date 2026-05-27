package daemon

// CEREBRO-PATCH(daemon-snapshot-prompt): the "snapshot_prompt" cost saving
// (FIR-2384). When a workspace turns the saving on, the server renders the
// issue + its recent thread at task-claim time and ships it in
// Task.IssueSnapshot. This file folds that snapshot into the agent's start
// prompt so the agent skips the per-run `multica issue get` +
// `multica issue comment list` round-trip it would otherwise make. The daemon
// runtime itself is unchanged — it only renders the prompt it is handed.

import "strings"

// snapshotIssueContext returns the issue-context section to inline when the
// server shipped a snapshot (saving "on"), and true. When no snapshot was
// shipped it returns "", false, and the caller emits the normal "run
// `multica issue get` yourself" instructions unchanged (saving "off"/"shadow").
func snapshotIssueContext(task Task) (string, bool) {
	snap := strings.TrimSpace(task.IssueSnapshot)
	if snap == "" {
		return "", false
	}
	var b strings.Builder
	b.WriteString(snap)
	b.WriteString("\n\nThe issue and its recent thread are included above — they have already been fetched for you. You do NOT need to run `multica issue get` or `multica issue comment list` to start. Fetch additional history only if you need threads older than what is shown.\n\n")
	return b.String(), true
}
