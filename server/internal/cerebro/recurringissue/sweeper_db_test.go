package recurringissue

// Integration tests for the recurring-issue sweeper (TECH-3064). They verify
// the core behaviour FIR-334 asks for:
//
//   1. The sweeper does nothing when the cerebro_recurring_issues workspace
//      flag is OFF (the default).
//   2. When a recurring issue reaches its trigger status, the sweeper spawns
//      the next occurrence with a fresh due date and the same data, repoints
//      the rule onto the new issue, and re-arms.
//
// Tests skip cleanly when no test DB is reachable (DATABASE_URL), mirroring
// the sprint sweeper integration tests.

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	recTestEmail         = "recurring-issue-sweeper-test@multica.ai"
	recTestName          = "Recurring Issue Sweeper Test"
	recTestWorkspaceSlug = "recurring-issue-sweeper-tests"
)

var (
	recTestPool        *pgxpool.Pool
	recTestWorkspaceID pgtype.UUID
	recTestUserID      pgtype.UUID
	recTestProjectID   pgtype.UUID
)

func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping recurring-issue sweeper integration tests: %v\n", err)
		os.Exit(m.Run())
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping recurring-issue sweeper integration tests: db not reachable: %v\n", err)
		pool.Close()
		os.Exit(m.Run())
	}
	if err := cleanupRecFixture(ctx, pool); err != nil {
		fmt.Printf("Failed to clean fixture: %v\n", err)
		pool.Close()
		os.Exit(1)
	}
	if err := setupRecFixture(ctx, pool); err != nil {
		fmt.Printf("Failed to set up fixture: %v\n", err)
		_ = cleanupRecFixture(ctx, pool)
		pool.Close()
		os.Exit(1)
	}
	recTestPool = pool
	code := m.Run()
	if err := cleanupRecFixture(context.Background(), pool); err != nil {
		fmt.Printf("Failed to clean fixture: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	pool.Close()
	os.Exit(code)
}

func setupRecFixture(ctx context.Context, pool *pgxpool.Pool) error {
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, recTestName, recTestEmail).Scan(&recTestUserID); err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, "Recurring Issue Sweeper Tests", recTestWorkspaceSlug, "Temporary workspace", "RIS").Scan(&recTestWorkspaceID); err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
	`, recTestWorkspaceID, recTestUserID); err != nil {
		return fmt.Errorf("create member: %w", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
	`, recTestWorkspaceID, "Recurrence Project").Scan(&recTestProjectID); err != nil {
		return fmt.Errorf("create project: %w", err)
	}
	return nil
}

func cleanupRecFixture(ctx context.Context, pool *pgxpool.Pool) error {
	stmts := []string{
		`DELETE FROM cerebro_feature_flags WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM cerebro_issue_recurrence_log WHERE recurrence_id IN (SELECT r.id FROM cerebro_issue_recurrence r JOIN workspace w ON w.id = r.workspace_id WHERE w.slug = $1)`,
		`DELETE FROM cerebro_issue_recurrence WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM attachment WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM issue WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM project WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM member WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM workspace WHERE slug = $1`,
	}
	for _, stmt := range stmts {
		if _, err := pool.Exec(ctx, stmt, recTestWorkspaceSlug); err != nil {
			return fmt.Errorf("cleanup %q: %w", stmt, err)
		}
	}
	if _, err := pool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, recTestEmail); err != nil {
		return fmt.Errorf("cleanup user: %w", err)
	}
	return nil
}

func resetRecState(t *testing.T, ctx context.Context) {
	t.Helper()
	for _, stmt := range []string{
		`DELETE FROM cerebro_feature_flags WHERE workspace_id = $1`,
		`DELETE FROM cerebro_issue_recurrence_log WHERE recurrence_id IN (SELECT id FROM cerebro_issue_recurrence WHERE workspace_id = $1)`,
		`DELETE FROM cerebro_issue_recurrence WHERE workspace_id = $1`,
		`DELETE FROM attachment WHERE workspace_id = $1`,
		`DELETE FROM issue WHERE workspace_id = $1`,
	} {
		if _, err := recTestPool.Exec(ctx, stmt, recTestWorkspaceID); err != nil {
			t.Fatalf("reset %q: %v", stmt, err)
		}
	}
}

func enableRecFlag(t *testing.T, ctx context.Context) {
	t.Helper()
	if _, err := recTestPool.Exec(ctx, `
		INSERT INTO cerebro_feature_flags (workspace_id, user_id, flag_key, enabled)
		VALUES ($1, '00000000-0000-0000-0000-000000000000', 'cerebro_recurring_issues', TRUE)
		ON CONFLICT (workspace_id, user_id, flag_key) DO UPDATE SET enabled = TRUE
	`, recTestWorkspaceID); err != nil {
		t.Fatalf("enable flag: %v", err)
	}
}

func newRecSweeper(t *testing.T, now time.Time) *Sweeper {
	t.Helper()
	sw := NewSweeper(recTestPool, cerebrodb.New(recTestPool), db.New(recTestPool))
	sw.nowFunc = func() time.Time { return now }
	return sw
}

// seedIssue creates an upstream issue and returns it.
func seedIssue(t *testing.T, ctx context.Context, status string, due time.Time) db.Issue {
	t.Helper()
	q := db.New(recTestPool)
	number, err := q.IncrementIssueCounter(ctx, recTestWorkspaceID)
	if err != nil {
		t.Fatalf("issue counter: %v", err)
	}
	issue, err := q.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID: recTestWorkspaceID,
		Title:       "Weekly campaign check",
		Description: pgtype.Text{String: "Check the campaign", Valid: true},
		Status:      status,
		Priority:    "high",
		CreatorType: "member",
		CreatorID:   recTestUserID,
		Position:    0,
		DueDate:     pgtype.Date{Time: due, Valid: true},
		Number:      number,
		ProjectID:   recTestProjectID,
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	return issue
}

func seedRecurrence(t *testing.T, ctx context.Context, sourceIssue pgtype.UUID, armed bool) cerebrodb.CerebroIssueRecurrence {
	t.Helper()
	q := cerebrodb.New(recTestPool)
	rec, err := q.CreateCerebroIssueRecurrence(ctx, cerebrodb.CreateCerebroIssueRecurrenceParams{
		WorkspaceID:    recTestWorkspaceID,
		ProjectID:      recTestProjectID,
		SourceIssueID:  sourceIssue,
		Frequency:      FreqWeekly,
		IntervalCount:  1,
		Weekdays:       []int16{},
		DaysAfter:      0,
		TriggerStatus:  "done",
		Anchor:         AnchorDueDate,
		CreateNewIssue: true,
		NewStatus:      "todo",
		RecurForever:   true,
		Armed:          armed,
		Enabled:        true,
		CreatedByType:  pgtype.Text{String: "member", Valid: true},
		CreatedByID:    recTestUserID,
	})
	if err != nil {
		t.Fatalf("create recurrence: %v", err)
	}
	return rec
}

func countIssues(t *testing.T, ctx context.Context) int {
	t.Helper()
	var n int
	if err := recTestPool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE workspace_id = $1`, recTestWorkspaceID).Scan(&n); err != nil {
		t.Fatalf("count issues: %v", err)
	}
	return n
}

func setIssueStatus(t *testing.T, ctx context.Context, id pgtype.UUID, status string) {
	t.Helper()
	if _, err := recTestPool.Exec(ctx, `UPDATE issue SET status = $2 WHERE id = $1`, id, status); err != nil {
		t.Fatalf("set status: %v", err)
	}
}

// TestSweeper_SkipsWhenFlagOff: with the flag OFF (default), closing a
// recurring issue must NOT spawn the next occurrence.
func TestSweeper_SkipsWhenFlagOff(t *testing.T) {
	if recTestPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	resetRecState(t, ctx)

	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	issue := seedIssue(t, ctx, "done", now)
	seedRecurrence(t, ctx, issue.ID, true)

	sw := newRecSweeper(t, now)
	if err := sw.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if got := countIssues(t, ctx); got != 1 {
		t.Fatalf("flag OFF must not spawn; issue count = %d, want 1", got)
	}
}

// TestSweeper_SpawnsNextOnClose: the core FIR-334 behaviour. A done issue
// spawns the next occurrence with the same data and a +1 week due date
// (anchor=due_date), the new issue is todo, and the rule repoints + re-arms.
func TestSweeper_SpawnsNextOnClose(t *testing.T) {
	if recTestPool == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()
	resetRecState(t, ctx)
	enableRecFlag(t, ctx)

	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	due := time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC)
	issue := seedIssue(t, ctx, "in_progress", due)
	rec := seedRecurrence(t, ctx, issue.ID, true)

	// Close the issue, then sweep.
	setIssueStatus(t, ctx, issue.ID, "done")
	sw := newRecSweeper(t, now)
	if err := sw.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}

	// A second issue must now exist.
	if got := countIssues(t, ctx); got != 2 {
		t.Fatalf("expected 2 issues after spawn, got %d", got)
	}

	// Find the spawned issue (the one that is not the original).
	rows, err := recTestPool.Query(ctx,
		`SELECT id, title, status, priority, due_date FROM issue WHERE workspace_id = $1 AND id <> $2`,
		recTestWorkspaceID, issue.ID)
	if err != nil {
		t.Fatalf("query spawned: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("no spawned issue found")
	}
	var (
		spawnedID pgtype.UUID
		title     string
		status    string
		priority  string
		dueDate   pgtype.Date
	)
	if err := rows.Scan(&spawnedID, &title, &status, &priority, &dueDate); err != nil {
		t.Fatalf("scan spawned: %v", err)
	}
	rows.Close()

	if title != "Weekly campaign check" {
		t.Errorf("spawned title = %q, want copied title", title)
	}
	if status != "todo" {
		t.Errorf("spawned status = %q, want todo", status)
	}
	if priority != "high" {
		t.Errorf("spawned priority = %q, want copied high", priority)
	}
	wantDue := "2026-06-24"
	if dueDate.Time.Format("2006-01-02") != wantDue {
		t.Errorf("spawned due_date = %s, want %s (+1 week from due date)", dueDate.Time.Format("2006-01-02"), wantDue)
	}

	// Rule must repoint to the new issue, bump the count, and re-arm
	// (new issue is todo != done).
	q := cerebrodb.New(recTestPool)
	updated, err := q.GetCerebroIssueRecurrence(ctx, rec.ID)
	if err != nil {
		t.Fatalf("get recurrence: %v", err)
	}
	if !uuidEqual(updated.SourceIssueID, spawnedID) {
		t.Errorf("recurrence source not repointed to spawned issue")
	}
	if updated.OccurrenceCount != 1 {
		t.Errorf("occurrence_count = %d, want 1", updated.OccurrenceCount)
	}
	if !updated.Armed {
		t.Errorf("rule should be re-armed after spawning into a non-trigger status")
	}

	// A second tick must NOT double-spawn: the new issue is todo, so the
	// rule is armed-but-not-triggered.
	if err := sw.Tick(ctx); err != nil {
		t.Fatalf("second tick: %v", err)
	}
	if got := countIssues(t, ctx); got != 2 {
		t.Fatalf("second tick must not double-spawn; issue count = %d, want 2", got)
	}
}
