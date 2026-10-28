package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// CanMemberInvokeAgent applies the existing invocation policy for durable
// triggers, failing closed: a lookup that did not answer reads as "no".
//
// That is the right default for a trigger that fires on its own schedule —
// autopilot and issue wakeups can wait for the next tick. A caller that is
// answering a person right now wants the error instead, so it can retry rather
// than tell them they have no permission: use memberMayInvokeAgent.
func CanMemberInvokeAgent(ctx context.Context, queries *db.Queries, agent db.Agent, memberUserID pgtype.UUID, workspaceID pgtype.UUID) bool {
	allowed, err := memberMayInvokeAgent(ctx, queries, agent, memberUserID, workspaceID)
	return err == nil && allowed
}

// memberMayInvokeAgent is the policy itself, with query failures kept apart
// from denials.
//
// (false, nil) is a real verdict: no user id, not the owner of a private
// agent, or a public_to agent this member is not a target of — including the
// member row simply not being there, which is what pgx.ErrNoRows means here.
//
// (false, err) is "we do not know". Collapsing the two is what turns one
// transient database error into a message the platform will never redeliver
// and a person told they lack a permission they have.
func memberMayInvokeAgent(ctx context.Context, queries *db.Queries, agent db.Agent, memberUserID pgtype.UUID, workspaceID pgtype.UUID) (bool, error) {
	userID := util.UUIDToString(memberUserID)
	if userID == "" {
		return false, nil
	}
	if util.UUIDToString(agent.OwnerID) == userID {
		return true, nil
	}
	if agent.PermissionMode != "public_to" {
		return false, nil
	}
	targets, err := queries.ListAgentInvocationTargets(ctx, agent.ID)
	if err != nil {
		return false, fmt.Errorf("list agent invocation targets: %w", err)
	}
	isWorkspaceMember := false
	switch _, err := queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      memberUserID,
		WorkspaceID: workspaceID,
	}); {
	case err == nil:
		isWorkspaceMember = true
	case errors.Is(err, pgx.ErrNoRows):
		// Not a member. A fact, not a failure.
	default:
		return false, fmt.Errorf("load workspace member: %w", err)
	}
	for _, t := range targets {
		switch t.TargetType {
		case "workspace":
			if isWorkspaceMember {
				return true, nil
			}
		case "member":
			if util.UUIDToString(t.TargetID) == userID {
				return true, nil
			}
		}
	}
	return false, nil
}
