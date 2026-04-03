package main

// MULTICA-LOCAL: SQLite migration runner (replaces PostgreSQL pgxpool-based runner).

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/multica-ai/multica/server/internal/logger"
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

	dbPath := os.Getenv("DATABASE_PATH")
	if dbPath == "" {
		homeDir, _ := os.UserHomeDir()
		dbPath = filepath.Join(homeDir, ".multica-local", "multica.db")
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		slog.Error("unable to create database directory", "error", err)
		os.Exit(1)
	}

	sqlDB, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)")
	if err != nil {
		slog.Error("unable to open database", "error", err)
		os.Exit(1)
	}
	defer sqlDB.Close()

	sqlDB.SetMaxOpenConns(1)

	if err := sqlDB.Ping(); err != nil {
		slog.Error("unable to ping database", "error", err)
		os.Exit(1)
	}

	// Create migrations tracking table
	_, err = sqlDB.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		)
	`)
	if err != nil {
		slog.Error("failed to create migrations table", "error", err)
		os.Exit(1)
	}

	// Find migration files
	migrationsDir := "migrations-sqlite"
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		migrationsDir = "server/migrations-sqlite"
	}

	suffix := "." + direction + ".sql"
	files, err := filepath.Glob(filepath.Join(migrationsDir, "*"+suffix))
	if err != nil {
		slog.Error("failed to find migration files", "error", err)
		os.Exit(1)
	}

	if direction == "up" {
		sort.Strings(files)
	} else {
		sort.Sort(sort.Reverse(sort.StringSlice(files)))
	}

	for _, file := range files {
		version := extractVersion(file)

		if direction == "up" {
			var exists bool
			err := sqlDB.QueryRow("SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)", version).Scan(&exists)
			if err != nil {
				slog.Error("failed to check migration status", "version", version, "error", err)
				os.Exit(1)
			}
			if exists {
				fmt.Printf("  skip  %s (already applied)\n", version)
				continue
			}
		} else {
			var exists bool
			err := sqlDB.QueryRow("SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)", version).Scan(&exists)
			if err != nil {
				slog.Error("failed to check migration status", "version", version, "error", err)
				os.Exit(1)
			}
			if !exists {
				fmt.Printf("  skip  %s (not applied)\n", version)
				continue
			}
		}

		sqlContent, err := os.ReadFile(file)
		if err != nil {
			slog.Error("failed to read migration file", "file", file, "error", err)
			os.Exit(1)
		}

		_, err = sqlDB.Exec(string(sqlContent))
		if err != nil {
			slog.Error("failed to run migration", "file", file, "error", err)
			os.Exit(1)
		}

		if direction == "up" {
			_, err = sqlDB.Exec("INSERT INTO schema_migrations (version) VALUES (?)", version)
		} else {
			_, err = sqlDB.Exec("DELETE FROM schema_migrations WHERE version = ?", version)
		}
		if err != nil {
			slog.Error("failed to record migration", "version", version, "error", err)
			os.Exit(1)
		}

		fmt.Printf("  %s  %s\n", direction, version)
	}

	fmt.Println("Done.")
}

func extractVersion(filename string) string {
	base := filepath.Base(filename)
	base = strings.TrimSuffix(base, ".up.sql")
	base = strings.TrimSuffix(base, ".down.sql")
	return base
}
