package handler

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// notifyParentOfChildDone posts a top-level system comment on the parent
// issue when a child issue transitions from non-done into done. This replaces
// the agent-prompt rule that previously made child agents post the
// notification themselves (PR #2918 user feedback — the agent rule caused
// self-mention loops, planner ping-pong, and accidental `MUL-` prefix
// hardcoding because the agent did not always know the workspace prefix).
//
// Guards on whether the comment fires at all:
//   - prev.Status must not already be "done" (idempotent — repeat saves of
//     done do not re-fire; only the transition fires)
//   - issue.Status must be "done"
//   - issue.ParentIssueID must be set
//   - parent must not be "done" or "cancelled" — the parent is already
//     closed and a notification has no follow-up to drive
//   - parent assignee must not be a member (human). Humans read their
//     issues manually; an automated system comment is pure noise for them
//     and there is nothing to "trigger" on a human assignee. Skipping the
//     comment entirely (Bohan's call on MUL-2538) also sidesteps the
//     mention question — no comment, no mention, no inbox row.
//
// The comment is inserted directly via db.Queries (not through the
// CreateComment HTTP handler) so it bypasses the generic on_comment trigger
// path. When the parent has an agent or squad assignee, the comment body
// embeds a single `mention://{agent,squad}/<id>` link that targets the
// parent assignee — Bohan's product call on MUL-2538 ("system child-done
// comment 无脑 mention parent assignee，member/squad/agent 都覆盖", later
// narrowed to skip member assignees outright). To keep the platform in
// control of side effects, the cmd/server notification + subscriber
// listeners still skip system comments wholesale, so smuggled mentions from
// the child title cannot light up unrelated members. The parent assignee's
// own trigger is fired explicitly by dispatchParentAssigneeTrigger below,
// with the loop and idempotency guards documented there.
//
// Errors are logged at warn level and swallowed: this is a best-effort
// notification on the side of a successful status update; failing it must
// not roll back the user's status change.
func (h *Handler) notifyParentOfChildDone(ctx context.Context, prev, issue db.Issue, actorType, actorID string) {
	if !issue.ParentIssueID.Valid {
		return
	}
	if prev.Status == "done" || issue.Status != "done" {
		return
	}
	parent, err := h.Queries.GetIssue(ctx, issue.ParentIssueID)
	if err != nil {
		slog.Warn("child done: failed to load parent",
			"error", err,
			"child_id", uuidToString(issue.ID),
			"parent_id", uuidToString(issue.ParentIssueID))
		return
	}
	if parent.Status == "done" || parent.Status == "cancelled" {
		return
	}
	// Human-assigned parents read their own timeline; an automated system
	// comment is just noise and there is no agent task to trigger. Skip the
	// whole notification (comment + mention + inbox row) — MUL-2538.
	if parent.AssigneeType.Valid && parent.AssigneeType.String == "member" {
		return
	}

	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	identifier := prefix + "-" + strconv.Itoa(int(issue.Number))
	childID := uuidToString(issue.ID)
	title := sanitizeChildTitleForSystemComment(issue.Title)

	// Build the parent-assignee mention prefix. Empty when the parent has no
	// assignee or the assignee row is missing (deleted member, archived
	// agent the workspace lost track of, etc.).
	mentionPrefix := h.buildParentAssigneeMention(ctx, parent)

	content := fmt.Sprintf(
		"%sSub-issue [%s](mention://issue/%s) — \"%s\" — is done. Before promoting any waiting `backlog` sub-issue, read each sibling's description and only promote items whose stated dependencies are already satisfied — do not rely on this parent's higher-level breakdown alone. If a sibling's description conflicts with that breakdown (e.g. it lists a prerequisite the parent treats as parallel), do NOT change its status — leave it `backlog` and post a comment to confirm first.",
		mentionPrefix, identifier, childID, title,
	)

	// author_type='system', author_id=zero UUID. The zero UUID is a valid 16
	// byte value and the column is NOT NULL; frontend code should branch on
	// author_type === 'system' rather than on the UUID value.
	comment, err := h.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     parent.ID,
		WorkspaceID: parent.WorkspaceID,
		AuthorType:  "system",
		AuthorID:    pgtype.UUID{Valid: true},
		Content:     content,
		Type:        "system",
		ParentID:    pgtype.UUID{Valid: false},
	})
	if err != nil {
		slog.Warn("child done: create system comment failed",
			"error", err,
			"child_id", childID,
			"parent_id", uuidToString(parent.ID))
		return
	}

	h.publish(protocol.EventCommentCreated, uuidToString(parent.WorkspaceID), "system", "", map[string]any{
		"comment":             commentToResponse(comment, nil, nil),
		"issue_title":         parent.Title,
		"issue_assignee_type": textToPtr(parent.AssigneeType),
		"issue_assignee_id":   uuidToPtr(parent.AssigneeID),
		"issue_status":        parent.Status,
	})

	// Dispatch the explicit trigger / inbox row for the parent assignee.
	// Listener-level mention parsing is intentionally NOT involved (the
	// notification + subscriber listeners both short-circuit on
	// author_type='system'); this keeps smuggled mentions from the child
	// title inert and gives the platform a single place to apply the loop
	// and idempotency guards.
	h.dispatchParentAssigneeTrigger(ctx, parent, issue, comment, actorType, actorID)
}

// sanitizeChildTitleForSystemComment removes mention-style markdown from a
// child issue's title before it is embedded into the parent's system
// comment. Smuggled mentions are already harmless on the listener path
// (notification + subscriber listeners both skip system comments), but the
// timeline still renders the title verbatim — stripping the markdown keeps
// the rendered comment readable and stops a maliciously titled child issue
// from looking like a directive ("@all please look").
func sanitizeChildTitleForSystemComment(title string) string {
	// Replace any markdown link target so the regex no longer matches it,
	// while preserving the human-readable label text. `]` and `(` are the
	// minimum delimiters of the mention regex; replacing the `(` is enough
	// to break the match without mangling the label.
	cleaned := strings.ReplaceAll(title, "](mention://", "] (mention-stripped://")
	return cleaned
}

// buildParentAssigneeMention returns the markdown prefix that the system
// comment should lead with, including a trailing space, so the body reads
// like a normal mention-led comment. Returns the empty string when the
// parent has no assignee or the assignee row could not be loaded.
func (h *Handler) buildParentAssigneeMention(ctx context.Context, parent db.Issue) string {
	if !parent.AssigneeType.Valid || !parent.AssigneeID.Valid {
		return ""
	}
	label, ok := h.resolveAssigneeMentionLabel(ctx, parent.WorkspaceID, parent.AssigneeType.String, parent.AssigneeID)
	if !ok {
		return ""
	}
	return fmt.Sprintf("[@%s](mention://%s/%s) ", label, parent.AssigneeType.String, uuidToString(parent.AssigneeID))
}

// resolveAssigneeMentionLabel returns the label text to render inside the
// mention link. The label is for human display only — the mention regex
// keys off the URL path, not the label — but a sensible fallback keeps the
// rendered comment legible if the frontend has not pre-loaded the assignee.
// Returns ok=false when the assignee row cannot be loaded; the caller
// should then omit the mention entirely rather than emit a broken link.
func (h *Handler) resolveAssigneeMentionLabel(ctx context.Context, workspaceID pgtype.UUID, assigneeType string, assigneeID pgtype.UUID) (string, bool) {
	switch assigneeType {
	case "agent":
		agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
			ID:          assigneeID,
			WorkspaceID: workspaceID,
		})
		if err != nil {
			return "", false
		}
		return sanitizeMentionLabel(agent.Name), true
	case "squad":
		squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
			ID:          assigneeID,
			WorkspaceID: workspaceID,
		})
		if err != nil {
			return "", false
		}
		return sanitizeMentionLabel(squad.Name), true
	}
	return "", false
}

// sanitizeMentionLabel strips characters that would break the mention
// markdown if a name contained them. The mention regex is non-greedy on the
// label, so a stray `]` would short-circuit it. Names with `]` are
// vanishingly rare but cheap to defend against.
func sanitizeMentionLabel(name string) string {
	cleaned := strings.ReplaceAll(name, "]", "")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return "assignee"
	}
	return cleaned
}

// dispatchParentAssigneeTrigger fires the explicit side effect that pairs
// with the @mention link in the system comment body — an agent task for
// agent or squad-leader assignees. Member assignees never reach this code
// path; notifyParentOfChildDone skips them outright. The generic comment
// listener is intentionally bypassed (it short-circuits on
// author_type='system'), so this is the single place where the platform
// applies loop and idempotency guards for the child-done notification.
//
// Side-effect semantics (intentionally narrower than a normal @mention):
//   - agent parent: one EnqueueTaskForMention on the parent assignee, same
//     trigger surface as a real @-mention so dedupe and readiness checks
//     match what users already rely on.
//   - squad parent: one EnqueueTaskForSquadLeader on the squad LEADER only.
//     Unlike a human @squad mention, this does NOT fan out to squad members
//     — child-done is a coordination signal, the leader decides whether
//     and how to wake the rest of the squad. Documented here so reviewers
//     don't read "system mention" as inheriting the full member fan-out.
//   - notification_preference is not consulted: this is a platform routing
//     signal targeted at the assignee that already owns the parent, not a
//     general notification. Per-user mute settings are evaluated by the
//     downstream agent_task / inbox pipeline once the task is dispatched.
//   - notification_listeners.go short-circuits on author_type='system', so
//     subscriber emails and member-inbox rows from smuggled mentions in the
//     child title are inert — only the explicit dispatch below runs.
//
// Guards applied here:
//   - No-op when the parent has no assignee row.
//   - Squad loop guard (squad parent only): skip when the finished child is
//     the same squad, or its effective owner is the parent squad's leader. A
//     squad leader already observes same-squad work through its own
//     coordination cycle — the worker's completion comment wakes the leader
//     via computeAssignedSquadLeaderCommentTrigger — so the child-done trigger would
//     be redundant; this also closes the cross-squad shared-leader loop. The
//     AGENT parent path intentionally has NO such guard (MUL-2808): a lone
//     agent that decomposes its parent into sub-issues it owns itself has no
//     other wake path, and waking the parent agent when its child finishes is
//     a serial sub-task handoff across two DIFFERENT issues — explicitly not a
//     self-loop per isAgentRunningOnIssue, and consistent with the @mention
//     self-trigger path (computeMentionedAgentCommentTriggers). Runaway re-triggering is
//     bounded by the idempotency guard below, not by suppressing the trigger.
//   - Idempotency: HasPendingTaskForIssueAndAgent dedupes rapid-fire enqueues
//     for the same parent (e.g. two children finishing back-to-back).
//   - Readiness: archived agents / missing runtimes are silently skipped
//     so a closed-out agent does not surface as a phantom assignee.
func (h *Handler) dispatchParentAssigneeTrigger(ctx context.Context, parent, child db.Issue, systemComment db.Comment, actorType, actorID string) {
	if !parent.AssigneeType.Valid || !parent.AssigneeID.Valid {
		return
	}

	switch parent.AssigneeType.String {
	case "agent":
		h.triggerChildDoneAgent(ctx, parent, systemComment.ID)
	case "squad":
		h.triggerChildDoneSquad(ctx, parent, child, systemComment.ID, actorType, actorID)
	}
}

// triggerChildDoneAgent enqueues a mention-style task for the parent's
// agent assignee.
//
// There is intentionally NO same-agent self-trigger guard here, unlike the
// squad path. Waking the parent agent when one of its children finishes is a
// serial sub-task handoff between two DIFFERENT issues, which the platform
// loop model treats as legitimate ("not a loop and must fire" — see
// isAgentRunningOnIssue); only re-entering the SAME issue is a loop. A lone
// agent that decomposes its parent into sub-issues it owns itself has no
// other wake path, so the old "child owner == parent agent" guard silently
// stranded those parents (MUL-2808). Runaway re-triggering is prevented by
// the HasPendingTaskForIssueAndAgent dedup below, exactly as the @mention
// self-trigger path relies on it (see computeMentionedAgentCommentTriggers).
func (h *Handler) triggerChildDoneAgent(ctx context.Context, parent db.Issue, triggerCommentID pgtype.UUID) {
	agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
		ID:          parent.AssigneeID,
		WorkspaceID: parent.WorkspaceID,
	})
	if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
		return
	}

	hasPending, err := h.Queries.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
		IssueID: parent.ID,
		AgentID: parent.AssigneeID,
	})
	if err != nil || hasPending {
		return
	}

	if _, err := h.TaskService.EnqueueTaskForMention(ctx, parent, parent.AssigneeID, triggerCommentID); err != nil {
		slog.Warn("child done: enqueue parent agent task failed",
			"error", err,
			"parent_id", uuidToString(parent.ID),
			"agent_id", uuidToString(parent.AssigneeID))
	}
}

// triggerChildDoneSquad enqueues a leader-role task for the parent's squad
// assignee, applying the self-trigger guard against:
//   - same squad on both sides (the leader already observed the child via
//     its own coordination cycle), and
//   - same effective leader on both sides — child agent == leader, or
//     child squad's leader == this squad's leader (the cross-squad shared
//     leader loop).
func (h *Handler) triggerChildDoneSquad(ctx context.Context, parent, child db.Issue, triggerCommentID pgtype.UUID, actorType, actorID string) {
	squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
		ID:          parent.AssigneeID,
		WorkspaceID: parent.WorkspaceID,
	})
	if err != nil {
		return
	}

	// Private-leader gate: deny if the actor cannot access the leader.
	if !h.canEnqueueSquadLeader(ctx, squad.LeaderID, actorType, actorID, uuidToString(parent.WorkspaceID)) {
		return
	}

	// Same-squad child → the leader has already observed the work via its
	// own coordination cycle on the child; firing again on the parent would
	// just re-trigger the same leader run with no new signal.
	if childAssigneeIsSquad(child, parent.AssigneeID) {
		return
	}
	// Shared-leader loop: child driven directly by the parent squad's leader,
	// or by another squad whose leader is the same agent.
	if owner := h.effectiveChildAgentOwner(ctx, child); owner.Valid &&
		uuidToString(owner) == uuidToString(squad.LeaderID) {
		return
	}

	agent, err := h.Queries.GetAgent(ctx, squad.LeaderID)
	if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
		return
	}

	hasPending, err := h.Queries.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
		IssueID: parent.ID,
		AgentID: squad.LeaderID,
	})
	if err != nil || hasPending {
		return
	}

	if _, err := h.TaskService.EnqueueTaskForSquadLeader(ctx, parent, squad.LeaderID, triggerCommentID); err != nil {
		slog.Warn("child done: enqueue parent squad leader task failed",
			"error", err,
			"parent_id", uuidToString(parent.ID),
			"squad_id", uuidToString(squad.ID),
			"leader_id", uuidToString(squad.LeaderID))
	}
}

// effectiveChildAgentOwner returns the agent identity that effectively
// "owns" the child issue from the perspective of the child-done trigger:
//
//   - child agent assignee → that agent
//   - child squad assignee → that squad's leader (the agent that would
//     actually act on a leader task and is the entry point for any squad
//     work; a shared leader across two squads is the loop vector the
//     callers above defend against)
//   - anything else (member assignee, no assignee, missing squad row) →
//     invalid UUID, signalling "no shared owner to compare against"
//
// Callers compare this against the agent they are about to trigger; equality
// means we'd be enqueueing the same agent that just finished the child,
// which is the loop case.
func (h *Handler) effectiveChildAgentOwner(ctx context.Context, child db.Issue) pgtype.UUID {
	if !child.AssigneeType.Valid || !child.AssigneeID.Valid {
		return pgtype.UUID{}
	}
	switch child.AssigneeType.String {
	case "agent":
		return child.AssigneeID
	case "squad":
		squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
			ID:          child.AssigneeID,
			WorkspaceID: child.WorkspaceID,
		})
		if err != nil {
			return pgtype.UUID{}
		}
		return squad.LeaderID
	}
	return pgtype.UUID{}
}

func childAssigneeIsSquad(child db.Issue, squadID pgtype.UUID) bool {
	if !child.AssigneeType.Valid || child.AssigneeType.String != "squad" || !child.AssigneeID.Valid {
		return false
	}
	return uuidToString(child.AssigneeID) == uuidToString(squadID)
}
