// FIR-1703 — net-new cerebro file. Makes the "wake the assignee agent on a
// member comment" path impossible to fail silently.
//
// The upstream on_comment gate (shouldEnqueueOnComment, issue.go) only answers
// yes/no and, when it answers no, the comment produced no run AND no log line —
// so when a wake did not start an agent there was nothing to look at afterwards.
// That is exactly the bug Jesper reported: "der må ikke fejle silent i wakeup".
//
// This observer mirrors that gate and, whenever a member comment that should
// have woken the assignee does NOT result in a runnable run, records a System
// Activity timeline entry (sysactivity) stating the cause. System Activity is
// the audit channel the platform already uses for the scheduled-wakeup feature
// (wakeup_scheduled / wakeup_parked); this brings the comment-trigger wake under
// the same visible, auditable trail.
//
// It deliberately stays silent on the genuine success path (agent online + a
// task enqueued or coalesced onto an existing pending task) so the timeline is
// not spammed on every ordinary comment — only outcomes the user needs to know
// about (won't run / deferred) are recorded. Keep the branches here in sync
// with shouldEnqueueOnComment.
package handler

import (
	"context"

	"github.com/multica-ai/multica/server/internal/cerebro/sysactivity"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// commentWakeAction is the System Activity action recorded when a comment-driven
// wake of the assignee agent does not produce a run it can start now. The
// frontend (issue-detail.tsx) renders a human label per details["reason"].
const commentWakeAction = "comment_wake_failed"

// Reasons recorded on the comment_wake_failed activity. These map 1:1 to the
// failure branches the upstream gate treats as a silent "no".
const (
	commentWakeReasonNoRuntime     = "no_runtime"      // agent has no runtime attached
	commentWakeReasonRuntimeOffline = "runtime_offline" // runtime attached but offline (task is queued, runs when back)
	commentWakeReasonArchived      = "archived"        // agent is archived
	commentWakeReasonLookup        = "lookup_failed"   // agent row could not be loaded
)

// observeCommentWakeOutcome records a System Activity entry when a member comment
// that should have woken the issue's assignee agent will not start a run now.
// Best-effort: it never blocks or alters comment creation. Call it after the
// upstream enqueue attempt for both the create- and edit-comment paths.
func (h *Handler) observeCommentWakeOutcome(ctx context.Context, issue db.Issue, comment db.Comment, authorType, _ string) {
	if authorType != "member" {
		return
	}
	if !issue.AssigneeType.Valid || issue.AssigneeType.String != "agent" || !issue.AssigneeID.Valid {
		return // no agent assignee → no wake was expected
	}

	record := func(reason string) {
		var agentName string
		if a, err := h.Queries.GetAgent(ctx, issue.AssigneeID); err == nil {
			agentName = a.Name
		}
		sysactivity.Record(ctx, h.Queries, h.Bus, issue.WorkspaceID, issue.ID, issue.AssigneeID, "agent", commentWakeAction, map[string]any{
			"reason":     reason,
			"agent_name": agentName,
			"comment_id": uuidToString(comment.ID),
			"trigger":    "comment",
		})
	}

	agent, err := h.Queries.GetAgent(ctx, issue.AssigneeID)
	if err != nil {
		record(commentWakeReasonLookup)
		return
	}
	if agent.ArchivedAt.Valid {
		record(commentWakeReasonArchived)
		return
	}
	if !agent.RuntimeID.Valid {
		record(commentWakeReasonNoRuntime)
		return
	}
	if !h.isRuntimeOnline(ctx, agent.RuntimeID) {
		// The upstream gate still enqueues a task (it only checks RuntimeID is
		// set, not that it is online), so the run is not lost — but it will not
		// start until the runtime comes back. Make that visible instead of
		// leaving the user staring at a comment that never got a reply.
		record(commentWakeReasonRuntimeOffline)
		return
	}
	// Agent online → upstream either enqueued a fresh task or coalesced onto an
	// existing pending one. Either way the agent will run; success needs no line.
}
