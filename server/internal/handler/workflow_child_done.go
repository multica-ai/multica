package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func (h *Handler) tryAdvanceWorkflowFromClosedStage(ctx context.Context, parent db.Issue, closedStage int32) bool {
	activeRun, err := h.Queries.GetActiveWorkflowRunForIssue(ctx, db.GetActiveWorkflowRunForIssueParams{
		WorkspaceID: parent.WorkspaceID,
		IssueID:     parent.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	if err != nil {
		slog.Warn("workflow child done: active-run lookup failed", "error", err, "parent_id", uuidToString(parent.ID))
		return true
	}
	// A batch may report a higher closed frontier than the run currently owns.
	// Reconcile from the durable current stage; the service will deterministically
	// skip any already-terminal later stages from the final sibling snapshot.
	reconcileStage := closedStage
	if reconcileStage > activeRun.CurrentStage {
		reconcileStage = activeRun.CurrentStage
	}
	result, err := h.WorkflowService.AdvanceFromClosedStage(ctx, service.AdvanceWorkflowParams{
		WorkspaceID: parent.WorkspaceID,
		IssueID:     parent.ID,
		ClosedStage: reconcileStage,
		Actor:       service.WorkflowActor{Type: "system"},
	})
	if err != nil {
		slog.Warn("workflow child done: deterministic advancement failed",
			"error", err, "parent_id", uuidToString(parent.ID), "closed_stage", closedStage)
		return true
	}

	h.applyWorkflowMutationSideEffects(ctx, nil, result, "system", "")
	h.logWorkflowTransitions(result)
	if result.Outcome == "noop" {
		return true
	}
	if parent.AssigneeType.Valid && parent.AssigneeType.String == "member" {
		return true
	}
	h.postWorkflowProgressComment(ctx, parent, result, closedStage)
	return true
}

func (h *Handler) postWorkflowProgressComment(ctx context.Context, parent db.Issue, result service.WorkflowMutationResult, closedStage int32) {
	content := workflowProgressComment(result, closedStage)
	if content == "" {
		return
	}
	created, err := h.Queries.CreateComment(ctx, db.CreateCommentParams{
		ID:          dbid.NewV7(),
		IssueID:     parent.ID,
		WorkspaceID: parent.WorkspaceID,
		AuthorType:  "system",
		AuthorID:    pgtype.UUID{Valid: true},
		Content:     content,
		Type:        "system",
		ParentID:    pgtype.UUID{Valid: false},
	})
	if err != nil {
		slog.Warn("workflow child done: create progress comment failed", "error", err, "parent_id", uuidToString(parent.ID))
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
	if result.Outcome == "blocked_materialization" {
		h.dispatchParentAssigneeTrigger(ctx, parent, comment)
	}
}
func workflowProgressComment(result service.WorkflowMutationResult, closedStage int32) string {
	switch result.Outcome {
	case "stage_advanced":
		return fmt.Sprintf("Stage %d is complete. Workflow advanced automatically to Stage %d and activated its ready sub-issues.", closedStage, result.Run.CurrentStage)
	case "blocked_materialization":
		return fmt.Sprintf("Stage %d is complete. Workflow requires Stage %d, but no Stage %d sub-issues exist yet. Create Stage %d sub-issues in backlog, then resume the workflow.", closedStage, result.Run.CurrentStage, result.Run.CurrentStage, result.Run.CurrentStage)
	case "completed_pending_review":
		return "The final declared workflow stage is complete. The workflow moved this issue to In Review."
	default:
		return ""
	}
}
