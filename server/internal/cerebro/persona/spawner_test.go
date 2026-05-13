package persona

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func uuidFromHex(t *testing.T, hex string) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan(hex); err != nil {
		t.Fatalf("scan uuid %q: %v", hex, err)
	}
	return u
}

func TestIssueSpawningUser_PrefersMemberAssignee(t *testing.T) {
	assignee := uuidFromHex(t, "11111111-1111-1111-1111-111111111111")
	creator := uuidFromHex(t, "22222222-2222-2222-2222-222222222222")
	issue := db.Issue{
		AssigneeID:   assignee,
		AssigneeType: pgtype.Text{String: "member", Valid: true},
		CreatorID:    creator,
		CreatorType:  "member",
	}
	got, ok := IssueSpawningUser(issue)
	if !ok {
		t.Fatal("expected member assignee to be resolved")
	}
	if got.Bytes != assignee.Bytes {
		t.Errorf("expected assignee, got %x", got.Bytes)
	}
}

func TestIssueSpawningUser_AgentAssigneeFallsBackToMemberCreator(t *testing.T) {
	assignee := uuidFromHex(t, "33333333-3333-3333-3333-333333333333")
	creator := uuidFromHex(t, "44444444-4444-4444-4444-444444444444")
	issue := db.Issue{
		AssigneeID:   assignee,
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		CreatorID:    creator,
		CreatorType:  "member",
	}
	got, ok := IssueSpawningUser(issue)
	if !ok {
		t.Fatal("expected creator fallback")
	}
	if got.Bytes != creator.Bytes {
		t.Errorf("expected creator, got %x", got.Bytes)
	}
}

func TestIssueSpawningUser_AgentOnlyReturnsFalse(t *testing.T) {
	issue := db.Issue{
		AssigneeID:   uuidFromHex(t, "55555555-5555-5555-5555-555555555555"),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		CreatorID:    uuidFromHex(t, "66666666-6666-6666-6666-666666666666"),
		CreatorType:  "agent",
	}
	if _, ok := IssueSpawningUser(issue); ok {
		t.Fatal("expected agent-only issue to produce no spawning user")
	}
}

func TestIssueSpawningUser_UnsetAssigneeFallsThrough(t *testing.T) {
	creator := uuidFromHex(t, "77777777-7777-7777-7777-777777777777")
	issue := db.Issue{
		// AssigneeID.Valid == false: unassigned issue.
		CreatorID:   creator,
		CreatorType: "member",
	}
	got, ok := IssueSpawningUser(issue)
	if !ok {
		t.Fatal("expected creator fallback on unassigned issue")
	}
	if got.Bytes != creator.Bytes {
		t.Errorf("expected creator, got %x", got.Bytes)
	}
}

// --- ResolveSpawnSubject -------------------------------------------------

type fakeResolver struct {
	groups []pgtype.UUID
	err    error
	calls  int
}

func (f *fakeResolver) ResolveGroupIDs(_ context.Context, _, _ pgtype.UUID) ([]pgtype.UUID, error) {
	f.calls++
	return f.groups, f.err
}

func TestResolveSpawnSubject_PopulatesGroups(t *testing.T) {
	issue := db.Issue{
		WorkspaceID:  uuidFromHex(t, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		AssigneeID:   uuidFromHex(t, "11111111-1111-1111-1111-111111111111"),
		AssigneeType: pgtype.Text{String: "member", Valid: true},
	}
	g1 := uuidFromHex(t, "00000000-0000-0000-0000-00000000000a")
	g2 := uuidFromHex(t, "00000000-0000-0000-0000-00000000000b")
	gp := &fakeResolver{groups: []pgtype.UUID{g1, g2}}

	sub := ResolveSpawnSubject(context.Background(), gp, issue)
	if sub.UserID == "" {
		t.Fatal("expected non-empty user id")
	}
	if len(sub.GroupIDs) != 2 {
		t.Fatalf("expected 2 group ids, got %v", sub.GroupIDs)
	}
	if gp.calls != 1 {
		t.Errorf("resolver should be called exactly once, was %d", gp.calls)
	}
}

func TestResolveSpawnSubject_NoUserSkipsResolve(t *testing.T) {
	issue := db.Issue{
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   uuidFromHex(t, "55555555-5555-5555-5555-555555555555"),
		CreatorType:  "agent",
		CreatorID:    uuidFromHex(t, "66666666-6666-6666-6666-666666666666"),
	}
	gp := &fakeResolver{}
	sub := ResolveSpawnSubject(context.Background(), gp, issue)
	if sub.UserID != "" {
		t.Errorf("agent-only issue should produce empty user id, got %q", sub.UserID)
	}
	if len(sub.GroupIDs) != 0 {
		t.Errorf("agent-only issue should produce no group ids, got %v", sub.GroupIDs)
	}
	if gp.calls != 0 {
		t.Errorf("resolver must not be called when no user is resolvable, was %d", gp.calls)
	}
}

func TestResolveSpawnSubject_FailSoftOnResolverError(t *testing.T) {
	issue := db.Issue{
		WorkspaceID:  uuidFromHex(t, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		AssigneeID:   uuidFromHex(t, "11111111-1111-1111-1111-111111111111"),
		AssigneeType: pgtype.Text{String: "member", Valid: true},
	}
	gp := &fakeResolver{err: errors.New("db down")}
	sub := ResolveSpawnSubject(context.Background(), gp, issue)
	if sub.UserID == "" {
		t.Error("a resolver error must not blank the user id — the claim still proceeds")
	}
	if len(sub.GroupIDs) != 0 {
		t.Errorf("a resolver error must produce no group ids (fail-safe), got %v", sub.GroupIDs)
	}
}

func TestResolveSpawnSubject_NilResolverOK(t *testing.T) {
	issue := db.Issue{
		WorkspaceID:  uuidFromHex(t, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		AssigneeID:   uuidFromHex(t, "11111111-1111-1111-1111-111111111111"),
		AssigneeType: pgtype.Text{String: "member", Valid: true},
	}
	sub := ResolveSpawnSubject(context.Background(), nil, issue)
	if sub.UserID == "" {
		t.Error("nil resolver must still yield the user id from the issue")
	}
	if len(sub.GroupIDs) != 0 {
		t.Errorf("nil resolver yields no groups, got %v", sub.GroupIDs)
	}
}

func TestUUIDString_CanonicalForm(t *testing.T) {
	u := uuidFromHex(t, "11223344-5566-7788-99aa-bbccddeeff00")
	got := uuidString(u)
	want := "11223344-5566-7788-99aa-bbccddeeff00"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
	// Invalid UUID returns empty string.
	if s := uuidString(pgtype.UUID{}); s != "" {
		t.Errorf("invalid uuid should map to empty string, got %q", s)
	}
}
