package projectauth

import (
	"context"
	"testing"
)

type fakeTaskRepo struct {
	fakeRepo
	grants map[string]map[Permission]bool
}

func (f *fakeTaskRepo) IssuePermission(_ context.Context, issueID, projectID, userID string, permission Permission) (bool, error) {
	return f.grants[issueID+":"+projectID+":"+userID][permission], nil
}

func (f *fakeTaskRepo) GrantIssuePermission(_ context.Context, issueID, projectID, userID, _ string, permission Permission) error {
	if f.grants == nil {
		f.grants = map[string]map[Permission]bool{}
	}
	key := issueID + ":" + projectID + ":" + userID
	if f.grants[key] == nil {
		f.grants[key] = map[Permission]bool{}
	}
	f.grants[key][permission] = true
	return nil
}

func (f *fakeTaskRepo) RevokeIssuePermission(_ context.Context, issueID, projectID, userID string, permission Permission) error {
	delete(f.grants[issueID+":"+projectID+":"+userID], permission)
	return nil
}

func (f *fakeTaskRepo) ListIssuePermissions(context.Context, string, string) ([]IssuePermissionRecord, error) {
	return nil, nil
}

func TestIssuePermissionRequiresProjectView(t *testing.T) {
	s := New(&fakeTaskRepo{
		fakeRepo: fakeRepo{workspace: string(WorkspaceMember), project: string(ProjectViewer), projectWorkspace: "ws-1"},
		grants:   map[string]map[Permission]bool{"issue-1:project-1:u-1": {Edit: true}},
	}, true)

	if err := s.CheckIssue(context.Background(), Subject{UserID: "u-1", WorkspaceID: "ws-1"}, "issue-1", "project-1", Edit); err != nil {
		t.Fatalf("viewer with direct edit grant should be allowed: %v", err)
	}
	if err := s.CheckIssue(context.Background(), Subject{UserID: "u-1", WorkspaceID: "ws-1"}, "issue-1", "project-1", IssueManage); err != nil {
		t.Fatalf("task editor should satisfy Multica's shared issue mutation permission: %v", err)
	}

	s = New(&fakeTaskRepo{
		fakeRepo: fakeRepo{workspace: string(WorkspaceMember), project: "", projectWorkspace: "ws-1"},
		grants:   map[string]map[Permission]bool{"issue-1:project-1:u-1": {Edit: true}},
	}, true)
	if err := s.CheckIssue(context.Background(), Subject{UserID: "u-1", WorkspaceID: "ws-1"}, "issue-1", "project-1", Edit); err == nil {
		t.Fatal("direct task grant must not bypass project membership")
	}
}

func TestIssuePermissionGrantOnlySupportsTaskPermissions(t *testing.T) {
	s := New(&fakeTaskRepo{
		fakeRepo: fakeRepo{workspace: string(WorkspaceMember), project: string(ProjectMember), projectWorkspace: "ws-1"},
	}, true)
	if err := s.GrantIssuePermission(context.Background(), Subject{UserID: "u-1", WorkspaceID: "ws-1"}, "issue-1", "project-1", "u-2", View); err == nil {
		t.Fatal("view should not be directly grantable")
	}
}

func TestIssuePermissionDoesNotSurviveProjectMove(t *testing.T) {
	s := New(&fakeTaskRepo{
		fakeRepo: fakeRepo{workspace: string(WorkspaceMember), project: string(ProjectViewer), projectWorkspace: "ws-1"},
		grants:   map[string]map[Permission]bool{"issue-1:old-project:u-1": {Edit: true}},
	}, true)
	if err := s.CheckIssue(context.Background(), Subject{UserID: "u-1", WorkspaceID: "ws-1"}, "issue-1", "new-project", Edit); err == nil {
		t.Fatal("grant from the old project must not authorize the moved issue")
	}
}
