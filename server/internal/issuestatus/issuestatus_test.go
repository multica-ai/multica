package issuestatus

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var testWorkspace = pgtype.UUID{Bytes: [16]byte{1}, Valid: true}

// fakeQuerier is an in-memory catalog keyed by (workspace, key). It records
// lookups so a test can assert that the built-in fast path issues NO query.
type fakeQuerier struct {
	entries map[string]db.IssueStatus
	lookups int
	lists   int
	err     error
}

func newFakeQuerier(entries ...db.IssueStatus) *fakeQuerier {
	m := make(map[string]db.IssueStatus, len(entries))
	for _, e := range entries {
		m[e.Key] = e
	}
	return &fakeQuerier{entries: m}
}

func (f *fakeQuerier) GetIssueStatusEntryByKey(_ context.Context, arg db.GetIssueStatusEntryByKeyParams) (db.IssueStatus, error) {
	f.lookups++
	if f.err != nil {
		return db.IssueStatus{}, f.err
	}
	entry, ok := f.entries[arg.Key]
	if !ok {
		return db.IssueStatus{}, pgx.ErrNoRows
	}
	return entry, nil
}

func (f *fakeQuerier) ListIssueStatusEntries(_ context.Context, _ db.ListIssueStatusEntriesParams) ([]db.IssueStatus, error) {
	f.lists++
	if f.err != nil {
		return nil, f.err
	}
	out := make([]db.IssueStatus, 0, len(f.entries))
	for _, e := range f.entries {
		if e.ArchivedAt.Valid {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func (f *fakeQuerier) SeedIssueStatusEntries(_ context.Context, _ pgtype.UUID) error {
	return f.err
}

func custom(key, category string) db.IssueStatus {
	return db.IssueStatus{Key: key, Category: category, WorkspaceID: testWorkspace}
}

// TestEffectiveIsIdentityOnBuiltInsWithoutQuerying is the load-bearing
// guarantee of MUL-6243: every pre-existing status check keeps its exact
// meaning, and no existing code path gains a database round trip.
func TestEffectiveIsIdentityOnBuiltInsWithoutQuerying(t *testing.T) {
	q := newFakeQuerier()
	for _, key := range Canonical() {
		if got := Effective(context.Background(), q, testWorkspace, key); got != key {
			t.Errorf("Effective(%q) = %q, want the key unchanged", key, got)
		}
	}
	if q.lookups != 0 {
		t.Errorf("built-in resolution made %d catalog lookups, want 0", q.lookups)
	}
}

func TestEffectiveMapsCustomStatusToItsCategory(t *testing.T) {
	q := newFakeQuerier(
		custom("human_review", InReview),
		custom("rework", Todo),
		custom("gate_approved", Done),
		custom("waiting_on_customer", Blocked),
	)

	cases := map[string]string{
		"human_review":        InReview,
		"rework":              Todo,
		"gate_approved":       Done,
		"waiting_on_customer": Blocked,
	}
	for key, want := range cases {
		if got := Effective(context.Background(), q, testWorkspace, key); got != want {
			t.Errorf("Effective(%q) = %q, want %q", key, got, want)
		}
	}
}

// An unresolvable key must resolve to itself, not to a guess. Returning a
// canonical key here would let an unknown status trigger an agent, finalize an
// autopilot run, or be swept back to todo.
func TestEffectiveFailsSafeOnUnknownAndBrokenCatalog(t *testing.T) {
	t.Run("absent from catalog", func(t *testing.T) {
		q := newFakeQuerier()
		if got := Effective(context.Background(), q, testWorkspace, "ghost"); got != "ghost" {
			t.Errorf("Effective(ghost) = %q, want %q", got, "ghost")
		}
	})

	t.Run("database error", func(t *testing.T) {
		q := newFakeQuerier()
		q.err = errors.New("connection refused")
		if got := Effective(context.Background(), q, testWorkspace, "human_review"); got != "human_review" {
			t.Errorf("Effective on DB error = %q, want the key unchanged", got)
		}
	})

	t.Run("corrupt category", func(t *testing.T) {
		q := newFakeQuerier(custom("weird", "not_a_category"))
		if got := Effective(context.Background(), q, testWorkspace, "weird"); got != "weird" {
			t.Errorf("Effective with bad category = %q, want the key unchanged", got)
		}
	})
}

// The catalog EXTENDS the built-in statuses rather than defining them, so all
// 7 must resolve even with an entirely empty catalog. Requiring a row here was
// a real regression: it made every issue write fail in a workspace whose seed
// had not landed yet.
func TestResolveAcceptsBuiltInsWithoutACatalogRow(t *testing.T) {
	q := newFakeQuerier()
	for _, key := range Canonical() {
		entry, err := Resolve(context.Background(), q, testWorkspace, key)
		if err != nil {
			t.Errorf("Resolve(%q) with an empty catalog failed: %v", key, err)
			continue
		}
		if entry.Key != key || entry.Category != key {
			t.Errorf("synthesized entry for %q = {key:%q category:%q}, want both %q",
				key, entry.Key, entry.Category, key)
		}
	}
	// Failing open is scoped to the built-ins; a custom key still needs a row.
	if _, err := Resolve(context.Background(), q, testWorkspace, "human_review"); !errors.Is(err, ErrUnknownStatus) {
		t.Errorf("a custom key with no row = %v, want ErrUnknownStatus", err)
	}
}

func TestResolveRejectsUnknownAndArchived(t *testing.T) {
	archived := custom("retired", InProgress)
	archived.ArchivedAt = pgtype.Timestamptz{Valid: true}
	q := newFakeQuerier(custom("human_review", InReview), archived)

	if _, err := Resolve(context.Background(), q, testWorkspace, "human_review"); err != nil {
		t.Errorf("Resolve of an active status failed: %v", err)
	}
	if _, err := Resolve(context.Background(), q, testWorkspace, "ghost"); !errors.Is(err, ErrUnknownStatus) {
		t.Errorf("Resolve of an unknown status = %v, want ErrUnknownStatus", err)
	}
	if _, err := Resolve(context.Background(), q, testWorkspace, "retired"); !errors.Is(err, ErrUnknownStatus) {
		t.Errorf("Resolve of an archived status = %v, want ErrUnknownStatus", err)
	}
	if _, err := Resolve(context.Background(), q, testWorkspace, "  "); !errors.Is(err, ErrUnknownStatus) {
		t.Errorf("Resolve of a blank status = %v, want ErrUnknownStatus", err)
	}
}

// Every category must be the key of a built-in, and every built-in the name of
// a category. This one-to-one correspondence is what lets Effective return
// entry.Category directly as a status key, with no mapping step.
func TestCategoriesAndBuiltInsAreTheSameSet(t *testing.T) {
	for _, key := range Canonical() {
		if !IsCategory(key) {
			t.Errorf("built-in %q is not a valid category", key)
		}
	}
	if len(Canonical()) != 7 {
		t.Fatalf("expected 7 canonical statuses, got %d", len(Canonical()))
	}
}

// The display order is copied from the frontend's historical STATUS_ORDER.
// Reordering it would visibly rearrange every existing user's board, so it is
// pinned here rather than left to look tidy.
func TestCategoryRankPreservesHistoricalStatusOrder(t *testing.T) {
	want := []string{"backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"}
	got := Canonical()
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Canonical()[%d] = %q, want %q (order matches frontend STATUS_ORDER)", i, got[i], want[i])
		}
		if CategoryRank(want[i]) != i {
			t.Errorf("CategoryRank(%q) = %d, want %d", want[i], CategoryRank(want[i]), i)
		}
	}
	if CategoryRank("not_a_category") != len(want) {
		t.Errorf("an unknown category should sort last, got rank %d", CategoryRank("not_a_category"))
	}
}

func TestValidateKeyRejectsReservedAndMalformed(t *testing.T) {
	for _, key := range Canonical() {
		if _, err := ValidateKey(key); err == nil {
			t.Errorf("built-in key %q must be reserved against reuse", key)
		}
	}
	for _, key := range []string{"", "  ", "In Review", "in-review", "_leading", "Ünicode", strings.Repeat("a", 33)} {
		if _, err := ValidateKey(key); err == nil {
			t.Errorf("malformed key %q should be rejected", key)
		}
	}
	got, err := ValidateKey("  Human_Review  ")
	if err != nil {
		t.Fatalf("ValidateKey on a well-formed key failed: %v", err)
	}
	if got != "human_review" {
		t.Errorf("ValidateKey normalized to %q, want %q", got, "human_review")
	}
}

func TestSlugifyKey(t *testing.T) {
	cases := map[string]string{
		"Human Review":   "human_review",
		"Gate Approved!": "gate_approved",
		"  Rework  ":     "rework",
		"Waiting — 客户":   "waiting",
	}
	for name, want := range cases {
		got, err := SlugifyKey(name)
		if err != nil {
			t.Errorf("SlugifyKey(%q) failed: %v", name, err)
			continue
		}
		if got != want {
			t.Errorf("SlugifyKey(%q) = %q, want %q", name, got, want)
		}
	}

	// A name that slugifies onto a built-in key must be refused, not silently
	// shadow the built-in.
	if _, err := SlugifyKey("In Progress"); err == nil {
		t.Error("a name slugifying to a built-in key should be rejected")
	}
	if _, err := SlugifyKey("客户"); err == nil {
		t.Error("a name with no slug-able characters should be rejected")
	}
}

// TestResolverAmortizesTheCatalogRead pins the list-endpoint guarantee: built-in
// statuses still cost nothing, and N custom rows cost ONE catalog read rather
// than one lookup per row.
func TestResolverAmortizesTheCatalogRead(t *testing.T) {
	q := newFakeQuerier(
		custom("human_review", InReview),
		custom("gate_approved", Done),
	)
	r := NewResolver(testWorkspace)
	ctx := context.Background()

	// Built-ins: no catalog access at all, however many times they are seen.
	for range 50 {
		for _, key := range Canonical() {
			if got := r.Effective(ctx, q, key); got != key {
				t.Fatalf("Effective(%q) = %q, want the key unchanged", key, got)
			}
		}
	}
	if q.lists != 0 || q.lookups != 0 {
		t.Errorf("built-in resolution touched the catalog: %d list(s), %d lookup(s)", q.lists, q.lookups)
	}

	// Custom keys: the catalog is read once and reused.
	for range 50 {
		if got := r.Effective(ctx, q, "human_review"); got != InReview {
			t.Fatalf("Effective(human_review) = %q, want %q", got, InReview)
		}
		if got := r.Effective(ctx, q, "gate_approved"); got != Done {
			t.Fatalf("Effective(gate_approved) = %q, want %q", got, Done)
		}
	}
	if q.lists != 1 {
		t.Errorf("catalog read %d times, want exactly 1", q.lists)
	}
	if q.lookups != 0 {
		t.Errorf("Resolver made %d per-key lookups, want 0", q.lookups)
	}

	// Unknown keys stay fail-safe and do not trigger a re-read.
	if got := r.Effective(ctx, q, "ghost"); got != "ghost" {
		t.Errorf("Effective(ghost) = %q, want the key unchanged", got)
	}
	if q.lists != 1 {
		t.Errorf("an unknown key re-read the catalog: %d reads", q.lists)
	}
}

// A catalog read failure must degrade to the fail-safe identity rather than
// guessing a category.
func TestResolverFailsSafeWhenTheCatalogReadFails(t *testing.T) {
	q := newFakeQuerier()
	q.err = errors.New("connection refused")
	r := NewResolver(testWorkspace)
	if got := r.Effective(context.Background(), q, "human_review"); got != "human_review" {
		t.Errorf("Effective on a failed catalog read = %q, want the key unchanged", got)
	}
}
