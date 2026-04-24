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

	// Create migrations tracking table
	_, err = pool.Exec(ctx, `
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
			err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)", version).Scan(&exists)
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
			err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)", version).Scan(&exists)
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

		_, err = pool.Exec(ctx, string(sql))
		if err != nil {
			slog.Error("failed to run migration", "file", file, "error", err)
			os.Exit(1)
		}

		if direction == "up" {
			_, err = pool.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version)
		} else {
			_, err = pool.Exec(ctx, "DELETE FROM schema_migrations WHERE version = $1", version)
		}
		if err != nil {
			slog.Error("failed to record migration", "version", version, "error", err)
			os.Exit(1)
		}

		fmt.Printf("  %s  %s\n", direction, version)
	}

	fmt.Println("Done.")
}
