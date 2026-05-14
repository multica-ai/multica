package cascade

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testDBPool reuses the multica dev default when DATABASE_URL is
// unset (same pattern internal/handler/handler_test.go uses). Tests
// skip when the DB is unreachable so local `go test` works without
// docker-compose running.
func testDBPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Skipf("no database: %v", err)
		return nil
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database not reachable: %v", err)
		return nil
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

// makeWorkspaceAndUser creates a throwaway workspace + member so the
// cascade test rows have a parent. Returns workspaceID and a cleanup
// func that drops everything created.
func makeWorkspaceAndUser(t *testing.T, pool *pgxpool.Pool) (uuid.UUID, func()) {
	t.Helper()
	ctx := context.Background()

	var userID, workspaceID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('test', $1) RETURNING id`,
		"cascade-test-"+uuid.New().String()+"@example.com").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug) VALUES ('cascade-test', $1) RETURNING id`,
		"cascade-test-"+uuid.New().String()[:8]).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`,
		workspaceID, userID); err != nil {
		t.Fatalf("insert member: %v", err)
	}
	wsUUID, err := uuid.Parse(workspaceID)
	if err != nil {
		t.Fatalf("parse workspace uuid: %v", err)
	}
	cleanup := func() {
		// Order: member → workspace (CASCADE cleans issues + cascade rows) → user.
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	}
	return wsUUID, cleanup
}

// insertCascadeIssue creates a minimal issue with cascade fields populated.
func insertCascadeIssue(t *testing.T, pool *pgxpool.Pool, workspaceID uuid.UUID, number int, state string, lastEventAt *time.Time, progressJSON string) uuid.UUID {
	t.Helper()
	var id string
	var lea any
	if lastEventAt != nil {
		lea = *lastEventAt
	}
	var pj any
	if progressJSON != "" {
		pj = progressJSON
	}
	// creator_id NOT NULL; reuse workspace owner's user_id by joining member.
	var creatorID string
	if err := pool.QueryRow(context.Background(),
		`SELECT user_id::text FROM member WHERE workspace_id = $1 LIMIT 1`, workspaceID).Scan(&creatorID); err != nil {
		t.Fatalf("lookup owner: %v", err)
	}

	if err := pool.QueryRow(context.Background(), `
        INSERT INTO issue (workspace_id, title, status, creator_type, creator_id, number,
                            cascade_state, cascade_started_at, cascade_last_event_at, cascade_progress)
        VALUES ($1, $2, 'in_progress', 'member', $3, $4, $5, now() - interval '5 minutes', $6, $7::jsonb)
        RETURNING id`,
		workspaceID, "test issue "+state, creatorID, number, state, lea, pj,
	).Scan(&id); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	u, _ := uuid.Parse(id)
	return u
}

func TestListCascades_WorkspaceFilter(t *testing.T) {
	pool := testDBPool(t)
	if pool == nil {
		return
	}
	ws, cleanup := makeWorkspaceAndUser(t, pool)
	defer cleanup()

	// Two cascades in our workspace, none in another.
	insertCascadeIssue(t, pool, ws, 1, "approved", nil, `{"total_prs":3,"current_step":1}`)
	insertCascadeIssue(t, pool, ws, 2, "paused", nil, `{"total_prs":5,"current_step":3}`)

	rows, err := ListCascades(context.Background(), pool, ListFilters{WorkspaceID: ws}, ListPage{})
	if err != nil {
		t.Fatalf("ListCascades: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	// Verify progress decoded.
	var sawApproved, sawPaused bool
	for _, r := range rows {
		switch r.CascadeState {
		case "approved":
			sawApproved = true
			if r.Progress == nil || r.Progress.TotalPRs != 3 {
				t.Errorf("approved row missing decoded progress: %+v", r.Progress)
			}
		case "paused":
			sawPaused = true
			if r.Progress == nil || r.Progress.CurrentStep != 3 {
				t.Errorf("paused row missing decoded progress: %+v", r.Progress)
			}
		}
	}
	if !sawApproved || !sawPaused {
		t.Errorf("expected both states represented, got %d rows", len(rows))
	}
}

func TestListCascades_StateFilter(t *testing.T) {
	pool := testDBPool(t)
	if pool == nil {
		return
	}
	ws, cleanup := makeWorkspaceAndUser(t, pool)
	defer cleanup()
	insertCascadeIssue(t, pool, ws, 10, "approved", nil, `{"total_prs":2,"current_step":1}`)
	insertCascadeIssue(t, pool, ws, 11, "paused", nil, `{"total_prs":2,"current_step":1}`)

	rows, err := ListCascades(context.Background(), pool, ListFilters{
		WorkspaceID:  ws,
		CascadeState: "paused",
	}, ListPage{})
	if err != nil {
		t.Fatalf("ListCascades: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 paused row, got %d", len(rows))
	}
	if rows[0].CascadeState != "paused" {
		t.Errorf("got state %q, want paused", rows[0].CascadeState)
	}
}

func TestListCascades_PaginationClamp(t *testing.T) {
	pool := testDBPool(t)
	if pool == nil {
		return
	}
	ws, cleanup := makeWorkspaceAndUser(t, pool)
	defer cleanup()
	for i := 0; i < 3; i++ {
		insertCascadeIssue(t, pool, ws, 100+i, "approved", nil, `{"total_prs":1,"current_step":1}`)
	}

	rows, err := ListCascades(context.Background(), pool, ListFilters{WorkspaceID: ws}, ListPage{Page: 1, PerPage: 2})
	if err != nil {
		t.Fatalf("ListCascades: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("page 1 per_page 2: got %d rows", len(rows))
	}

	rows, err = ListCascades(context.Background(), pool, ListFilters{WorkspaceID: ws}, ListPage{Page: 2, PerPage: 2})
	if err != nil {
		t.Fatalf("ListCascades page 2: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("page 2 per_page 2: got %d rows", len(rows))
	}

	// PerPage too large → clamped to 50; we have 3 rows total so this returns 3.
	rows, err = ListCascades(context.Background(), pool, ListFilters{WorkspaceID: ws}, ListPage{PerPage: 9999})
	if err != nil {
		t.Fatalf("ListCascades large per_page: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("large per_page: got %d rows", len(rows))
	}
}

func TestListCascades_MalformedProgressDoesNotFail(t *testing.T) {
	pool := testDBPool(t)
	if pool == nil {
		return
	}
	ws, cleanup := makeWorkspaceAndUser(t, pool)
	defer cleanup()
	// Schema-valid JSON but violates Progress invariants
	// (current_step > total_prs).
	insertCascadeIssue(t, pool, ws, 200, "approved", nil, `{"total_prs":1,"current_step":99}`)

	rows, err := ListCascades(context.Background(), pool, ListFilters{WorkspaceID: ws}, ListPage{})
	if err != nil {
		t.Fatalf("ListCascades should swallow per-row decode errors: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row even with bad progress, got %d", len(rows))
	}
	if rows[0].Progress != nil {
		t.Errorf("expected nil Progress on bad row, got %+v", rows[0].Progress)
	}
}

func TestListCascades_RejectsZeroWorkspaceID(t *testing.T) {
	// Defensive: passing a zero UUID would otherwise match every
	// row with a zero workspace_id, which is a cross-tenant leak.
	_, err := ListCascades(context.Background(), nil, ListFilters{}, ListPage{})
	if err == nil {
		t.Fatal("expected error on zero WorkspaceID")
	}
}
