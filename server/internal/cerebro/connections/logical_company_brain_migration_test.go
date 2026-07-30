package connections

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	logicalCompanyBrainUpMigration           = "../../../migrations/9163_cerebro_company_brain_connection.up.sql"
	logicalCompanyBrainDownMigration         = "../../../migrations/9163_cerebro_company_brain_connection.down.sql"
	companyBrainPermissionScopeUpMigration   = "../../../migrations/9164_cerebro_company_brain_permission_scope.up.sql"
	companyBrainPermissionScopeDownMigration = "../../../migrations/9164_cerebro_company_brain_permission_scope.down.sql"
)

func TestLogicalCompanyBrainMigrationDefinesOneCatalogOwningConnection(t *testing.T) {
	up := readLogicalCompanyBrainMigration(t, logicalCompanyBrainUpMigration)

	for _, contract := range []string{
		"CREATE TABLE IF NOT EXISTS cerebro_company_brain_connection",
		"FOREIGN KEY (workspace_id, connection_id)",
		"REFERENCES workspace_connection (workspace_id, id)",
		"CONSTRAINT cerebro_company_brain_connection_workspace_unique",
		"CONSTRAINT cerebro_company_brain_connection_connection_unique",
		"UNIQUE (workspace_id)",
		"UNIQUE (connection_id)",
		"tool_contract_sha256",
	} {
		if !strings.Contains(up, contract) {
			t.Errorf("logical Company Brain migration missing %q", contract)
		}
	}

	upper := strings.ToUpper(up)
	for _, mutation := range []string{"INSERT INTO", "UPDATE ", "DELETE FROM"} {
		if strings.Contains(upper, mutation) {
			t.Errorf("schema-only migration must not mutate existing rows: found %q", mutation)
		}
	}
}

func TestLogicalCompanyBrainMigrationConstraints(t *testing.T) {
	pool := openConnectionMigrationPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	up := readLogicalCompanyBrainMigration(t, logicalCompanyBrainUpMigration)
	if _, err := tx.Exec(ctx, up); err != nil {
		t.Fatalf("apply up migration: %v", err)
	}

	workspaceA := insertMigrationWorkspace(t, tx, "company-brain-logical-a")
	workspaceB := insertMigrationWorkspace(t, tx, "company-brain-logical-b")
	connectionA := insertMigrationConnection(t, tx, workspaceA, "company-brain-target")
	connectionB := insertMigrationConnection(t, tx, workspaceA, "ordinary-mcp")
	otherWorkspaceConnection := insertMigrationConnection(t, tx, workspaceB, "other-workspace")

	var seeded int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM cerebro_company_brain_connection
		WHERE workspace_id = $1
	`, workspaceA).Scan(&seeded); err != nil {
		t.Fatal(err)
	}
	if seeded != 0 {
		t.Fatalf("migration seeded %d logical rows, want 0", seeded)
	}

	const contractHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := tx.Exec(ctx, `
		INSERT INTO cerebro_company_brain_connection
			(workspace_id, connection_id, tool_contract_sha256)
		VALUES ($1, $2, $3)
	`, workspaceA, connectionA, contractHash); err != nil {
		t.Fatalf("insert logical Company Brain connection: %v", err)
	}

	expectMigrationConstraintFailure(t, tx, "duplicate workspace", `
		INSERT INTO cerebro_company_brain_connection
			(workspace_id, connection_id, tool_contract_sha256)
		VALUES ($1, $2, $3)
	`, workspaceA, connectionB, contractHash)

	expectMigrationConstraintFailure(t, tx, "cross-workspace connection", `
		INSERT INTO cerebro_company_brain_connection
			(workspace_id, connection_id, tool_contract_sha256)
		VALUES ($1, $2, $3)
	`, workspaceA, otherWorkspaceConnection, contractHash)

	expectMigrationConstraintFailure(t, tx, "invalid contract hash", `
		INSERT INTO cerebro_company_brain_connection
			(workspace_id, connection_id, tool_contract_sha256)
		VALUES ($1, $2, 'not-a-sha256')
	`, workspaceB, otherWorkspaceConnection)

	var ordinaryCount int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM workspace_connection
		WHERE workspace_id = $1
	`, workspaceA).Scan(&ordinaryCount); err != nil {
		t.Fatal(err)
	}
	if ordinaryCount != 2 {
		t.Fatalf("ordinary workspace connections = %d, want 2 unchanged rows", ordinaryCount)
	}

	down := readLogicalCompanyBrainMigration(t, logicalCompanyBrainDownMigration)
	if _, err := tx.Exec(ctx, down); err != nil {
		t.Fatalf("apply down migration: %v", err)
	}
	var tableName *string
	if err := tx.QueryRow(ctx, `SELECT to_regclass('cerebro_company_brain_connection')::text`).Scan(&tableName); err != nil {
		t.Fatal(err)
	}
	if tableName != nil {
		t.Fatalf("down migration left table %q", *tableName)
	}
}

func TestCompanyBrainPermissionScopeMigrationExtendsExistingPermissionRows(t *testing.T) {
	up := readLogicalCompanyBrainMigration(t, companyBrainPermissionScopeUpMigration)

	for _, contract := range []string{
		"ALTER TABLE cerebro_tool_policy",
		"company_brain_connection_id",
		"company_brain_allowed_read_sources",
		"company_brain_write_source",
		"company_brain_access_version",
		"company_brain_lifecycle_state",
		"layer = 'agent'",
		"resource_pattern = ''",
		"setting = 'allow'",
	} {
		if !strings.Contains(up, contract) {
			t.Errorf("source-scoped permission migration missing %q", contract)
		}
	}

	upper := strings.ToUpper(up)
	for _, mutation := range []string{"INSERT INTO", "UPDATE ", "DELETE FROM"} {
		if strings.Contains(upper, mutation) {
			t.Errorf("schema-only migration must not mutate existing rows: found %q", mutation)
		}
	}
}

func TestCompanyBrainPermissionScopeMigrationConstraints(t *testing.T) {
	pool := openConnectionMigrationPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	logicalUp := readLogicalCompanyBrainMigration(t, logicalCompanyBrainUpMigration)
	if _, err := tx.Exec(ctx, logicalUp); err != nil {
		t.Fatalf("apply logical connection migration: %v", err)
	}

	workspaceID := insertMigrationWorkspace(t, tx, "company-brain-permission-scope")
	agentID := insertMigrationAgent(t, tx, workspaceID)
	connectionID := insertMigrationConnection(t, tx, workspaceID, "company-brain")
	logicalConnectionID := insertLogicalCompanyBrainConnection(t, tx, workspaceID, connectionID)
	if _, err := tx.Exec(ctx, `
		INSERT INTO cerebro_tool_policy
			(workspace_id, tool_key, layer, subject_id, setting, resource_pattern)
		VALUES ($1, 'connection:ordinary', 'agent', $2, 'deny', '')
	`, workspaceID, agentID); err != nil {
		t.Fatalf("insert ordinary permission before migration: %v", err)
	}

	scopeUp := readLogicalCompanyBrainMigration(t, companyBrainPermissionScopeUpMigration)
	if _, err := tx.Exec(ctx, scopeUp); err != nil {
		t.Fatalf("apply source-scoped permission migration: %v", err)
	}

	var ordinaryCount int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM cerebro_tool_policy
		WHERE workspace_id = $1
		  AND tool_key = 'connection:ordinary'
		  AND company_brain_connection_id IS NULL
		  AND company_brain_allowed_read_sources IS NULL
		  AND company_brain_write_source IS NULL
		  AND company_brain_access_version IS NULL
		  AND company_brain_lifecycle_state IS NULL
	`, workspaceID).Scan(&ordinaryCount); err != nil {
		t.Fatal(err)
	}
	if ordinaryCount != 1 {
		t.Fatalf("ordinary permission rows preserved with empty Company Brain scope = %d, want 1", ordinaryCount)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO cerebro_tool_policy (
			workspace_id, tool_key, layer, subject_id, setting, resource_pattern,
			company_brain_connection_id,
			company_brain_allowed_read_sources, company_brain_write_source,
			company_brain_access_version, company_brain_lifecycle_state
		)
		VALUES (
			$1, 'connection:company-brain', 'agent', $2, 'allow', '',
			$3, ARRAY['shared', 'commercial'], 'commercial', 1, 'draft'
		)
	`, workspaceID, agentID, logicalConnectionID); err != nil {
		t.Fatalf("insert source-scoped Company Brain permission: %v", err)
	}

	for _, tc := range []struct {
		name      string
		layer     string
		setting   string
		pattern   string
		reads     string
		write     string
		version   string
		lifecycle string
	}{
		{name: "partial scope", layer: "agent", setting: "allow", reads: "ARRAY['shared']", write: "NULL", version: "1", lifecycle: "'draft'"},
		{name: "non-agent layer", layer: "workspace", setting: "allow", reads: "ARRAY['shared']", write: "'shared'", version: "1", lifecycle: "'draft'"},
		{name: "tool-level row", layer: "agent", setting: "allow", pattern: "search", reads: "ARRAY['shared']", write: "'shared'", version: "1", lifecycle: "'draft'"},
		{name: "deny carrying scope", layer: "agent", setting: "deny", reads: "ARRAY['shared']", write: "'shared'", version: "1", lifecycle: "'draft'"},
		{name: "empty read scope", layer: "agent", setting: "allow", reads: "ARRAY[]::text[]", write: "'shared'", version: "1", lifecycle: "'draft'"},
		{name: "blank read source", layer: "agent", setting: "allow", reads: "ARRAY['', 'shared']", write: "'shared'", version: "1", lifecycle: "'draft'"},
		{name: "write outside read scope", layer: "agent", setting: "allow", reads: "ARRAY['shared']", write: "'commercial'", version: "1", lifecycle: "'draft'"},
		{name: "invalid version", layer: "agent", setting: "allow", reads: "ARRAY['shared']", write: "'shared'", version: "0", lifecycle: "'draft'"},
		{name: "invalid lifecycle", layer: "agent", setting: "allow", reads: "ARRAY['shared']", write: "'shared'", version: "1", lifecycle: "'unknown'"},
	} {
		statement := `
			INSERT INTO cerebro_tool_policy (
				workspace_id, tool_key, layer, subject_id, setting, resource_pattern,
				company_brain_connection_id,
				company_brain_allowed_read_sources, company_brain_write_source,
				company_brain_access_version, company_brain_lifecycle_state
			)
			VALUES (
				$1, 'connection:company-brain-' || gen_random_uuid(), '` + tc.layer + `', $2, '` + tc.setting + `', '` + tc.pattern + `',
				$3, ` + tc.reads + `, ` + tc.write + `, ` + tc.version + `, ` + tc.lifecycle + `
			)
		`
		expectMigrationConstraintFailure(t, tx, tc.name, statement, workspaceID, agentID, logicalConnectionID)
	}

	otherWorkspaceID := insertMigrationWorkspace(t, tx, "company-brain-permission-scope-other")
	otherConnectionID := insertMigrationConnection(t, tx, otherWorkspaceID, "company-brain")
	otherLogicalConnectionID := insertLogicalCompanyBrainConnection(t, tx, otherWorkspaceID, otherConnectionID)
	expectMigrationConstraintFailure(t, tx, "cross-workspace logical connection", `
		INSERT INTO cerebro_tool_policy (
			workspace_id, tool_key, layer, subject_id, setting, resource_pattern,
			company_brain_connection_id,
			company_brain_allowed_read_sources, company_brain_write_source,
			company_brain_access_version, company_brain_lifecycle_state
		)
		VALUES (
			$1, 'connection:company-brain-cross-workspace', 'agent', $2, 'allow', '',
			$3, ARRAY['shared'], 'shared', 1, 'draft'
		)
	`, workspaceID, agentID, otherLogicalConnectionID)

	scopeDown := readLogicalCompanyBrainMigration(t, companyBrainPermissionScopeDownMigration)
	if _, err := tx.Exec(ctx, scopeDown); err != nil {
		t.Fatalf("apply source-scoped permission down migration: %v", err)
	}
	var scopedColumns int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'cerebro_tool_policy'
		  AND column_name LIKE 'company_brain_%'
	`).Scan(&scopedColumns); err != nil {
		t.Fatal(err)
	}
	if scopedColumns != 0 {
		t.Fatalf("down migration left %d Company Brain permission columns", scopedColumns)
	}
}

func readLogicalCompanyBrainMigration(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

func openConnectionMigrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database unreachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func insertMigrationWorkspace(t *testing.T, tx pgx.Tx, slugPrefix string) string {
	t.Helper()
	var id string
	if err := tx.QueryRow(context.Background(), `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Logical Company Brain migration', $1 || '-' || gen_random_uuid(), '', 'LCB')
		RETURNING id
	`, slugPrefix).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertMigrationConnection(t *testing.T, tx pgx.Tx, workspaceID, name string) string {
	t.Helper()
	var id string
	if err := tx.QueryRow(context.Background(), `
		INSERT INTO workspace_connection
			(workspace_id, name, display_name, type, url, tools, instructions)
		VALUES ($1, $2, $2, 'mcp_http', 'http://company-brain.internal:3131/mcp',
		        '[{"name":"search","description":"Search"}]'::jsonb,
		        'Use Company Brain for durable company knowledge.')
		RETURNING id
	`, workspaceID, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertMigrationAgent(t *testing.T, tx pgx.Tx, workspaceID string) string {
	t.Helper()
	var id string
	if err := tx.QueryRow(context.Background(), `
		INSERT INTO agent (workspace_id, name, runtime_mode)
		VALUES ($1, 'Company Brain permission migration', 'local')
		RETURNING id
	`, workspaceID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertLogicalCompanyBrainConnection(t *testing.T, tx pgx.Tx, workspaceID, connectionID string) string {
	t.Helper()
	var id string
	if err := tx.QueryRow(context.Background(), `
		INSERT INTO cerebro_company_brain_connection
			(workspace_id, connection_id, tool_contract_sha256)
		VALUES ($1, $2, repeat('a', 64))
		RETURNING id
	`, workspaceID, connectionID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func expectMigrationConstraintFailure(t *testing.T, tx pgx.Tx, name, statement string, args ...any) {
	t.Helper()
	savepoint := "constraint_check"
	if _, err := tx.Exec(context.Background(), "SAVEPOINT "+savepoint); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(context.Background(), statement, args...); err == nil {
		t.Fatalf("%s unexpectedly passed", name)
	}
	if _, err := tx.Exec(context.Background(), "ROLLBACK TO SAVEPOINT "+savepoint); err != nil {
		t.Fatalf("recover after %s: %v", name, err)
	}
}
