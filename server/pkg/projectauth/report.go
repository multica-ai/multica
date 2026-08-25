package projectauth

import (
	"context"
	"fmt"
)

// 2026-08-24 coder(lq): Permission reports expose authorization evidence
// (role-derived and task-direct grants) without changing Multica's native
// member or project models.
type PermissionReportFilter struct {
	WorkspaceID string
	ProjectID   string
	IssueID     string
	UserID      string
	Role        string
	Permission  Permission
	Scope       string // all, project, or issue
	Limit       int
	Offset      int
}

type PermissionReportRow struct {
	Scope         string        `json:"scope"`
	ProjectID     string        `json:"project_id"`
	ProjectTitle  string        `json:"project_title"`
	IssueID       string        `json:"issue_id,omitempty"`
	IssueTitle    string        `json:"issue_title,omitempty"`
	UserID        string        `json:"user_id"`
	UserName      string        `json:"user_name"`
	UserEmail     string        `json:"user_email"`
	WorkspaceRole WorkspaceRole `json:"workspace_role"`
	ProjectRole   ProjectRole   `json:"project_role,omitempty"`
	Permission    Permission    `json:"permission"`
	Source        string        `json:"source"` // workspace_role, project_role, issue_grant
	GrantedBy     string        `json:"granted_by,omitempty"`
}

type PermissionReportResult struct {
	Rows  []PermissionReportRow
	Total int64
}

type PermissionReportRepository interface {
	Repository
	IssueProject(ctx context.Context, issueID string) (string, error)
	ListPermissionReport(ctx context.Context, filter PermissionReportFilter) (PermissionReportResult, error)
}

// 2026-08-24 coder(lq): Reports are administrative reads. Workspace
// owners/admins may report across the workspace; other users need project
// settings permission and must scope the report to one project or task.
func (s *Service) ListPermissionReport(ctx context.Context, subject Subject, filter PermissionReportFilter) (PermissionReportResult, error) {
	if s == nil || !s.enabled {
		return PermissionReportResult{}, nil
	}
	if s.repo == nil {
		return PermissionReportResult{}, ErrDisabled
	}
	rr, ok := s.repo.(PermissionReportRepository)
	if !ok {
		return PermissionReportResult{}, ErrDisabled
	}
	if filter.WorkspaceID == "" {
		filter.WorkspaceID = subject.WorkspaceID
	}
	if filter.WorkspaceID == "" || filter.WorkspaceID != subject.WorkspaceID {
		return PermissionReportResult{}, ErrCrossWorkspace
	}
	if filter.Scope == "" {
		filter.Scope = "all"
	}
	if filter.Scope != "all" && filter.Scope != "project" && filter.Scope != "issue" {
		return PermissionReportResult{}, fmt.Errorf("%w: scope=%s", ErrInvalidReportFilter, filter.Scope)
	}
	if filter.Role != "" && !validReportRole(filter.Role) {
		return PermissionReportResult{}, fmt.Errorf("%w: role=%s", ErrInvalidReportFilter, filter.Role)
	}
	if filter.Permission != "" && !validReportPermission(filter.Permission) {
		return PermissionReportResult{}, fmt.Errorf("%w: permission=%s", ErrInvalidReportFilter, filter.Permission)
	}
	if filter.Limit <= 0 || filter.Limit > 1000 {
		filter.Limit = 1000
	}
	if filter.Offset < 0 {
		return PermissionReportResult{}, fmt.Errorf("%w: offset=%d", ErrInvalidReportFilter, filter.Offset)
	}
	role, err := s.repo.WorkspaceRole(ctx, subject.WorkspaceID, subject.UserID)
	if err != nil {
		return PermissionReportResult{}, ErrNotWorkspaceMember
	}
	if role != WorkspaceOwner && role != WorkspaceAdmin {
		if filter.ProjectID == "" {
			if filter.IssueID == "" {
				return PermissionReportResult{}, ErrForbidden
			}
			filter.ProjectID, err = rr.IssueProject(ctx, filter.IssueID)
			if err != nil {
				return PermissionReportResult{}, ErrNoProjectAccess
			}
		}
		if err := s.Check(ctx, subject, filter.ProjectID, SettingsManage); err != nil {
			return PermissionReportResult{}, err
		}
	}
	return rr.ListPermissionReport(ctx, filter)
}

func validReportRole(role string) bool {
	switch role {
	case string(WorkspaceOwner), string(WorkspaceAdmin), string(WorkspaceMember), string(ProjectManager), string(ProjectViewer):
		return true
	default:
		return false
	}
}

func validReportPermission(permission Permission) bool {
	switch permission {
	case View, Edit, IssueCreate, IssueManage, AgentUse, MemberManage, SettingsManage:
		return true
	default:
		return false
	}
}
