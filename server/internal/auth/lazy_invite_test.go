package auth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// uuidLit returns a valid pgtype.UUID for tests. The exact bytes don't matter,
// only that .Valid is true and the value round-trips through generated code.
func uuidLit(t *testing.T, hex string) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan(hex); err != nil {
		t.Fatal(err)
	}
	return u
}

func TestLazyInviteMatch_ExactCaseInsensitive(t *testing.T) {
	rules := LazyInviteRules{
		{Domain: "spendbase.com", WorkspaceID: uuidLit(t, "11111111-1111-1111-1111-111111111111"), WorkspaceSlug: "spendbase"},
	}
	rule, ok := rules.Match("Alice@SpendBase.COM")
	if !ok {
		t.Fatal("expected match")
	}
	if rule.WorkspaceSlug != "spendbase" {
		t.Fatalf("got slug=%q", rule.WorkspaceSlug)
	}
}

func TestLazyInviteMatch_NoSubdomain(t *testing.T) {
	rules := LazyInviteRules{
		{Domain: "spendbase.com", WorkspaceID: uuidLit(t, "11111111-1111-1111-1111-111111111111"), WorkspaceSlug: "spendbase"},
	}
	if _, ok := rules.Match("bob@dev.spendbase.com"); ok {
		t.Fatal("subdomain must not match")
	}
}

func TestLazyInviteMatch_NoMatch(t *testing.T) {
	rules := LazyInviteRules{
		{Domain: "spendbase.com", WorkspaceID: uuidLit(t, "11111111-1111-1111-1111-111111111111"), WorkspaceSlug: "spendbase"},
	}
	if _, ok := rules.Match("alice@other.com"); ok {
		t.Fatal("non-matching domain must not match")
	}
	if _, ok := rules.Match("not-an-email"); ok {
		t.Fatal("malformed email must not match")
	}
}

func TestLazyInviteIsAllowedDomain(t *testing.T) {
	rules := LazyInviteRules{
		{Domain: "spendbase.com", WorkspaceID: uuidLit(t, "11111111-1111-1111-1111-111111111111"), WorkspaceSlug: "spendbase"},
	}
	if !rules.IsAllowedDomain("alice@spendbase.com") {
		t.Fatal("expected true for matching domain")
	}
	if rules.IsAllowedDomain("alice@other.com") {
		t.Fatal("expected false for non-matching domain")
	}
}

// fakeSlugResolver implements just enough of the queries surface to exercise
// ParseLazyInviteRules. We can't use db.Queries directly because we don't
// want to spin up Postgres for unit tests.
type fakeSlugResolver struct {
	bySlug map[string]pgtype.UUID
}

func (f *fakeSlugResolver) GetWorkspaceBySlug(ctx context.Context, slug string) (db.Workspace, error) {
	uid, ok := f.bySlug[slug]
	if !ok {
		return db.Workspace{}, pgx.ErrNoRows
	}
	return db.Workspace{ID: uid, Slug: slug, Name: slug}, nil
}

func TestParseLazyInviteRules_Empty(t *testing.T) {
	rules, err := ParseLazyInviteRules(context.Background(), "", &fakeSlugResolver{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 0 {
		t.Fatalf("expected empty rules, got %d", len(rules))
	}
}

func TestParseLazyInviteRules_TwoValidRules(t *testing.T) {
	uid1 := uuidLit(t, "11111111-1111-1111-1111-111111111111")
	uid2 := uuidLit(t, "22222222-2222-2222-2222-222222222222")
	resolver := &fakeSlugResolver{bySlug: map[string]pgtype.UUID{
		"spendbase": uid1,
		"acme-team": uid2,
	}}
	rules, err := ParseLazyInviteRules(
		context.Background(),
		"spendbase.com:spendbase, acme.com :acme-team",
		resolver,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 {
		t.Fatalf("got %d rules", len(rules))
	}
	if rules[0].Domain != "spendbase.com" {
		t.Fatalf("domain[0]=%q", rules[0].Domain)
	}
	if rules[1].Domain != "acme.com" {
		t.Fatalf("domain[1]=%q", rules[1].Domain)
	}
	if rules[1].WorkspaceSlug != "acme-team" {
		t.Fatalf("slug[1]=%q", rules[1].WorkspaceSlug)
	}
}

func TestParseLazyInviteRules_MalformedPair(t *testing.T) {
	_, err := ParseLazyInviteRules(context.Background(), "missing-colon", &fakeSlugResolver{})
	if err == nil || !strings.Contains(err.Error(), "must be") {
		t.Fatalf("expected format error, got %v", err)
	}
}

func TestParseLazyInviteRules_EmptyHalf(t *testing.T) {
	_, err := ParseLazyInviteRules(context.Background(), ":spendbase", &fakeSlugResolver{})
	if err == nil {
		t.Fatal("expected error for empty domain")
	}
	_, err = ParseLazyInviteRules(context.Background(), "spendbase.com:", &fakeSlugResolver{})
	if err == nil {
		t.Fatal("expected error for empty slug")
	}
}

func TestParseLazyInviteRules_DomainNeedsDot(t *testing.T) {
	resolver := &fakeSlugResolver{bySlug: map[string]pgtype.UUID{"spendbase": uuidLit(t, "11111111-1111-1111-1111-111111111111")}}
	_, err := ParseLazyInviteRules(context.Background(), "localhost:spendbase", resolver)
	if err == nil || !strings.Contains(err.Error(), "domain") {
		t.Fatalf("expected domain sanity error, got %v", err)
	}
}

func TestParseLazyInviteRules_DuplicateDomain(t *testing.T) {
	uid := uuidLit(t, "11111111-1111-1111-1111-111111111111")
	resolver := &fakeSlugResolver{bySlug: map[string]pgtype.UUID{
		"spendbase": uid,
		"acme-team": uid,
	}}
	_, err := ParseLazyInviteRules(
		context.Background(),
		"spendbase.com:spendbase,SpendBase.com:acme-team",
		resolver,
	)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate-domain error, got %v", err)
	}
}

func TestParseLazyInviteRules_UnknownSlug(t *testing.T) {
	resolver := &fakeSlugResolver{} // no slugs registered
	_, err := ParseLazyInviteRules(context.Background(), "spendbase.com:nope", resolver)
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("expected unknown-slug error mentioning slug, got %v", err)
	}
}

// errResolver simulates a real DB error (not ErrNoRows).
type errResolver struct{}

func (errResolver) GetWorkspaceBySlug(ctx context.Context, slug string) (db.Workspace, error) {
	return db.Workspace{}, errors.New("connection refused")
}

func TestParseLazyInviteRules_DBError(t *testing.T) {
	_, err := ParseLazyInviteRules(context.Background(), "spendbase.com:spendbase", errResolver{})
	if err == nil || !strings.Contains(err.Error(), "connection") {
		t.Fatalf("expected DB error to surface, got %v", err)
	}
}
