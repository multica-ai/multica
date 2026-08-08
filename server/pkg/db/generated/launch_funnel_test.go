package db

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestListLaunchFunnelEventsDeduplicatesAndExcludesTestWorkspace(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set; skipping live-Postgres funnel query test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	workspaceID := uuid.New()
	userID := uuid.New()
	runtimeID := uuid.New()
	agentID := uuid.New()
	firstIssueID := uuid.New()
	secondIssueID := uuid.New()
	base := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, query, args...); err != nil {
			t.Fatalf("seed query failed: %v\n%s", err, query)
		}
	}

	exec(`INSERT INTO "user" (id, name, email, acquisition_attribution, created_at, updated_at)
		VALUES ($1, 'Funnel Test', $2, '{"source":"validation","medium":"test","campaign":"har14"}', $3, $3)`,
		userID, "har14-"+userID.String()+"@example.test", base)
	exec(`INSERT INTO workspace (id, name, slug, created_at, updated_at)
		VALUES ($1, 'Funnel Test', $2, $3, $3)`, workspaceID, "har14-"+workspaceID.String(), base)
	exec(`INSERT INTO member (id, workspace_id, user_id, role, created_at)
		VALUES ($1, $2, $3, 'owner', $4)`, uuid.New(), workspaceID, userID, base)
	exec(`INSERT INTO agent_runtime
		(id, workspace_id, name, runtime_mode, provider, status, created_at, updated_at)
		VALUES ($1, $2, 'Test Runtime', 'local', 'codex', 'online', $3, $3)`,
		runtimeID, workspaceID, base.Add(time.Minute))
	exec(`INSERT INTO agent
		(id, workspace_id, name, runtime_mode, runtime_id, owner_id, created_at, updated_at)
		VALUES ($1, $2, 'Test Agent', 'local', $3, $4, $5, $5)`,
		agentID, workspaceID, runtimeID, userID, base.Add(2*time.Minute))
	exec(`INSERT INTO issue
		(id, workspace_id, number, title, status, assignee_type, assignee_id, creator_type, creator_id, created_at, updated_at)
		VALUES ($1, $2, 1, 'First', 'in_review', 'agent', $3, 'member', $4, $5, $5),
		       ($6, $2, 2, 'Second', 'todo', 'agent', $3, 'member', $4, $7, $7)`,
		firstIssueID, workspaceID, agentID, userID, base.Add(3*time.Minute), secondIssueID, base.Add(9*time.Minute))
	exec(`INSERT INTO agent_task_queue
		(id, agent_id, issue_id, runtime_id, status, created_at, started_at, completed_at)
		VALUES ($1, $2, $3, $4, 'completed', $5, $6, $7),
		       ($8, $2, $3, $4, 'completed', $9, $10, $11),
		       ($12, $2, $13, $4, 'queued', $14, NULL, NULL)`,
		uuid.New(), agentID, firstIssueID, runtimeID,
		base.Add(4*time.Minute), base.Add(5*time.Minute), base.Add(7*time.Minute),
		uuid.New(), base.Add(6*time.Minute), base.Add(6*time.Minute), base.Add(8*time.Minute),
		uuid.New(), secondIssueID, base.Add(10*time.Minute))
	exec(`INSERT INTO activity_log
		(id, workspace_id, issue_id, actor_type, actor_id, action, details, created_at)
		VALUES ($1, $2, $3, 'agent', $4, 'status_changed', '{"from":"in_progress","to":"in_review"}', $5)`,
		uuid.New(), workspaceID, firstIssueID, agentID, base.Add(8*time.Minute))

	queries := New(tx)
	params := ListLaunchFunnelEventsParams{
		FromTime:             pgtype.Timestamptz{Time: base.Add(-time.Minute), Valid: true},
		ToTime:               pgtype.Timestamptz{Time: base.Add(time.Hour), Valid: true},
		ExcludedWorkspaceIds: nil,
	}
	events, err := queries.ListLaunchFunnelEvents(ctx, params)
	if err != nil {
		t.Fatalf("list funnel events: %v", err)
	}

	wantNames := []string{
		"workspace_created",
		"runtime_connected",
		"agent_created",
		"issue_assigned",
		"task_started",
		"issue_in_review",
		"second_issue_assigned",
	}
	if len(events) != len(wantNames) {
		t.Fatalf("event count = %d, want %d: %#v", len(events), len(wantNames), events)
	}
	for i, want := range wantNames {
		if events[i].EventName != want {
			t.Errorf("event[%d] = %q, want %q", i, events[i].EventName, want)
		}
		if events[i].Source != "validation" || events[i].Campaign != "har14" {
			t.Errorf("event[%d] attribution = %q/%q, want validation/har14", i, events[i].Source, events[i].Campaign)
		}
	}

	params.ExcludedWorkspaceIds = []pgtype.UUID{{Bytes: workspaceID, Valid: true}}
	excluded, err := queries.ListLaunchFunnelEvents(ctx, params)
	if err != nil {
		t.Fatalf("list excluded funnel events: %v", err)
	}
	if len(excluded) != 0 {
		t.Fatalf("excluded event count = %d, want 0", len(excluded))
	}
}
