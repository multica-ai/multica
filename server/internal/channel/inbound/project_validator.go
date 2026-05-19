package inbound

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type DBProjectWorkspaceValidator struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewDBProjectWorkspaceValidator(pool *pgxpool.Pool) *DBProjectWorkspaceValidator {
	if pool == nil {
		return &DBProjectWorkspaceValidator{}
	}
	return &DBProjectWorkspaceValidator{pool: pool, queries: db.New(pool)}
}

func (v *DBProjectWorkspaceValidator) ValidateProjectInWorkspace(ctx context.Context, workspaceID, projectID pgtype.UUID) error {
	if v == nil || v.pool == nil || v.queries == nil {
		return errors.New("project validator is not configured")
	}
	if !workspaceID.Valid || !projectID.Valid {
		return errors.New("project validator received invalid workspace_id or project_id")
	}
	_, err := v.queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{
		ID:          projectID,
		WorkspaceID: workspaceID,
	})
	return err
}

var _ ProjectWorkspaceValidator = (*DBProjectWorkspaceValidator)(nil)
