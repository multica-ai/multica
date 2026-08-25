package projectauth

// 2026-08-24 coder(lq): Keep workspace roles sourced from Multica's native member table.
type WorkspaceRole string

const (
	WorkspaceOwner  WorkspaceRole = "owner"
	WorkspaceAdmin  WorkspaceRole = "admin"
	WorkspaceMember WorkspaceRole = "member"
)

// 2026-08-24 coder(lq): Keep project roles independent from the native workspace role.
type ProjectRole string

const (
	ProjectOwner   ProjectRole = "owner"
	ProjectManager ProjectRole = "manager"
	ProjectMember  ProjectRole = "member"
	ProjectViewer  ProjectRole = "viewer"
)

// 2026-08-24 coder(lq): Carry the already-authenticated identity and native workspace
// membership. The HTTP adapter can construct it from Multica's request context.
type Subject struct {
	UserID        string
	WorkspaceID   string
	WorkspaceRole WorkspaceRole
}

// 2026-08-24 coder(lq): Use strings so new permissions can be added
// without changing the storage schema or the native Multica models.
type Permission string

const (
	View           Permission = "project.view"
	Edit           Permission = "project.edit"
	IssueCreate    Permission = "project.issue.create"
	IssueManage    Permission = "project.issue.manage"
	AgentUse       Permission = "project.agent.use"
	MemberManage   Permission = "project.member.manage"
	SettingsManage Permission = "project.settings.manage"
)
