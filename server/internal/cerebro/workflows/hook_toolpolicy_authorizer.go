package workflows

import (
	"context"

	"github.com/multica-ai/multica/server/internal/cerebro/platformaccess"
	"github.com/multica-ai/multica/server/internal/cerebro/platformcatalog"
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
	capability, ok := platformcatalog.ByKey(string(permission))
	if !ok {
		return false
	}
	platformActor := platformaccess.Actor{
		Authenticated: actor.Type == "agent" || actor.Type == "member",
		Agent:         actor.Type == "agent",
		Owner:         actor.Type == "member" && actor.IsOwner,
	}
	if capability.Enforcement == platformaccess.EnforcementAuthenticatedRead || capability.Enforcement == platformaccess.EnforcementOwnerOnly {
		return toolpolicy.ResolvePlatformAction(toolpolicy.Input{}, capability.Enforcement, platformActor).Setting == toolpolicy.SettingAllow
	}
	if a == nil || a.store == nil {
		return false
	}
	query, ok := hookActorQuery(workspaceID, actor, string(permission))
	if !ok {
		return false
	}
	effective, err := a.store.ResolvePlatformAction(ctx, query, capability.Enforcement, platformActor)
	return err == nil && effective.Setting == toolpolicy.SettingAllow
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
