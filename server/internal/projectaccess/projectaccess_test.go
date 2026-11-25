package projectaccess

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func uid(b byte) pgtype.UUID {
	var id pgtype.UUID
	id.Bytes[15] = b
	id.Valid = true
	return id
}

// fakeQuerier: private projects p1 and p2. User 1 is a member of both, user 2
// of p1 only, user 3 of neither. User 5 is a workspace admin and user 6 the
// workspace owner, members of neither. User 4 is not in the workspace. User 7
// is a guest of p1, and p3 is a workspace-visible project.
type fakeQuerier struct{}

var (
	p1, p2, p3                            = uid(101), uid(102), uid(103)
	bothMember, p1Member, nonMember       = uid(1), uid(2), uid(3)
	notInWork, outsideAdmin, outsideOwner = uid(4), uid(5), uid(6)
	p1Guest                               = uid(7)
)

func (fakeQuerier) GetMemberByUserAndWorkspace(_ context.Context, arg db.GetMemberByUserAndWorkspaceParams) (db.Member, error) {
	switch arg.UserID {
	case outsideOwner:
		return db.Member{UserID: arg.UserID, Role: "owner"}, nil
	case outsideAdmin:
		return db.Member{UserID: arg.UserID, Role: "admin"}, nil
	case bothMember, p1Member, nonMember:
		return db.Member{UserID: arg.UserID, Role: "member"}, nil
	case p1Guest:
		return db.Member{UserID: arg.UserID, Role: RoleGuest}, nil
	}
	return db.Member{}, pgx.ErrNoRows
}

func (fakeQuerier) ListHiddenProjectIDsForUser(_ context.Context, arg db.ListHiddenProjectIDsForUserParams) ([]pgtype.UUID, error) {
	switch arg.UserID {
	case bothMember:
		return nil, nil
	case p1Member:
		return []pgtype.UUID{p2}, nil
	}
	return []pgtype.UUID{p1, p2}, nil
}

func (fakeQuerier) ListHiddenProjectIDsForGuest(_ context.Context, arg db.ListHiddenProjectIDsForGuestParams) ([]pgtype.UUID, error) {
	if arg.UserID == p1Guest {
		return []pgtype.UUID{p2, p3}, nil
	}
	return []pgtype.UUID{p1, p2, p3}, nil
}

func (fakeQuerier) ListPrivateProjectIDs(context.Context, pgtype.UUID) ([]pgtype.UUID, error) {
	return []pgtype.UUID{p1, p2}, nil
}

func TestForPrincipals(t *testing.T) {
	ws := uid(200)
	for _, tc := range []struct {
		name       string
		principals []pgtype.UUID
		seeP1      bool
		seeP2      bool
	}{
		{"member of both sees both", []pgtype.UUID{bothMember}, true, true},
		{"member sees their project only", []pgtype.UUID{p1Member}, true, false},
		{"workspace admin outside a project does not see it", []pgtype.UUID{outsideAdmin}, false, false},
		{"workspace owner outside a project does not see it", []pgtype.UUID{outsideOwner}, false, false},
		{"run needs both humans: two members", []pgtype.UUID{bothMember, p1Member}, true, false},
		{"run needs both humans: member and outsider", []pgtype.UUID{p1Member, nonMember}, false, false},
		{"run needs both humans: member and outside admin", []pgtype.UUID{bothMember, outsideAdmin}, false, false},
		{"principal outside the workspace sees nothing private", []pgtype.UUID{bothMember, notInWork}, false, false},
		{"no principal sees nothing private", nil, false, false},
		{"invalid principals are ignored", []pgtype.UUID{{}, p1Member}, true, false},
		{"guest sees its project only", []pgtype.UUID{p1Guest}, true, false},
	} {
		hidden, err := ForPrincipals(context.Background(), fakeQuerier{}, ws, tc.principals...)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := hidden.CanSeeProject(p1); got != tc.seeP1 {
			t.Errorf("%s: p1 visible = %v, want %v", tc.name, got, tc.seeP1)
		}
		if got := hidden.CanSeeProject(p2); got != tc.seeP2 {
			t.Errorf("%s: p2 visible = %v, want %v", tc.name, got, tc.seeP2)
		}
		if tc.principals != nil && tc.principals[0] == p1Guest {
			continue
		}
		if !hidden.CanSeeProject(pgtype.UUID{}) {
			t.Errorf("%s: an object outside any project must stay visible", tc.name)
		}
	}
}

func TestHiddenParamIsNilWhenNothingHidden(t *testing.T) {
	hidden, err := ForMember(context.Background(), fakeQuerier{}, db.Member{WorkspaceID: uid(200), UserID: bothMember, Role: "member"})
	if err != nil {
		t.Fatal(err)
	}
	if hidden.Param() != nil || !hidden.Empty() {
		t.Fatalf("hidden set of a member of every project = %v", hidden.Param())
	}
	if hidden.IDs() == nil {
		t.Fatal("IDs must never be nil")
	}
}

func TestRunPrincipalsFallsBackToAccountableUser(t *testing.T) {
	owner := uid(9)
	got := RunPrincipals(db.AgentTaskQueue{AccountableUserID: owner}, owner)
	if got[0] != owner || got[1] != owner {
		t.Fatalf("owner_fallback run principals = %v", got)
	}
	got = RunPrincipals(db.AgentTaskQueue{OriginatorUserID: p1Member, AccountableUserID: owner}, owner)
	if got[0] != p1Member {
		t.Fatalf("originator must win over accountable: %v", got)
	}
}

func TestGuestSeesOnlyItsProjects(t *testing.T) {
	hidden, err := ForMember(context.Background(), fakeQuerier{}, db.Member{WorkspaceID: uid(200), UserID: p1Guest, Role: RoleGuest})
	if err != nil {
		t.Fatal(err)
	}
	if !hidden.CanSeeProject(p1) {
		t.Error("a guest must see its project")
	}
	if hidden.CanSeeProject(p2) || hidden.CanSeeProject(p3) {
		t.Error("a guest must not see other projects, private or not")
	}
	if hidden.CanSeeProject(pgtype.UUID{}) {
		t.Error("a guest must not see objects outside any project")
	}
	found := false
	for _, id := range hidden.Param() {
		found = found || id == NoProject
	}
	if !found {
		t.Error("the SQL parameter must carry NoProject so project-less rows are filtered")
	}
}

func TestGuestBoundsARunEvenWithoutPrivateProjects(t *testing.T) {
	hidden, err := ForPrincipals(context.Background(), noPrivateQuerier{}, uid(200), bothMember, p1Guest)
	if err != nil {
		t.Fatal(err)
	}
	if hidden.CanSeeProject(p3) || hidden.CanSeeProject(pgtype.UUID{}) {
		t.Error("a guest principal must narrow the viewer in a workspace without private projects")
	}
}

type noPrivateQuerier struct{ fakeQuerier }

func (noPrivateQuerier) ListPrivateProjectIDs(context.Context, pgtype.UUID) ([]pgtype.UUID, error) {
	return nil, nil
}
