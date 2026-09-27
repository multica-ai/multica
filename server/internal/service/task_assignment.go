package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/attribution"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// AttributionForIssueAssignment resolves the actor of this assignment, never
// the issue's creator. Missing or unusable agent lineage carries no authority.
// The caller supplies the actor and source task from the authenticated request.
func (s *TaskService) AttributionForIssueAssignment(ctx context.Context, issue db.Issue, actorType, actorID string, sourceTaskID pgtype.UUID) attribution.Result {
	actorUUID, err := util.ParseUUID(actorID)
	if err == nil && actorType == "member" {
		return attribution.DirectHumanRun(actorUUID, attribution.EvidenceIssueAssignment, issue.ID)
	}
	attr := attribution.Result{Source: attribution.SourceUnattributed, EvidenceKind: attribution.EvidenceIssueAssignment, EvidenceRefID: issue.ID}
	if err != nil || actorType != "agent" || !sourceTaskID.Valid {
		return attr
	}
	parent, err := s.Queries.GetAgentTaskInWorkspace(ctx, db.GetAgentTaskInWorkspaceParams{ID: sourceTaskID, WorkspaceID: issue.WorkspaceID})
	if err != nil || parent.AgentID != actorUUID {
		return attr
	}
	switch parent.Status {
	case "completed", "failed", "cancelled":
		return attr
	}
	attr.DelegatedFromTaskID = parent.ID
	attr.UserID = parent.OriginatorUserID
	attr.AccountableUserID = parent.AccountableUserID
	if attr.UserID.Valid {
		attr.AccountableUserID = attr.UserID
	}
	if attr.UserID.Valid || attr.AccountableUserID.Valid {
		attr.Source = attribution.SourceDelegation
	}
	return attr
}
