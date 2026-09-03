// Package issuestatus owns the per-workspace issue status catalog (MUL-6243).
//
// MODEL. There are 7 categories and they map one-to-one onto the 7 built-in
// statuses: a category's value IS its canonical status key. A custom status
// declares a category and inherits that canonical's platform behavior in full.
//
// That one-to-one correspondence is what makes this cheap. Category is defined
// as a BEHAVIOR EQUIVALENCE CLASS — two statuses share a category only when the
// platform treats them identically — and by that definition the 7 built-ins are
// 7 distinct classes: in_progress, in_review and blocked differ on whether they
// finalize an autopilot run, whether they notify a delegated subscriber,
// whether they dismiss stale task_failed inbox rows, and whether the stuck-issue
// sweeper resets them. Collapsing them into one "started" category (the Linear
// model) is what would force a second per-status "behaves_as" concept to
// re-express the difference, and with it the possibility of a status that
// inherits only half a behavior.
//
// CONSEQUENCES. Effective is the identity function on built-in keys, so every
// existing `issue.Status == "todo"` comparison keeps its exact meaning and
// `issue.status` stays the authoritative TEXT column: no status_id, no
// backfill, no double-write. Behavior changes only on custom statuses, a set
// that is empty until an admin creates one.
package issuestatus

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The 7 canonical status keys. Each is simultaneously a status key and the
// name of the category it defines.
const (
	Backlog    = "backlog"
	Todo       = "todo"
	InProgress = "in_progress"
	InReview   = "in_review"
	Done       = "done"
	Blocked    = "blocked"
	Cancelled  = "cancelled"
)

// canonicalOrder is the historical STATUS_ORDER from the frontend's static
// status config. Category ranking copies it verbatim so a workspace with no
// custom statuses sees a board and picker identical to before this feature.
//
// Note the order is NOT grouped by lifecycle: in_review and done sit between
// in_progress and blocked. That is the shipped order, and reordering it to look
// tidier would visibly rearrange every existing user's board.
var canonicalOrder = []string{
	Backlog,
	Todo,
	InProgress,
	InReview,
	Done,
	Blocked,
	Cancelled,
}

var canonicalRank = func() map[string]int {
	m := make(map[string]int, len(canonicalOrder))
	for i, key := range canonicalOrder {
		m[key] = i
	}
	return m
}()

// ErrUnknownStatus is returned when a status key is absent from a workspace's
// catalog, or present but archived.
var ErrUnknownStatus = errors.New("unknown issue status")

// keyPattern mirrors the issue_status.key CHECK constraint. Keys are lowercase
// so `multica issue status <id> human_review` is unambiguous to type.
var keyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_]{0,31}$`)

// Querier is the slice of the generated query set this package needs. Taking an
// interface keeps the resolver testable without a live database.
type Querier interface {
	GetIssueStatusEntryByKey(ctx context.Context, arg db.GetIssueStatusEntryByKeyParams) (db.IssueStatus, error)
	ListIssueStatusEntries(ctx context.Context, arg db.ListIssueStatusEntriesParams) ([]db.IssueStatus, error)
	SeedIssueStatusEntries(ctx context.Context, workspaceID pgtype.UUID) error
	ListIssueStatusKeysByCategories(ctx context.Context, arg db.ListIssueStatusKeysByCategoriesParams) ([]string, error)
}

// Canonical returns the 7 built-in status keys in display order.
func Canonical() []string {
	out := make([]string, len(canonicalOrder))
	copy(out, canonicalOrder)
	return out
}

// IsBuiltIn reports whether key is one of the 7 canonical statuses.
func IsBuiltIn(key string) bool {
	_, ok := canonicalRank[key]
	return ok
}

// IsCategory reports whether value names a valid category. Identical to
// IsBuiltIn by construction — categories and canonical keys are the same set —
// and exists so calling code can say which of the two it means.
func IsCategory(value string) bool { return IsBuiltIn(value) }

// CategoryRank returns the display rank of a category, or len(canonicalOrder)
// for an unrecognized one so it sorts last instead of colliding with rank 0.
func CategoryRank(category string) int {
	if rank, ok := canonicalRank[category]; ok {
		return rank
	}
	return len(canonicalOrder)
}

// ValidateKey checks a proposed custom status key against the storage
// constraint and the reserved built-in names.
func ValidateKey(key string) (string, error) {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return "", errors.New("status key is required")
	}
	if !keyPattern.MatchString(key) {
		return "", errors.New("status key must be 1-32 characters of lowercase letters, digits or underscore, starting with a letter or digit")
	}
	if IsBuiltIn(key) {
		return "", fmt.Errorf("%q is a built-in status key and cannot be reused", key)
	}
	return key, nil
}

// maxKeyLen mirrors the 32-character ceiling in keyPattern and the issue_status
// CHECK. Suffixing a key has to stay under it, so the base gets truncated
// rather than the suffix dropped — a truncated base is still unique-able, a
// dropped suffix is not.
const maxKeyLen = 32

// slugify reduces a display name to the ASCII key alphabet, returning "" when
// nothing survives. Lowercase because `multica issue status <id> human_review`
// has to be unambiguous to type; runs of everything else collapse to a single
// underscore so "Gate — Approved!" does not become "gate___approved".
func slugify(name string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastUnderscore = false
		case !lastUnderscore && b.Len() > 0:
			b.WriteRune('_')
			lastUnderscore = true
		}
	}
	slug := strings.Trim(b.String(), "_")
	if len(slug) > maxKeyLen {
		slug = strings.Trim(slug[:maxKeyLen], "_")
	}
	return slug
}

// DeriveKey picks the stored key for a status whose creator supplied only a
// display name (MUL-6749 / GitHub #7627).
//
// The key is an ASCII machine handle — it is what issue.status stores, what the
// CLI takes as an argument, and what the column's CHECK constraint permits —
// while the display name is free Unicode. Deriving one from the other therefore
// has to survive names that share no alphabet with it:
//
//   - a name that slugs to something usable keeps that slug, so an
//     English-named status still gets the readable `human_review` it always
//     got;
//   - a name written entirely in a non-Latin script slugs to nothing, and falls
//     back to its CATEGORY plus an ordinal. Category, not a random suffix: it
//     is already the anchor an agent reasons from, so `in_review_2` still says
//     which platform behavior the status inherits, and it degrades into the
//     right neighborhood when a model confuses it with its category. The
//     meaning of the status travels with its name and description, which the
//     agent brief prints beside the key.
//
// Either way the result is disambiguated against keys the workspace already
// owns, so two names that collapse onto one slug ("待客户 Review" and
// "待供应商 Review" both slug to "review") no longer make the second create
// fail with a conflict that blames the display name.
//
// taken reports whether a key already exists in this workspace and MUST count
// ARCHIVED statuses: idx_issue_status_workspace_key is not partial, so an
// archived row still owns its key. Built-in keys are treated as taken here
// regardless of what taken says, which both stops a custom status from
// shadowing one and is what makes the fallback start at `<category>_2`.
//
// Derivation is expected to succeed for any name that reaches it. The one
// refusal left is a name that slugs ONTO a built-in key: that is a naming
// collision the admin should see rather than have quietly renamed, and it is
// unchanged from before.
func DeriveKey(name, category string, taken map[string]bool) (string, error) {
	if slug := slugify(name); slug != "" {
		if IsBuiltIn(slug) {
			return "", fmt.Errorf("%q is a built-in status key and cannot be reused; rename the status or pass an explicit key", slug)
		}
		return firstFreeKey(slug, taken)
	}
	// Nothing in the name belongs to the key alphabet. Category is a valid key
	// by construction, and its built-in always occupies it, so this lands on
	// <category>_2 for the first such status in that category.
	if !IsCategory(category) {
		return "", fmt.Errorf("category must be one of: %s", strings.Join(canonicalOrder, ", "))
	}
	return firstFreeKey(category, taken)
}

// firstFreeKey returns base when it is unclaimed, otherwise the first
// base_<n> that is. n starts at 2 so the series reads as "the second one".
//
// The scan is bounded by the CATALOG, not by a policy number. Every candidate
// it tests is distinct, and at most len(taken)+7 keys can be occupied (the
// workspace's own plus the built-ins), so by the pigeonhole principle a free
// one has to turn up within that many attempts plus one. Picking a round
// constant instead would invent a cap on custom statuses that exists nowhere
// else in the product, and would fail with "provide one explicitly" — the very
// error this package was changed to stop showing a UI that has no key field.
func firstFreeKey(base string, taken map[string]bool) (string, error) {
	if !keyOccupied(base, taken) {
		return ValidateKey(base)
	}
	limit := len(taken) + len(canonicalOrder) + 2
	for n := 2; n <= limit; n++ {
		suffix := "_" + strconv.Itoa(n)
		candidate := truncateForSuffix(base, len(suffix)) + suffix
		if !keyOccupied(candidate, taken) {
			return ValidateKey(candidate)
		}
	}
	// Unreachable by the argument above; kept so a future change to candidate
	// generation surfaces as an error rather than an infinite loop.
	return "", errors.New("could not derive an unused status key from that name; provide one explicitly")
}

// keyOccupied folds the built-in set into the workspace's own keys. A built-in
// counts as occupied even in a workspace whose catalog rows have not been
// seeded yet, so an unseeded workspace cannot mint a custom status that
// shadows one.
func keyOccupied(key string, taken map[string]bool) bool {
	if IsBuiltIn(key) {
		return true
	}
	return taken[key]
}

// truncateForSuffix shortens base so base+suffix fits maxKeyLen. The trailing
// underscore trim keeps "human_review" from becoming "human_" + "_2"; base
// always starts with an alphanumeric, so trimming can never empty it.
func truncateForSuffix(base string, suffixLen int) string {
	limit := maxKeyLen - suffixLen
	if len(base) <= limit {
		return base
	}
	return strings.TrimRight(base[:limit], "_")
}

// Ensure idempotently seeds a workspace's 7 built-in statuses. Safe to call
// concurrently — the unique (workspace_id, key) index turns a losing racer into
// a no-op, which matters during a rolling deploy where an old pod may create a
// workspace while a new pod seeds it.
func Ensure(ctx context.Context, q Querier, workspaceID pgtype.UUID) error {
	return q.SeedIssueStatusEntries(ctx, workspaceID)
}

// Effective maps a status key to the canonical key whose platform behavior it
// carries. This is THE function that keeps existing logic correct:
//
//   - a built-in key returns itself, unchanged, WITHOUT touching the database,
//     so no existing code path gains a query or changes behavior;
//   - a custom key returns its category, i.e. the canonical status it inherits.
//
// On an unresolvable key it returns the key unchanged. That is the fail-safe
// direction: an unrecognized status matches none of the canonical comparisons,
// so the issue is left alone rather than being swept, auto-triggered, or having
// its autopilot run finalized on a guess.
func Effective(ctx context.Context, q Querier, workspaceID pgtype.UUID, status string) string {
	if IsBuiltIn(status) {
		return status
	}
	entry, err := q.GetIssueStatusEntryByKey(ctx, db.GetIssueStatusEntryByKeyParams{
		WorkspaceID: workspaceID,
		Key:         status,
	})
	if err != nil {
		return status
	}
	if !IsCategory(entry.Category) {
		return status
	}
	return entry.Category
}

// EffectiveAndName resolves a status to BOTH its category and its display name
// in one catalog read, for payload builders that need the pair.
//
// Two separate calls would double the query on every background event carrying
// a custom status. Same fail-safe direction as Effective: an unresolvable key
// yields the key unchanged and an empty name.
//
// The name is empty for a built-in on purpose — clients localize those from the
// key, so echoing the seeded English one would be the single string a
// non-English workspace has to ignore. (MUL-6749)
func EffectiveAndName(ctx context.Context, q Querier, workspaceID pgtype.UUID, status string) (string, string) {
	if IsBuiltIn(status) {
		return status, ""
	}
	entry, err := q.GetIssueStatusEntryByKey(ctx, db.GetIssueStatusEntryByKeyParams{
		WorkspaceID: workspaceID,
		Key:         status,
	})
	if err != nil {
		return status, ""
	}
	if !IsCategory(entry.Category) {
		return status, entry.Name
	}
	return entry.Category, entry.Name
}

// Resolve validates that status is usable in this workspace, returning the
// catalog entry. Write paths use this; it is the application-layer replacement
// for the enum CHECK that migration 337 dropped.
//
// The 7 built-in keys resolve even when the workspace has no catalog row for
// them. The catalog EXTENDS the built-in statuses; it does not define them, and
// a workspace has always been able to use all 7. Requiring a row would mean a
// workspace whose seed has not landed yet — created by a pod that predates this
// feature, or mid-rollout before migration 339 runs — could not create or
// update an issue at all. Failing open here is limited precisely to the set
// that was valid before this feature existed; anything else still needs a row.
func Resolve(ctx context.Context, q Querier, workspaceID pgtype.UUID, status string) (db.IssueStatus, error) {
	key := strings.ToLower(strings.TrimSpace(status))
	if key == "" {
		return db.IssueStatus{}, ErrUnknownStatus
	}
	entry, err := q.GetIssueStatusEntryByKey(ctx, db.GetIssueStatusEntryByKeyParams{
		WorkspaceID: workspaceID,
		Key:         key,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if IsBuiltIn(key) {
				return builtInEntry(workspaceID, key), nil
			}
			return db.IssueStatus{}, ErrUnknownStatus
		}
		return db.IssueStatus{}, err
	}
	if entry.ArchivedAt.Valid {
		// A built-in can never be archived (enforced by a table constraint),
		// so reaching here means a custom status was retired.
		return db.IssueStatus{}, ErrUnknownStatus
	}
	return entry, nil
}

// builtInEntry synthesizes the catalog row for a built-in status in a workspace
// whose seed has not landed. It carries the fields that define behavior — key
// and category, which are equal by construction — and is deliberately not
// persisted: Ensure owns seeding.
func builtInEntry(workspaceID pgtype.UUID, key string) db.IssueStatus {
	return db.IssueStatus{
		WorkspaceID: workspaceID,
		Key:         key,
		Category:    key,
		IsSystem:    true,
	}
}

// ActiveKeys returns the workspace's usable status keys in display order, for
// error messages and CLI validation. An unseeded workspace still reports the 7
// built-ins, so an error message can never omit a key that Resolve accepts.
func ActiveKeys(ctx context.Context, q Querier, workspaceID pgtype.UUID) ([]string, error) {
	entries, err := q.ListIssueStatusEntries(ctx, db.ListIssueStatusEntriesParams{
		WorkspaceID:     workspaceID,
		IncludeArchived: false,
	})
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(entries)+len(canonicalOrder))
	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		keys = append(keys, e.Key)
		seen[e.Key] = true
	}
	for _, key := range canonicalOrder {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

// ActiveKeyLabels is ActiveKeys with each CUSTOM status's display name attached,
// as `key (Name)`. It backs the error a caller sees after writing an unknown
// status, where a bare key list is the least useful thing to hand back: a key
// derived from a non-Latin name carries no meaning on its own (`in_review_2`),
// so the reader — a person or an agent — has nothing to match against the name
// they were told to use.
//
// Built-ins stay bare. Their key IS their category, their seeded name is
// English, and every client localizes them from the key anyway, so printing
// `todo (Todo)` would add noise rather than information.
func ActiveKeyLabels(ctx context.Context, q Querier, workspaceID pgtype.UUID) ([]string, error) {
	entries, err := q.ListIssueStatusEntries(ctx, db.ListIssueStatusEntriesParams{
		WorkspaceID:     workspaceID,
		IncludeArchived: false,
	})
	if err != nil {
		return nil, err
	}
	labels := make([]string, 0, len(entries)+len(canonicalOrder))
	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		seen[e.Key] = true
		if IsBuiltIn(e.Key) || strings.TrimSpace(e.Name) == "" {
			labels = append(labels, e.Key)
			continue
		}
		labels = append(labels, e.Key+" ("+e.Name+")")
	}
	for _, key := range canonicalOrder {
		if !seen[key] {
			labels = append(labels, key)
		}
	}
	return labels, nil
}

// Resolver resolves many statuses against ONE workspace's catalog using at most
// one query, for list endpoints that would otherwise issue a lookup per row.
//
// The built-in fast path is unchanged: a built-in key returns itself without
// touching the catalog, so a workspace with no custom statuses still performs
// zero queries no matter how long the list is. The catalog is fetched lazily on
// the first custom key and reused for every row after it.
//
// Not safe for concurrent use, and scoped to a single request: it caches the
// catalog for its lifetime, so a long-lived Resolver would serve stale
// categories after an admin edits the catalog.
type Resolver struct {
	workspaceID pgtype.UUID
	categories  map[string]string
	names       map[string]string
	loaded      bool
}

// NewResolver returns a Resolver for one workspace. It performs no I/O.
func NewResolver(workspaceID pgtype.UUID) *Resolver {
	return &Resolver{workspaceID: workspaceID}
}

// load fetches the catalog once, on the first call that needs it. A failed read
// leaves the maps nil, which makes every lookup below fall back to its
// key-unchanged branch — the same fail-safe the single-shot resolver applies.
func (r *Resolver) load(ctx context.Context, q Querier) {
	if r.loaded {
		return
	}
	r.loaded = true
	entries, err := q.ListIssueStatusEntries(ctx, db.ListIssueStatusEntriesParams{
		WorkspaceID:     r.workspaceID,
		IncludeArchived: true,
	})
	if err != nil {
		return
	}
	r.categories = make(map[string]string, len(entries))
	r.names = make(map[string]string, len(entries))
	for _, e := range entries {
		r.categories[e.Key] = e.Category
		r.names[e.Key] = e.Name
	}
}

// Effective mirrors the package-level Effective, but amortizes the catalog read
// across every call. Same fail-safe direction: an unresolvable key is returned
// unchanged rather than guessed at.
func (r *Resolver) Effective(ctx context.Context, q Querier, status string) string {
	if IsBuiltIn(status) {
		return status
	}
	r.load(ctx, q)
	category, ok := r.categories[status]
	if !ok || !IsCategory(category) {
		return status
	}
	return category
}

// Name returns a CUSTOM status's display name, or "" for a built-in and for a
// key the catalog does not hold.
//
// Built-ins return "" on purpose rather than their seeded English name: every
// client renders those from the key through i18n, so handing back "In Progress"
// would be the one string a Chinese workspace must ignore. Custom names are
// user-authored and untranslatable, so they are the label everywhere — which is
// also why a payload carrying a custom key should carry this beside it. A
// generated key like `in_review_2` says nothing on its own. (MUL-6749)
//
// Shares the Resolver's single catalog read with Effective, so adding the name
// to a response costs no extra query.
func (r *Resolver) Name(ctx context.Context, q Querier, status string) string {
	if IsBuiltIn(status) {
		return ""
	}
	r.load(ctx, q)
	return r.names[status]
}

// ExpandCategories turns a set of categories into the status keys that belong
// to them, for use as an INDEXED `status = ANY(...)` predicate.
//
// This exists instead of filtering on issue_effective_status(workspace, status):
// wrapping the column in a function makes the (workspace_id, status) index
// unusable, turning a two-page index read into a full workspace scan. Expanding
// first keeps the original access path — and for a workspace with no custom
// statuses each category expands to exactly its own key, so the query is
// byte-for-byte the one that ran before this feature.
//
// Archived statuses are included: archiving stops FUTURE assignment but leaves
// existing issues in place, and those issues must still appear in their
// category's column.
//
// An unseeded workspace yields no rows; the categories themselves are returned
// in that case, which is correct because a built-in key IS its own category.
func ExpandCategories(ctx context.Context, q Querier, workspaceID pgtype.UUID, categories []string) ([]string, error) {
	valid := make([]string, 0, len(categories))
	for _, c := range categories {
		if IsCategory(c) {
			valid = append(valid, c)
		}
	}
	if len(valid) == 0 {
		return nil, nil
	}
	keys, err := q.ListIssueStatusKeysByCategories(ctx, db.ListIssueStatusKeysByCategoriesParams{
		WorkspaceID: workspaceID,
		Categories:  valid,
	})
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(keys)+len(valid))
	out := make([]string, 0, len(keys)+len(valid))
	for _, k := range keys {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	// A category always contains at least its own canonical key, even if the
	// catalog row is missing (unseeded workspace, mid-rollout).
	for _, c := range valid {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out, nil
}

// CustomKeyCategories returns the workspace's CUSTOM status keys mapped to the
// category each belongs to. Built-ins are deliberately absent: a built-in key IS
// its own category, so a caller mapping key -> category only needs the
// exceptions.
//
// Callers use this to build a static `CASE i.status WHEN ... ELSE i.status END`
// scalar expression for GROUP BY. That keeps category grouping a plain column
// rewrite rather than a per-row function call or a join, and for a workspace
// with no custom statuses the map is empty and the CASE collapses to `i.status`
// — byte-for-byte the expression that ran before this feature.
//
// Archived statuses are included, for the same reason ExpandCategories includes
// them: issues left on one must still group into their category.
func CustomKeyCategories(ctx context.Context, q Querier, workspaceID pgtype.UUID) (map[string]string, error) {
	entries, err := q.ListIssueStatusEntries(ctx, db.ListIssueStatusEntriesParams{
		WorkspaceID:     workspaceID,
		IncludeArchived: true,
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		if IsBuiltIn(e.Key) || !IsCategory(e.Category) {
			continue
		}
		out[e.Key] = e.Category
	}
	return out, nil
}
