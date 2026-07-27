package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// AgentInvocationQueries is the read-only DB surface required by
// CanInvokeAgent. Keeping the authorization predicate behind this small
// interface lets the handler and provider-failover service share one contract
// without creating a handler/service import cycle.
type AgentInvocationQueries interface {
	ListAgentInvocationTargets(context.Context, pgtype.UUID) ([]db.AgentInvocationTarget, error)
	GetMemberByUserAndWorkspace(context.Context, db.GetMemberByUserAndWorkspaceParams) (db.Member, error)
}

// CanInvokeAgent is the authoritative agent-run admission predicate.
//
// The effective invoking user is the member actor itself for direct human
// calls, and the top-of-chain human originator for agent/system calls. Private
// agents are owner-only. public_to agents use their workspace/member target
// allow-list; team targets remain inert until team membership exists.
//
// Provider failover calls this exact predicate before creating a continuation,
// so failover cannot turn an otherwise unauthorized agent into a credential,
// tool, or paid-plan execution path.
func CanInvokeAgent(
	ctx context.Context,
	queries AgentInvocationQueries,
	agent db.Agent,
	actorType, actorID, originatorUserID, workspaceID string,
) bool {
	effectiveUser := actorID
	if actorType != "member" {
		effectiveUser = originatorUserID
	}

	if effectiveUser != "" && util.UUIDToString(agent.OwnerID) == effectiveUser {
		return true
	}
	if agent.PermissionMode != "public_to" {
		return false
	}

	targets, err := queries.ListAgentInvocationTargets(ctx, agent.ID)
	if err != nil {
		return false
	}

	workspaceBroad := actorType == "agent" || actorType == "system"
	isWorkspaceMember := false
	if effectiveUser != "" {
		userID, userErr := util.ParseUUID(effectiveUser)
		wsID, workspaceErr := util.ParseUUID(workspaceID)
		if userErr == nil && workspaceErr == nil {
			_, err = queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
				UserID:      userID,
				WorkspaceID: wsID,
			})
			isWorkspaceMember = err == nil
		}
	}

	for _, target := range targets {
		switch target.TargetType {
		case "workspace":
			if isWorkspaceMember || workspaceBroad {
				return true
			}
		case "member":
			if effectiveUser != "" && util.UUIDToString(target.TargetID) == effectiveUser {
				return true
			}
		case "team":
			// Reserved and deliberately fail-closed until team membership is
			// implemented.
		}
	}
	return false
}
