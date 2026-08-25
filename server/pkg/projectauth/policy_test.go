package projectauth

import (
	"context"
	"testing"
)

type fakeMemberRepo struct {
	fakeRepo
	addedProject string
	addedUser    string
	addedRole    ProjectRole
}

func (f *fakeMemberRepo) AddProjectMember(_ context.Context, projectID, userID string, role ProjectRole) error {
	f.addedProject, f.addedUser, f.addedRole = projectID, userID, role
	return nil
}

func (f *fakeMemberRepo) RemoveProjectMember(context.Context, string, string) error { return nil }

func (f *fakeMemberRepo) ListProjectMembers(context.Context, string) ([]ProjectMemberRecord, error) {
	return nil, nil
}

type fakeRepo struct {
	workspace, project, projectWorkspace string
	projects                             []string
}

func (f fakeRepo) WorkspaceRole(context.Context, string, string) (WorkspaceRole, error) {
	return WorkspaceRole(f.workspace), nil
}
func (f fakeRepo) ProjectRole(context.Context, string, string) (ProjectRole, error) {
	return ProjectRole(f.project), nil
}
func (f fakeRepo) ProjectWorkspace(context.Context, string) (string, error) {
	return f.projectWorkspace, nil
}
func (f fakeRepo) VisibleProjectIDs(context.Context, string, string) ([]string, error) {
	return f.projects, nil
}

func TestPolicyInheritanceAndRoles(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		ws   WorkspaceRole
		pr   ProjectRole
		p    Permission
		ok   bool
	}{
		{"owner bypass", WorkspaceOwner, "", SettingsManage, true},
		{"admin bypass", WorkspaceAdmin, "", MemberManage, true},
		{"viewer read", WorkspaceMember, ProjectViewer, View, true},
		{"viewer cannot edit", WorkspaceMember, ProjectViewer, Edit, false},
		{"member creates issue", WorkspaceMember, ProjectMember, IssueCreate, true},
		{"member cannot manage members", WorkspaceMember, ProjectMember, MemberManage, false},
		{"manager manages issue", WorkspaceMember, ProjectManager, IssueManage, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New(fakeRepo{workspace: string(tc.ws), project: string(tc.pr), projectWorkspace: "ws-1"}, true)
			err := s.Check(ctx, Subject{UserID: "u-1", WorkspaceID: "ws-1"}, "p-1", tc.p)
			if (err == nil) != tc.ok {
				t.Fatalf("got %v, want allowed=%v", err, tc.ok)
			}
		})
	}
}

func TestDisabledPreservesLegacyBehavior(t *testing.T) {
	s := New(nil, false)
	if err := s.Check(context.Background(), Subject{}, "", Edit); err != nil {
		t.Fatal(err)
	}
}

func TestEnabledWithoutRepositoryFailsClosed(t *testing.T) {
	s := New(nil, true)
	if err := s.Check(context.Background(), Subject{UserID: "u", WorkspaceID: "w"}, "p", View); err != ErrDisabled {
		t.Fatalf("got %v, want %v", err, ErrDisabled)
	}
	if _, err := s.Scope(context.Background(), Subject{UserID: "u", WorkspaceID: "w"}); err != ErrDisabled {
		t.Fatalf("scope got %v, want %v", err, ErrDisabled)
	}
}

func TestEnsureOwnerSeedsCreator(t *testing.T) {
	repo := &fakeMemberRepo{fakeRepo: fakeRepo{workspace: string(WorkspaceMember), projectWorkspace: "ws-1"}}
	s := New(repo, true)
	if err := s.EnsureOwner(context.Background(), "p-1", "u-1"); err != nil {
		t.Fatal(err)
	}
	if repo.addedProject != "p-1" || repo.addedUser != "u-1" || repo.addedRole != ProjectOwner {
		t.Fatalf("unexpected seed: project=%q user=%q role=%q", repo.addedProject, repo.addedUser, repo.addedRole)
	}
}
