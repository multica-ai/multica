package handler

import (
	"context"
	"errors"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func (h *Handler) evaluateKnowledgeCandidateForIssueDone(ctx context.Context, prevIssue, issue db.Issue) {
	if h.KnowledgeService == nil || prevIssue.Status == issue.Status || issue.Status != "done" {
		return
	}
	if _, err := h.KnowledgeService.EvaluateIssueDoneCandidate(ctx, issue); err != nil && !errors.Is(err, service.ErrKnowledgeValidation) {
		slog.Warn("knowledge candidate issue evaluation failed",
			"issue_id", util.UUIDToString(issue.ID),
			"workspace_id", util.UUIDToString(issue.WorkspaceID),
			"error", err,
		)
	}
}

func (h *Handler) evaluateKnowledgeCandidateForPRMerge(ctx context.Context, issue db.Issue) {
	if h.KnowledgeService == nil {
		return
	}
	if _, err := h.KnowledgeService.EvaluateIssuePRMergedCandidate(ctx, issue); err != nil && !errors.Is(err, service.ErrKnowledgeValidation) {
		slog.Warn("knowledge candidate PR-merge evaluation failed",
			"issue_id", util.UUIDToString(issue.ID),
			"workspace_id", util.UUIDToString(issue.WorkspaceID),
			"error", err,
		)
	}
}
