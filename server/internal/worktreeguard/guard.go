package worktreeguard

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Enabled reports whether the current process is running with a worktree env.
func Enabled() bool {
	return os.Getenv("MULTICA_ENV_KIND") == "worktree"
}

// EnsureReady validates that a worktree database is fully initialized before runtime code uses it.
func EnsureReady(ctx context.Context, pool *pgxpool.Pool) error {
	if !Enabled() {
		return nil
	}

	var migrationsTableExists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public.schema_migrations') IS NOT NULL").Scan(&migrationsTableExists); err != nil {
		return fmt.Errorf("check schema_migrations: %w", err)
	}
	if !migrationsTableExists {
		return fmt.Errorf("worktree database is missing schema_migrations; run 'make setup-worktree'")
	}

	expectedVersions, err := loadExpectedMigrations()
	if err != nil {
		return err
	}

	appliedVersions := make(map[string]struct{}, len(expectedVersions))
	rows, err := pool.Query(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return fmt.Errorf("load applied migrations: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return fmt.Errorf("scan applied migration: %w", err)
		}
		appliedVersions[version] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate applied migrations: %w", err)
	}

	missingVersions := make([]string, 0)
	for _, version := range expectedVersions {
		if _, ok := appliedVersions[version]; !ok {
			missingVersions = append(missingVersions, version)
		}
	}

	if len(missingVersions) > 0 {
		return fmt.Errorf("worktree database is missing applied migrations: %s; run 'make setup-worktree'", strings.Join(missingVersions, ", "))
	}

	return nil
}

func loadExpectedMigrations() ([]string, error) {
	migrationsDir, err := resolveMigrationsDir()
	if err != nil {
		return nil, err
	}

	files, err := filepath.Glob(filepath.Join(migrationsDir, "*.up.sql"))
	if err != nil {
		return nil, fmt.Errorf("find migration files: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no migration files found under %s", migrationsDir)
	}

	sort.Strings(files)

	versions := make([]string, 0, len(files))
	for _, file := range files {
		base := filepath.Base(file)
		versions = append(versions, strings.TrimSuffix(base, ".up.sql"))
	}

	return versions, nil
}

func resolveMigrationsDir() (string, error) {
	candidates := make([]string, 0, 4)

	if envDir := os.Getenv("MULTICA_MIGRATIONS_DIR"); envDir != "" {
		candidates = append(candidates, envDir)
	}

	if callerPath, ok := callerMigrationsDir(); ok {
		candidates = append(candidates, callerPath)
	}

	candidates = append(candidates,
		"migrations",
		filepath.Join("server", "migrations"),
	)

	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return candidate, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("stat migrations dir %s: %w", candidate, err)
		}
	}

	return "", fmt.Errorf("could not locate migrations directory")
}

func callerMigrationsDir() (string, bool) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", false
	}

	dir := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "migrations"))
	return dir, true
}
