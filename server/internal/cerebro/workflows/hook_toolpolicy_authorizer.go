package workflows

import (
	"context"

	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
)

type ToolPolicyHookAuthorizer struct {
	store *toolpolicy.Store
}

func NewToolPolicyHookAuthorizer(store *toolpolicy.Store) *ToolPolicyHookAuthorizer {
	return &ToolPolicyHookAuthorizer{store: store}
}

func (a *ToolPolicyHookAuthorizer) Can(ctx context.Context, workspaceID string, actor HookPermissionActor, permission HookPermission) bool {
	baseline := HookPermissionEvaluator{}
	if permission == HookPermissionRead || permission == HookPermissionManageManaged {
		return baseline.Can(actor, permission)
	}
	if actor.Type == "agent" && permission == HookPermissionEnforce {
		return false
	}
	if a == nil || a.store == nil {
		return false
	}
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return false
	}
	query := toolpolicy.Query{WorkspaceID: wsID, ToolKey: string(permission)}
	if actor.Type == "agent" {
		agentID, err := util.ParseUUID(actor.ID)
		if err != nil {
			return false
		}
		query.AgentID = agentID
		if actor.OwnerUserID != "" {
			if ownerID, err := util.ParseUUID(actor.OwnerUserID); err == nil {
				query.UserID = ownerID
			}
		}
	} else {
		userID, err := util.ParseUUID(actor.ID)
		if err != nil {
			return false
		}
		query.UserID = userID
	}
	allowed, err := a.store.ResolveActorOptIn(ctx, query, actor.Type == "agent")
	return err == nil && allowed
}

func (a *ToolPolicyHookAuthorizer) CanAction(ctx context.Context, workspaceID string, actor HookPermissionActor, capability string) bool {
	if a == nil || a.store == nil || capability == "" {
		return false
	}
	query, ok := hookActorQuery(workspaceID, actor, capability)
	if !ok {
		return false
	}
	effective, err := a.store.Resolve(ctx, query)
	return err == nil && effective.Setting == toolpolicy.SettingAllow
}

func hookActorQuery(workspaceID string, actor HookPermissionActor, key string) (toolpolicy.Query, bool) {
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return toolpolicy.Query{}, false
	}
	query := toolpolicy.Query{WorkspaceID: wsID, ToolKey: key}
	if actor.Type == "agent" {
		agentID, err := util.ParseUUID(actor.ID)
		if err != nil {
			return toolpolicy.Query{}, false
		}
		query.AgentID = agentID
		if ownerID, err := util.ParseUUID(actor.OwnerUserID); err == nil {
			query.UserID = ownerID
		}
	} else {
		userID, err := util.ParseUUID(actor.ID)
		if err != nil {
			return toolpolicy.Query{}, false
		}
		query.UserID = userID
	}
	return query, true
}
