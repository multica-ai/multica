package handler

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
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
//   - the child must transition from a non-terminal status INTO a terminal one
//     (done or cancelled). Repeat saves of an already-terminal child do not
//     re-fire; only the entering transition does. Cancelled counts because a
//     cancelled sibling never finishes and so closes its stage (see the entry
//     guard and isTerminalChildStatus).
//   - issue.ParentIssueID must be set
//   - parent must not be "done" or "cancelled" — the parent is already
//     closed and a notification has no follow-up to drive
//   - parent must not be "backlog" — a parent parked in backlog is being
//     deliberately held for later; waking its assignee (which can then
//     promote sibling backlog sub-issues into todo) is exactly the
//     unwanted auto-activation reported in #4320 / MUL-3497. A parked
//     parent stays inert until the user explicitly moves it out of backlog.
//   - parent assignee must not be a member (human). Humans read their
//     issues manually; an automated system comment is pure noise for them
//     and there is nothing to "trigger" on a human assignee. Skipping the
//     comment entirely (Bohan's call on MUL-2538) also sidesteps the
//     mention question — no comment, no mention, no inbox row.
//   - the completion must close a STAGE barrier (MUL-3508). Sub-issues under
//     a parent can be grouped into ordered stages via issue.stage; the
//     notification + wake fire only when every sibling in the lowest
//     unfinished stage is terminal (stageBarrierClosed). An unstaged sibling
//     set is one implicit stage, so this fires once when the last sub-issue
//     finishes instead of on every child — the default fix for the
//     fire-on-every-child cascade reported in #4320. The woken assignee
//     decides whether to promote the next stage (agent-driven advancement);
//     the server only detects the barrier and wakes.
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
// with the idempotency guard documented there.
//
// Errors are logged at warn level and swallowed: this is a best-effort
// notification on the side of a successful status update; failing it must
// not roll back the user's status change.
func (h *Handler) notifyParentOfChildDone(ctx context.Context, prev, issue db.Issue) {
	if !issue.ParentIssueID.Valid {
		return
	}
	// Fire on a transition INTO a terminal status (done OR cancelled), not only
	// `done`. A cancelled child can close a stage too: isTerminalChildStatus
	// treats cancelled as terminal (a cancelled sibling never finishes, so it
	// must not hold the stage open), so the barrier has to be evaluated when the
	// last open child of a stage is cancelled. Keying on the transition also
	// makes a later cancelled -> done edit a no-op (terminal -> terminal), which
	// avoids a lagging duplicate wake.
	if isTerminalChildStatus(prev.Status) || !isTerminalChildStatus(issue.Status) {
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
	// A parent parked in backlog is deliberately held for later. Posting the
	// system comment would wake its assignee, and the woken agent can then
	// promote sibling backlog sub-issues into todo — the surprise auto-
	// activation reported in #4320 / MUL-3497. Skip the whole notification so
	// a backlog parent stays inert until the user explicitly promotes it.
	if parent.Status == "backlog" {
		return
	}
	// Human-assigned parents read their own timeline; an automated system
	// comment is just noise and there is no agent task to trigger. Skip the
	// whole notification (comment + mention + inbox row) — MUL-2538.
	if parent.AssigneeType.Valid && parent.AssigneeType.String == "member" {
		return
	}

	// Stage barrier (MUL-3508 / discussion #4320). The notification + assignee
	// wake fire only when this completion *closes a stage* — i.e. every sibling
	// in the lowest unfinished stage is now terminal. An unstaged sibling set is
	// one implicit stage, so this collapses to "wake once when the last
	// sub-issue finishes" instead of the old fire-on-every-child behavior that
	// caused the surprise cascade. A completion that does not close a stage is
	// silent: no comment, no wake. ListChildIssues already reflects this child's
	// committed `done` status (the status update commits before this runs).
	children, err := h.Queries.ListChildIssues(ctx, parent.ID)
	if err != nil {
		slog.Warn("child done: failed to list siblings for stage barrier",
			"error", err,
			"child_id", uuidToString(issue.ID),
			"parent_id", uuidToString(parent.ID))
		return
	}
	if !stageBarrierClosed(children, issue) {
		return
	}
	staged := siblingsAreStaged(children)

	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	identifier := prefix + "-" + strconv.Itoa(int(issue.Number))
	childID := uuidToString(issue.ID)
	title := sanitizeChildTitleForSystemComment(issue.Title)
	parentID := uuidToString(parent.ID)

	// Build the parent-assignee mention prefix. Empty when the parent has no
	// assignee or the assignee row is missing (deleted member, archived
	// agent the workspace lost track of, etc.).
	mentionPrefix := h.buildParentAssigneeMention(ctx, parent)

	var content string
	// When the set is staged and the barrier closed, the completed child is
	// guaranteed to carry a stage (stageBarrierClosed returns false for an
	// unstaged completed child in a staged set), so issue.Stage.Int32 is safe.
	if staged {
		closedStage := issue.Stage.Int32
		summary, nextStage := stageProgressSummary(children, closedStage)
		advance := stageAdvanceInstruction(nextStage, parentID)
		content = fmt.Sprintf(
			"%sStage %d of this issue is complete — its last sub-issue [%s](mention://issue/%s) — \"%s\" — just finished. Stage progress — %s.%s",
			mentionPrefix, closedStage, identifier, childID, title, summary, advance,
		)
	} else {
		content = fmt.Sprintf(
			"%sAll sub-issues are complete — the last one, [%s](mention://issue/%s) — \"%s\", just finished. Continue the parent: synthesize the children's results and move it forward, or close it out if nothing remains.",
			mentionPrefix, identifier, childID, title,
		)
	}

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
	h.dispatchParentAssigneeTrigger(ctx, parent, comment)
}

// isTerminalChildStatus reports whether a child issue status counts as
// "finished" for stage-barrier purposes. Cancelled counts as terminal: a
// cancelled sibling will never complete, so it must not hold a stage open.
func isTerminalChildStatus(status string) bool {
	return status == "done" || status == "cancelled"
}

// siblingsAreStaged reports whether any child in the set carries an explicit
// stage. A set with no stages is treated as a single implicit stage.
func siblingsAreStaged(children []db.Issue) bool {
	for _, c := range children {
		if c.Stage.Valid {
			return true
		}
	}
	return false
}

// stageBarrierClosed reports whether the completion of `completed` closed a
// stage barrier among `children` — the full sibling set under one parent,
// already reflecting completed's terminal status.
//
//   - Unstaged sibling set (no child carries a stage): a single implicit
//     stage. The barrier closes only when every child is terminal — the "wake
//     once when the last sub-issue finishes" default.
//   - Staged sibling set: only children that carry a stage form stages.
//     Unstaged children do NOT participate (matches migration 123: a NULL
//     stage does not take part in staged grouping) — completing one closes
//     nothing, and a non-terminal unstaged child never holds a stage open.
//     The completed child's stage S closes when every *staged* child with
//     stage <= S is terminal (frontier closure). Later stages are normally
//     parked in `backlog`, so they cannot fire out of order; the caller's
//     idempotency guard collapses any duplicate wake.
func stageBarrierClosed(children []db.Issue, completed db.Issue) bool {
	if !siblingsAreStaged(children) {
		for _, c := range children {
			if !isTerminalChildStatus(c.Status) {
				return false
			}
		}
		return true
	}
	// Staged set: an unstaged completed child belongs to no stage, so it closes
	// nothing.
	if !completed.Stage.Valid {
		return false
	}
	s := completed.Stage.Int32
	for _, c := range children {
		if !c.Stage.Valid {
			continue // unstaged children are ignored by the frontier
		}
		if c.Stage.Int32 <= s && !isTerminalChildStatus(c.Status) {
			return false
		}
	}
	return true
}

// stageProgressSummary renders a compact per-stage breakdown for the
// child-done system comment (e.g. "Stage 1: 3/3 done; Stage 2: 0/4 done") and
// returns the lowest stage above closedStage that still has non-terminal
// children — the next group to promote — or 0 when none remain. Unstaged
// children are skipped (they are not part of any stage), so the breakdown
// never renders a "Stage 0".
func stageProgressSummary(children []db.Issue, closedStage int32) (summary string, nextStage int32) {
	type agg struct{ total, done int }
	byStage := map[int32]*agg{}
	order := []int32{}
	for _, c := range children {
		if !c.Stage.Valid {
			continue // unstaged children do not belong to any stage
		}
		s := c.Stage.Int32
		a, ok := byStage[s]
		if !ok {
			a = &agg{}
			byStage[s] = a
			order = append(order, s)
		}
		a.total++
		if isTerminalChildStatus(c.Status) {
			a.done++
		}
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	parts := make([]string, 0, len(order))
	for _, s := range order {
		a := byStage[s]
		label := fmt.Sprintf("Stage %d: %d/%d done", s, a.done, a.total)
		if nextStage == 0 && s > closedStage && a.done < a.total {
			nextStage = s
			label += " (next)"
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, "; "), nextStage
}

// stageAdvanceInstruction returns the trailing instruction appended to a
// staged child-done system comment, given the next stage with pending work
// among the sub-issues that currently exist (nextStage, 0 = none).
//
//   - nextStage > 0: a later stage with unfinished work already exists, so
//     point the leader at it.
//   - nextStage == 0: no later stage exists *among the sub-issues created so
//     far*. This deliberately does NOT assert that the workflow is finished.
//     The server has no declarative workflow model — stages are agent-driven
//     and often created lazily (stage N+1's sub-issues are only written after
//     stage N produces the inputs they depend on), so an intermediate stage in
//     such a pipeline reaches nextStage == 0 exactly like a true final stage
//     does. The old wording ("This was the final stage. Wrap up the parent")
//     asserted a finality the server cannot know and pushed leaders to wrap up
//     mid-workflow (MUL-4062 / #4927). The message now names both possibilities
//     and hands the create-next-vs-wrap-up decision back to the leader.
func stageAdvanceInstruction(nextStage int32, parentID string) string {
	if nextStage > 0 {
		return fmt.Sprintf(
			" Stage %d is next. Review the full layout with `multica issue children %s`, and if Stage %d's dependencies are satisfied promote its `backlog` sub-issues to `todo` to continue. Read each sub-issue's description first and only promote items whose stated dependencies are already met — do not rely on this parent's higher-level breakdown alone. If a description conflicts with that breakdown, leave it `backlog` and post a comment to confirm first.",
			nextStage, parentID, nextStage,
		)
	}
	return " Completing this stage does not mean the whole issue is done. Decide whether the issue is actually complete — if so, wrap up the parent (synthesize the results and move it forward, or close it out) — or whether the next stage still needs to be created, in which case create that stage and its sub-issues now."
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
// applies the idempotency guard for the child-done notification.
//
// Side-effect semantics (intentionally narrower than a normal @mention):
//   - agent parent: one EnqueueTaskForMention on the parent assignee, same
//     trigger surface as a real @-mention so dedupe and readiness checks
//     match what users already rely on.
//   - squad parent: one EnqueueTaskForSquadLeader on the squad LEADER only.
//     Unlike a human @squad mention, this does NOT fan out to squad members
//     — child-done is a coordination signal, the leader decides whether
//     and how to wake the rest of the squad. Documented here so reviewers
//     don't read "system mention" as inheriting the full member fan-out. The
//     actor that closed the child is irrelevant to routing: the target is the
//     parent's own leader, chosen (and permission-checked) at squad-assign
//     time, so no actor identity is threaded in — see triggerChildDoneSquad.
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
//   - NO self-trigger guard on either the agent OR the squad path. Waking the
//     parent assignee when one of its children finishes is a serial sub-task
//     handoff across two DIFFERENT issues, not a self-loop — legitimate per
//     isAgentRunningOnIssue and the @mention self-trigger path
//     (computeMentionedAgentCommentTriggers). The squad path used to skip a
//     same-squad or shared-leader child on the theory that the leader had
//     already observed the work through its own coordination cycle on the
//     child. That stranded the common pattern where a squad decomposes its
//     parent into sub-issues assigned to its own squad: the stage-barrier
//     system comment lands on the PARENT carrying the "advance the next stage /
//     wrap up" instruction, which a child-side wake never delivers — so the
//     parent silently stalled in in_progress (MUL-3969). The squad path now
//     mirrors the agent path (MUL-2808): always dispatch, bounded only by
//     idempotency.
//   - Idempotency: HasPendingTaskForIssueAndAgent dedupes rapid-fire enqueues
//     for the same parent (e.g. two children finishing back-to-back). It also
//     bounds any re-trigger, since a leader waking on the parent does not by
//     itself push a child back into a terminal transition.
//   - Readiness: archived agents / missing runtimes are silently skipped
//     so a closed-out agent does not surface as a phantom assignee.
func (h *Handler) dispatchParentAssigneeTrigger(ctx context.Context, parent db.Issue, systemComment db.Comment) {
	if !parent.AssigneeType.Valid || !parent.AssigneeID.Valid {
		return
	}

	switch parent.AssigneeType.String {
	case "agent":
		h.triggerChildDoneAgent(ctx, parent, systemComment.ID)
	case "squad":
		h.triggerChildDoneSquad(ctx, parent, systemComment.ID)
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
		// Key dedup on the reviewed head (TEN-356).
		HeadSha: h.TaskService.ResolveIssueReviewSHAParam(ctx, parent.ID),
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
// assignee. It mirrors the agent path (see triggerChildDoneAgent) exactly:
//
//   - NO self-trigger guard: even when the finished child is owned by the same
//     squad or by another squad sharing this leader, the leader must still be
//     woken on the PARENT to advance the next stage or wrap up. The prior
//     same-squad / shared-leader guards assumed the leader had already observed
//     the child via its own coordination cycle, but that wake lands on the
//     CHILD and never carries the parent-level stage-barrier instruction, so it
//     stranded the common "squad decomposes its parent into sub-issues assigned
//     to its own squad" pattern (MUL-3969).
//   - NO leader-invocation gate. Waking the parent's OWN squad leader on
//     child-done is a coordination handoff on an issue the leader already owns,
//     not a fresh invocation — invocation permission was already enforced when
//     the parent was assigned to the squad (validateAssigneePair). The agent
//     path has never gated this. Re-checking it here on behalf of the child's
//     completer — an agent/system actor with no resolvable human originator —
//     failed closed for the DEFAULT private leader, silently stranding every
//     process-squad pipeline after its first stage while direct-to-leader-agent
//     parents advanced fine (MUL-4063 / GH #4928). Removed so agent and squad
//     child-done follow one path; if invocation permission is ever reintroduced
//     it must be added to BOTH paths together.
//
// Re-triggering is bounded by the HasPendingTaskForIssueAndAgent idempotency
// check below, exactly as the agent path relies on it.
func (h *Handler) triggerChildDoneSquad(ctx context.Context, parent db.Issue, triggerCommentID pgtype.UUID) {
	squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
		ID:          parent.AssigneeID,
		WorkspaceID: parent.WorkspaceID,
	})
	if err != nil {
		return
	}

	agent, err := h.Queries.GetAgent(ctx, squad.LeaderID)
	if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
		return
	}

	hasPending, err := h.Queries.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
		IssueID: parent.ID,
		AgentID: squad.LeaderID,
		// Key dedup on the reviewed head (TEN-356).
		HeadSha: h.TaskService.ResolveIssueReviewSHAParam(ctx, parent.ID),
	})
	if err != nil || hasPending {
		return
	}

	if _, err := h.TaskService.EnqueueTaskForSquadLeader(ctx, parent, squad.LeaderID, squad.ID, triggerCommentID); err != nil {
		slog.Warn("child done: enqueue parent squad leader task failed",
			"error", err,
			"parent_id", uuidToString(parent.ID),
			"squad_id", uuidToString(squad.ID),
			"leader_id", uuidToString(squad.LeaderID))
	}
}
