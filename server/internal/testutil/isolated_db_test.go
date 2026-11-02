package testutil

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestIsolatedDatabaseDSN(t *testing.T) {
	for _, dsn := range []string{
		"postgres://tester:p%40ss@localhost:5432/original?sslmode=disable",
		"postgresql://tester:p%40ss@localhost:5432/original?database=override&dbname=other&sslmode=disable",
		"host=localhost port=5432 user=tester password='p@ss' dbname=original sslmode=disable",
		"host=localhost port=5432 user=tester password='p@ss' database=original dbname=override sslmode=disable",
	} {
		config, err := pgx.ParseConfig(isolatedDatabaseDSN(t, dsn, "multica_test_target"))
		if err != nil {
			t.Fatal(err)
		}
		if config.Database != "multica_test_target" || config.User != "tester" || config.Password != "p@ss" || config.TLSConfig != nil {
			t.Fatal("database override did not preserve the other connection settings")
		}
	}
}

func TestIsolatedDBCleanup(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("explicit disposable DATABASE_URL required")
	}
	var name string
	t.Run("migrated database", func(t *testing.T) {
		pool := IsolatedDB(t, dsn)
		var migrated bool
		if err := pool.QueryRow(context.Background(), `SELECT current_database(),
			to_regclass('public.channel_installation') IS NOT NULL AND
			to_regclass('public.channel_chat_session_binding') IS NOT NULL`).Scan(&name, &migrated); err != nil {
			t.Fatal(err)
		}
		if !migrated {
			t.Fatal("isolated database is missing migrated channel tables")
		}
		if os.Getenv("DATABASE_URL") != dsn {
			t.Fatal("isolated database changed process DATABASE_URL")
		}
	})
	if name == "" {
		t.Fatal("isolated database was not created")
	}
	admin, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(context.Background())
	var exists bool
	if err := admin.QueryRow(context.Background(), "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname=$1)", name).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("isolated database survived test cleanup")
	}
}
