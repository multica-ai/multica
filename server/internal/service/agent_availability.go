package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// MemberCanInvokeAgent evaluates the legacy-compatible invocation audience for
// a concrete workspace member. Availability remains a separate location gate
// and must be checked independently by the caller.
func MemberCanInvokeAgent(
	ctx context.Context,
	q *db.Queries,
	agent db.Agent,
	workspaceID, userID pgtype.UUID,
) bool {
	if !workspaceID.Valid || !userID.Valid || workspaceID != agent.WorkspaceID {
		return false
	}
	if userID == agent.OwnerID {
		return true
	}
	if agent.PermissionMode != "public_to" {
		return false
	}
	if _, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
	}); err != nil {
		return false
	}
	targets, err := q.ListAgentInvocationTargets(ctx, agent.ID)
	if err != nil {
		return false
	}
	for _, target := range targets {
		switch target.TargetType {
		case "workspace":
			return true
		case "member":
			if target.TargetID == userID {
				return true
			}
		case "team":
			// Team membership is not implemented; legacy placeholders stay inert.
		}
	}
	return false
}

// AgentAvailableInSpace evaluates only the Agent Availability location gate.
// It deliberately does not decide who may invoke the agent; HTTP entry points
// still apply their actor/audience rules before enqueueing work.
//
// Keeping this structural check in the service layer protects continuations
// and non-HTTP issue creation paths that already have an Issue/Space but no
// fresh human actor (for example child-done handoffs). Private availability is
// valid only while the owner can still view the target Space.
func AgentAvailableInSpace(
	ctx context.Context,
	q *db.Queries,
	agent db.Agent,
	workspaceID, spaceID pgtype.UUID,
) bool {
	if !workspaceID.Valid || !spaceID.Valid || workspaceID != agent.WorkspaceID {
		return false
	}
	space, err := q.GetWorkspaceSpace(ctx, db.GetWorkspaceSpaceParams{
		ID:          spaceID,
		WorkspaceID: workspaceID,
	})
	if err != nil || space.ArchivedAt.Valid {
		return false
	}

	switch agent.AvailabilityMode {
	case "private":
		allowed, err := q.CanViewWorkspaceSpace(ctx, db.CanViewWorkspaceSpaceParams{
			WorkspaceID: workspaceID,
			ID:          spaceID,
			UserID:      agent.OwnerID,
		})
		return err == nil && allowed
	case "selected_spaces":
		allowed, err := q.IsAgentAvailableInActiveSpace(ctx, db.IsAgentAvailableInActiveSpaceParams{
			AgentID:     agent.ID,
			WorkspaceID: workspaceID,
			SpaceID:     spaceID,
		})
		return err == nil && allowed
	case "workspace":
		return true
	default:
		return false
	}
}
