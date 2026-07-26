package migrations

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoMigrationsOutsideScannedDirectory fails when a migration file is added
// somewhere Files() cannot see.
//
// Files() globs one directory with no recursion. A migration in a subdirectory
// is therefore not "pending" — it is invisible, and nothing reports it. That is
// how 9085_cerebro_agent_capability_scan_history sat in server/migrations/cerebro/
// from the commit that introduced it, never ran in production, and surfaced only
// as a nightly `relation "cerebro_agent_capability_scan" does not exist` in the
// Postgres log 42-47 times a night. The README in that directory claimed "the
// migrate runner picks up both directories", which the code has never done.
//
// This guard turns that silent failure into a build failure. When the multi-dir
// scanner lands (docs/upstream-sync/preflight/P4.md), update Files() first and
// then relax this test to match the directories it actually reads.
func TestNoMigrationsOutsideScannedDirectory(t *testing.T) {
	// Deliberately NOT ResolveDir(): from this package's own directory that
	// helper matches server/internal/migrations (a "migrations" directory
	// holding .go files) before it ever reaches server/migrations, so the walk
	// would find no SQL at all and the guard would pass vacuously. The test
	// lives two levels below the SQL directory, so address it directly.
	dir := filepath.Join("..", "..", "migrations")
	if _, err := os.Stat(filepath.Join(dir, "1_init.up.sql")); err != nil {
		entries, readErr := os.ReadDir(dir)
		if readErr != nil || len(entries) == 0 {
			t.Fatalf("migrations directory not found at %s: %v", dir, err)
		}
	}

	var stranded []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".up.sql") && !strings.HasSuffix(name, ".down.sql") {
			return nil
		}
		if filepath.Dir(path) != filepath.Clean(dir) {
			rel, relErr := filepath.Rel(dir, path)
			if relErr != nil {
				rel = path
			}
			stranded = append(stranded, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk migrations dir: %v", err)
	}

	if len(stranded) > 0 {
		t.Fatalf("migration files live outside the scanned directory and will NEVER run:\n  %s\n\n"+
			"Files() globs %q with no recursion. Move them up one level, or extend Files() "+
			"to read the subdirectory before placing migrations there.",
			strings.Join(stranded, "\n  "), dir)
	}
}
