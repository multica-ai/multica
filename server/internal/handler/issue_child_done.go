package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
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
	// Both sides of the transition are resolved to the canonical status they
	// inherit, so a move into a custom done/cancelled status fires the barrier
	// exactly like a move into Done or Cancelled. (MUL-6243)
	effective := h.childStatusResolver(ctx)
	prevStatus, err := effective(prev)
	if err != nil {
		slog.Warn("child done: failed to resolve previous child status", "error", err, "child_id", uuidToString(issue.ID))
		return
	}
	nowStatus, err := effective(issue)
	if err != nil {
		slog.Warn("child done: failed to resolve child status", "error", err, "child_id", uuidToString(issue.ID))
		return
	}
	prevTerminal := isTerminalChildStatus(prevStatus)
	nowTerminal := isTerminalChildStatus(nowStatus)
	if prevTerminal || !nowTerminal {
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
	// Custom statuses inherit the canonical status they name, so a custom
	// terminal status closes this out and a custom backlog status parks it,
	// exactly like Done/Cancelled and Backlog do. (MUL-6243)
	parentStatus, err := effective(parent)
	if err != nil {
		slog.Warn("child done: failed to resolve parent status", "error", err, "parent_id", uuidToString(parent.ID))
		return
	}
	if parentStatus == "done" || parentStatus == "cancelled" {
		return
	}
	// A parent parked in backlog is deliberately held for later. Posting the
	// system comment would wake its assignee, and the woken agent can then
	// promote sibling backlog sub-issues into todo — the surprise auto-
	// activation reported in #4320 / MUL-3497. Skip the whole notification so
	// a backlog parent stays inert until the user explicitly promotes it.
	if parentStatus == "backlog" {
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
	isTerminal, err := resolveTerminalChildren(children, effective)
	if err != nil {
		slog.Warn("child done: failed to resolve sibling statuses", "error", err, "parent_id", uuidToString(parent.ID))
		return
	}
	if len(currentStageCompletions(children, []db.Issue{issue}, isTerminal)) == 0 || !stageBarrierClosed(children, issue, isTerminal) {
		return
	}
	staged := siblingsAreStaged(children)
	// When the set is staged and the barrier closed, the completed child is
	// guaranteed to carry a stage (stageBarrierClosed returns false for an
	// unstaged completed child in a staged set), so issue.Stage.Int32 is safe.
	var closedStage int32
	if staged {
		closedStage = issue.Stage.Int32
	}
	h.postChildDoneComment(ctx, parent, issue, children, staged, closedStage, false, isTerminal)
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
// Best-effort, mirroring notifyParentOfChildDone: a failure on one parent is
// logged and skipped; it never rolls back the committed batch.
func (h *Handler) notifyParentsOfBatchChildDone(ctx context.Context, completed []db.Issue) {
	if len(completed) == 0 {
		return
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

	effective := h.childStatusResolver(ctx)
	for _, g := range groups {
		parent, err := h.Queries.GetIssue(ctx, g.parentID)
		if err != nil {
			slog.Warn("batch child done: failed to load parent",
				"error", err, "parent_id", uuidToString(g.parentID))
			continue
		}
		// Same parent guards as the single path (see notifyParentOfChildDone).
		parentStatus, err := effective(parent)
		if err != nil {
			slog.Warn("batch child done: failed to resolve parent status", "error", err, "parent_id", uuidToString(parent.ID))
			continue
		}
		if parentStatus == "done" || parentStatus == "cancelled" {
			continue
		}
		if parentStatus == "backlog" {
			continue
		}
		if parent.AssigneeType.Valid && parent.AssigneeType.String == "member" {
			continue
		}

		children, err := h.Queries.ListChildIssues(ctx, parent.ID)
		if err != nil {
			slog.Warn("batch child done: failed to list siblings for stage barrier",
				"error", err, "parent_id", uuidToString(parent.ID))
			continue
		}

		isTerminal, err := resolveTerminalChildren(children, effective)
		if err != nil {
			slog.Warn("batch child done: failed to resolve sibling statuses", "error", err, "parent_id", uuidToString(parent.ID))
			continue
		}
		batch := len(g.children) > 1
		current := currentStageCompletions(children, g.children, isTerminal)
		if len(current) == 0 {
			continue
		}
		if !siblingsAreStaged(children) {
			// Unstaged: one implicit stage. Fire once iff every child is terminal
			// in the final state. stageBarrierClosed ignores `completed` on the
			// unstaged path, so any completed child stands in for the barrier check.
			if !stageBarrierClosed(children, current[0], isTerminal) {
				continue
			}
			h.postChildDoneComment(ctx, parent, current[0], children, false, 0, batch, isTerminal)
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
		rep, found := highestClosedBatchStage(children, current, isTerminal)
		if !found {
			continue
		}
		h.postChildDoneComment(ctx, parent, rep, children, true, rep.Stage.Int32, batch, isTerminal)
	}
}

// highestClosedBatchStage selects the first completed child in the highest
// closed stage of a staged sibling set. The terminal predicate must come from
// resolveTerminalChildren: all required statuses must be known before selection.
// A stage S is closed iff no non-terminal staged sibling has stage <= S, so
// finding the earliest open stage once reduces selection from O(N*K) to O(N+K).
func highestClosedBatchStage(children, completed []db.Issue, isTerminal func(db.Issue) bool) (db.Issue, bool) {
	var lowestCompleted pgtype.Int4
	for _, c := range completed {
		if c.Stage.Valid && (!lowestCompleted.Valid || c.Stage.Int32 < lowestCompleted.Int32) {
			lowestCompleted = c.Stage
		}
	}
	if !lowestCompleted.Valid {
		return db.Issue{}, false
	}
	var firstOpen pgtype.Int4
	for _, c := range children {
		if !c.Stage.Valid {
			continue // Unstaged siblings were not resolved and cannot block a stage.
		}
		if !isTerminal(c) {
			if c.Stage.Int32 <= lowestCompleted.Int32 {
				return db.Issue{}, false // This sibling blocks every candidate; do not scan the rest.
			}
			if !firstOpen.Valid || c.Stage.Int32 < firstOpen.Int32 {
				firstOpen = c.Stage
			}
		}
	}
	var rep db.Issue
	found := false
	for _, c := range completed {
		if !c.Stage.Valid || (firstOpen.Valid && c.Stage.Int32 >= firstOpen.Int32) {
			continue
		}
		if !found || c.Stage.Int32 > rep.Stage.Int32 {
			found = true
			rep = c
		}
	}
	return rep, found
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
func (h *Handler) postChildDoneComment(ctx context.Context, parent, completed db.Issue, children []db.Issue, staged bool, closedStage int32, batch bool, isTerminal func(db.Issue) bool) {
	prefix := h.getIssuePrefix(ctx, completed.WorkspaceID)
	identifier := prefix + "-" + strconv.Itoa(int(completed.Number))
	childID := uuidToString(completed.ID)
	title := sanitizeChildTitleForSystemComment(completed.Title)
	parentID := uuidToString(parent.ID)

	// Build the parent-assignee mention prefix. Empty when the parent has no
	// assignee or the assignee row is missing (deleted member, archived
	// agent the workspace lost track of, etc.).
	mentionPrefix := h.buildParentAssigneeMention(ctx, parent)

	var content string
	if staged {
		summary, nextStage := stageProgressSummary(children, closedStage, isTerminal)
		advance := stageAdvanceInstruction(nextStage, parentID)
		if batch {
			content = fmt.Sprintf(
				"%sStage %d of this issue is complete — its sub-issues just finished together in a batch update, most recently [%s](mention://issue/%s) — \"%s\". Stage progress — %s.%s",
				mentionPrefix, closedStage, identifier, childID, title, summary, advance,
			)
		} else {
			content = fmt.Sprintf(
				"%sStage %d of this issue is complete — its last sub-issue [%s](mention://issue/%s) — \"%s\" — just finished. Stage progress — %s.%s",
				mentionPrefix, closedStage, identifier, childID, title, summary, advance,
			)
		}
	} else {
		if batch {
			content = fmt.Sprintf(
				"%sAll sub-issues are complete — they just finished together in a batch update, most recently [%s](mention://issue/%s) — \"%s\". Continue the parent: synthesize the children's results and move it forward, or — if nothing remains — run `multica issue status %s in_review` to mark the parent ready for review.",
				mentionPrefix, identifier, childID, title, parentID,
			)
		} else {
			content = fmt.Sprintf(
				"%sAll sub-issues are complete — the last one, [%s](mention://issue/%s) — \"%s\", just finished. Continue the parent: synthesize the children's results and move it forward, or — if nothing remains — run `multica issue status %s in_review` to mark the parent ready for review.",
				mentionPrefix, identifier, childID, title, parentID,
			)
		}
	}

	// author_type='system', author_id=zero UUID. The zero UUID is a valid 16
	// byte value and the column is NOT NULL; frontend code should branch on
	// author_type === 'system' rather than on the UUID value.
	created, err := h.Queries.CreateComment(ctx, db.CreateCommentParams{
		ID:          childDoneNotificationID(parent, children, staged, closedStage),
		IssueID:     parent.ID,
		WorkspaceID: parent.WorkspaceID,
		AuthorType:  "system",
		AuthorID:    pgtype.UUID{Valid: true},
		Content:     content,
		Type:        "system",
		ParentID:    pgtype.UUID{Valid: false},
	})
	if err != nil {
		// CreateComment touches the issue and inserts the comment atomically.
		// A concurrent/late publisher of this same closure loses the primary-key
		// insert, rolls back the touch, and must neither publish nor dispatch.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "comment_pkey" {
			return
		}
		slog.Warn("child done: create system comment failed",
			"error", err,
			"child_id", childID,
			"parent_id", uuidToString(parent.ID))
		return
	}
	comment := created.Comment()

	h.publish(protocol.EventCommentCreated, uuidToString(parent.WorkspaceID), "system", "", map[string]any{
		"comment":             commentToResponse(comment, nil, nil),
		"issue_title":         parent.Title,
		"issue_assignee_type": textToPtr(parent.AssigneeType),
		"issue_assignee_id":   uuidToPtr(parent.AssigneeID),
		"issue_status":        parent.Status,
		"issue_revision":      created.IssueRevision,
	})

	// Dispatch the explicit trigger / inbox row for the parent assignee.
	// Listener-level mention parsing is intentionally NOT involved (the
	// notification + subscriber listeners both short-circuit on
	// author_type='system'); this keeps smuggled mentions from the child
	// title inert and gives the platform a single place to apply the loop
	// and idempotency guards.
	h.dispatchParentAssigneeTrigger(ctx, parent, comment)
}

// currentStageCompletions rejects delayed callbacks after a child was
// reopened or moved. Indexing the sibling snapshot keeps a batch linear.
func currentStageCompletions(children, completed []db.Issue, isTerminal func(db.Issue) bool) []db.Issue {
	byID := make(map[pgtype.UUID]db.Issue, len(children))
	for _, child := range children {
		byID[child.ID] = child
	}
	current := make([]db.Issue, 0, len(completed))
	for _, candidate := range completed {
		child, ok := byID[candidate.ID]
		if ok && child.ParentIssueID == candidate.ParentIssueID && child.Stage == candidate.Stage &&
			child.StageGeneration == candidate.StageGeneration && isTerminal(child) {
			current = append(current, child)
		}
	}
	return current
}

// childDoneNotificationID identifies one completion of one stage membership.
// The representative child, batch wording, ordinary issue revision, and later
// stages do not identify the event. Reopening or moving a participant advances
// its persisted generation, so completing the new membership can notify again.
// Sorting makes independent and batch requests agree regardless of row order.
// The comment's primary key is the atomic claim: only its creator dispatches.
// This is intentionally an idempotency-derived UUID, not a fresh row identity.
func childDoneNotificationID(parent db.Issue, children []db.Issue, staged bool, closedStage int32) pgtype.UUID {
	members := make([]string, 0, len(children))
	for _, child := range children {
		if staged && (!child.Stage.Valid || child.Stage.Int32 != closedStage) {
			continue
		}
		members = append(members, fmt.Sprintf("%s:%d", uuidToString(child.ID), child.StageGeneration))
	}
	sort.Strings(members)
	key := fmt.Sprintf("multica/child-done/v1/%s/%s/%t/%d/%s",
		uuidToString(parent.WorkspaceID), uuidToString(parent.ID), staged, closedStage, strings.Join(members, ","))
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte(key))
	return pgtype.UUID{Bytes: [16]byte(id), Valid: true}
}

// isTerminalChildStatus reports whether a child issue status counts as
// "finished" for stage-barrier purposes. Cancelled counts as terminal: a
// cancelled sibling will never complete, so it must not hold a stage open.
//
// Takes a CANONICAL status. Callers that hold a raw `issue.status` must pass it
// through childStatusResolver first, so a custom status in the done or
// cancelled category closes a stage exactly like Done and Cancelled do.
func isTerminalChildStatus(status string) bool {
	return status == "done" || status == "cancelled"
}

// childStatusResolver shares each workspace's catalog across the guards,
// sibling scans and progress summary of one completion notification pass.
// It must not outlive that pass: later notifications need a fresh catalog.
//
// Resolve without rewriting the issue rows, which also supply the original
// user-selected status to downstream rendering. Built-in keys need no I/O.
// Unlike display-oriented Resolver callers, this side-effecting path must
// reject unresolved custom keys: parent or sibling rows can be newer than the
// catalog snapshot. A miss must not bypass a parked/terminal parent's guard.
func (h *Handler) childStatusResolver(ctx context.Context) func(db.Issue) (string, error) {
	resolvers := make(map[pgtype.UUID]*issuestatus.Resolver)
	return func(c db.Issue) (string, error) {
		if issuestatus.IsBuiltIn(c.Status) {
			return c.Status, nil
		}
		resolver := resolvers[c.WorkspaceID]
		if resolver == nil {
			resolver = issuestatus.NewResolver(c.WorkspaceID)
			resolvers[c.WorkspaceID] = resolver
		}
		status := resolver.Effective(ctx, h.issueStatusCatalog(), c.Status)
		if err := resolver.Err(); err != nil {
			return "", err
		}
		if !issuestatus.IsCategory(status) {
			return "", fmt.Errorf("unresolved status %q in workspace %s", c.Status, uuidToString(c.WorkspaceID))
		}
		return status, nil
	}
}

// resolveTerminalChildren checks every status needed by the stage barrier and
// progress summary before either can produce a notification. The returned
// predicate only reads this snapshot, so a late catalog failure cannot be
// hidden by the bool-only stage helpers. Stored status keys stay untouched.
func resolveTerminalChildren(children []db.Issue, effective func(db.Issue) (string, error)) (func(db.Issue) bool, error) {
	terminal := make(map[pgtype.UUID]bool, len(children))
	staged := siblingsAreStaged(children)
	for _, child := range children {
		if staged && !child.Stage.Valid {
			continue // Neither stage helper considers unstaged siblings.
		}
		status, err := effective(child)
		if err != nil {
			return nil, fmt.Errorf("resolve child %s status %q: %w", uuidToString(child.ID), child.Status, err)
		}
		terminal[child.ID] = isTerminalChildStatus(status)
	}
	return func(child db.Issue) bool { return terminal[child.ID] }, nil
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
func stageBarrierClosed(children []db.Issue, completed db.Issue, isTerminal func(db.Issue) bool) bool {
	if !siblingsAreStaged(children) {
		for _, c := range children {
			if !isTerminal(c) {
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
		if c.Stage.Int32 <= s && !isTerminal(c) {
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
func stageProgressSummary(children []db.Issue, closedStage int32, isTerminal func(db.Issue) bool) (summary string, nextStage int32) {
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
		if isTerminal(c) {
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
