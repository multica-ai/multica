package testutil

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Cols is one fixture row: column name to value. Values are bound as query
// parameters unless they are Raw.
type Cols map[string]any

// Raw is a SQL expression inlined into the statement instead of being bound as
// a parameter, for the defaults a fixture cannot express as a Go value —
// `now() - interval '2 hours'`, or the next issue number in a workspace.
//
// It is inlined verbatim. Never build one out of anything but literals and
// ids the fixture itself produced.
type Raw string

// Fixture inserts rows for one test workspace and removes them again when the
// test that asked for them finishes.
//
// The zero value is not usable; call New. WorkspaceID and UserID are the
// defaults every builder falls back to, so a test that only cares about one
// column names only that column.
type Fixture struct {
	Pool        *pgxpool.Pool
	WorkspaceID string
	UserID      string
}

// New returns a Fixture bound to the workspace and user a test suite set up in
// its TestMain.
func New(pool *pgxpool.Pool, workspaceID, userID string) *Fixture {
	return &Fixture{Pool: pool, WorkspaceID: workspaceID, UserID: userID}
}

// Insert writes one row into table, registers its deletion with t.Cleanup and
// returns the new row's id.
//
// Cleanup runs in reverse creation order, so a row inserted after the row it
// references is deleted before it and foreign keys hold without the test
// spelling out a teardown order.
func (f *Fixture) Insert(t TB, table string, cols Cols) string {
	t.Helper()

	names, placeholders, args := compile(cols)
	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) RETURNING id",
		quoteIdent(table), strings.Join(names, ", "), strings.Join(placeholders, ", "))

	var id string
	if err := f.Pool.QueryRow(context.Background(), sql, args...).Scan(&id); err != nil {
		t.Fatalf("setup: insert into %s: %v", table, err)
	}
	f.deleteOnCleanup(t, table, id)
	return id
}

// InsertNoID writes one row into a table that has no id column — a join table,
// typically — and deletes it again with the WHERE clause the caller gives.
// where is appended to `DELETE FROM <table> WHERE `, and its parameters are
// numbered from $1.
func (f *Fixture) InsertNoID(t TB, table string, cols Cols, where string, whereArgs ...any) {
	t.Helper()

	names, placeholders, args := compile(cols)
	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		quoteIdent(table), strings.Join(names, ", "), strings.Join(placeholders, ", "))
	if _, err := f.Pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("setup: insert into %s: %v", table, err)
	}

	t.Cleanup(func() {
		_, _ = f.Pool.Exec(context.Background(),
			fmt.Sprintf("DELETE FROM %s WHERE %s", quoteIdent(table), where), whereArgs...)
	})
}

// Exec runs a statement and fails the test if it errors.
func (f *Fixture) Exec(t TB, sql string, args ...any) {
	t.Helper()
	if _, err := f.Pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec: %v\nSQL: %s", err, strings.TrimSpace(sql))
	}
}

// Cleanup runs a statement when the test finishes, ignoring its error the way
// teardown always should: a failing test has usually already left the row in a
// state teardown cannot reach, and reporting that on top of the real failure
// buries it.
func (f *Fixture) Cleanup(t TB, sql string, args ...any) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = f.Pool.Exec(context.Background(), sql, args...)
	})
}

// Row is a pending single-row query.
type Row struct {
	f    *Fixture
	t    TB
	sql  string
	args []any
}

// QueryRow prepares a single-row query whose Scan fails the test on error.
func (f *Fixture) QueryRow(t TB, sql string, args ...any) *Row {
	t.Helper()
	return &Row{f: f, t: t, sql: sql, args: args}
}

// Scan reads the row into dest, failing the test if the query errors or
// matched nothing.
func (r *Row) Scan(dest ...any) {
	r.t.Helper()
	if err := r.f.Pool.QueryRow(context.Background(), r.sql, r.args...).Scan(dest...); err != nil {
		r.t.Fatalf("query: %v\nSQL: %s", err, strings.TrimSpace(r.sql))
	}
}

// Count returns the count a `SELECT count(*) ...` query produced.
func (f *Fixture) Count(t TB, sql string, args ...any) int {
	t.Helper()
	var n int
	f.QueryRow(t, sql, args...).Scan(&n)
	return n
}

// ---- typed builders -------------------------------------------------------
//
// Each one fills the columns a row cannot be inserted without, and takes
// optional overrides for the column the test is actually about. Defaults are
// deliberately boring: a test that depends on one must name it.

// User inserts a user row.
func (f *Fixture) User(t TB, name, email string, over ...Cols) string {
	t.Helper()
	return f.Insert(t, "user", merge(Cols{
		"name":  name,
		"email": email,
	}, over))
}

// Workspace inserts a workspace row.
func (f *Fixture) Workspace(t TB, name, slug string, over ...Cols) string {
	t.Helper()
	return f.Insert(t, "workspace", merge(Cols{
		"name":         name,
		"slug":         slug,
		"description":  "",
		"issue_prefix": "",
	}, over))
}

// Member joins a user to a workspace.
func (f *Fixture) Member(t TB, workspaceID, userID, role string, over ...Cols) string {
	t.Helper()
	return f.Insert(t, "member", merge(Cols{
		"workspace_id": workspaceID,
		"user_id":      userID,
		"role":         role,
	}, over))
}

// Issue inserts an issue owned by the fixture's workspace and user.
//
// number is derived from the workspace's existing issues rather than fixed, so
// two issues built by the same test do not collide on the workspace's issue
// numbering.
func (f *Fixture) Issue(t TB, title string, over ...Cols) string {
	t.Helper()
	cols := merge(Cols{
		"workspace_id": f.WorkspaceID,
		"title":        title,
		"status":       "todo",
		"priority":     "none",
		"creator_type": "member",
		"creator_id":   f.UserID,
		"position":     0,
	}, over)
	if _, ok := cols["number"]; !ok {
		workspaceID, _ := cols["workspace_id"].(string)
		cols["number"] = Raw(fmt.Sprintf(
			"(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = %s)",
			quoteLiteral(workspaceID)))
	}
	return f.Insert(t, "issue", cols)
}

// Comment inserts a comment on issueID.
func (f *Fixture) Comment(t TB, issueID, content string, over ...Cols) string {
	t.Helper()
	return f.Insert(t, "comment", merge(Cols{
		"issue_id":     issueID,
		"workspace_id": f.WorkspaceID,
		"author_type":  "member",
		"author_id":    f.UserID,
		"content":      content,
		"type":         "comment",
	}, over))
}

// Runtime inserts an online cloud runtime.
func (f *Fixture) Runtime(t TB, name string, over ...Cols) string {
	t.Helper()
	return f.Insert(t, "agent_runtime", merge(Cols{
		"workspace_id": f.WorkspaceID,
		"daemon_id":    nil,
		"name":         name,
		"runtime_mode": "cloud",
		"provider":     "handler_test_runtime",
		"status":       "online",
		"device_info":  "",
		"metadata":     Raw("'{}'::jsonb"),
		"last_seen_at": Raw("now()"),
		"visibility":   "private",
		"owner_id":     f.UserID,
	}, over))
}

// Agent inserts an agent bound to runtimeID. Pass an empty runtimeID for an
// agent with no runtime.
func (f *Fixture) Agent(t TB, name, runtimeID string, over ...Cols) string {
	t.Helper()
	cols := merge(Cols{
		"workspace_id":         f.WorkspaceID,
		"name":                 name,
		"description":          "",
		"runtime_mode":         "cloud",
		"runtime_config":       Raw("'{}'::jsonb"),
		"visibility":           "private",
		"permission_mode":      "private",
		"max_concurrent_tasks": 1,
		"owner_id":             f.UserID,
	}, over)
	if _, ok := cols["runtime_id"]; !ok && runtimeID != "" {
		cols["runtime_id"] = runtimeID
	}
	return f.Insert(t, "agent", cols)
}

// Task queues a task for agentID.
func (f *Fixture) Task(t TB, agentID string, over ...Cols) string {
	t.Helper()
	return f.Insert(t, "agent_task_queue", merge(Cols{
		"agent_id": agentID,
		"status":   "queued",
		"priority": 0,
	}, over))
}

// Squad inserts a squad led by leaderID.
func (f *Fixture) Squad(t TB, name, leaderID string, over ...Cols) string {
	t.Helper()
	return f.Insert(t, "squad", merge(Cols{
		"workspace_id": f.WorkspaceID,
		"name":         name,
		"description":  "",
		"leader_id":    leaderID,
		"creator_id":   f.UserID,
	}, over))
}

// SquadMember adds an agent or member to a squad.
func (f *Fixture) SquadMember(t TB, squadID, memberType, memberID string, over ...Cols) string {
	t.Helper()
	return f.Insert(t, "squad_member", merge(Cols{
		"squad_id":    squadID,
		"member_type": memberType,
		"member_id":   memberID,
	}, over))
}

// Project inserts a project.
func (f *Fixture) Project(t TB, title string, over ...Cols) string {
	t.Helper()
	return f.Insert(t, "project", merge(Cols{
		"workspace_id": f.WorkspaceID,
		"title":        title,
		"status":       "planned",
		"priority":     "none",
	}, over))
}

// ChatSession inserts a chat session with agentID.
func (f *Fixture) ChatSession(t TB, agentID string, over ...Cols) string {
	t.Helper()
	return f.Insert(t, "chat_session", merge(Cols{
		"workspace_id": f.WorkspaceID,
		"agent_id":     agentID,
		"creator_id":   f.UserID,
		"title":        "",
		"status":       "active",
	}, over))
}

// ---- internals ------------------------------------------------------------

func (f *Fixture) deleteOnCleanup(t TB, table, id string) {
	t.Cleanup(func() {
		_, _ = f.Pool.Exec(context.Background(),
			fmt.Sprintf("DELETE FROM %s WHERE id = $1", quoteIdent(table)), id)
	})
}

// compile turns a column map into column names, value placeholders and bound
// arguments. Columns are sorted so the generated SQL is stable, which keeps a
// failing statement readable and diffable between runs.
func compile(cols Cols) (names []string, placeholders []string, args []any) {
	names = make([]string, 0, len(cols))
	for name := range cols {
		names = append(names, name)
	}
	sort.Strings(names)

	placeholders = make([]string, 0, len(names))
	args = make([]any, 0, len(names))
	for _, name := range names {
		if raw, ok := cols[name].(Raw); ok {
			placeholders = append(placeholders, string(raw))
			continue
		}
		args = append(args, cols[name])
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	for i, name := range names {
		names[i] = quoteIdent(name)
	}
	return names, placeholders, args
}

func merge(base Cols, over []Cols) Cols {
	out := make(Cols, len(base))
	for k, v := range base {
		out[k] = v
	}
	for _, o := range over {
		for k, v := range o {
			out[k] = v
		}
	}
	return out
}

// quoteIdent double-quotes an identifier so reserved words — `user`, the
// table, is one — survive as table and column names.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func quoteLiteral(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `''`) + `'`
}
