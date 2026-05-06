package localmode

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/util"
)

func TestBootstrapEnsuresLocalIdentityAndSpaceIdempotently(t *testing.T) {
	ctx := context.Background()
	pool := openBootstrapTestPool(t)
	defer pool.Close()

	cleanupLocalBootstrapFixture(t, pool)
	t.Cleanup(func() { cleanupLocalBootstrapFixture(t, pool) })

	bootstrapper := NewBootstrapper(pool)

	first, err := bootstrapper.EnsureLocal(ctx)
	if err != nil {
		t.Fatalf("EnsureLocal first call: %v", err)
	}

	if first.User.Email != "local@multica.local" {
		t.Fatalf("local user email = %q, want local@multica.local", first.User.Email)
	}
	if first.User.Name != "Local User" {
		t.Fatalf("local user name = %q, want Local User", first.User.Name)
	}
	if !first.User.OnboardedAt.Valid {
		t.Fatal("local user should be marked onboarded")
	}
	if first.Workspace.Name != "Local" {
		t.Fatalf("local workspace name = %q, want Local", first.Workspace.Name)
	}
	if first.Workspace.Slug != "local" {
		t.Fatalf("local workspace slug = %q, want local", first.Workspace.Slug)
	}
	if first.Workspace.IssuePrefix != "LOC" {
		t.Fatalf("local workspace issue prefix = %q, want LOC", first.Workspace.IssuePrefix)
	}
	if first.Member.Role != "owner" {
		t.Fatalf("local member role = %q, want owner", first.Member.Role)
	}
	if util.UUIDToString(first.Member.UserID) != util.UUIDToString(first.User.ID) {
		t.Fatalf("local member user ID = %s, want %s", util.UUIDToString(first.Member.UserID), util.UUIDToString(first.User.ID))
	}
	if util.UUIDToString(first.Member.WorkspaceID) != util.UUIDToString(first.Workspace.ID) {
		t.Fatalf("local member workspace ID = %s, want %s", util.UUIDToString(first.Member.WorkspaceID), util.UUIDToString(first.Workspace.ID))
	}

	second, err := bootstrapper.EnsureLocal(ctx)
	if err != nil {
		t.Fatalf("EnsureLocal second call: %v", err)
	}

	if util.UUIDToString(second.User.ID) != util.UUIDToString(first.User.ID) {
		t.Fatalf("second user ID = %s, want %s", util.UUIDToString(second.User.ID), util.UUIDToString(first.User.ID))
	}
	if util.UUIDToString(second.Workspace.ID) != util.UUIDToString(first.Workspace.ID) {
		t.Fatalf("second workspace ID = %s, want %s", util.UUIDToString(second.Workspace.ID), util.UUIDToString(first.Workspace.ID))
	}
	if util.UUIDToString(second.Member.ID) != util.UUIDToString(first.Member.ID) {
		t.Fatalf("second member ID = %s, want %s", util.UUIDToString(second.Member.ID), util.UUIDToString(first.Member.ID))
	}

	assertBootstrapRowCount(t, pool, `SELECT count(*) FROM "user" WHERE email = 'local@multica.local'`, 1)
	assertBootstrapRowCount(t, pool, `SELECT count(*) FROM workspace WHERE slug = 'local'`, 1)
	assertBootstrapRowCount(t, pool, `
		SELECT count(*)
		FROM member m
		JOIN "user" u ON u.id = m.user_id
		JOIN workspace w ON w.id = m.workspace_id
		WHERE u.email = 'local@multica.local' AND w.slug = 'local'
	`, 1)
}

func openBootstrapTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skipf("could not connect to database: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("database not reachable: %v", err)
	}
	return pool
}

func cleanupLocalBootstrapFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DELETE FROM workspace WHERE slug = 'local'`); err != nil {
		t.Fatalf("delete local workspace fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM "user" WHERE email = 'local@multica.local'`); err != nil {
		t.Fatalf("delete local user fixture: %v", err)
	}
}

func assertBootstrapRowCount(t *testing.T, pool *pgxpool.Pool, query string, want int) {
	t.Helper()

	var got int
	if err := pool.QueryRow(context.Background(), query).Scan(&got); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if got != want {
		t.Fatalf("%s: got %d rows, want %d", fmt.Sprintf("%q", query), got, want)
	}
}
