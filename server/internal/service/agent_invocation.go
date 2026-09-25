package service

import (
	"context"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// CanMemberInvokeAgent applies the existing invocation policy for durable triggers.
func CanMemberInvokeAgent(ctx context.Context, queries *db.Queries, agent db.Agent, memberUserID pgtype.UUID, workspaceID pgtype.UUID) bool {
	userID := util.UUIDToString(memberUserID)
	if userID == "" {
		return false
	}
	if util.UUIDToString(agent.OwnerID) == userID {
		return true
	}
	if agent.PermissionMode != "public_to" {
		return false
	}
	targets, err := queries.ListAgentInvocationTargets(ctx, agent.ID)
	if err != nil {
		return false
	}
	isWorkspaceMember := false
	if _, err := queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      memberUserID,
		WorkspaceID: workspaceID,
	}); err == nil {
		isWorkspaceMember = true
	}
	for _, t := range targets {
		switch t.TargetType {
		case "workspace":
			if isWorkspaceMember {
				return true
			}
		case "member":
			if util.UUIDToString(t.TargetID) == userID {
				return true
			}
		}
	}
	return false
}
