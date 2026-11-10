package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/projectaccess"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ErrProjectAccessDenied refuses a run that would read a private project
// (MUL-8472) its human (originator, else accountable user) or its agent's
// owner cannot see. An agent has no project access of its own: a run reads a
// private project only when both of those people can.
var ErrProjectAccessDenied = errors.New("the run's member or the agent's owner cannot access this private project")

// guardRunProjectAccess is the enqueue-time check behind ErrProjectAccessDenied
// for a run on an existing issue.
func (s *TaskService) guardRunProjectAccess(ctx context.Context, issue db.Issue, agent db.Agent, originatorUserID, accountableUserID pgtype.UUID) error {
	return s.guardRunProjectsAccess(ctx, issue.WorkspaceID, agent, originatorUserID, accountableUserID, issue.ProjectID)
}

// guardQuickCreateProjectAccess covers a quick-create run, which has no issue
// yet but is handed the target project's context and, for a sub-issue, its
// parent. Both the chosen project and the parent's project are checked.
func (s *TaskService) guardQuickCreateProjectAccess(ctx context.Context, workspaceID pgtype.UUID, agent db.Agent, requesterID, accountableUserID, projectID, parentIssueID pgtype.UUID) error {
	projects := []pgtype.UUID{projectID}
	if parentIssueID.Valid {
		parentProject, err := s.Queries.GetIssueProjectID(ctx, parentIssueID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("load parent issue project: %w", err)
		}
		projects = append(projects, parentProject)
	}
	return s.guardRunProjectsAccess(ctx, workspaceID, agent, requesterID, accountableUserID, projects...)
}

// guardRunProjectsAccess refuses the run when any of projectIDs is a private
// project the run's human or the agent's owner cannot see. Projects that are
// not private pass after one read each.
func (s *TaskService) guardRunProjectsAccess(ctx context.Context, workspaceID pgtype.UUID, agent db.Agent, originatorUserID, accountableUserID pgtype.UUID, projectIDs ...pgtype.UUID) error {
	var private []pgtype.UUID
	for _, projectID := range projectIDs {
		if !projectID.Valid {
			continue
		}
		project, err := s.Queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{
			ID:          projectID,
			WorkspaceID: workspaceID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("load project: %w", err)
		}
		if project.Visibility == projectaccess.VisibilityPrivate {
			private = append(private, projectID)
		}
	}
	if len(private) == 0 {
		return nil
	}
	human := originatorUserID
	if !human.Valid {
		human = accountableUserID
	}
	hidden, err := projectaccess.ForPrincipals(ctx, s.Queries, workspaceID, human, agent.OwnerID)
	if err != nil {
		return fmt.Errorf("resolve project access: %w", err)
	}
	for _, projectID := range private {
		if !hidden.CanSeeProject(projectID) {
			slog.Info("task enqueue refused: private project access",
				"agent_id", util.UUIDToString(agent.ID),
				"project_id", util.UUIDToString(projectID))
			return ErrProjectAccessDenied
		}
	}
	return nil
}
