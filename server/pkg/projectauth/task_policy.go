package projectauth

import (
	"context"
	"fmt"
)

// 2026-08-24 coder(lq): Task grants are intentionally narrower than project
// grants. A task grant can add capabilities on one issue, but can never grant
// project membership, project visibility, or project administration.
type IssuePermissionRecord struct {
	IssueID    string     `json:"issue_id"`
	ProjectID  string     `json:"project_id"`
	UserID     string     `json:"user_id"`
	Permission Permission `json:"permission"`
	GrantedBy  string     `json:"granted_by"`
}

type TaskAuthorizationRepository interface {
	Repository
	IssuePermission(ctx context.Context, issueID, projectID, userID string, permission Permission) (bool, error)
	GrantIssuePermission(ctx context.Context, issueID, projectID, userID, grantedBy string, permission Permission) error
	RevokeIssuePermission(ctx context.Context, issueID, projectID, userID string, permission Permission) error
	ListIssuePermissions(ctx context.Context, issueID, projectID string) ([]IssuePermissionRecord, error)
}

func validIssuePermission(permission Permission) bool {
	switch permission {
	case Edit, IssueManage, AgentUse:
		return true
	default:
		return false
	}
}

func ValidateIssuePermission(permission Permission) error {
	if !validIssuePermission(permission) {
		return fmt.Errorf("%w: %s", ErrInvalidIssuePermission, permission)
	}
	return nil
}

// 2026-08-24 coder(lq): CheckIssue applies the fixed authorization order:
// workspace membership and project visibility are hard prerequisites, then a
// project role or an issue-specific grant may satisfy the requested action.
func (s *Service) CheckIssue(ctx context.Context, subject Subject, issueID, projectID string, permission Permission) error {
	if s == nil || !s.enabled {
		return nil
	}
	if !validIssuePermission(permission) && permission != View {
		return ValidateIssuePermission(permission)
	}
	if err := s.Check(ctx, subject, projectID, View); err != nil {
		return err
	}
	if permission == View {
		return nil
	}
	if err := s.Check(ctx, subject, projectID, permission); err == nil {
		return nil
	}
	tar, ok := s.repo.(TaskAuthorizationRepository)
	if !ok {
		return ErrDisabled
	}
	allowed, err := tar.IssuePermission(ctx, issueID, projectID, subject.UserID, permission)
	if err != nil {
		return ErrDisabled
	}
	// 2026-08-24 coder(lq): Multica's existing update/delete handlers share
	// project.issue.manage. Treat a direct project.edit grant as the task-level
	// editor capability for that shared path, without widening project scope.
	if !allowed && permission == IssueManage {
		allowed, err = tar.IssuePermission(ctx, issueID, projectID, subject.UserID, Edit)
		if err != nil {
			return ErrDisabled
		}
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

// 2026-08-24 coder(lq): Only users who already have project access can receive
// a task grant; this prevents the grant table from becoming an implicit project
// membership mechanism.
func (s *Service) GrantIssuePermission(ctx context.Context, actor Subject, issueID, projectID, userID string, permission Permission) error {
	if s == nil || !s.enabled {
		return nil
	}
	if err := ValidateIssuePermission(permission); err != nil {
		return err
	}
	if err := s.Require(ctx, actor, projectID, IssueManage); err != nil {
		return err
	}
	tar, ok := s.repo.(TaskAuthorizationRepository)
	if !ok {
		return ErrDisabled
	}
	workspaceID, err := s.repo.ProjectWorkspace(ctx, projectID)
	if err != nil || workspaceID != actor.WorkspaceID {
		return ErrNoProjectAccess
	}
	workspaceRole, err := s.repo.WorkspaceRole(ctx, workspaceID, userID)
	if err != nil {
		return ErrCrossWorkspace
	}
	if workspaceRole != WorkspaceOwner && workspaceRole != WorkspaceAdmin {
		if _, err := s.repo.ProjectRole(ctx, projectID, userID); err != nil {
			return ErrNoProjectAccess
		}
	}
	return tar.GrantIssuePermission(ctx, issueID, projectID, userID, actor.UserID, permission)
}

func (s *Service) ListIssuePermissions(ctx context.Context, actor Subject, issueID, projectID string) ([]IssuePermissionRecord, error) {
	if s == nil || !s.enabled {
		return nil, nil
	}
	if err := s.Require(ctx, actor, projectID, View); err != nil {
		return nil, err
	}
	tar, ok := s.repo.(TaskAuthorizationRepository)
	if !ok {
		return nil, ErrDisabled
	}
	return tar.ListIssuePermissions(ctx, issueID, projectID)
}

func (s *Service) RevokeIssuePermission(ctx context.Context, actor Subject, issueID, projectID, userID string, permission Permission) error {
	if s == nil || !s.enabled {
		return nil
	}
	if err := ValidateIssuePermission(permission); err != nil {
		return err
	}
	if err := s.Require(ctx, actor, projectID, IssueManage); err != nil {
		return err
	}
	tar, ok := s.repo.(TaskAuthorizationRepository)
	if !ok {
		return ErrDisabled
	}
	return tar.RevokeIssuePermission(ctx, issueID, projectID, userID, permission)
}
