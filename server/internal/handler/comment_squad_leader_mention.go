package handler

import (
	"context"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// A personal mention continues a leader role only for a reply to a delegation
// the speaking worker was given. Neither squad membership nor the agent's most
// recent run proves which role authored the delegation.
func (h *Handler) squadLeaderRoleForWorkerMention(ctx context.Context, issue db.Issue, parent *db.Comment, authorID string, agent db.Agent, opts commentTriggerComputeOptions) *db.Squad {
	if parent == nil || parent.DeletedAt.Valid || parent.IssueID != issue.ID || parent.WorkspaceID != issue.WorkspaceID ||
		parent.AuthorType != "agent" || parent.AuthorID != agent.ID || !parent.SourceTaskID.Valid ||
		authorID == uuidToString(agent.ID) || !opts.AuthoringTaskID.Valid ||
		!issue.AssigneeType.Valid || issue.AssigneeType.String != "squad" || !issue.AssigneeID.Valid {
		return nil
	}
	worker, err := h.Queries.GetAgentTaskInWorkspace(ctx, db.GetAgentTaskInWorkspaceParams{ID: opts.AuthoringTaskID, WorkspaceID: issue.WorkspaceID})
	if err != nil || worker.IsLeaderTask || uuidToString(worker.AgentID) != authorID || worker.IssueID != issue.ID || !taskCoversReplyParent(worker, parent.ID) {
		return nil
	}
	leader, err := h.Queries.GetAgentTaskInWorkspace(ctx, db.GetAgentTaskInWorkspaceParams{ID: parent.SourceTaskID, WorkspaceID: issue.WorkspaceID})
	if err != nil || !leader.IsLeaderTask || leader.AgentID != agent.ID || leader.IssueID != issue.ID || leader.SquadID != issue.AssigneeID {
		return nil
	}
	squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: leader.SquadID, WorkspaceID: issue.WorkspaceID})
	if err != nil || squad.ArchivedAt.Valid || squad.LeaderID != agent.ID {
		return nil
	}
	return &squad
}
