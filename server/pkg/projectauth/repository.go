package projectauth

import "context"

// 2026-08-24 coder(lq): Keep the persistence boundary narrow. Keeping it
// separate from sqlc's generated Queries makes upstream rebases low-friction.
type Repository interface {
	WorkspaceRole(ctx context.Context, workspaceID, userID string) (WorkspaceRole, error)
	ProjectRole(ctx context.Context, projectID, userID string) (ProjectRole, error)
	ProjectWorkspace(ctx context.Context, projectID string) (string, error)
	VisibleProjectIDs(ctx context.Context, workspaceID, userID string) ([]string, error)
}
