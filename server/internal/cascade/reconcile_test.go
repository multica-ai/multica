package cascade

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func reconTestPool(t *testing.T) *pgxpool.Pool {
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

func reconMakeWorkspace(t *testing.T, pool *pgxpool.Pool) (uuid.UUID, func()) {
	t.Helper()
	ctx := context.Background()
	var userID, wsID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('r','recon-'||$1||'@x') RETURNING id`,
		uuid.New().String()).Scan(&userID); err != nil {
		t.Fatalf("user: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug) VALUES ('r', $1) RETURNING id`,
		"recon-"+uuid.New().String()[:8]).Scan(&wsID); err != nil {
		t.Fatalf("ws: %v", err)
	}
	_, _ = pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, wsID, userID)
	wsUUID, _ := uuid.Parse(wsID)
	return wsUUID, func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	}
}

func TestReconciler_NudgesStuckCascades(t *testing.T) {
	pool := reconTestPool(t)
	if pool == nil {
		return
	}
	ws, cleanup := reconMakeWorkspace(t, pool)
	defer cleanup()

	// Owner user_id for creator_id.
	var creator string
	_ = pool.QueryRow(context.Background(), `SELECT user_id::text FROM member WHERE workspace_id = $1`, ws).Scan(&creator)

	// Three issues: one stuck (last event 30h ago), one fresh (1h
	// ago, must not nudge), one not in cascade at all.
	old := time.Now().Add(-30 * time.Hour)
	fresh := time.Now().Add(-1 * time.Hour)

	var stuckID, freshID, plainID string
	if err := pool.QueryRow(context.Background(), `
        INSERT INTO issue (workspace_id, title, status, creator_type, creator_id, number,
                            cascade_state, cascade_started_at, cascade_last_event_at)
        VALUES ($1, 'stuck', 'in_progress', 'member', $2, 100,
                'approved', now() - interval '40 hours', $3)
        RETURNING id`, ws, creator, old).Scan(&stuckID); err != nil {
		t.Fatalf("insert stuck: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `
        INSERT INTO issue (workspace_id, title, status, creator_type, creator_id, number,
                            cascade_state, cascade_started_at, cascade_last_event_at)
        VALUES ($1, 'fresh', 'in_progress', 'member', $2, 101,
                'approved', now() - interval '2 hours', $3)
        RETURNING id`, ws, creator, fresh).Scan(&freshID); err != nil {
		t.Fatalf("insert fresh: %v", err)
	}
	if err := pool.QueryRow(context.Background(), `
        INSERT INTO issue (workspace_id, title, status, creator_type, creator_id, number)
        VALUES ($1, 'plain', 'in_progress', 'member', $2, 102)
        RETURNING id`, ws, creator).Scan(&plainID); err != nil {
		t.Fatalf("insert plain: %v", err)
	}

	var notified atomic.Int64
	var mu sync.Mutex
	var reports []StuckCascadeReport
	notify := func(_ context.Context, r StuckCascadeReport) {
		notified.Add(1)
		mu.Lock()
		reports = append(reports, r)
		mu.Unlock()
	}

	r := NewReconciler(pool, notify, nil)
	r.RunOnce(context.Background())

	if notified.Load() != 1 {
		t.Fatalf("expected 1 nudge (stuck only), got %d. Reports: %+v", notified.Load(), reports)
	}
	if reports[0].IssueID != stuckID {
		t.Errorf("nudged wrong issue: got %q, want %q", reports[0].IssueID, stuckID)
	}
	if reports[0].StalenessHours < 24 {
		t.Errorf("staleness should be >= 24h, got %f", reports[0].StalenessHours)
	}

	// After nudge, cascade_last_event_at must be ~now so the next
	// reconciliation pass within 24h doesn't double-nudge.
	var newLast time.Time
	_ = pool.QueryRow(context.Background(),
		`SELECT cascade_last_event_at FROM issue WHERE id = $1`, stuckID).Scan(&newLast)
	if time.Since(newLast) > 10*time.Second {
		t.Errorf("cascade_last_event_at not refreshed: %v ago", time.Since(newLast))
	}

	// Run again immediately — must not nudge.
	notified.Store(0)
	r.RunOnce(context.Background())
	if notified.Load() != 0 {
		t.Errorf("second pass should not re-nudge: got %d", notified.Load())
	}
}

func TestReconciler_CleansOldRetriggers(t *testing.T) {
	pool := reconTestPool(t)
	if pool == nil {
		return
	}
	ws, cleanup := reconMakeWorkspace(t, pool)
	defer cleanup()

	var creator string
	_ = pool.QueryRow(context.Background(), `SELECT user_id::text FROM member WHERE workspace_id = $1`, ws).Scan(&creator)

	var issueID string
	_ = pool.QueryRow(context.Background(),
		`INSERT INTO issue (workspace_id, title, status, creator_type, creator_id, number)
         VALUES ($1, 't', 'in_progress', 'member', $2, 200) RETURNING id`, ws, creator).Scan(&issueID)

	// Insert two retriggers — one >30d old, one fresh.
	old := uuid.New()
	fresh := uuid.New()
	_, _ = pool.Exec(context.Background(), `
        INSERT INTO cascade_retrigger (event_id, issue_id, pr_url, pr_number, head_sha, event_type, fired_at)
        VALUES ($1, $2, 'u1', 1, 's1', 'ci_failure', now() - interval '40 days'),
               ($3, $2, 'u2', 2, 's2', 'ci_failure', now() - interval '1 day')`,
		old, issueID, fresh)

	r := NewReconciler(pool, nil, nil)
	r.RunOnce(context.Background())

	var oldExists, freshExists bool
	_ = pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM cascade_retrigger WHERE event_id = $1)`, old).Scan(&oldExists)
	_ = pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM cascade_retrigger WHERE event_id = $1)`, fresh).Scan(&freshExists)
	if oldExists {
		t.Errorf("40-day-old retrigger should have been deleted")
	}
	if !freshExists {
		t.Errorf("1-day-old retrigger must survive")
	}
	// Cleanup remaining row.
	_, _ = pool.Exec(context.Background(), `DELETE FROM cascade_retrigger WHERE event_id = $1`, fresh)
}
