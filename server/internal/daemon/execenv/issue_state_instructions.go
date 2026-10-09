package execenv

import (
	"fmt"
	"strings"
)

// BuildIssueStateHint returns the workflow-step-1 pointer for a comment-
// triggered run: either the unconditional "read the issue" instruction, or —
// when the server actually compared the issue against this agent's previous run
// on it — what that comparison found.
//
// The problem it solves is the mirror of the comment delta's (MUL-6984, and
// MUL-7344 for the `--since` form). A follow-up turn that resumes its provider
// session already holds the issue body it read minutes ago, yet the per-turn
// message told it to run `multica issue get` again before doing anything. On an
// issue that did not move, that read returns bytes the session already has.
//
// Three renderings, and which one applies is decided by the CALLER, not here:
//
//   - resumed && known && nothing changed → the comparison is reported as
//     step 1's answer, and the read becomes conditional on the agent's own
//     memory being insufficient. This is the only rendering that may replace
//     the read, and it is allowed for exactly the reason the empty comment
//     delta is: an affirmative server report about the whole record.
//   - resumed && known && something changed → the changed field NAMES plus the
//     read, so the agent knows what to look for rather than diffing blind.
//   - anything else → the pre-existing instruction, byte for byte. A cold
//     start, a dropped resume, an unknown delta and an old server all land
//     here, and none of them may be softened: the agent has no prior context
//     to continue from, or nothing looked.
//
// The compared set is named in full in the unchanged rendering rather than
// summarised as "the issue", because it is NOT the whole issue: labels, parent,
// due date, stage, project and metadata are not compared, and an agent told
// "unchanged" must be able to see that those were out of scope.
//
// Status and assignee ride along in both known renderings. They are what
// workflow step 3 needs ("is it already in progress", "is it mine"), and a run
// that is not reading the issue would otherwise have to read it for them.
//
// Like every other per-turn helper, this must never reach the runtime brief:
// its values are per-run, and the brief stays byte-identical across runs of a
// resumed session (MUL-5377).
func BuildIssueStateHint(issueID, issueStatus, assigneeType, assigneeID string, changedFields []string, deltaKnown, resumed bool) string {
	readCommand := fmt.Sprintf("`multica issue get %s --output json`", issueID)
	// The cold instruction is a byte-for-byte literal, not a format of the
	// other two: it is what every non-resumed run has always been handed, and
	// the tests that pin the cold path pin these exact bytes.
	cold := fmt.Sprintf("Start by running %s to understand your task, then decide how to proceed.\n\n", readCommand)
	// issueStatus empty means the server sent no issue state at all, which can
	// only happen on a server predating these fields. Treat it as unknown even
	// if deltaKnown somehow arrived set: reporting a status we do not have
	// would be worse than an extra read.
	if !resumed || !deltaKnown || issueStatus == "" {
		return cold
	}
	state := fmt.Sprintf("status: %s; assignee: %s", issueStatus, issueAssigneeLabel(assigneeType, assigneeID))
	if len(changedFields) == 0 {
		return fmt.Sprintf(
			"The issue is unchanged since your last run — the server compared title and description (%s). "+
				"That answers workflow step 1: continue from your resumed context, and re-read with %s only if resumed memory is not enough.\n\n",
			state, readCommand,
		)
	}
	return fmt.Sprintf(
		"Since your last run the issue changed: %s (%s). Read it: %s.\n\n",
		strings.Join(changedFields, ", "), state, readCommand,
	)
}

// issueAssigneeLabel renders the polymorphic assignee as one token pair. Both
// halves are required to mean anything — an assignee_type with no id, or an id
// with no type, is not an assignment — so a half-populated pair reads as
// unassigned rather than as a partial claim about ownership.
func issueAssigneeLabel(assigneeType, assigneeID string) string {
	if assigneeType == "" || assigneeID == "" {
		return "unassigned"
	}
	return assigneeType + " " + assigneeID
}
