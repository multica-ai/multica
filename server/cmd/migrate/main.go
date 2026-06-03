package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/migrations"
)

// migrationAdvisoryLockKey is the int64 identifier used with Postgres
// pg_advisory_lock to serialize the migration loop across concurrent
// runners (multi-replica backend Deployment, scale-up, or a manual
// `migrate up` overlapping with pod startup). The exact value is
// arbitrary — it just needs to be stable across every process that runs
// migrations against the same database. See GitHub multica-ai/multica#3647.
const migrationAdvisoryLockKey int64 = 7244554146635925501

func main() {
	logger.Init()

	if len(os.Args) < 2 {
		fmt.Println("Usage: go run ./cmd/migrate <up|down>")
		os.Exit(1)
	}

	direction := os.Args[1]
	if direction != "up" && direction != "down" {
		fmt.Println("Usage: go run ./cmd/migrate <up|down>")
		os.Exit(1)
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.Error("unable to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		slog.Error("unable to ping database", "error", err)
		os.Exit(1)
	}

	// Serialize the entire migration run with a Postgres session-level
	// advisory lock. pg_advisory_lock is scoped to a single session, so we
	// must pin one *pgxpool.Conn for the whole run — calling pool.Exec
	// would attach the lock to a random connection that pgxpool could
	// hand back out before the loop finishes, making the lock effectively
	// a no-op. We use the blocking pg_advisory_lock (not pg_try_*) so a
	// late-arriving pod queues behind the current runner instead of
	// crash-looping; once it acquires the lock the EXISTS checks below
	// turn into a no-op skip. See GitHub multica-ai/multica#3647.
	//
	// We deliberately do NOT wrap the loop in a single transaction: the
	// repo already ships migrations using CREATE INDEX CONCURRENTLY,
	// which Postgres rejects inside a transaction block.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		slog.Error("unable to acquire migration connection", "error", err)
		os.Exit(1)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationAdvisoryLockKey); err != nil {
		slog.Error("failed to acquire migration advisory lock", "error", err)
		os.Exit(1)
	}
	// Best-effort explicit unlock on the success path. On os.Exit error
	// paths this defer does not run, but session-level advisory locks are
	// released automatically when the connection closes at process exit,
	// so the next runner is never permanently blocked.
	defer func() {
		if _, err := conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", migrationAdvisoryLockKey); err != nil {
			slog.Warn("failed to release migration advisory lock", "error", err)
		}
	}()

	// Create migrations tracking table
	_, err = conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	if err != nil {
		slog.Error("failed to create migrations table", "error", err)
		os.Exit(1)
	}

	files, err := migrations.Files(direction)
	if err != nil {
		slog.Error("failed to find migration files", "error", err)
		os.Exit(1)
	}

	for _, file := range files {
		version := migrations.ExtractVersion(file)

		if direction == "up" {
			// Check if already applied
			var exists bool
			err := conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)", version).Scan(&exists)
			if err != nil {
				slog.Error("failed to check migration status", "version", version, "error", err)
				os.Exit(1)
			}
			if exists {
				fmt.Printf("  skip  %s (already applied)\n", version)
				continue
			}
		} else {
			// Check if applied (only rollback applied ones)
			var exists bool
			err := conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)", version).Scan(&exists)
			if err != nil {
				slog.Error("failed to check migration status", "version", version, "error", err)
				os.Exit(1)
			}
			if !exists {
				fmt.Printf("  skip  %s (not applied)\n", version)
				continue
			}
		}

		sql, err := os.ReadFile(file)
		if err != nil {
			slog.Error("failed to read migration file", "file", file, "error", err)
			os.Exit(1)
		}

		_, err = conn.Exec(ctx, string(sql))
		if err != nil {
			slog.Error("failed to run migration", "file", file, "error", err)
			os.Exit(1)
		}

		if direction == "up" {
			_, err = conn.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version)
		} else {
			_, err = conn.Exec(ctx, "DELETE FROM schema_migrations WHERE version = $1", version)
		}
		if err != nil {
			slog.Error("failed to record migration", "version", version, "error", err)
			os.Exit(1)
		}

		fmt.Printf("  %s  %s\n", direction, version)
	}

	fmt.Println("Done.")
}
