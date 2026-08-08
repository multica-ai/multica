package issuestatus

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// WriteInputError is a bad-input error from ResolveWriteInput that an HTTP caller
// maps to 400. It is distinct from an internal DB error (which maps to 500) and
// from *InvalidStatusError (also 400) so a caller can treat all user-facing status
// errors uniformly without importing net/http here.
type WriteInputError struct{ Msg string }

func (e *WriteInputError) Error() string { return e.Msg }

func writeInputErr(msg string) error { return &WriteInputError{Msg: msg} }

// IsBadStatusInput reports whether err is a user-facing status-input error (either
// an unresolvable/ambiguous input or an invalid catalog reference), so HTTP
// callers can respond 400 for these and 500 for everything else.
func IsBadStatusInput(err error) bool {
	var w *WriteInputError
	var inv *InvalidStatusError
	return errors.As(err, &w) || errors.As(err, &inv)
}

// ResolveWriteInput resolves an issue write's status inputs to the authoritative
// ACTIVE catalog row (MUL-4809 §3.1 / §6.1). Exactly one of the two forms drives
// the result, and either may be given:
//
//   - statusID targets a catalog row directly (what the UI picker sends). It must
//     name a status in THIS workspace and must NOT be archived — archived statuses
//     stay readable for old issues but must never accept a new assignment.
//   - status is the alias form (Category alias / legacy alias / exact active name),
//     resolved via Resolve, which only ever returns active statuses.
//
// Both may be sent together, but only when they resolve to the SAME row; a
// mismatch is a WriteInputError, never a silent winner. Returns (row, token,
// seeded): token is the legacy `status` projection to double-write, seeded is
// false when the workspace catalog is not seeded yet (the pre-backfill window),
// where the caller writes the legacy token only and leaves status_id NULL.
//
// CONCURRENCY: q MUST be the write transaction's queries, and the caller MUST have
// already taken LockWorkspaceForStatusWrite on the same tx. Resolving under that
// lock, then writing in the same tx, is what makes an assignment atomic against an
// archive-with-migration: the archive either censused this issue first (and it
// resolves against an active status) or waits for this write to commit (§5.5).
func ResolveWriteInput(
	ctx context.Context,
	q *db.Queries,
	workspaceID pgtype.UUID,
	status *string,
	statusID *pgtype.UUID,
) (row db.IssueStatus, token string, seeded bool, err error) {
	count, err := q.CountWorkspaceIssueStatuses(ctx, workspaceID)
	if err != nil {
		return db.IssueStatus{}, "", false, fmt.Errorf("count workspace issue statuses: %w", err)
	}
	if count == 0 {
		// Unseeded workspace (rolling-deploy / pre-backfill). Nothing to resolve
		// against; the caller keeps the legacy token authoritative and status_id NULL.
		if statusID != nil {
			return db.IssueStatus{}, "", false, writeInputErr("status_id does not name a status in this workspace")
		}
		if status == nil {
			return db.IssueStatus{}, "", false, nil
		}
		// The 7 legacy tokens are the 5 Categories plus in_review / blocked. Reject
		// anything else so an unseeded write can't persist a junk status token.
		tok := strings.ToLower(strings.TrimSpace(*status))
		_, isCategory := categoryAliasSet[tok]
		_, isLegacy := legacyAliases[tok]
		if !isCategory && !isLegacy {
			return db.IssueStatus{}, "", false, writeInputErr(fmt.Sprintf("invalid status %q", *status))
		}
		return db.IssueStatus{}, tok, false, nil
	}

	var byID db.IssueStatus
	if statusID != nil {
		byID, err = q.GetWorkspaceIssueStatus(ctx, db.GetWorkspaceIssueStatusParams{
			ID:          *statusID,
			WorkspaceID: workspaceID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return db.IssueStatus{}, "", true, writeInputErr("status_id does not name a status in this workspace")
		}
		if err != nil {
			return db.IssueStatus{}, "", true, fmt.Errorf("load status by id: %w", err)
		}
		if byID.ArchivedAt.Valid {
			return db.IssueStatus{}, "", true, writeInputErr("status_id names an archived status")
		}
	}

	if status == nil {
		return byID, LegacyStatusToken(byID), true, nil
	}

	byName, err := Resolve(ctx, q, workspaceID, *status)
	if err != nil {
		return db.IssueStatus{}, "", true, err
	}
	if statusID != nil && uuidToString(byName.ID) != uuidToString(byID.ID) {
		return db.IssueStatus{}, "", true, writeInputErr("status and status_id refer to different statuses")
	}
	return byName, LegacyStatusToken(byName), true, nil
}

// Categories are the 5 immutable machine-readable status categories. This is
// the ONLY status semantics any automation may branch on.
var Categories = []string{"backlog", "todo", "in_progress", "done", "cancelled"}

// CategoryForStatusToken maps the compatibility issue.status projection to its
// machine Category (MUL-4809 §4.2). Automation must branch on Category, never on
// the raw status token: the two built-in legacy statuses in_review and blocked
// both belong to the in_progress Category, while every other token already equals
// its Category — the five Category keys, and custom statuses which project to
// their own Category. This is the DB-free resolver for call sites that already
// hold issue.status; a status_id → issue_status.category lookup would be redundant
// because the compat projection already carries the Category for every status.
func CategoryForStatusToken(status string) string {
	switch status {
	case "in_review", "blocked":
		return "in_progress"
	default:
		return status
	}
}

// ResolveForWrite is Resolve for issue WRITE paths, with the one degradation the
// two-phase rollout requires (MUL-4809 §6.1): a workspace whose catalog has not
// been seeded yet has nothing to resolve against, so it reports resolved=false
// and the caller writes only the legacy `status` token, leaving status_id NULL.
// That is the same behaviour the create path had before the catalog existed, and
// it keeps status authoritative until the backfill lands.
//
// A seeded workspace always resolves, so an unknown input is still a hard error.
func ResolveForWrite(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, input string) (db.IssueStatus, bool, error) {
	seeded, err := q.CountWorkspaceIssueStatuses(ctx, workspaceID)
	if err != nil {
		return db.IssueStatus{}, false, fmt.Errorf("count workspace issue statuses: %w", err)
	}
	if seeded == 0 {
		return db.IssueStatus{}, false, nil
	}
	resolved, err := Resolve(ctx, q, workspaceID, input)
	if err != nil {
		return db.IssueStatus{}, false, err
	}
	return resolved, true, nil
}

// LegacyStatusToken projects a catalog status onto the legacy `issue.status`
// token that the compat column, older clients, and every not-yet-migrated read
// path still use (MUL-4809 §6.1). Built-ins keep their exact system_key, so
// `in_review` / `blocked` survive; a custom status projects to its Category,
// which is always one of the five Category keys and therefore always a legal
// legacy token. This is the inverse of Resolve and the two must stay in step:
// Resolve(LegacyStatusToken(s)) returns s for every built-in, and returns the
// Category default for a custom status.
func LegacyStatusToken(s db.IssueStatus) string {
	if s.SystemKey.Valid && s.SystemKey.String != "" {
		return s.SystemKey.String
	}
	return s.Category
}

// IsTerminalCategory reports whether a machine Category is terminal — the work is
// finished (done) or abandoned (cancelled). in_review and blocked are NOT terminal
// (they are in_progress), which is why machine logic keys off Category and not the
// display token (MUL-4809 §4.2).
func IsTerminalCategory(category string) bool {
	return category == "done" || category == "cancelled"
}

// categoryAliasSet is the set of Category alias tokens. Each resolves to the
// workspace's current default status for that Category.
var categoryAliasSet = map[string]struct{}{
	"backlog": {}, "todo": {}, "in_progress": {}, "done": {}, "cancelled": {},
}

// legacyAliases maps the 2 legacy status tokens to the built-in system_key they
// resolve to. They survive display-name renames because they key on system_key,
// not on the (mutable) name.
var legacyAliases = map[string]string{
	"in_review": "in_review",
	"blocked":   "blocked",
}

// ReservedStatusTokens are the 7 tokens that no custom status display name may
// take, because the alias resolver claims them first (5 Category aliases + 2
// legacy aliases). The status-management API rejects a create/rename to any of
// these.
var ReservedStatusTokens = []string{
	"backlog", "todo", "in_progress", "in_review", "blocked", "done", "cancelled",
}

// IsReservedStatusToken reports whether name (case-insensitive, trimmed) is one
// of the reserved alias tokens.
func IsReservedStatusToken(name string) bool {
	norm := strings.ToLower(strings.TrimSpace(name))
	if _, ok := categoryAliasSet[norm]; ok {
		return true
	}
	_, ok := legacyAliases[norm]
	return ok
}

// InvalidStatusError is returned by Resolve when the input matches no Category
// alias, legacy alias, or active display name. It enumerates the currently
// legal values so the API/CLI can echo them back (plan §3.2) instead of leaving
// an agent to guess after a status has been renamed. It maps to HTTP 400
// invalid_status at the handler boundary.
type InvalidStatusError struct {
	Input           string
	CategoryAliases []string // the 5 Category alias tokens
	LegacyAliases   []string // in_review, blocked
	Names           []string // exact active display names
}

func (e *InvalidStatusError) Error() string {
	return fmt.Sprintf("invalid status %q: expected a Category alias (%s), a legacy alias (%s), or an exact status name (%s)",
		e.Input,
		strings.Join(e.CategoryAliases, ", "),
		strings.Join(e.LegacyAliases, ", "),
		strings.Join(e.Names, ", "),
	)
}

// Resolve maps a status string to the workspace's issue_status row (MUL-4809,
// plan §3.1). Resolution is case-insensitive, trims surrounding whitespace, and
// applies a fixed priority order:
//
//  1. Category alias (backlog | todo | in_progress | done | cancelled) ->
//     that Category's current default status. So `todo` keeps working even
//     after the default Todo status is renamed.
//  2. Legacy alias (in_review | blocked) -> the built-in status with that
//     system_key. Survives renames for the same reason.
//  3. Exact active display name (case-insensitive) -> that status. This is how
//     a caller targets a specific workflow stage or a custom status.
//
// No fuzzy matching: anything else yields *InvalidStatusError carrying the
// legal values. Category aliases use underscores (`in_progress`) and never
// collide with display names, which render with spaces (`In Progress`).
func Resolve(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, input string) (db.IssueStatus, error) {
	norm := strings.ToLower(strings.TrimSpace(input))

	statuses, err := q.ListWorkspaceIssueStatuses(ctx, db.ListWorkspaceIssueStatusesParams{WorkspaceID: workspaceID})
	if err != nil {
		return db.IssueStatus{}, fmt.Errorf("load workspace issue statuses: %w", err)
	}

	if norm != "" {
		// 1. Category alias -> current default for that Category.
		if _, ok := categoryAliasSet[norm]; ok {
			for _, s := range statuses {
				if s.Category == norm && s.IsDefault {
					return s, nil
				}
			}
			// The one-default-per-category invariant is seeded and maintained in
			// the service layer; a missing default is a data-integrity bug, not a
			// user input error.
			return db.IssueStatus{}, fmt.Errorf("workspace %s has no default status for category %q", uuidToString(workspaceID), norm)
		}

		// 2. Legacy alias -> built-in status by system_key.
		if systemKey, ok := legacyAliases[norm]; ok {
			for _, s := range statuses {
				if s.SystemKey.Valid && s.SystemKey.String == systemKey {
					return s, nil
				}
			}
			return db.IssueStatus{}, fmt.Errorf("workspace %s is missing built-in status %q", uuidToString(workspaceID), systemKey)
		}

		// 3. Exact active display name (case-insensitive).
		for _, s := range statuses {
			if strings.ToLower(s.Name) == norm {
				return s, nil
			}
		}
	}

	return db.IssueStatus{}, newInvalidStatusError(input, statuses)
}

// newInvalidStatusError builds the enumerated-options error from the current
// active catalog.
func newInvalidStatusError(input string, statuses []db.IssueStatus) *InvalidStatusError {
	names := make([]string, 0, len(statuses))
	for _, s := range statuses {
		names = append(names, s.Name)
	}
	sort.Strings(names)
	return &InvalidStatusError{
		Input:           input,
		CategoryAliases: append([]string(nil), Categories...),
		LegacyAliases:   []string{"in_review", "blocked"},
		Names:           names,
	}
}

// uuidToString renders a pgtype.UUID for error messages. Empty/invalid UUIDs
// render as the zero UUID rather than panicking.
func uuidToString(id pgtype.UUID) string {
	if !id.Valid {
		return "00000000-0000-0000-0000-000000000000"
	}
	b := id.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
