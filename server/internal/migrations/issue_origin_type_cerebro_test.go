package migrations

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestIssueOriginTypesAreAllowedByConstraint fails when a query filters on an
// issue.origin_type value that the issue_origin_type_check CHECK constraint does
// not allow.
//
// FIR-4012 shipped origin_type='capability_digest' (driftwatch/sweeper.go plus
// GetOpenCapabilityDigestIssue) without a migration extending the CHECK. The
// write side is a nightly background sweep, so the only symptom was a Postgres
// `new row for relation "issue" violates check constraint
// "issue_origin_type_check"` — invisible in the app, and the digest issue the
// whole feature exists to produce was never created. Migration 9170 added the
// value; this guard makes the next omission a build failure instead of a silent
// nightly rejection.
func TestIssueOriginTypesAreAllowedByConstraint(t *testing.T) {
	// Deliberately NOT ResolveDir(): see scanned_dir_cerebro_test.go — that
	// helper matches this package's own directory first.
	allowed := allowedOriginTypes(t, filepath.Join("..", "..", "migrations"))
	if len(allowed) == 0 {
		t.Fatal("no issue_origin_type_check constraint found in migrations")
	}

	used := usedOriginTypes(t,
		filepath.Join("..", "cerebro", "queries"),
		filepath.Join("..", "..", "pkg", "db", "queries"),
	)
	if len(used) == 0 {
		t.Fatal("no origin_type literals found in the query files — check the paths")
	}

	var missing []string
	for _, value := range used {
		if !allowed[value] {
			missing = append(missing, value)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("origin_type values used in queries but rejected by issue_origin_type_check: %s\n\n"+
			"Add them in a new 9NNN_cerebro_*.sql migration that rewrites the constraint, "+
			"or the INSERT fails at runtime with a Postgres check-constraint error.",
			strings.Join(missing, ", "))
	}
}

var (
	constraintRe = regexp.MustCompile(`(?is)ADD CONSTRAINT issue_origin_type_check\s*CHECK \(origin_type IN \(([^)]*)\)\)`)
	literalRe    = regexp.MustCompile(`'([a-z_]+)'`)
	usageRe      = regexp.MustCompile(`origin_type\s*=\s*'([a-z_]+)'`)
)

// allowedOriginTypes returns the values permitted by the LAST up-migration that
// rewrites the constraint. Migrations run in filename order and each one drops
// and re-adds the constraint, so the highest-numbered file is the live shape.
func allowedOriginTypes(t *testing.T, dir string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migrations dir %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			names = append(names, e.Name())
		}
	}
	sort.Slice(names, func(i, j int) bool {
		return ExtractVersion(names[i]) < ExtractVersion(names[j])
	})

	allowed := map[string]bool{}
	for _, name := range names {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		match := constraintRe.FindSubmatch(body)
		if match == nil {
			continue
		}
		allowed = map[string]bool{}
		for _, lit := range literalRe.FindAllSubmatch(match[1], -1) {
			allowed[string(lit[1])] = true
		}
	}
	return allowed
}

// usedOriginTypes collects every `origin_type = 'x'` literal from the sqlc query
// files. The read side of a feature always filters on the value its write side
// stamps, so this catches a new origin_type without parsing Go constants.
func usedOriginTypes(t *testing.T, dirs ...string) []string {
	t.Helper()
	seen := map[string]bool{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read query dir %s: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
				continue
			}
			body, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatalf("read %s: %v", e.Name(), err)
			}
			for _, m := range usageRe.FindAllSubmatch(body, -1) {
				seen[string(m[1])] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
