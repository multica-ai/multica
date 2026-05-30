package handler

// CEREBRO-PATCH(orchestration-cerebro): FIR-2564 — server-side trigger for the
// cerebro orchestration layer. This whole file is cerebro-only; it is wired
// into upstream code from exactly two marked call sites (AttachLabel and the
// child-done path in UpdateIssue), documented in docs/cerebro-patches.md.
//
// Behavior: attaching the `orchestrate` label to a parent issue makes the
// platform drive that issue's sub-issues automatically. The sub-issues are the
// tasks; their `blocks` edges are the dependencies. On label attach we start
// every child whose blockers are already terminal; each time a child finishes,
// we start the children it just unblocked. Jesper works in the UI, not the
// CLI — setting the label IS the run command, and removing it stops further
// auto-starts. A workspace-level `cerebro_orchestrate=false` flag override is
// the kill-switch.

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/orchestration"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const orchestrateLabelName = "orchestrate"

// cerebroOrchestrateEnabled is the workspace-level kill-switch for FIR-2564.
// Default-on: a missing override row (or any lookup failure) resolves to
// enabled, matching the registry.ts default. Only an explicit workspace-level
// override of `false` (user_id = zero UUID) disables the trigger without a
// redeploy.
func (h *Handler) cerebroOrchestrateEnabled(ctx context.Context, workspaceID pgtype.UUID) bool {
	if h.CerebroQueries == nil {
		return true
	}
	on, err := h.CerebroQueries.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
		WorkspaceID: workspaceID,
		UserID:      pgtype.UUID{Valid: true}, // zero UUID = workspace-level override row
		FlagKey:     "cerebro_orchestrate",
	})
	if errors.Is(err, pgx.ErrNoRows) || err != nil {
		return true
	}
	return on
}

// maybeStartOrchestrationOnLabel is the entrypoint from AttachLabel. It is a
// no-op unless the just-attached label is named `orchestrate` and the feature
// is enabled for the workspace. Best-effort: failures are logged, never
// surfaced to the label-attach response (the label attach already succeeded).
func (h *Handler) maybeStartOrchestrationOnLabel(ctx context.Context, issue db.Issue, labelID pgtype.UUID) {
	label, err := h.Queries.GetLabel(ctx, db.GetLabelParams{ID: labelID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(label.Name), orchestrateLabelName) {
		return
	}
	if !h.cerebroOrchestrateEnabled(ctx, issue.WorkspaceID) {
		slog.Info("orchestration: skipped, disabled for workspace", "issue_id", uuidToString(issue.ID))
		return
	}
	h.runOrchestration(ctx, issue, true)
}

// advanceOrchestrationOnChildDone is the entrypoint from the UpdateIssue
// child-done path. When a child of an orchestrated parent reaches `done`, it
// may have unblocked siblings — start whichever are now ready. The parent is
// "orchestrated" iff it currently carries the `orchestrate` label.
func (h *Handler) advanceOrchestrationOnChildDone(ctx context.Context, prev, issue db.Issue) {
	if !issue.ParentIssueID.Valid {
		return
	}
	if prev.Status == "done" || issue.Status != "done" {
		return
	}
	parent, err := h.Queries.GetIssue(ctx, issue.ParentIssueID)
	if err != nil {
		return
	}
	if parent.Status == "done" || parent.Status == "cancelled" {
		return
	}
	if !h.issueHasOrchestrateLabel(ctx, parent) {
		return
	}
	if !h.cerebroOrchestrateEnabled(ctx, parent.WorkspaceID) {
		return
	}
	h.runOrchestration(ctx, parent, false)
}

// issueHasOrchestrateLabel reports whether the issue currently carries the
// `orchestrate` label.
func (h *Handler) issueHasOrchestrateLabel(ctx context.Context, issue db.Issue) bool {
	labels, err := h.Queries.ListLabelsByIssue(ctx, db.ListLabelsByIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return false
	}
	for _, l := range labels {
		if strings.EqualFold(strings.TrimSpace(l.Name), orchestrateLabelName) {
			return true
		}
	}
	return false
}

// runOrchestration loads the parent's children + their blocker edges, validates
// the dependency graph, and starts every child that is ready now. When
// postSummary is true (the label-attach entrypoint) it also posts a system
// comment describing the wave plan and what it started — so a UI-only user sees
// the engine working. The child-done entrypoint passes postSummary=false to
// keep step-by-step advancement quiet (one comment per finished child would be
// noise); it still posts a short note when it actually starts something.
func (h *Handler) runOrchestration(ctx context.Context, parent db.Issue, postSummary bool) {
	childIssues, err := h.Queries.ListChildIssues(ctx, parent.ID)
	if err != nil {
		slog.Warn("orchestration: list children failed", "parent_id", uuidToString(parent.ID), "error", err)
		return
	}

	issueByID := map[string]db.Issue{}
	childIDs := make([]pgtype.UUID, 0, len(childIssues))
	children := make([]orchestration.ChildState, 0, len(childIssues))
	for _, c := range childIssues {
		id := uuidToString(c.ID)
		issueByID[orchestration.NormalizeID(id)] = c
		childIDs = append(childIDs, c.ID)
		children = append(children, orchestration.ChildState{
			ID:     id,
			Number: c.Number,
			Title:  c.Title,
			Status: c.Status,
		})
	}

	if len(children) == 0 {
		if postSummary {
			h.postOrchestrationComment(ctx, parent,
				"Orchestration label set, but this issue has no sub-issues to drive. "+
					"Add sub-issues and `blocks` dependencies between them, then re-apply the label.")
		}
		return
	}

	blockersByChild := h.loadBlockers(ctx, parent.WorkspaceID, childIDs)
	plan := orchestration.PlanFromChildren(parent.Title, children, blockersByChild)

	if orchestration.DetectCycle(plan.Nodes) {
		if postSummary {
			h.postOrchestrationComment(ctx, parent,
				"Cannot orchestrate: the sub-issues have a circular `blocks` dependency "+
					"(some sub-issue waits on one that waits back on it). Fix the dependency "+
					"loop and re-apply the `orchestrate` label.")
		}
		return
	}

	ready := orchestration.ReadyToStart(children, blockersByChild)
	started := []string{}
	for _, rc := range ready {
		issue, ok := issueByID[orchestration.NormalizeID(rc.ID)]
		if !ok {
			continue
		}
		if h.startOrchestratedChild(ctx, issue) {
			started = append(started, "#"+strconv.Itoa(int(issue.Number)))
		}
	}

	if postSummary {
		waves := orchestration.RenderWaves(children, plan)
		var b strings.Builder
		b.WriteString("Orchestration started for this issue's sub-issues.\n\n")
		b.WriteString("Plan (waves run top to bottom; everything in a wave can run at once):\n")
		for _, line := range waves {
			b.WriteString("- " + line + "\n")
		}
		if len(started) > 0 {
			b.WriteString("\nStarted now: " + strings.Join(started, ", ") + ".")
		} else {
			b.WriteString("\nNothing started yet — the first wave's sub-issues are either not agent-assigned or already running.")
		}
		h.postOrchestrationComment(ctx, parent, b.String())
	} else if len(started) > 0 {
		h.postOrchestrationComment(ctx, parent,
			"Orchestration advanced: started "+strings.Join(started, ", ")+" now that their dependencies are done.")
	}
}

// loadBlockers returns, per child issue id (string), the issues that block it.
func (h *Handler) loadBlockers(ctx context.Context, workspaceID pgtype.UUID, childIDs []pgtype.UUID) map[string][]orchestration.BlockerState {
	out := map[string][]orchestration.BlockerState{}
	if len(childIDs) == 0 {
		return out
	}
	rows, err := h.Queries.ListBlockedByForIssues(ctx, db.ListBlockedByForIssuesParams{
		IssueIds:    childIDs,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		slog.Warn("orchestration: list blockers failed", "error", err)
		return out
	}
	for _, row := range rows {
		blocked := uuidToString(row.IssueID)
		out[blocked] = append(out[blocked], orchestration.BlockerState{
			ID:     uuidToString(row.ID),
			Status: row.Status,
		})
	}
	return out
}

// startOrchestratedchild promotes a ready child out of `backlog` (so the
// platform's own enqueue gates accept it) and enqueues a task for its agent
// assignee. Returns true only when a task was actually enqueued. Children
// without a ready agent assignee, or that already have a pending task, are left
// for a human to pick up — orchestration never restarts running work and never
// starts a human-owned issue automatically.
func (h *Handler) startOrchestratedChild(ctx context.Context, child db.Issue) bool {
	if !h.isAgentAssigneeReady(ctx, child) {
		return false
	}
	hasPending, err := h.Queries.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
		IssueID: child.ID,
		AgentID: child.AssigneeID,
	})
	if err != nil || hasPending {
		return false
	}

	if child.Status == "backlog" {
		updated, err := h.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
			ID:          child.ID,
			Status:      "todo",
			WorkspaceID: child.WorkspaceID,
		})
		if err != nil {
			slog.Warn("orchestration: promote child failed", "child_id", uuidToString(child.ID), "error", err)
			return false
		}
		child = updated
		h.publishOrchestratedStatus(ctx, child)
	}

	if _, err := h.TaskService.EnqueueTaskForIssue(ctx, child); err != nil {
		slog.Warn("orchestration: enqueue child failed", "child_id", uuidToString(child.ID), "error", err)
		return false
	}
	return true
}

// publishOrchestratedStatus emits the issue:updated event so UI boards reflect
// the backlog→todo promotion immediately, mirroring the payload shape that
// UpdateIssue broadcasts.
func (h *Handler) publishOrchestratedStatus(ctx context.Context, issue db.Issue) {
	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	h.publishToAudience(protocol.EventIssueUpdated, uuidToString(issue.WorkspaceID), "system", "", map[string]any{
		"issue":            issueToResponse(issue, prefix),
		"assignee_changed": false,
		"status_changed":   true,
		"prev_status":      "backlog",
	}, h.audienceForIssue(ctx, issue))
}

// postOrchestrationComment writes a system comment on the parent issue and
// broadcasts it. Best-effort: failure is logged, not propagated.
func (h *Handler) postOrchestrationComment(ctx context.Context, parent db.Issue, content string) {
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
		slog.Warn("orchestration: create system comment failed", "parent_id", uuidToString(parent.ID), "error", err)
		return
	}
	h.publish(protocol.EventCommentCreated, uuidToString(parent.WorkspaceID), "system", "", map[string]any{
		"comment":             commentToResponse(comment, nil, nil),
		"issue_title":         parent.Title,
		"issue_assignee_type": textToPtr(parent.AssigneeType),
		"issue_assignee_id":   uuidToPtr(parent.AssigneeID),
		"issue_status":        parent.Status,
	})
}
