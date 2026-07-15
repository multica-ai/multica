package workflows

type HookPermission string

const (
	HookPermissionRead          HookPermission = "hooks:read"
	HookPermissionWrite         HookPermission = "hooks:write"
	HookPermissionEnforce       HookPermission = "hooks:enforce"
	HookPermissionManageManaged HookPermission = "hooks:manage_managed"
)

type HookPermissionActor struct {
	Type        string
	ID          string
	OwnerUserID string
	IsOwner     bool
}

// HookPermissionEvaluator pins the security defaults independently from the
// HTTP/tool-policy adapter. Explicit contains only authoritatively resolved
// grants; inherited manage_workflows access is deliberately ignored.
type HookPermissionEvaluator struct {
	Explicit              map[string]map[HookPermission]bool
	LegacyManageWorkflows map[string]bool
}

func (e HookPermissionEvaluator) Can(actor HookPermissionActor, permission HookPermission) bool {
	switch permission {
	case HookPermissionRead:
		return actor.Type == "member" || actor.Type == "agent"
	case HookPermissionEnforce:
		return actor.Type == "member" && e.explicit(actor.ID, permission)
	case HookPermissionManageManaged:
		return actor.Type == "member" && actor.IsOwner
	case HookPermissionWrite:
		return e.explicit(actor.ID, permission)
	default:
		return false
	}
}

func (e HookPermissionEvaluator) explicit(actorID string, permission HookPermission) bool {
	permissions, ok := e.Explicit[actorID]
	return ok && permissions[permission]
}
