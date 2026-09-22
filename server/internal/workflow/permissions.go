package workflow

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func requireWorkflowMember(ctx context.Context, q db.DBTX, workspaceID, actorID string) error {
	var exists bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM member WHERE workspace_id=$1 AND user_id=$2)`, workspaceID, actorID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return &Error{Status: 403, Code: "action_forbidden", Message: "you are not a member of this workspace"}
	}
	return nil
}

// requireWorkflowManager is the service-side guard for publish, release
// activation/copy, archive, and delete. Handler membership checks are not
// enough because these methods are also called by background and CLI paths.
func requireWorkflowManager(ctx context.Context, q db.DBTX, actor string, w Workflow) error {
	if actor == "" {
		return &Error{Status: 403, Code: "action_forbidden", Message: "a workflow manager is required"}
	}
	if actor == w.OwnerID && w.OwnerID != "" {
		return nil
	}
	if w.WorkspaceID != "" && w.ID != "" {
		var creatorID string
		err := q.QueryRow(ctx, `SELECT creator_id::text FROM workflow WHERE workspace_id=$1 AND id=$2`, w.WorkspaceID, w.ID).Scan(&creatorID)
		if err == nil && creatorID == actor {
			return nil
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	workspaceID := w.WorkspaceID
	var allowed bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM member WHERE workspace_id=$1 AND user_id=$2 AND role IN ('owner','admin'))`, workspaceID, actor).Scan(&allowed); err != nil {
		return err
	}
	if !allowed {
		return &Error{Status: 403, Code: "action_forbidden", Message: "only the workflow owner or a workspace administrator can perform this action"}
	}
	return nil
}
