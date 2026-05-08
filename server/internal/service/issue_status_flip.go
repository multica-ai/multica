package service

import (
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// SourceHookComment is the issue_status_history.source value emitted whenever
// the comment-create handler auto-flips an issue's status. It is one of the
// values whitelisted by the table's CHECK constraint (see migration
// 070_issue_status_flow_p0.up.sql).
const SourceHookComment = "hook_comment"

// FlipTransition describes a single waiting↔in_progress status change derived
// from a comment event. It is returned by DecideFlip and consumed by the
// comment-create handler to drive the actual UPDATE issue + INSERT
// issue_status_history pair.
//
// Both fields are always one of {"waiting", "in_progress"}. Other status values
// are not considered automatic-flip-eligible (see DecideFlip).
type FlipTransition struct {
	FromStatus string
	ToStatus   string
}

// DecideFlip is the pure decision layer for the round-trip waiting↔in_progress
// auto-flip introduced by PUL-13 P1.
//
// It is a pure function: given a freshly-created comment and the locked issue
// it was attached to, it returns either a transition the caller should apply
// or nil if no automatic flip should occur.
//
// The function does NOT perform any I/O. The caller is responsible for:
//   - Holding a row lock on the issue (SELECT ... FOR UPDATE in the same tx).
//   - Calling UpdateIssueStatus + InsertStatusHistory inside the tx if a
//     transition is returned.
//   - Treating a UNIQUE(source, ref_id) violation on the history insert as an
//     idempotent skip (duplicate hook fire).
//
// Rules:
//
//	A. comment.author_type='member' on a 'waiting' issue assigned to an agent
//	   → flip waiting → in_progress.
//	B. comment.author_type='agent' on an 'in_progress' issue where the agent
//	   is the assignee → flip in_progress → waiting.
//
// Filters that short-circuit to nil:
//
//   - issue.assignee_type != 'agent' (member-to-member discussions are
//     human-managed; auto-flip would be noise).
//   - comment.type != 'comment' (status_change/system are server-generated;
//     progress_update is the explicit opt-out for agents posting interim
//     updates).
//   - Status not in {waiting, in_progress}. The other lifecycle states
//     (planned, developing, deployed, in_review, blocked, done, cancelled,
//     todo, backlog) are managed by other source kinds (skill_publish,
//     skill_pickup, webhook_forge, manual). Single responsibility per source.
//   - For Rule B, comment author must equal issue.assignee_id. A second agent
//     joining the discussion as a non-assignee does not move the ball.
func DecideFlip(comment db.Comment, issue db.Issue) *FlipTransition {
	// Skip non-agent-assigned tickets entirely.
	if !issue.AssigneeType.Valid || issue.AssigneeType.String != "agent" {
		return nil
	}

	// Filter on comment.type — only regular comments trigger flips.
	// status_change/system are auto-generated; progress_update is the opt-out
	// for agents posting interim updates.
	if comment.Type != "comment" {
		return nil
	}

	// Rule A: member comment on waiting ticket.
	if comment.AuthorType == "member" && issue.Status == "waiting" {
		return &FlipTransition{FromStatus: "waiting", ToStatus: "in_progress"}
	}

	// Rule B: agent assignee comment on in_progress ticket.
	// Single-assignee model: the comment author must be the issue's assignee.
	// A non-assignee agent joining the thread does not move the ball.
	if comment.AuthorType == "agent" &&
		issue.Status == "in_progress" &&
		issue.AssigneeID.Valid &&
		comment.AuthorID.Valid &&
		issue.AssigneeID.Bytes == comment.AuthorID.Bytes {
		return &FlipTransition{FromStatus: "in_progress", ToStatus: "waiting"}
	}

	// All other combinations: no flip.
	return nil
}
