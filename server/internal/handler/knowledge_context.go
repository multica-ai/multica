package handler

import (
	"context"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// issueKnowledgeContext returns the company knowledge an issue run starts with.
// A task without an issue, and a workspace whose knowledge is not ready, get
// no retrieval at all. Failures inside the retrieval come back as an empty
// result, so this never fails the claim.
func (h *Handler) issueKnowledgeContext(ctx context.Context, task db.AgentTaskQueue, workspaceID string) []service.KnowledgeHitView {
	if !task.IssueID.Valid {
		return nil
	}
	wsID := parseUUID(workspaceID)
	if !h.Knowledge.Ready(ctx, wsID) {
		return nil
	}
	issue, err := h.Queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		return nil
	}
	return h.Knowledge.IssueContext(ctx, wsID, issue.Title, issue.Description.String)
}
