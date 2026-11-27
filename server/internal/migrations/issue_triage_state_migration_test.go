package migrations

import (
	"context"
	"testing"
)

// TestIssueTriageStateMigration pins the lock shape of the Triage column: 483
// registers the CHECK without validating it, so its ACCESS EXCLUSIVE lock never
// covers a scan of the issue table, and 489 runs that scan afterwards under
// SHARE UPDATE EXCLUSIVE, which readers and writers pass straight through.
// Skipping the scan is only acceptable while both halves still reject an
// unknown triage_state on every write, so each step asserts that too.
func TestIssueTriageStateMigration(t *testing.T) {
	ctx := context.Background()
	s := newTriageMigrationSandbox(t, "issue_triage_state_migration_test")

	// Rows that exist before the column does are exactly what a validated CHECK
	// would have forced the ALTER to scan.
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO issue (workspace_id, title, status) VALUES
			($1, 'existing issue', 'todo'),
			($1, 'another existing issue', 'in_progress')
	`, triageWSClean); err != nil {
		t.Fatalf("seed pre-existing issues: %v", err)
	}

	applyMigrationFile(t, ctx, s.pool, "483_issue_triage_state.up.sql")

	if s.triageStateValidated(t, ctx) {
		t.Error("483 validated the CHECK, scanning the issue table under ACCESS EXCLUSIVE")
	}
	if hasDefault, notNull := s.triageStateColumn(t, ctx); hasDefault || notNull {
		t.Errorf("483 added triage_state with default=%v not_null=%v, want a nullable column with no default so the add stays catalog-only", hasDefault, notNull)
	}
	s.assertTriageStateEnforced(t, ctx, "483")

	applyMigrationFile(t, ctx, s.pool, "489_issue_triage_state_validate.up.sql")

	if !s.triageStateValidated(t, ctx) {
		t.Error("489 left the CHECK NOT VALID")
	}
	s.assertTriageStateEnforced(t, ctx, "489")

	// Rolling back the validation cannot restore a validated constraint, so it
	// has to leave the state 483 produced rather than no constraint at all.
	applyMigrationFile(t, ctx, s.pool, "489_issue_triage_state_validate.down.sql")

	if s.triageStateValidated(t, ctx) {
		t.Error("489 down left the CHECK validated")
	}
	s.assertTriageStateEnforced(t, ctx, "489 down")

	applyMigrationFile(t, ctx, s.pool, "483_issue_triage_state.down.sql")

	var columnExists bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_attribute
			WHERE attrelid = (quote_ident($1) || '.issue')::regclass
			  AND attname = 'triage_state'
			  AND NOT attisdropped
		)
	`, s.schema).Scan(&columnExists); err != nil {
		t.Fatalf("read triage_state column after rollback: %v", err)
	}
	if columnExists {
		t.Error("483 down left triage_state behind")
	}
}

// assertTriageStateEnforced checks that the constraint governs new writes at
// this point in the sequence — the guarantee NOT VALID keeps and the reason
// deferring the scan costs nothing.
func (s *triageMigrationSandbox) assertTriageStateEnforced(t *testing.T, ctx context.Context, step string) {
	t.Helper()
	assertInsertCheckViolation(t, ctx, s.pool, `
		INSERT INTO issue (workspace_id, title, status, triage_state) VALUES ($1, 'rejected', 'todo', 'bogus')
	`, triageWSClean)
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO issue (workspace_id, title, status, triage_state) VALUES
			($1, 'in triage', 'todo', 'pending'),
			($1, 'ordinary issue', 'todo', NULL)
	`, triageWSClean); err != nil {
		t.Fatalf("insert known triage_state values after %s: %v", step, err)
	}
}

func (s *triageMigrationSandbox) triageStateValidated(t *testing.T, ctx context.Context) bool {
	t.Helper()
	var validated bool
	if err := s.pool.QueryRow(ctx, `
		SELECT convalidated FROM pg_constraint
		WHERE conname = 'issue_triage_state_known'
		  AND conrelid = (quote_ident($1) || '.issue')::regclass
	`, s.schema).Scan(&validated); err != nil {
		t.Fatalf("read triage state constraint: %v", err)
	}
	return validated
}

func (s *triageMigrationSandbox) triageStateColumn(t *testing.T, ctx context.Context) (hasDefault, notNull bool) {
	t.Helper()
	if err := s.pool.QueryRow(ctx, `
		SELECT atthasdef, attnotnull FROM pg_attribute
		WHERE attrelid = (quote_ident($1) || '.issue')::regclass
		  AND attname = 'triage_state'
		  AND NOT attisdropped
	`, s.schema).Scan(&hasDefault, &notNull); err != nil {
		t.Fatalf("read triage_state column: %v", err)
	}
	return hasDefault, notNull
}
