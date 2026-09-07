package issuestatus

import (
	"context"
	"errors"
	"strconv"
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

func (f *fakeQuerier) ListIssueStatusKeysByCategories(_ context.Context, arg db.ListIssueStatusKeysByCategoriesParams) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	wanted := map[string]bool{}
	for _, c := range arg.Categories {
		wanted[c] = true
	}
	var out []string
	for _, e := range f.entries {
		if wanted[e.Category] {
			out = append(out, e.Key)
		}
	}
	return out, nil
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

// takenSet builds the DeriveKey callback from a literal list of keys the
// workspace already owns.
func takenSet(keys ...string) map[string]bool {
	owned := make(map[string]bool, len(keys))
	for _, k := range keys {
		owned[k] = true
	}
	return owned
}

// TestDeriveKeyKeepsTheSlugForSluggableNames pins the behavior an English
// workspace already had: the key stays a readable slug of the name, and the
// fallback introduced for MUL-6749 must not reach names that never needed it.
func TestDeriveKeyKeepsTheSlugForSluggableNames(t *testing.T) {
	cases := map[string]string{
		"Human Review":   "human_review",
		"Gate Approved!": "gate_approved",
		"  Rework  ":     "rework",
		"Waiting — 客户":   "waiting",
	}
	for name, want := range cases {
		got, err := DeriveKey(name, InReview, takenSet())
		if err != nil {
			t.Errorf("DeriveKey(%q) failed: %v", name, err)
			continue
		}
		if got != want {
			t.Errorf("DeriveKey(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestDeriveKeyFallsBackToCategoryForNonLatinNames is the bug this package was
// changed for (MUL-6749 / GitHub #7627): a display name written entirely in a
// non-Latin script has nothing to slug, and used to be REFUSED outright — with
// no field in the settings form to supply a key instead, that made the status
// impossible to create at all.
//
// The fallback is the category plus an ordinal rather than a random suffix, so
// the key still says which platform behavior the status inherits. It starts at
// 2 because the category's own built-in already owns the bare key.
func TestDeriveKeyFallsBackToCategoryForNonLatinNames(t *testing.T) {
	for _, name := range []string{"客户确认", "고객 확인", "確認待ち", "تأكيد العميل", "客戶確認"} {
		got, err := DeriveKey(name, InReview, takenSet())
		if err != nil {
			t.Fatalf("DeriveKey(%q) failed: %v", name, err)
		}
		if got != "in_review_2" {
			t.Errorf("DeriveKey(%q) = %q, want %q", name, got, "in_review_2")
		}
	}

	// A second non-Latin status in the same category takes the next ordinal
	// rather than colliding with the first.
	got, err := DeriveKey("供应商确认", InReview, takenSet("in_review_2"))
	if err != nil {
		t.Fatalf("DeriveKey on a second non-Latin name failed: %v", err)
	}
	if got != "in_review_3" {
		t.Errorf("second non-Latin in_review status = %q, want %q", got, "in_review_3")
	}

	// The ordinal is per category, so a different category starts over.
	got, err = DeriveKey("待排期", Todo, takenSet("in_review_2", "in_review_3"))
	if err != nil {
		t.Fatalf("DeriveKey in another category failed: %v", err)
	}
	if got != "todo_2" {
		t.Errorf("first non-Latin todo status = %q, want %q", got, "todo_2")
	}
}

// TestDeriveKeyEvenWhenTheWorkspaceIsUnseeded covers the rolling-deploy case.
// A workspace whose catalog rows have not landed reports NO keys as taken, so
// the fallback would hand a custom status the bare category key — which is a
// built-in — and let it shadow one. Built-ins count as occupied regardless of
// what the workspace reports.
func TestDeriveKeyEvenWhenTheWorkspaceIsUnseeded(t *testing.T) {
	got, err := DeriveKey("客户确认", InReview, takenSet())
	if err != nil {
		t.Fatalf("DeriveKey on an unseeded workspace failed: %v", err)
	}
	if IsBuiltIn(got) {
		t.Fatalf("DeriveKey returned the built-in key %q on an unseeded workspace", got)
	}
}

// TestDeriveKeyDisambiguatesCollidingSlugs covers the sharper half of #7627:
// the reported workaround was to mix ASCII into the display name, but the
// non-ASCII part is dropped, so two DIFFERENT names collapse onto one slug. The
// second create used to fail on the key's unique index with a message blaming
// the display name, which was not taken at all.
func TestDeriveKeyDisambiguatesCollidingSlugs(t *testing.T) {
	first, err := DeriveKey("待客户 Review", InReview, takenSet())
	if err != nil {
		t.Fatalf("DeriveKey failed: %v", err)
	}
	if first != "review" {
		t.Fatalf("DeriveKey(\"待客户 Review\") = %q, want %q", first, "review")
	}
	second, err := DeriveKey("待供应商 Review", InReview, takenSet(first))
	if err != nil {
		t.Fatalf("DeriveKey on the colliding name failed: %v", err)
	}
	if second != "review_2" {
		t.Errorf("colliding slug resolved to %q, want %q", second, "review_2")
	}
	third, err := DeriveKey("待财务 Review", InReview, takenSet(first, second))
	if err != nil {
		t.Fatalf("DeriveKey on the third colliding name failed: %v", err)
	}
	if third != "review_3" {
		t.Errorf("third colliding slug resolved to %q, want %q", third, "review_3")
	}
}

// TestDeriveKeyCountsArchivedKeysAsTaken is a storage constraint, not a
// preference: idx_issue_status_workspace_key is NOT partial, so an archived
// status still owns its key. Handing it out again would fail on insert.
func TestDeriveKeyCountsArchivedKeysAsTaken(t *testing.T) {
	got, err := DeriveKey("Human Review", InReview, takenSet("human_review"))
	if err != nil {
		t.Fatalf("DeriveKey failed: %v", err)
	}
	if got != "human_review_2" {
		t.Errorf("DeriveKey around an archived key = %q, want %q", got, "human_review_2")
	}
}

// TestDeriveKeyStillRefusesABuiltInSlug keeps the pre-existing guard: a name
// that slugifies ONTO a built-in key is a collision the admin should see, not
// something to silently renumber into a near-duplicate of a built-in status.
func TestDeriveKeyStillRefusesABuiltInSlug(t *testing.T) {
	if _, err := DeriveKey("In Progress", InProgress, takenSet()); err == nil {
		t.Error("a name slugifying to a built-in key should be rejected")
	}
}

// TestDeriveKeyRejectsAnUnknownCategory guards the fallback's own input: it
// builds the key OUT of the category, so an unvalidated one would mint a key
// that the storage CHECK may not even accept.
func TestDeriveKeyRejectsAnUnknownCategory(t *testing.T) {
	if _, err := DeriveKey("客户确认", "started", takenSet()); err == nil {
		t.Error("a non-Latin name in an unknown category should be rejected")
	}
}

// TestDeriveKeyKeepsSuffixedKeysWithinTheStorageLimit pins the truncation: the
// key column caps at 32 characters, so the BASE gives way to the suffix rather
// than the suffix being dropped — dropping it would return a key already taken.
func TestDeriveKeyKeepsSuffixedKeysWithinTheStorageLimit(t *testing.T) {
	long := strings.Repeat("a", 40)
	base, err := DeriveKey(long, InReview, takenSet())
	if err != nil {
		t.Fatalf("DeriveKey on a long name failed: %v", err)
	}
	if len(base) != 32 {
		t.Fatalf("a 40-character name produced a %d-character key %q, want 32", len(base), base)
	}
	suffixed, err := DeriveKey(long, InReview, takenSet(base))
	if err != nil {
		t.Fatalf("DeriveKey on a long colliding name failed: %v", err)
	}
	if len(suffixed) > 32 {
		t.Errorf("suffixed key %q is %d characters, over the 32 the column allows", suffixed, len(suffixed))
	}
	if suffixed == base {
		t.Error("the suffixed key collided with the key it was supposed to avoid")
	}
	if _, err := ValidateKey(suffixed); err != nil {
		t.Errorf("suffixed key %q fails the storage pattern: %v", suffixed, err)
	}
}

// TestDeriveKeyScanIsBoundedByTheCatalogNotAConstant pins that disambiguation
// keeps going as long as the workspace has keys to collide with. An arbitrary
// ceiling would fail with "provide one explicitly" — the exact error a UI with
// no key field cannot act on, which is the whole reason this package changed.
func TestDeriveKeyScanIsBoundedByTheCatalogNotAConstant(t *testing.T) {
	// Every candidate `zz`, `zz_2` … `zz_1200` is already taken, so a fixed
	// 1000-ish bound would give up here.
	keys := []string{"zz"}
	for n := 2; n <= 1200; n++ {
		keys = append(keys, "zz_"+strconv.Itoa(n))
	}
	got, err := DeriveKey("ZZ", Todo, takenSet(keys...))
	if err != nil {
		t.Fatalf("DeriveKey gave up on a large catalog: %v", err)
	}
	if got != "zz_1201" {
		t.Errorf("DeriveKey = %q, want %q", got, "zz_1201")
	}

	// Same for the non-Latin fallback: the ordinal walks past any fixed cap.
	keys = nil
	for n := 2; n <= 1100; n++ {
		keys = append(keys, "todo_"+strconv.Itoa(n))
	}
	got, err = DeriveKey("待排期", Todo, takenSet(keys...))
	if err != nil {
		t.Fatalf("non-Latin fallback gave up on a large catalog: %v", err)
	}
	if got != "todo_1101" {
		t.Errorf("non-Latin fallback = %q, want %q", got, "todo_1101")
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

// ExpandCategories is what keeps the (workspace_id, status) index usable for a
// category filter; wrapping the column in issue_effective_status() instead made
// it a full workspace scan.
func TestExpandCategories(t *testing.T) {
	ctx := context.Background()

	t.Run("no catalog rows still yields the canonical keys", func(t *testing.T) {
		got, err := ExpandCategories(ctx, newFakeQuerier(), testWorkspace, []string{"blocked"})
		if err != nil {
			t.Fatalf("expand: %v", err)
		}
		if len(got) != 1 || got[0] != "blocked" {
			t.Errorf("expand(blocked) on an empty catalog = %v, want [blocked]", got)
		}
	})

	t.Run("includes custom keys and never duplicates the canonical one", func(t *testing.T) {
		q := newFakeQuerier(custom("in_review", InReview), custom("human_review", InReview))
		got, err := ExpandCategories(ctx, q, testWorkspace, []string{"in_review"})
		if err != nil {
			t.Fatalf("expand: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("expand(in_review) = %v, want exactly 2 keys with no duplicate", got)
		}
	})

	t.Run("ignores values that are not categories", func(t *testing.T) {
		got, err := ExpandCategories(ctx, newFakeQuerier(), testWorkspace, []string{"not_a_category"})
		if err != nil {
			t.Fatalf("expand: %v", err)
		}
		if got != nil {
			t.Errorf("expand of a non-category = %v, want nil", got)
		}
	})
}
