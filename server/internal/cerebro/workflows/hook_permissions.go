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
