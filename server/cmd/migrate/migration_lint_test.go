package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/migrations"
)

// stripSQLComments removes -- line comments so the statement count below
// doesn't trip on semicolons inside prose.
func stripSQLComments(sql string) string {
	var b strings.Builder
	for line := range strings.Lines(sql) {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i] + "\n"
		}
		b.WriteString(line)
	}
	return b.String()
}

// countStatements is a deliberately naive statement counter: it counts
// semicolons after stripping line comments. Good enough for the guard
// below, which only inspects files containing CONCURRENTLY — those must
// be simple single-statement files by construction.
func countStatements(sql string) int {
	return strings.Count(stripSQLComments(sql), ";")
}

// TestConcurrentlyMigrationsAreSingleStatement pins the constraint that
// broke migration 145: pgx sends a multi-statement Exec as one simple-
// protocol query, which Postgres wraps in an implicit transaction — and
// CREATE/DROP INDEX CONCURRENTLY cannot run inside a transaction block.
// A migration file that mixes CONCURRENTLY with any other statement can
// therefore never apply, on any database. Each CONCURRENTLY operation
// must live alone in its own migration file.
func TestConcurrentlyMigrationsAreSingleStatement(t *testing.T) {
	dir, err := migrations.ResolveDir()
	if err != nil {
		t.Fatalf("resolve migrations dir: %v", err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no migration files found")
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		sql := string(raw)
		if !strings.Contains(strings.ToUpper(stripSQLComments(sql)), "CONCURRENTLY") {
			continue
		}
		if n := countStatements(sql); n > 1 {
			t.Errorf("%s: contains CONCURRENTLY with %d statements; CONCURRENTLY cannot run inside the implicit transaction pgx uses for multi-statement files — split into one file per statement or drop CONCURRENTLY", filepath.Base(file), n)
		}
	}
}
