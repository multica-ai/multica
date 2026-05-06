package handler

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// makeLazyInviteFixture creates a throwaway workspace and returns a
// LazyInviteRules pointing the given domain at it. The caller relies on
// t.Cleanup for teardown.
func makeLazyInviteFixture(t *testing.T, ctx context.Context, domain, slug string) auth.LazyInviteRules {
	t.Helper()
	if testHandler == nil {
		t.Skip("requires DB; TestMain skipped")
	}
	ws, err := testHandler.Queries.CreateWorkspace(ctx, db.CreateWorkspaceParams{
		Name:        "lazy-test-" + slug,
		Slug:        slug,
		IssuePrefix: "LZY",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_ = testHandler.Queries.DeleteWorkspace(context.Background(), ws.ID)
	})
	return auth.LazyInviteRules{
		{Domain: domain, WorkspaceID: ws.ID, WorkspaceSlug: slug},
	}
}

// makeUser creates a throwaway user row. There's no DeleteUser sqlc query,
// so cleanup goes through the test pool directly. ON DELETE CASCADE on
// member rows handles any auto-created memberships.
func makeUser(t *testing.T, ctx context.Context, email string) db.User {
	t.Helper()
	u, err := testHandler.Queries.CreateUser(ctx, db.CreateUserParams{
		Name:          email,
		Email:         email,
		EmailVerified: true,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, u.ID)
	})
	return u
}

func memberCount(t *testing.T, ctx context.Context, userID pgtype.UUID) int64 {
	t.Helper()
	n, err := testHandler.Queries.CountMembershipsByUser(ctx, userID)
	if err != nil {
		t.Fatalf("count memberships: %v", err)
	}
	return n
}

func TestEnsureLazyInviteMembership_NewUserMatchingDomain(t *testing.T) {
	ctx := context.Background()
	if testHandler == nil {
		t.Skip("requires DB")
	}
	rules := makeLazyInviteFixture(t, ctx, "lazyinvite-new.test", "lazy-new")
	u := makeUser(t, ctx, "alice@lazyinvite-new.test")

	saved := testHandler.LazyInvite
	t.Cleanup(func() { testHandler.LazyInvite = saved })
	testHandler.LazyInvite = rules

	testHandler.EnsureLazyInviteMembership(ctx, u, true /*isNew*/)

	if got := memberCount(t, ctx, u.ID); got != 1 {
		t.Fatalf("expected 1 membership, got %d", got)
	}
}

func TestEnsureLazyInviteMembership_NewUserNonMatchingDomain(t *testing.T) {
	ctx := context.Background()
	if testHandler == nil {
		t.Skip("requires DB")
	}
	rules := makeLazyInviteFixture(t, ctx, "lazyinvite-other.test", "lazy-other")
	u := makeUser(t, ctx, "bob@somewhere-else.test")

	saved := testHandler.LazyInvite
	t.Cleanup(func() { testHandler.LazyInvite = saved })
	testHandler.LazyInvite = rules

	testHandler.EnsureLazyInviteMembership(ctx, u, true)

	if got := memberCount(t, ctx, u.ID); got != 0 {
		t.Fatalf("expected 0 memberships, got %d", got)
	}
}

func TestEnsureLazyInviteMembership_ExistingUserZeroMemberships(t *testing.T) {
	ctx := context.Background()
	if testHandler == nil {
		t.Skip("requires DB")
	}
	rules := makeLazyInviteFixture(t, ctx, "lazyinvite-zero.test", "lazy-zero")
	u := makeUser(t, ctx, "carol@lazyinvite-zero.test")

	saved := testHandler.LazyInvite
	t.Cleanup(func() { testHandler.LazyInvite = saved })
	testHandler.LazyInvite = rules

	// isNew=false to simulate a pre-existing user signing in again.
	testHandler.EnsureLazyInviteMembership(ctx, u, false)

	if got := memberCount(t, ctx, u.ID); got != 1 {
		t.Fatalf("expected 1 membership, got %d", got)
	}
}

func TestEnsureLazyInviteMembership_ExistingUserHasMemberships(t *testing.T) {
	ctx := context.Background()
	if testHandler == nil {
		t.Skip("requires DB")
	}
	rules := makeLazyInviteFixture(t, ctx, "lazyinvite-busy.test", "lazy-busy")
	u := makeUser(t, ctx, "dave@lazyinvite-busy.test")

	// Give the user an unrelated membership first.
	otherWS, err := testHandler.Queries.CreateWorkspace(ctx, db.CreateWorkspaceParams{
		Name: "lazy-test-other-busy", Slug: "lazy-test-other-busy", IssuePrefix: "LZY",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = testHandler.Queries.DeleteWorkspace(context.Background(), otherWS.ID) })
	if _, err := testHandler.Queries.CreateMember(ctx, db.CreateMemberParams{
		WorkspaceID: otherWS.ID, UserID: u.ID, Role: "member",
	}); err != nil {
		t.Fatal(err)
	}

	saved := testHandler.LazyInvite
	t.Cleanup(func() { testHandler.LazyInvite = saved })
	testHandler.LazyInvite = rules

	testHandler.EnsureLazyInviteMembership(ctx, u, false)

	// Should still be 1 — the unrelated membership; lazy-invite skipped.
	if got := memberCount(t, ctx, u.ID); got != 1 {
		t.Fatalf("expected 1 membership (unchanged), got %d", got)
	}
}

func TestEnsureLazyInviteMembership_AlreadyMemberOfTarget(t *testing.T) {
	ctx := context.Background()
	if testHandler == nil {
		t.Skip("requires DB")
	}
	rules := makeLazyInviteFixture(t, ctx, "lazyinvite-already.test", "lazy-already")
	u := makeUser(t, ctx, "eve@lazyinvite-already.test")

	// Pre-add the user to the lazy-invite workspace.
	if _, err := testHandler.Queries.CreateMember(ctx, db.CreateMemberParams{
		WorkspaceID: rules[0].WorkspaceID, UserID: u.ID, Role: "member",
	}); err != nil {
		t.Fatal(err)
	}

	saved := testHandler.LazyInvite
	t.Cleanup(func() { testHandler.LazyInvite = saved })
	testHandler.LazyInvite = rules

	// isNew=true should still trigger an attempt — the unique constraint
	// makes it a no-op rather than an error.
	testHandler.EnsureLazyInviteMembership(ctx, u, true)

	if got := memberCount(t, ctx, u.ID); got != 1 {
		t.Fatalf("expected 1 membership, got %d", got)
	}
}

func TestEnsureLazyInviteMembership_NoRulesNoOp(t *testing.T) {
	ctx := context.Background()
	if testHandler == nil {
		t.Skip("requires DB")
	}
	u := makeUser(t, ctx, "frank@anywhere.test")

	saved := testHandler.LazyInvite
	t.Cleanup(func() { testHandler.LazyInvite = saved })
	testHandler.LazyInvite = nil

	testHandler.EnsureLazyInviteMembership(ctx, u, true)

	if got := memberCount(t, ctx, u.ID); got != 0 {
		t.Fatalf("expected 0 memberships, got %d", got)
	}
}
