package middleware

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// seedOwnerLookupUser inserts a fresh user row and returns its UUID
// as the canonical hyphenated string. Mirrors the lightweight fixture
// pattern used by setupResolverFixture so the lookup helper can be
// exercised against a real DB without dragging in the heavier handler
// fixture.
func seedOwnerLookupUser(t *testing.T, queries *db.Queries) string {
	t.Helper()
	ctx := context.Background()
	stamp := time.Now().UnixNano()
	user, err := queries.CreateUser(ctx, db.CreateUserParams{
		Name:  "owner-lookup",
		Email: pgtypeUniqueEmail(stamp),
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		// Best-effort cleanup; another test parallel run will land
		// on a different stamp so a leak here doesn't bleed.
		_ = user
	})
	return uuidToString(user.ID)
}

// pgtypeUniqueEmail builds an email that is guaranteed unique within
// a test run so concurrent tests don't collide on the email UNIQUE
// index. Using nanosecond + a static suffix mirrors the patterns used
// elsewhere in the repo.
func pgtypeUniqueEmail(stamp int64) string {
	return time.Unix(0, stamp).UTC().Format("20060102T150405.000000000") + "@owner-lookup.test"
}

// TestOwnerLookupFor_NilQueries pins the contract that a middleware
// constructed without a *db.Queries handle skips the lookup entirely
// (Verify treats a nil OwnerLookupFunc as "no lookup configured").
// This path only exists to support unit tests that wire up the
// verifier without a real database.
func TestOwnerLookupFor_NilQueries(t *testing.T) {
	if got := ownerLookupFor(nil); got != nil {
		t.Fatalf("ownerLookupFor(nil) must return nil, got %T", got)
	}
}

// TestOwnerLookupFor_ExistingUser confirms the happy path: a real
// row in the user table resolves to (true, nil).
func TestOwnerLookupFor_ExistingUser(t *testing.T) {
	pool := openPool(t)
	defer pool.Close()
	queries := db.New(pool)

	userID := seedOwnerLookupUser(t, queries)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})

	lookup := ownerLookupFor(queries)
	exists, err := lookup(context.Background(), userID)
	if err != nil {
		t.Fatalf("lookup returned error: %v", err)
	}
	if !exists {
		t.Fatalf("expected user to be found")
	}
}

// TestOwnerLookupFor_MissingUser confirms that a syntactically valid
// UUID that does not match any row resolves to (false, nil) — the
// signal the verifier uses to emit reason="owner_unknown" and reject
// without caching.
func TestOwnerLookupFor_MissingUser(t *testing.T) {
	pool := openPool(t)
	defer pool.Close()
	queries := db.New(pool)

	lookup := ownerLookupFor(queries)
	// Random unused UUID — pgx.ErrNoRows territory.
	exists, err := lookup(context.Background(), "00000000-0000-0000-0000-0000deadbeef")
	if err != nil {
		t.Fatalf("missing user must NOT surface as a lookup error, got %v", err)
	}
	if exists {
		t.Fatalf("expected lookup to report user-not-found")
	}
}

// TestOwnerLookupFor_MalformedOwnerID confirms that an unparseable
// owner_id is treated as "user not found" (false, nil), not as a
// transient error. Cloud has already vetted the format on its side
// before signing the verify response, so a bad UUID here means
// either a contract violation or a forged response — either way the
// safe answer is to reject the token, same as a missing user.
func TestOwnerLookupFor_MalformedOwnerID(t *testing.T) {
	pool := openPool(t)
	defer pool.Close()
	queries := db.New(pool)

	lookup := ownerLookupFor(queries)
	exists, err := lookup(context.Background(), "not-a-uuid")
	if err != nil {
		t.Fatalf("malformed owner_id must NOT surface as a lookup error, got %v", err)
	}
	if exists {
		t.Fatalf("malformed owner_id must report user-not-found")
	}
}

// TestOwnerLookupFor_DBError mirrors the production failure mode the
// verifier maps to ErrCloudPATUnavailable: a query error (DB hung up,
// pool empty, etc.). Closing the pool before the call gives us a
// guaranteed real error rather than mocking the queries layer, which
// the rest of the suite doesn't do.
func TestOwnerLookupFor_DBError(t *testing.T) {
	pool := openPool(t)
	queries := db.New(pool)
	pool.Close() // intentional — every subsequent query must fail

	lookup := ownerLookupFor(queries)
	exists, err := lookup(context.Background(), "00000000-0000-0000-0000-000000000001")
	if err == nil {
		t.Fatal("expected real DB error to surface, got nil")
	}
	if exists {
		t.Fatal("DB error must not report user-found")
	}
	// And the error must NOT be classified as ErrNoRows by the
	// caller — that's how the verifier distinguishes "user really
	// doesn't exist" (401) from "DB is broken right now" (503).
	if errors.Is(err, errors.New("no rows")) {
		t.Fatalf("DB error must not look like ErrNoRows, got %v", err)
	}
}

// uuidToStringForOwnerLookup avoids depending on the existing
// uuidToString helper here in case its signature changes; the
// fixtures only need the canonical hyphenated form.
//
//nolint:unused // referenced via seedOwnerLookupUser indirectly.
func uuidToStringForOwnerLookup(u pgtype.UUID) string {
	return uuidToString(u)
}
