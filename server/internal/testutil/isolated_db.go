package testutil

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// IsolatedDB creates an empty database on the explicitly supplied test server
// and applies the normal migration command, including its hooks and conditions.
// The role must have CREATEDB. Call from within the server source tree; cleanup
// closes the pool before dropping the database, after callers' fixture cleanups.
// Unlike workspace fixtures, this also isolates queries that scan all workspaces.
func IsolatedDB(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	if dsn == "" {
		t.Fatal("isolated database requires an explicit disposable test DSN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test database server: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := admin.Close(ctx); err != nil {
			t.Errorf("close test database admin connection: %v", err)
		}
	})

	name := "multica_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	ident := pgx.Identifier{name}.Sanitize()
	// template0 is empty even when the shared test database contains active
	// fixtures from other packages. Never clone those rows into this test.
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+ident+" TEMPLATE template0"); err != nil {
		t.Fatalf("create isolated test database: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP DATABASE "+ident+" WITH (FORCE)"); err != nil {
			t.Errorf("drop isolated test database %s: %v", name, err)
		}
	})

	targetDSN := isolatedDatabaseDSN(t, dsn, name)
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(root, "cmd", "migrate", "main.go")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatal("server source directory not found for test database migrations")
		}
		root = parent
	}
	// Only the child receives the new DSN. Changing the process environment
	// would redirect other tests that are still using the shared database.
	migrateCtx, migrateCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer migrateCancel()
	// Execute the migrator directly so cancellation terminates it, rather
	// than just the parent go-run process while the migrator keeps running.
	binary := filepath.Join(t.TempDir(), "migrate.exe")
	build := exec.CommandContext(migrateCtx, "go", "build", "-o", binary, "./cmd/migrate")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build test database migrator: %v\n%s", err, output)
	}
	cmd := exec.CommandContext(migrateCtx, binary, "up")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "DATABASE_URL="+targetDSN)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("migrate isolated test database: %v\n%s", err, output)
	}

	poolCtx, poolCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer poolCancel()
	pool, err := pgxpool.New(poolCtx, targetDSN)
	if err != nil {
		t.Fatalf("open isolated test database: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(poolCtx); err != nil {
		t.Fatalf("ping isolated test database: %v", err)
	}
	return pool
}

func isolatedDatabaseDSN(t *testing.T, dsn, name string) string {
	t.Helper()
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		u.Path, u.RawPath = "/"+name, ""
		q := u.Query()
		// Both spellings override the URL path in pgx.
		q.Del("database")
		q.Del("dbname")
		u.RawQuery = q.Encode()
		return u.String()
	}
	// In keyword/value DSNs the last database setting wins. name contains
	// only the generated prefix and hex digits, so needs no value escaping.
	return dsn + " dbname=" + name
}
