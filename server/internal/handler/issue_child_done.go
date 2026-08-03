package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
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
//     (done, blocked, or cancelled). Repeat saves of an already-terminal child
//     do not re-fire, except blocked -> done/cancelled: resolving the blocker is
//     a distinct completion handoff. Blocked wakes the coordinator to resolve
//     the blocker, while cancelled closes work that will never finish (see the
//     entry guard and isTerminalChildStatus).
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
// Terminal status writers persist a child_done_transition in the same database
// transaction. A leased worker turns that transition into the system-comment
// outbox row, and a second lease phase dispatches the task. Both phases survive
// process restarts; permanent routing loss is recorded as skipped rather than
// retried in a hot loop.
func (h *Handler) notifyParentOfChildDone(ctx context.Context, prev, issue db.Issue) {
	if !issue.ParentIssueID.Valid || !entersChildDoneBarrier(prev.Status, issue.Status) {
		return
	}
	if err := h.notifyParentsOfBatchChildDone(ctx, []db.Issue{issue}); err != nil {
		slog.Warn("child done: notification deferred", "error", err,
			"child_id", uuidToString(issue.ID),
			"parent_id", uuidToString(issue.ParentIssueID))
	}
}

// entersChildDoneBarrier allows the coordinator to be woken once when work is
// blocked and again when that blocker is actually resolved. Other
// terminal-to-terminal edits (for example cancelled -> done) remain silent.
func entersChildDoneBarrier(previous, current string) bool {
	if !isTerminalChildStatus(current) {
		return false
	}
	if !isTerminalChildStatus(previous) {
		return true
	}
	return previous == "blocked" && current != "blocked"
}

// notifyParentsOfBatchChildDone emits child-done parent notifications for a
// whole batch AFTER every status write has committed. `completed` is the set of
// children that transitioned non-terminal -> terminal during the batch.
//
// Evaluating the stage barrier per-child inside the batch loop used the
// mid-batch sibling snapshot, so a batch that closed several stages at once
// fired one comment per intermediate stage: the first (stale) comment pinned the
// parent assignee's wake to an already-superseded "advance Stage N+1"
// instruction while the accurate final wake was swallowed by the pending-task
// dedup, and the outcome depended on issue_ids order (MUL-4155). Aggregating
// here makes the result order-independent — each affected parent gets at most
// one comment built from the final state, plus one wake pinned to that comment.
//
// Transient reads or outbox writes are returned to the transition worker so the
// whole group is retried. Permanent guards (deleted/closed/backlog/human parent
// or a barrier that is not closed) consume the event without a comment.
func (h *Handler) notifyParentsOfBatchChildDone(ctx context.Context, completed []db.Issue) error {
	if len(completed) == 0 {
		return nil
	}

	// Group the completed children by parent, preserving first-seen order so the
	// emitted comments (and any test assertions) are deterministic.
	type parentGroup struct {
		parentID pgtype.UUID
		children []db.Issue
	}
	var groups []*parentGroup
	index := map[string]*parentGroup{}
	for _, c := range completed {
		if !c.ParentIssueID.Valid {
			continue
		}
		key := uuidToString(c.ParentIssueID)
		g, ok := index[key]
		if !ok {
			g = &parentGroup{parentID: c.ParentIssueID}
			index[key] = g
			groups = append(groups, g)
		}
		g.children = append(g.children, c)
	}

	for _, g := range groups {
		parent, err := h.Queries.GetIssue(ctx, g.parentID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			slog.Warn("batch child done: failed to load parent",
				"error", err, "parent_id", uuidToString(g.parentID))
			return fmt.Errorf("load child-done parent: %w", err)
		}
		// Same parent guards as the single path (see notifyParentOfChildDone).
		if parent.Status == "done" || parent.Status == "cancelled" {
			continue
		}
		if parent.Status == "backlog" {
			continue
		}
		if parent.AssigneeType.Valid && parent.AssigneeType.String == "member" {
			continue
		}

		children, err := h.Queries.ListChildIssues(ctx, parent.ID)
		if err != nil {
			slog.Warn("batch child done: failed to list siblings for stage barrier",
				"error", err, "parent_id", uuidToString(parent.ID))
			return fmt.Errorf("list child-done siblings: %w", err)
		}

		batch := len(g.children) > 1
		if !siblingsAreStaged(children) {
			// Unstaged: one implicit stage. Fire once iff every child is terminal
			// in the final state. stageBarrierClosed ignores `completed` on the
			// unstaged path, so any completed child stands in for the barrier check.
			if !stageBarrierClosed(children, g.children[0]) {
				continue
			}
			dispatchCandidates := childDoneDispatchCandidates(children, false, 0)
			if err := h.postChildDoneComment(ctx, parent, g.children[0], children, false, 0, batch, dispatchCandidates); err != nil {
				return err
			}
			continue
		}

		// Staged: announce the HIGHEST stage among this batch's completed children
		// whose barrier is closed in the final state. This is what makes the
		// result order-independent — whether the caller sent [stage1, stage2] or
		// [stage2, stage1], the final committed state is identical, so the same
		// top stage wins and stageProgressSummary's "Stage N is next" reflects
		// reality rather than a mid-batch snapshot. A lower closed stage would
		// re-introduce the stale "advance the next stage" instruction the bug was
		// about.
		var rep db.Issue
		var bestStage int32
		found := false
		for _, c := range g.children {
			if !c.Stage.Valid {
				continue // an unstaged child in a staged set closes no stage
			}
			if !stageBarrierClosed(children, c) {
				continue
			}
			if !found || c.Stage.Int32 > bestStage {
				found = true
				bestStage = c.Stage.Int32
				rep = c
			}
		}
		if !found {
			continue
		}
		dispatchCandidates := childDoneDispatchCandidates(children, true, bestStage)
		if err := h.postChildDoneComment(ctx, parent, rep, children, true, bestStage, batch, dispatchCandidates); err != nil {
			return err
		}
	}
	return nil
}

// childDoneDispatchCandidates returns the complete sibling set that owns the
// barrier being announced. Routing provenance must be validated against this
// final-state set, not only the children transitioned by the current request;
// otherwise the last request to close a mixed-origin barrier chooses the squad.
func childDoneDispatchCandidates(children []db.Issue, staged bool, closedStage int32) []db.Issue {
	if !staged {
		return children
	}

	candidates := make([]db.Issue, 0, len(children))
	for _, child := range children {
		if child.Stage.Valid && child.Stage.Int32 == closedStage {
			candidates = append(candidates, child)
		}
	}
	return candidates
}

func barrierContainsStatus(children []db.Issue, staged bool, closedStage int32, status string) bool {
	for _, child := range children {
		if child.Status != status {
			continue
		}
		if !staged || (child.Stage.Valid && child.Stage.Int32 == closedStage) {
			return true
		}
	}
	return false
}

// postChildDoneComment builds and posts the parent's child-done system comment
// for a closed stage barrier, then dispatches the parent-assignee trigger. It
// assumes every guard in notifyParentOfChildDone / notifyParentsOfBatchChildDone
// has already passed and that `completed` is a terminal child whose barrier is
// closed within `children` (the final sibling set).
//
// `completed` is the representative finished child named in the comment.
// `staged`/`closedStage` describe the closed barrier (closedStage is unused for
// an unstaged set). `batch` selects batch-aware wording: a single update keeps
// its historical byte-identical copy, while a batch that finished several
// children at once must not claim "the last sub-issue just finished".
func (h *Handler) postChildDoneComment(ctx context.Context, parent, completed db.Issue, children []db.Issue, staged bool, closedStage int32, batch bool, dispatchCandidates []db.Issue) error {
	prefix := h.getIssuePrefix(ctx, completed.WorkspaceID)
	identifier := prefix + "-" + strconv.Itoa(int(completed.Number))
	childID := uuidToString(completed.ID)
	title := sanitizeChildTitleForSystemComment(completed.Title)
	parentID := uuidToString(parent.ID)

	// An explicit @squad mention starts a leader task without assigning the
	// issue. If that task created this child, its task row is durable proof of
	// the orchestration owner and can carry the stage-complete handoff back to
	// the same squad (GH #5706). A real parent assignee always wins.
	dispatchTarget, err := h.resolveChildDoneDispatchTarget(ctx, parent, dispatchCandidates)
	if err != nil {
		return err
	}

	// Build the dispatch-target mention prefix. Empty when neither a parent
	// assignee nor a proven originating squad context exists.
	mentionPrefix := h.buildParentAssigneeMention(ctx, dispatchTarget.Issue)
	barrierBlocked := barrierContainsStatus(children, staged, closedStage, "blocked")

	var content string
	if staged {
		summary, nextStage := stageProgressSummary(children, closedStage)
		advance := stageAdvanceInstruction(nextStage, parentID)
		if batch && barrierBlocked {
			content = fmt.Sprintf(
				"%sStage %d of this issue reached a barrier — its sub-issues reached terminal states together in a batch update, most recently [%s](mention://issue/%s) — \"%s\". Stage progress — %s. Resolve the blocked work before advancing.%s",
				mentionPrefix, closedStage, identifier, childID, title, summary, advance,
			)
		} else if batch {
			content = fmt.Sprintf(
				"%sStage %d of this issue is complete — its sub-issues just finished together in a batch update, most recently [%s](mention://issue/%s) — \"%s\". Stage progress — %s.%s",
				mentionPrefix, closedStage, identifier, childID, title, summary, advance,
			)
		} else if completed.Status == "blocked" {
			content = fmt.Sprintf(
				"%sStage %d of this issue reached a barrier — its last sub-issue [%s](mention://issue/%s) — \"%s\" — became blocked. Stage progress — %s. Resolve the blocker before advancing.%s",
				mentionPrefix, closedStage, identifier, childID, title, summary, advance,
			)
		} else {
			content = fmt.Sprintf(
				"%sStage %d of this issue is complete — its last sub-issue [%s](mention://issue/%s) — \"%s\" — just finished. Stage progress — %s.%s",
				mentionPrefix, closedStage, identifier, childID, title, summary, advance,
			)
		}
	} else {
		if batch && barrierBlocked {
			content = fmt.Sprintf(
				"%sAll sub-issues reached terminal states together in a batch update, most recently [%s](mention://issue/%s) — \"%s\". Resolve the blocked work before advancing the parent.",
				mentionPrefix, identifier, childID, title,
			)
		} else if batch {
			content = fmt.Sprintf(
				"%sAll sub-issues are complete — they just finished together in a batch update, most recently [%s](mention://issue/%s) — \"%s\". Continue the parent: synthesize the children's results and move it forward, or — if nothing remains — run `multica issue status %s in_review` to mark the parent ready for review.",
				mentionPrefix, identifier, childID, title, parentID,
			)
		} else if completed.Status == "blocked" {
			content = fmt.Sprintf(
				"%sAll sub-issues reached terminal states — the last one, [%s](mention://issue/%s) — \"%s\", became blocked. Resolve the blocker before advancing the parent.",
				mentionPrefix, identifier, childID, title,
			)
		} else {
			content = fmt.Sprintf(
				"%sAll sub-issues are complete — the last one, [%s](mention://issue/%s) — \"%s\", just finished. Continue the parent: synthesize the children's results and move it forward, or — if nothing remains — run `multica issue status %s in_review` to mark the parent ready for review.",
				mentionPrefix, identifier, childID, title, parentID,
			)
		}
	}

	targetType := dispatchTarget.Issue.AssigneeType
	targetID := dispatchTarget.Issue.AssigneeID
	var originTaskID pgtype.UUID
	if dispatchTarget.OriginTask != nil {
		originTaskID = dispatchTarget.OriginTask.ID
	}
	outcome := "complete"
	if barrierBlocked {
		outcome = "blocked"
	}
	barrierName := "implicit"
	if staged {
		barrierName = "stage:" + strconv.Itoa(int(closedStage))
	}
	generation, err := h.Queries.GetChildDoneBarrierGeneration(ctx, db.GetChildDoneBarrierGenerationParams{
		ParentIssueID: parent.ID,
		Staged:        staged,
		Stage:         pgtype.Int4{Int32: closedStage, Valid: staged},
	})
	if err != nil {
		return fmt.Errorf("load child-done barrier generation: %w", err)
	}
	if !generation.Valid {
		// The triggering transition is normally present. Keep the fallback for
		// pre-migration terminal siblings and tests that call this helper with
		// synthetic issues.
		generation = completed.UpdatedAt
	}
	barrierKey := fmt.Sprintf("%s:%s:%s", barrierName, outcome, generation.Time.UTC().Format(time.RFC3339Nano))

	// The comment doubles as a durable outbox row. A zero-row return means the
	// same closed barrier generation was already persisted by another group or
	// replica.
	comment, err := h.Queries.CreateChildDoneDispatchComment(ctx, db.CreateChildDoneDispatchCommentParams{
		IssueID:               parent.ID,
		WorkspaceID:           parent.WorkspaceID,
		AuthorID:              pgtype.UUID{Valid: true},
		Content:               content,
		ChildDoneBarrierKey:   pgtype.Text{String: barrierKey, Valid: true},
		ChildDoneTargetType:   targetType,
		ChildDoneTargetID:     targetID,
		ChildDoneOriginTaskID: originTaskID,
	})
	created := true
	if errors.Is(err, pgx.ErrNoRows) {
		created = false
		comment, err = h.Queries.GetChildDoneDispatchByBarrier(ctx, db.GetChildDoneDispatchByBarrierParams{
			IssueID:             parent.ID,
			ChildDoneBarrierKey: pgtype.Text{String: barrierKey, Valid: true},
		})
	}
	if err != nil {
		slog.Warn("child done: persist dispatch comment failed",
			"error", err,
			"child_id", childID,
			"parent_id", uuidToString(parent.ID))
		return fmt.Errorf("persist child-done dispatch comment: %w", err)
	}

	if created {
		h.publish(protocol.EventCommentCreated, uuidToString(parent.WorkspaceID), "system", "", map[string]any{
			"comment":             commentToResponse(comment, nil, nil),
			"issue_title":         parent.Title,
			"issue_assignee_type": textToPtr(parent.AssigneeType),
			"issue_assignee_id":   uuidToPtr(parent.AssigneeID),
			"issue_status":        parent.Status,
		})
	}
	if h.ChildDoneDispatchWorker != nil {
		worked, dispatchErr := h.ChildDoneDispatchWorker.ProcessID(ctx, comment.ID)
		if dispatchErr != nil {
			slog.Warn("child done: immediate dispatch deferred to durable worker",
				"error", dispatchErr,
				"comment_id", uuidToString(comment.ID),
				"parent_id", uuidToString(parent.ID))
		}
		if !worked || dispatchErr != nil {
			h.ChildDoneDispatchWorker.Notify()
		}
	}
	return nil
}

type childDoneDispatchTarget struct {
	Issue      db.Issue
	OriginTask *db.AgentTaskQueue
}

// resolveChildDoneDispatchTarget returns the durable owner and attribution
// source of a child-done handoff without mutating the parent issue's assignment.
//
// Assigned parents keep their explicit assignee. For an unassigned parent, an
// agent-created child's origin_id names the exact task that created it. Every
// child participating in one batch handoff must name the same task; otherwise
// request order would decide which squad and human authority receive the wake.
// The task's squad_id is the fallback routing target only when it is a leader
// task on this parent by every child creator.
func (h *Handler) resolveChildDoneDispatchTarget(ctx context.Context, parent db.Issue, completed []db.Issue) (childDoneDispatchTarget, error) {
	target := childDoneDispatchTarget{Issue: parent}
	if parent.AssigneeType.Valid || parent.AssigneeID.Valid {
		return target, nil
	}
	if len(completed) == 0 {
		return target, nil
	}

	originTaskID := completed[0].OriginID
	for _, child := range completed {
		if child.CreatorType != "agent" || !child.CreatorID.Valid ||
			!child.OriginType.Valid || child.OriginType.String != "agent_create" || !child.OriginID.Valid {
			return target, nil
		}
		if uuidToString(child.OriginID) != uuidToString(originTaskID) {
			slog.Warn("child done: batch has ambiguous squad orchestration provenance",
				"child_id", uuidToString(child.ID),
				"parent_id", uuidToString(parent.ID),
				"first_origin_task_id", uuidToString(originTaskID),
				"conflicting_origin_task_id", uuidToString(child.OriginID))
			return target, nil
		}
	}

	originTask, err := h.Queries.GetAgentTask(ctx, originTaskID)
	if err != nil {
		slog.Warn("child done: failed to load exact origin task",
			"error", err,
			"child_id", uuidToString(completed[0].ID),
			"parent_id", uuidToString(parent.ID),
			"origin_task_id", uuidToString(originTaskID))
		if errors.Is(err, pgx.ErrNoRows) {
			return target, nil
		}
		return target, fmt.Errorf("load child-done origin task: %w", err)
	}
	if uuidToString(originTask.IssueID) != uuidToString(parent.ID) || !originTask.IsLeaderTask || !originTask.SquadID.Valid {
		slog.Warn("child done: exact origin task is not valid squad orchestration context",
			"child_id", uuidToString(completed[0].ID),
			"parent_id", uuidToString(parent.ID),
			"origin_task_id", uuidToString(originTask.ID))
		return target, nil
	}
	for _, child := range completed {
		if uuidToString(originTask.AgentID) != uuidToString(child.CreatorID) {
			slog.Warn("child done: exact origin task creator does not match batch child",
				"child_id", uuidToString(child.ID),
				"parent_id", uuidToString(parent.ID),
				"origin_task_id", uuidToString(originTask.ID))
			return target, nil
		}
	}

	target.Issue.AssigneeType = pgtype.Text{String: "squad", Valid: true}
	target.Issue.AssigneeID = originTask.SquadID
	target.OriginTask = &originTask
	return target, nil
}

// isTerminalChildStatus reports whether a child issue status counts as
// "finished" for stage-barrier purposes. Blocked wakes the coordinator to
// resolve the blocker; cancelled work will never complete and must not hold a
// stage open.
func isTerminalChildStatus(status string) bool {
	return status == "done" || status == "blocked" || status == "cancelled"
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
	type agg struct {
		total, terminal, done, blocked, cancelled int
	}
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
			a.terminal++
		}
		switch c.Status {
		case "done":
			a.done++
		case "blocked":
			a.blocked++
		case "cancelled":
			a.cancelled++
		}
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	parts := make([]string, 0, len(order))
	for _, s := range order {
		a := byStage[s]
		label := ""
		if a.blocked == 0 {
			// Preserve the historical presentation for done/cancelled stages.
			// Blocked is the only newly terminal status and needs an explicit
			// breakdown so the coordinator cannot mistake it for completed work.
			label = fmt.Sprintf("Stage %d: %d/%d done", s, a.terminal, a.total)
		} else {
			label = fmt.Sprintf(
				"Stage %d: %d/%d terminal (%d done, %d blocked, %d cancelled)",
				s, a.terminal, a.total, a.done, a.blocked, a.cancelled,
			)
		}
		if nextStage == 0 && s > closedStage && a.terminal < a.total {
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
	return fmt.Sprintf(" Completing this stage does not mean the whole issue is done. Decide whether the issue is actually complete — if so, synthesize the results and run `multica issue status %s in_review` to mark the parent ready for review — or whether the next stage still needs to be created, in which case create that stage and its sub-issues now.", parentID)
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
func (h *Handler) dispatchParentAssigneeTrigger(ctx context.Context, target childDoneDispatchTarget, systemComment db.Comment) error {
	parent := target.Issue
	if !parent.AssigneeType.Valid || !parent.AssigneeID.Valid {
		return nil
	}

	switch parent.AssigneeType.String {
	case "agent":
		return h.triggerChildDoneAgent(ctx, parent, systemComment.ID)
	case "squad":
		return h.triggerChildDoneSquad(ctx, parent, systemComment.ID, target.OriginTask)
	}
	return nil
}

var errChildDoneDispatchSkipped = errors.New("child-done dispatch target is permanently unavailable")

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
func (h *Handler) triggerChildDoneAgent(ctx context.Context, parent db.Issue, triggerCommentID pgtype.UUID) error {
	agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
		ID:          parent.AssigneeID,
		WorkspaceID: parent.WorkspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return errChildDoneDispatchSkipped
	}
	if err != nil {
		return fmt.Errorf("load child-done agent target: %w", err)
	}
	if !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
		return errChildDoneDispatchSkipped
	}

	hasPending, err := h.Queries.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
		IssueID: parent.ID,
		AgentID: parent.AssigneeID,
		// Key dedup on the reviewed head (TEN-356).
		HeadSha: h.TaskService.ResolveIssueReviewSHAParam(ctx, parent.ID),
	})
	if err != nil || hasPending {
		return err
	}

	if _, err := h.TaskService.EnqueueTaskForChildDoneAgent(ctx, parent, parent.AssigneeID, triggerCommentID); err != nil {
		return err
	}
	return nil
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
//   - Assigned squad parents need NO leader-invocation gate. Waking the parent's
//     OWN squad leader on child-done is a coordination handoff on an issue the
//     leader already owns, not a fresh invocation; permission was enforced when
//     the parent was assigned (MUL-4063 / GH #4928).
//   - Unassigned parents recovered through originTask are different: the task
//     proves provenance but does not grant permanent authority. The original
//     human must still be allowed to invoke the squad's current leader, so a
//     permission revocation or leader rotation takes effect before a new task
//     is created.
//
// Re-triggering is bounded by the HasPendingTaskForIssueAndAgent idempotency
// check below, exactly as the agent path relies on it.
func (h *Handler) triggerChildDoneSquad(ctx context.Context, parent db.Issue, triggerCommentID pgtype.UUID, originTask *db.AgentTaskQueue) error {
	const maxAttempts = 3

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
			ID:          parent.AssigneeID,
			WorkspaceID: parent.WorkspaceID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return errChildDoneDispatchSkipped
		}
		if err != nil {
			lastErr = fmt.Errorf("load child-done squad target: %w", err)
			continue
		}
		if squad.ArchivedAt.Valid {
			return errChildDoneDispatchSkipped
		}

		agent, err := h.Queries.GetAgent(ctx, squad.LeaderID)
		if errors.Is(err, pgx.ErrNoRows) {
			return errChildDoneDispatchSkipped
		}
		if err != nil {
			lastErr = fmt.Errorf("load child-done squad leader: %w", err)
			continue
		}
		if !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
			return errChildDoneDispatchSkipped
		}
		if originTask != nil && !h.canInvokeAgent(
			ctx,
			agent,
			"agent",
			uuidToString(originTask.AgentID),
			uuidToString(originTask.OriginatorUserID),
			uuidToString(parent.WorkspaceID),
		) {
			slog.Debug("child done: originator cannot invoke current squad leader",
				"parent_id", uuidToString(parent.ID),
				"squad_id", uuidToString(squad.ID),
				"leader_id", uuidToString(squad.LeaderID))
			return errChildDoneDispatchSkipped
		}

		hasPending, err := h.Queries.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
			IssueID: parent.ID,
			AgentID: squad.LeaderID,
			// Key dedup on the reviewed head (TEN-356).
			HeadSha: h.TaskService.ResolveIssueReviewSHAParam(ctx, parent.ID),
		})
		if err != nil {
			lastErr = err
			continue
		}
		if hasPending {
			return nil
		}

		if originTask != nil {
			_, err = h.TaskService.EnqueueTaskForSquadLeaderFromOriginTask(ctx, parent, squad.LeaderID, squad.ID, triggerCommentID, *originTask)
		} else {
			_, err = h.TaskService.EnqueueTaskForChildDoneSquadLeader(ctx, parent, squad.LeaderID, squad.ID, triggerCommentID)
		}
		if err == nil {
			return nil
		}
		if errors.Is(err, service.ErrIssueAssignedForTask) {
			return err
		}
		lastErr = err
		slog.Warn("child done: retrying parent squad leader enqueue",
			"error", err,
			"attempt", attempt,
			"parent_id", uuidToString(parent.ID),
			"squad_id", uuidToString(squad.ID),
			"leader_id", uuidToString(squad.LeaderID))
	}
	return lastErr
}
