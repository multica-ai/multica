package issuestatus

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// seededWorkspace returns a workspace with the 7 built-in statuses already
// seeded, ready for resolution tests.
func seededWorkspace(ctx context.Context, t *testing.T) (pgtype.UUID, *db.Queries) {
	t.Helper()
	q := db.New(testPool)
	wsID := freshWorkspace(ctx, t)
	if err := Ensure(ctx, q, wsID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return wsID, q
}

func TestResolveCategoryAlias(t *testing.T) {
	ctx := context.Background()
	wsID, q := seededWorkspace(ctx, t)

	cases := map[string]string{ // input -> expected system_key of the category default
		"backlog":     "backlog",
		"todo":        "todo",
		"in_progress": "in_progress",
		"done":        "done",
		"cancelled":   "cancelled",
		"  TODO ":     "todo",        // trimmed + case-insensitive
		"In_Progress": "in_progress", // case-insensitive
	}
	for input, wantKey := range cases {
		s, err := Resolve(ctx, q, wsID, input)
		if err != nil {
			t.Errorf("Resolve(%q): %v", input, err)
			continue
		}
		if !s.IsDefault {
			t.Errorf("Resolve(%q): category alias must resolve to a default status, got is_default=false (%q)", input, s.Name)
		}
		if !s.SystemKey.Valid || s.SystemKey.String != wantKey {
			t.Errorf("Resolve(%q): want default system_key %q, got %v", input, wantKey, s.SystemKey)
		}
	}
}

func TestResolveLegacyAlias(t *testing.T) {
	ctx := context.Background()
	wsID, q := seededWorkspace(ctx, t)

	cases := map[string]struct{ key, category string }{
		"in_review": {"in_review", "in_progress"},
		"blocked":   {"blocked", "in_progress"},
		"BLOCKED":   {"blocked", "in_progress"},
	}
	for input, want := range cases {
		s, err := Resolve(ctx, q, wsID, input)
		if err != nil {
			t.Errorf("Resolve(%q): %v", input, err)
			continue
		}
		if !s.SystemKey.Valid || s.SystemKey.String != want.key {
			t.Errorf("Resolve(%q): want system_key %q, got %v", input, want.key, s.SystemKey)
		}
		if s.Category != want.category {
			t.Errorf("Resolve(%q): want category %q, got %q", input, want.category, s.Category)
		}
		if s.IsDefault {
			t.Errorf("Resolve(%q): in_review/blocked are non-default statuses, got is_default=true", input)
		}
	}
}

func TestResolveExactName(t *testing.T) {
	ctx := context.Background()
	wsID, q := seededWorkspace(ctx, t)

	// Display names render with spaces; the underscore alias is a separate path.
	cases := map[string]string{ // name input -> expected system_key
		"In Progress": "in_progress",
		"in progress": "in_progress", // case-insensitive
		"In Review":   "in_review",
		"Done":        "done",
	}
	for input, wantKey := range cases {
		s, err := Resolve(ctx, q, wsID, input)
		if err != nil {
			t.Errorf("Resolve(%q): %v", input, err)
			continue
		}
		if !s.SystemKey.Valid || s.SystemKey.String != wantKey {
			t.Errorf("Resolve(%q): want system_key %q, got %v", input, wantKey, s.SystemKey)
		}
	}
}

// TestResolveCategoryAliasFollowsRenamedDefault is plan example A: renaming the
// default Todo status must not break the `todo` alias, and a non-default custom
// Todo status is reachable only by its exact name.
func TestResolveCategoryAliasFollowsRenamedDefault(t *testing.T) {
	ctx := context.Background()
	wsID, q := seededWorkspace(ctx, t)

	if _, err := testPool.Exec(ctx,
		"UPDATE issue_status SET name = $2 WHERE workspace_id = $1 AND system_key = 'todo'",
		wsID, "待排期"); err != nil {
		t.Fatalf("rename todo default: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`INSERT INTO issue_status (workspace_id, name, description, icon, color, category, system_key, is_default, position)
		 VALUES ($1, '需求澄清', '', 'todo', 'muted-foreground', 'todo', NULL, FALSE, 10)`,
		wsID); err != nil {
		t.Fatalf("insert custom todo status: %v", err)
	}

	// `todo` alias still lands on the (renamed) default.
	got, err := Resolve(ctx, q, wsID, "todo")
	if err != nil {
		t.Fatalf("Resolve(todo): %v", err)
	}
	if got.Name != "待排期" || !got.IsDefault {
		t.Fatalf("Resolve(todo): want renamed default 待排期, got name=%q is_default=%v", got.Name, got.IsDefault)
	}

	// The custom non-default status is reachable by exact name only.
	got, err = Resolve(ctx, q, wsID, "需求澄清")
	if err != nil {
		t.Fatalf("Resolve(需求澄清): %v", err)
	}
	if got.Name != "需求澄清" || got.IsDefault {
		t.Fatalf("Resolve(需求澄清): want custom non-default status, got name=%q is_default=%v", got.Name, got.IsDefault)
	}

	// And the renamed default is reachable by its new exact name too.
	got, err = Resolve(ctx, q, wsID, "待排期")
	if err != nil {
		t.Fatalf("Resolve(待排期): %v", err)
	}
	if !got.SystemKey.Valid || got.SystemKey.String != "todo" {
		t.Fatalf("Resolve(待排期): want the todo built-in, got system_key=%v", got.SystemKey)
	}
}

func TestResolveUnknownReturnsInvalidStatusError(t *testing.T) {
	ctx := context.Background()
	wsID, q := seededWorkspace(ctx, t)

	_, err := Resolve(ctx, q, wsID, "no-such-status")
	if err == nil {
		t.Fatal("Resolve(no-such-status): want error, got nil")
	}
	var invalid *InvalidStatusError
	if !errors.As(err, &invalid) {
		t.Fatalf("Resolve(no-such-status): want *InvalidStatusError, got %T: %v", err, err)
	}
	if len(invalid.CategoryAliases) != 5 {
		t.Errorf("want 5 category aliases in error, got %v", invalid.CategoryAliases)
	}
	if len(invalid.LegacyAliases) != 2 {
		t.Errorf("want 2 legacy aliases in error, got %v", invalid.LegacyAliases)
	}
	if len(invalid.Names) != len(wantSystemStatuses) {
		t.Errorf("want %d names in error, got %d (%v)", len(wantSystemStatuses), len(invalid.Names), invalid.Names)
	}

	// Empty input is also invalid, not a silent match.
	if _, err := Resolve(ctx, q, wsID, "   "); !errors.As(err, &invalid) {
		t.Errorf("Resolve(blank): want *InvalidStatusError, got %v", err)
	}
}

func TestIsReservedStatusToken(t *testing.T) {
	reserved := []string{"backlog", "todo", "in_progress", "in_review", "blocked", "done", "cancelled", "  TODO ", "In_Review"}
	for _, tok := range reserved {
		if !IsReservedStatusToken(tok) {
			t.Errorf("IsReservedStatusToken(%q) = false, want true", tok)
		}
	}
	notReserved := []string{"待排期", "in review", "in progress", "custom", ""}
	for _, tok := range notReserved {
		if IsReservedStatusToken(tok) {
			t.Errorf("IsReservedStatusToken(%q) = true, want false", tok)
		}
	}
}

// TestCategoryForStatusToken covers the compat-projection → Category mapping used
// by machine logic (MUL-4809 §4.2). The two legacy tokens collapse to in_progress;
// everything else (Category keys, custom-status projections) maps to itself.
func TestCategoryForStatusToken(t *testing.T) {
	cases := map[string]string{
		"backlog":     "backlog",
		"todo":        "todo",
		"in_progress": "in_progress",
		"in_review":   "in_progress",
		"blocked":     "in_progress",
		"done":        "done",
		"cancelled":   "cancelled",
	}
	for token, want := range cases {
		if got := CategoryForStatusToken(token); got != want {
			t.Errorf("CategoryForStatusToken(%q) = %q, want %q", token, got, want)
		}
	}
}

// TestIsTerminalCategory covers the terminal-Category predicate: only done and
// cancelled are terminal; in_review/blocked map to in_progress and are not.
func TestIsTerminalCategory(t *testing.T) {
	terminal := []string{"done", "cancelled"}
	nonTerminal := []string{"backlog", "todo", "in_progress"}
	for _, c := range terminal {
		if !IsTerminalCategory(c) {
			t.Errorf("IsTerminalCategory(%q) = false, want true", c)
		}
	}
	for _, c := range nonTerminal {
		if IsTerminalCategory(c) {
			t.Errorf("IsTerminalCategory(%q) = true, want false", c)
		}
	}
	// The in_progress display tokens resolve through their Category, never terminal.
	for _, token := range []string{"in_review", "blocked"} {
		if IsTerminalCategory(CategoryForStatusToken(token)) {
			t.Errorf("%q resolved as terminal, want non-terminal", token)
		}
	}
}
