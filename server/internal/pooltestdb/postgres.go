package pooltestdb

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Open(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return open(t, nil)
}

func OpenWithSearchPath(t *testing.T, schemas ...string) *pgxpool.Pool {
	t.Helper()
	if len(schemas) == 0 {
		t.Fatal("at least one search path schema is required for Pool DB tests")
	}
	quotedSchemas := make([]string, len(schemas))
	for i, schema := range schemas {
		if strings.TrimSpace(schema) == "" {
			t.Fatal("search path schema must not be empty for Pool DB tests")
		}
		quotedSchemas[i] = pgx.Identifier{schema}.Sanitize()
	}
	searchPath := strings.Join(quotedSchemas, ",")
	return open(t, func(config *pgxpool.Config) {
		if config.ConnConfig.RuntimeParams == nil {
			config.ConnConfig.RuntimeParams = make(map[string]string)
		}
		config.ConnConfig.RuntimeParams["search_path"] = searchPath
	})
}

func open(t *testing.T, configure func(*pgxpool.Config)) *pgxpool.Pool {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dsn == "" {
		t.Fatal("DATABASE_URL is required for Pool DB tests")
	}

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("open Pool test database: %v", err)
	}
	if configure != nil {
		configure(config)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open Pool test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("reach Pool test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
