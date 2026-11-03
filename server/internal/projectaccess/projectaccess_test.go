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

// fakeQuerier: private projects p1 and p2; user 1 is an admin, user 2 is a
// member of p1 only, user 3 is a member of nothing, user 4 is not in the
// workspace.
type fakeQuerier struct{}

var (
	p1, p2                                = uid(101), uid(102)
	admin, p1Member, nonMember, notInWork = uid(1), uid(2), uid(3), uid(4)
)

func (fakeQuerier) GetMemberByUserAndWorkspace(_ context.Context, arg db.GetMemberByUserAndWorkspaceParams) (db.Member, error) {
	switch arg.UserID {
	case admin:
		return db.Member{UserID: admin, Role: "admin"}, nil
	case p1Member, nonMember:
		return db.Member{UserID: arg.UserID, Role: "member"}, nil
	}
	return db.Member{}, pgx.ErrNoRows
}

func (fakeQuerier) ListHiddenProjectIDsForUser(_ context.Context, arg db.ListHiddenProjectIDsForUserParams) ([]pgtype.UUID, error) {
	if arg.UserID == p1Member {
		return []pgtype.UUID{p2}, nil
	}
	return []pgtype.UUID{p1, p2}, nil
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
		{"admin sees every project", []pgtype.UUID{admin}, true, true},
		{"member sees their project only", []pgtype.UUID{p1Member}, true, false},
		{"run needs both humans: admin and member", []pgtype.UUID{admin, p1Member}, true, false},
		{"run needs both humans: member and outsider", []pgtype.UUID{p1Member, nonMember}, false, false},
		{"principal outside the workspace sees nothing private", []pgtype.UUID{admin, notInWork}, false, false},
		{"no principal sees nothing private", nil, false, false},
		{"invalid principals are ignored", []pgtype.UUID{{}, p1Member}, true, false},
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
		if !hidden.CanSeeProject(pgtype.UUID{}) {
			t.Errorf("%s: an object outside any project must stay visible", tc.name)
		}
	}
}

func TestHiddenParamIsNilWhenNothingHidden(t *testing.T) {
	hidden, err := ForMember(context.Background(), fakeQuerier{}, uid(200), admin, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if hidden.Param() != nil || !hidden.Empty() {
		t.Fatalf("owner hidden set = %v", hidden.Param())
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
