package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/projectaccess"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ErrProjectAccessDenied refuses a run on an issue in a private project
// (MUL-8472) that its human (originator, else accountable user) or its agent's
// owner cannot see. An agent has no project access of its own: a run reads a
// private project only when both of those people can.
var ErrProjectAccessDenied = errors.New("the run's member or the agent's owner cannot access this issue's private project")

// guardRunProjectAccess is the enqueue-time check behind ErrProjectAccessDenied.
// Issues outside a private project pass after one project read.
func (s *TaskService) guardRunProjectAccess(ctx context.Context, issue db.Issue, agent db.Agent, originatorUserID, accountableUserID pgtype.UUID) error {
	if !issue.ProjectID.Valid {
		return nil
	}
	project, err := s.Queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{
		ID:          issue.ProjectID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return fmt.Errorf("load issue project: %w", err)
	}
	if project.Visibility != projectaccess.VisibilityPrivate {
		return nil
	}
	human := originatorUserID
	if !human.Valid {
		human = accountableUserID
	}
	hidden, err := projectaccess.ForPrincipals(ctx, s.Queries, issue.WorkspaceID, human, agent.OwnerID)
	if err != nil {
		return fmt.Errorf("resolve project access: %w", err)
	}
	if !hidden.CanSeeProject(issue.ProjectID) {
		slog.Info("task enqueue refused: private project access",
			"issue_id", util.UUIDToString(issue.ID),
			"agent_id", util.UUIDToString(agent.ID),
			"project_id", util.UUIDToString(issue.ProjectID))
		return ErrProjectAccessDenied
	}
	return nil
}
