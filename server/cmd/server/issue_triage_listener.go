package main

import (
	"context"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Triage entries do not notify and do not subscribe (MUL-7189 §2.5).
//
// An entry in Triage is a proposal nobody has taken on. Subscribing its
// pre-filled assignee, or dropping an inbox row on a member named in it, would
// pull people into work that has not been accepted — the exact thing Triage
// exists to prevent. So the subscriber and notification listeners ask this
// question at the top of every block, and return when the answer is yes.
//
// Activity logging is deliberately NOT gated: the history of what happened in
// Triage is worth keeping, and an activity row notifies nobody.
//
// accept is the seam. Once an entry leaves Triage the column is already NULL by
// the time the event is published, so nothing here suppresses it; what accept
// needs on top of that is acceptedFromTriage below.

// issueInTriage reports whether the issue is currently a Triage entry.
//
// It reads the column rather than the event payload: no publisher carries
// `triage_state` today, and a primary-key lookup of one column is cheap next to
// the subscriber fan-out and inbox writes it guards.
//
// An unreadable issue counts as in Triage. A missing row is an issue that was
// deleted mid-event, which has nobody to notify anyway; any other error means
// the caller cannot tell, and the failure that wakes a human about a proposal
// is worse than the failure that drops one notification — the same fail-closed
// choice the queue door makes in §2.3.
func issueInTriage(ctx context.Context, queries *db.Queries, issueID string) bool {
	id, err := util.ParseUUID(issueID)
	if err != nil {
		slog.Warn("triage check: unparseable issue id", "issue_id", issueID, "error", err)
		return true
	}
	state, err := queries.GetIssueTriageState(ctx, id)
	if err != nil {
		slog.Warn("triage check: could not read triage_state", "issue_id", issueID, "error", err)
		return true
	}
	return state.Valid
}

// acceptedFromTriage reports whether an `issue:updated` payload is the accept
// that moved an entry out of Triage.
//
// Accept is not an ordinary update. The issue was invisible on every work
// surface a moment ago and has no subscribers, so the update listeners have
// nothing to build on: "assignee changed" is false (the triager pre-filled it),
// "description changed" is false, and the creator was never subscribed. Worse,
// the update path would read the unchanged assignee as an unassignment and the
// unchanged status as a transition. Both listeners therefore treat this one
// flag as `issue:created` and run the creation rules instead (MUL-7189 §2.5).
func acceptedFromTriage(payload map[string]any) bool {
	accepted, _ := payload["accepted_from_triage"].(bool)
	return accepted
}
