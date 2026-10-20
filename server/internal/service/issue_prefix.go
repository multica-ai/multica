package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// WorkspaceIssuePrefix returns the workspace's effective identifier prefix,
// falling back to the stored value used by every legacy row.
func WorkspaceIssuePrefix(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID) string {
	ws, err := q.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return ""
	}
	return ws.IssuePrefix
}

// IssuePrefixForScope resolves the prefix an issue is numbered under: its
// project's prefix when that project owns one, otherwise the workspace prefix.
// Identifier rendering outside the HTTP handler must use this instead of the
// workspace prefix, or a project-scoped issue leaks the wrong key into events,
// activity rows and agent briefs.
func IssuePrefixForScope(ctx context.Context, q *db.Queries, workspaceID, projectID pgtype.UUID) string {
	if projectID.Valid {
		project, err := q.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{
			ID:          projectID,
			WorkspaceID: workspaceID,
		})
		if err == nil && project.IssuePrefix != "" {
			return project.IssuePrefix
		}
	}
	return WorkspaceIssuePrefix(ctx, q, workspaceID)
}
