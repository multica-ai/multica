package projectauth

import "context"

type ProjectMemberRecord struct {
	ProjectID string      `json:"project_id"`
	UserID    string      `json:"user_id"`
	Role      ProjectRole `json:"role"`
}

type MemberRepository interface {
	Repository
	AddProjectMember(ctx context.Context, projectID, userID string, role ProjectRole) error
	RemoveProjectMember(ctx context.Context, projectID, userID string) error
	ListProjectMembers(ctx context.Context, projectID string) ([]ProjectMemberRecord, error)
}

// 2026-08-24 coder(lq): Seed the creator as project owner so enabling the
// overlay cannot strand projects created by ordinary workspace members.
func (s *Service) EnsureOwner(ctx context.Context, projectID, userID string) error {
	if s == nil || !s.enabled {
		return nil
	}
	mr, ok := s.repo.(MemberRepository)
	if !ok {
		return ErrDisabled
	}
	workspaceID, err := mr.ProjectWorkspace(ctx, projectID)
	if err != nil {
		return ErrNoProjectAccess
	}
	if _, err = mr.WorkspaceRole(ctx, workspaceID, userID); err != nil {
		return ErrCrossWorkspace
	}
	return mr.AddProjectMember(ctx, projectID, userID, ProjectOwner)
}

// 2026-08-24 coder(lq): Perform the two tenant checks that cannot be represented by a
// simple project_members foreign key: the project exists and the user belongs
// to the same native workspace.
func (s *Service) AddMember(ctx context.Context, actor Subject, projectID, userID string, role ProjectRole) error {
	if s == nil || !s.enabled {
		return nil
	}
	if err := s.Require(ctx, actor, projectID, MemberManage); err != nil {
		return err
	}
	if !validProjectRole(role) {
		return ErrInvalidRole
	}
	mr, ok := s.repo.(MemberRepository)
	if !ok {
		return ErrDisabled
	}
	workspaceID, err := mr.ProjectWorkspace(ctx, projectID)
	if err != nil {
		return ErrNoProjectAccess
	}
	if _, err = mr.WorkspaceRole(ctx, workspaceID, userID); err != nil {
		return ErrCrossWorkspace
	}
	return mr.AddProjectMember(ctx, projectID, userID, role)
}

func (s *Service) ListMembers(ctx context.Context, actor Subject, projectID string) ([]ProjectMemberRecord, error) {
	if s == nil || !s.enabled {
		return nil, nil
	}
	if err := s.Require(ctx, actor, projectID, View); err != nil {
		return nil, err
	}
	mr, ok := s.repo.(MemberRepository)
	if !ok {
		return nil, ErrDisabled
	}
	return mr.ListProjectMembers(ctx, projectID)
}

func (s *Service) RemoveMember(ctx context.Context, actor Subject, projectID, userID string) error {
	if s == nil || !s.enabled {
		return nil
	}
	if err := s.Require(ctx, actor, projectID, MemberManage); err != nil {
		return err
	}
	mr, ok := s.repo.(MemberRepository)
	if !ok {
		return ErrDisabled
	}
	return mr.RemoveProjectMember(ctx, projectID, userID)
}
