package projectauth

import (
	"context"
	"fmt"
)

type Service struct {
	repo    Repository
	policy  Policy
	enabled bool
}

func New(repo Repository, enabled bool) *Service {
	return &Service{repo: repo, policy: DefaultPolicy(), enabled: enabled}
}

func (s *Service) Enabled() bool { return s != nil && s.enabled }

// 2026-08-24 coder(lq): Return nil only when the subject may perform the
// permission; disabled deployments preserve legacy behavior during rollout.
func (s *Service) Check(ctx context.Context, subject Subject, projectID string, permission Permission) error {
	if s == nil || !s.enabled {
		return nil
	}
	// 2026-08-24 coder(lq): Fail closed when the rollout flag is enabled but
	// the persistence adapter was not wired, instead of panicking in a request.
	if s.repo == nil {
		return ErrDisabled
	}
	if subject.UserID == "" || subject.WorkspaceID == "" || projectID == "" {
		return ErrNoProjectAccess
	}
	workspaceID, err := s.repo.ProjectWorkspace(ctx, projectID)
	if err != nil || workspaceID != subject.WorkspaceID {
		return ErrNoProjectAccess
	}
	role, err := s.repo.WorkspaceRole(ctx, subject.WorkspaceID, subject.UserID)
	if err != nil {
		return ErrNotWorkspaceMember
	}
	if role == WorkspaceOwner || role == WorkspaceAdmin {
		return nil
	}
	projectRole, err := s.repo.ProjectRole(ctx, projectID, subject.UserID)
	if err != nil {
		return ErrNoProjectAccess
	}
	if !s.policy.Allows(projectRole, permission) {
		return fmt.Errorf("%w: role=%s permission=%s", ErrForbidden, projectRole, permission)
	}
	return nil
}

// 2026-08-24 coder(lq): Keep the HTTP adapter on one authorization entry point.
func (s *Service) Require(ctx context.Context, subject Subject, projectID string, permission Permission) error {
	return s.Check(ctx, subject, projectID, permission)
}

// 2026-08-24 coder(lq): Scope project lists to native admins or project_members.
func (s *Service) Scope(ctx context.Context, subject Subject) ([]string, error) {
	if s == nil || !s.enabled {
		return nil, nil
	}
	if s.repo == nil {
		return nil, ErrDisabled
	}
	if subject.UserID == "" || subject.WorkspaceID == "" {
		return nil, ErrNotWorkspaceMember
	}
	_, err := s.repo.WorkspaceRole(ctx, subject.WorkspaceID, subject.UserID)
	if err != nil {
		return nil, ErrNotWorkspaceMember
	}
	return s.repo.VisibleProjectIDs(ctx, subject.WorkspaceID, subject.UserID)
}

func ValidateProjectRole(role ProjectRole) error {
	if !validProjectRole(role) {
		return fmt.Errorf("%w: %s", ErrInvalidRole, role)
	}
	return nil
}
