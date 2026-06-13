package semantic

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// dbPool returns the shared test pool or skips when unreachable. Each test
// in this file is independent and tolerates a missing DB so the unit tests
// in provider_test.go can run anywhere.
func dbPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Skipf("DB unavailable: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		t.Skipf("DB ping failed: %v", err)
	}
	t.Cleanup(pool.Close)
	if !StoreAvailable(context.Background(), pool) {
		t.Skip("pgvector store unavailable — skipping vector integration tests")
	}
	return pool
}

func TestWorker_DrainsQueueAndUpsertsEmbedding(t *testing.T) {
	pool := dbPool(t)
	ctx := context.Background()

	// Seed a workspace + member + issue. The migration triggers will enqueue
	// the issue automatically.
	wsID, userID := seedWorkspace(t, pool)
	issueID := seedIssue(t, pool, wsID, userID, "Deploy pipeline fails intermittently", "")

	w := NewWorker(pool, NewDeterministicProvider(), Config{
		Enabled:   true,
		Provider:  "deterministic",
		BatchSize: 8,
	})

	if err := tickUntilEmbedding(ctx, t, pool, w, issueID); err != nil {
		t.Fatalf("worker tick: %v", err)
	}

	// Embedding row should now exist.
	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM cerebro_search_embedding WHERE target_type='issue' AND target_id=$1`,
		issueID,
	).Scan(&count); err != nil {
		t.Fatalf("count embedding: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 embedding row, got %d", count)
	}

	// Queue row should now be drained.
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM cerebro_search_embedding_queue WHERE target_type='issue' AND target_id=$1`,
		issueID,
	).Scan(&count); err != nil {
		t.Fatalf("count queue: %v", err)
	}
	if count != 0 {
		t.Errorf("expected queue row drained, got %d remaining", count)
	}

	// Re-running with no fresh writes must not requeue this issue. Other
	// packages share the CI database and may enqueue unrelated rows in
	// parallel, so assert on the target row instead of the global processed
	// count.
	if _, err := w.Tick(ctx); err != nil {
		t.Fatalf("idle worker tick: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM cerebro_search_embedding_queue WHERE target_type='issue' AND target_id=$1`,
		issueID,
	).Scan(&count); err != nil {
		t.Fatalf("count queue after idle tick: %v", err)
	}
	if count != 0 {
		t.Errorf("expected no queue row for target after idle tick, got %d", count)
	}
}

func TestNearestNeighbours_ReturnsClosestEmbedding(t *testing.T) {
	pool := dbPool(t)
	ctx := context.Background()

	wsID, userID := seedWorkspace(t, pool)

	// Two issues with hand-crafted vectors: one near, one far from the query.
	nearVec := unitVec(7)
	farVec := unitVec(150)
	queryVec := unitVec(7) // same bucket as near → cosine ≈ 1

	provider := NewLookupProvider("test:nn", map[string][]float32{
		"near issue": nearVec,
		"far issue":  farVec,
	})

	nearID := seedIssue(t, pool, wsID, userID, "near issue", "")
	farID := seedIssue(t, pool, wsID, userID, "far issue", "")

	w := NewWorker(pool, provider, Config{Enabled: true, Provider: "deterministic", BatchSize: 16})
	if _, err := w.Tick(ctx); err != nil {
		t.Fatalf("worker tick: %v", err)
	}

	hits, err := NearestNeighbours(ctx, pool, wsID, queryVec, 5)
	if err != nil {
		t.Fatalf("nearest neighbours: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected at least one hit, got 0")
	}
	if hits[0].TargetID != nearID {
		t.Errorf("expected near issue (%s) on top, got %s (far=%s)",
			nearID.String(), hits[0].TargetID.String(), farID.String())
	}
}

// --- fixtures ---

// seedWorkspace creates a throwaway workspace + member + user. Uses fixed
// fixture names so a leftover row from a crashed run is overwritten on the
// next call.
func seedWorkspace(t *testing.T, pool *pgxpool.Pool) (pgtype.UUID, pgtype.UUID) {
	t.Helper()
	ctx := context.Background()
	var userID, wsID pgtype.UUID

	// User
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (email, name)
		VALUES ($1, $2)
		ON CONFLICT (email) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, "semantic-test@multica.ai", "Semantic Test").Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	// Workspace
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug)
		VALUES ($1, $2)
		ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, "Semantic Tests", "semantic-tests").Scan(&wsID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	// Membership
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'admin')
		ON CONFLICT (workspace_id, user_id) DO NOTHING
	`, wsID, userID); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	t.Cleanup(func() {
		// Cascade through workspace; cerebro_search tables ride the issue
		// delete trigger from migration 9056.
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})
	return wsID, userID
}

func seedIssue(t *testing.T, pool *pgxpool.Pool, wsID, userID pgtype.UUID, title, description string) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	var id pgtype.UUID
	const sql = `INSERT INTO issue (workspace_id, title, description, status, priority, kind, creator_type, creator_id, number)
		VALUES ($1, $2, $3, 'todo', 'none', 'issue', 'member', $4,
			(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id`
	if err := pool.QueryRow(ctx, sql, wsID, title, description, userID).Scan(&id); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, id)
	})
	return id
}

func tickUntilEmbedding(ctx context.Context, t *testing.T, pool *pgxpool.Pool, w *Worker, issueID pgtype.UUID) error {
	t.Helper()
	for i := 0; i < 20; i++ {
		if _, err := w.Tick(ctx); err != nil {
			return err
		}
		var count int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM cerebro_search_embedding WHERE target_type='issue' AND target_id=$1`,
			issueID,
		).Scan(&count); err != nil {
			return err
		}
		if count == 1 {
			return nil
		}
	}
	t.Fatalf("expected worker to process the seeded queue row")
	return nil
}

// unitVec returns a EmbeddingDim-length unit vector with weight at one bucket.
func unitVec(bucket int) []float32 {
	v := make([]float32, EmbeddingDim)
	v[bucket%EmbeddingDim] = 1
	return v
}
