package loops

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var loopTestPool *pgxpool.Pool

func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping loops integration tests: %v\n", err)
		os.Exit(m.Run())
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping loops integration tests: db not reachable: %v\n", err)
		pool.Close()
		os.Exit(m.Run())
	}
	loopTestPool = pool
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

func seedIssue(t *testing.T) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	var workspaceID, issueID pgtype.UUID
	if err := loopTestPool.QueryRow(ctx,
		`INSERT INTO workspace (name, slug) VALUES ('Loops Test', 'loops-test-'||gen_random_uuid()) RETURNING id`).
		Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := loopTestPool.QueryRow(ctx,
		`INSERT INTO issue (workspace_id, title, creator_type, creator_id)
		 VALUES ($1, 'Chain state test', 'member', gen_random_uuid()) RETURNING id`,
		workspaceID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = loopTestPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	})
	return issueID
}

func uuidToString(u pgtype.UUID) string {
	b := u.Bytes
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
