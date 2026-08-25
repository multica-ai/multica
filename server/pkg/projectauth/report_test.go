package projectauth

import (
	"context"
	"errors"
	"testing"
)

type fakeReportRepo struct {
	fakeRepo
	issueProject string
	lastFilter   PermissionReportFilter
}

func (f *fakeReportRepo) IssueProject(context.Context, string) (string, error) {
	if f.issueProject == "" {
		return "", errors.New("issue not found")
	}
	return f.issueProject, nil
}

func (f *fakeReportRepo) ListPermissionReport(_ context.Context, filter PermissionReportFilter) (PermissionReportResult, error) {
	f.lastFilter = filter
	return PermissionReportResult{Total: 1}, nil
}

func reportSubject(role WorkspaceRole) Subject {
	return Subject{UserID: "u-1", WorkspaceID: "ws-1", WorkspaceRole: role}
}

func TestPermissionReportWorkspaceAdminCanQueryWorkspace(t *testing.T) {
	repo := &fakeReportRepo{fakeRepo: fakeRepo{workspace: string(WorkspaceAdmin)}}
	s := New(repo, true)
	if _, err := s.ListPermissionReport(context.Background(), reportSubject(WorkspaceAdmin), PermissionReportFilter{}); err != nil {
		t.Fatalf("admin report: %v", err)
	}
	if repo.lastFilter.WorkspaceID != "ws-1" || repo.lastFilter.Limit != 1000 {
		t.Fatalf("unexpected normalized filter: %+v", repo.lastFilter)
	}
}

func TestPermissionReportMemberMustScopeProject(t *testing.T) {
	repo := &fakeReportRepo{fakeRepo: fakeRepo{workspace: string(WorkspaceMember), project: string(ProjectOwner), projectWorkspace: "ws-1"}}
	s := New(repo, true)
	_, err := s.ListPermissionReport(context.Background(), reportSubject(WorkspaceMember), PermissionReportFilter{})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("got %v, want %v", err, ErrForbidden)
	}
}

func TestPermissionReportMemberNeedsSettingsManage(t *testing.T) {
	repo := &fakeReportRepo{fakeRepo: fakeRepo{workspace: string(WorkspaceMember), project: string(ProjectManager), projectWorkspace: "ws-1"}}
	s := New(repo, true)
	_, err := s.ListPermissionReport(context.Background(), reportSubject(WorkspaceMember), PermissionReportFilter{ProjectID: "p-1"})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("got %v, want %v", err, ErrForbidden)
	}
}

func TestPermissionReportIssueScopeResolvesProject(t *testing.T) {
	repo := &fakeReportRepo{fakeRepo: fakeRepo{
		workspace: string(WorkspaceMember), project: string(ProjectOwner), projectWorkspace: "ws-1",
	}, issueProject: "p-1"}
	s := New(repo, true)
	if _, err := s.ListPermissionReport(context.Background(), reportSubject(WorkspaceMember), PermissionReportFilter{IssueID: "i-1"}); err != nil {
		t.Fatalf("issue-scoped report: %v", err)
	}
	if repo.lastFilter.ProjectID != "p-1" {
		t.Fatalf("project was not resolved from issue: %+v", repo.lastFilter)
	}
}

func TestPermissionReportRejectsCrossWorkspaceAndInvalidFilters(t *testing.T) {
	repo := &fakeReportRepo{fakeRepo: fakeRepo{workspace: string(WorkspaceAdmin)}}
	s := New(repo, true)
	for name, filter := range map[string]PermissionReportFilter{
		"cross workspace":    {WorkspaceID: "ws-2"},
		"invalid scope":      {Scope: "user"},
		"invalid role":       {Role: "unknown"},
		"invalid permission": {Permission: "project.delete"},
		"negative offset":    {Offset: -1},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := s.ListPermissionReport(context.Background(), reportSubject(WorkspaceAdmin), filter)
			if !errors.Is(err, ErrCrossWorkspace) && !errors.Is(err, ErrInvalidReportFilter) {
				t.Fatalf("got %v", err)
			}
		})
	}
}
