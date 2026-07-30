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
	logicalCompanyBrainUpMigration             = "../../../migrations/9163_cerebro_company_brain_connection.up.sql"
	logicalCompanyBrainDownMigration           = "../../../migrations/9163_cerebro_company_brain_connection.down.sql"
	companyBrainPermissionScopeUpMigration     = "../../../migrations/9164_cerebro_company_brain_permission_scope.up.sql"
	companyBrainPermissionScopeDownMigration   = "../../../migrations/9164_cerebro_company_brain_permission_scope.down.sql"
	companyBrainRollbackUpMigration            = "../../../migrations/9165_cerebro_company_brain_rollback_tombstones.up.sql"
	companyBrainRollbackDownMigration          = "../../../migrations/9165_cerebro_company_brain_rollback_tombstones.down.sql"
	companyBrainMigrationDecisionUpMigration   = "../../../migrations/9166_cerebro_company_brain_migration_decisions.up.sql"
	companyBrainMigrationDecisionDownMigration = "../../../migrations/9166_cerebro_company_brain_migration_decisions.down.sql"
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

func TestCompanyBrainRollbackMigrationDefinesProtectedTombstones(t *testing.T) {
	up := readLogicalCompanyBrainMigration(t, companyBrainRollbackUpMigration)

	for _, contract := range []string{
		"CREATE TABLE IF NOT EXISTS cerebro_company_brain_rollback_window",
		"CREATE TABLE IF NOT EXISTS cerebro_company_brain_connection_tombstone",
		"CREATE TABLE IF NOT EXISTS cerebro_company_brain_permission_tombstone",
		"CREATE TABLE IF NOT EXISTS cerebro_company_brain_approval_tombstone",
		"CREATE TABLE IF NOT EXISTS cerebro_company_brain_approval_audit_tombstone",
		"CREATE TABLE IF NOT EXISTS cerebro_company_brain_permission_audit_tombstone",
		"CREATE TABLE IF NOT EXISTS cerebro_company_brain_tool_alias_tombstone",
		"legacy_connection_id",
		"permission_id",
		"approval_id",
		"approval_audit_id",
		"permission_audit_id",
		"alias_id",
		"snapshot_sha256",
		"metadata_sha256",
		"ON DELETE RESTRICT",
		"cerebro_protect_active_company_brain_rollback",
		"active rollback metadata cannot be deleted or shortened",
	} {
		if !strings.Contains(up, contract) {
			t.Errorf("rollback tombstone migration missing %q", contract)
		}
	}

	upper := strings.ToUpper(up)
	for _, line := range strings.Split(upper, "\n") {
		statement := strings.TrimSpace(line)
		for _, mutation := range []string{"INSERT INTO", "UPDATE ", "DELETE FROM"} {
			if strings.HasPrefix(statement, mutation) {
				t.Errorf(
					"schema-only migration must not mutate existing rows: found %q in %q",
					mutation,
					statement,
				)
			}
		}
	}
	if strings.Contains(upper, " JSONB") {
		t.Error("rollback tombstones must not accept arbitrary JSON that could copy credentials or secrets")
	}
}

func TestCompanyBrainRollbackMigrationConstraints(t *testing.T) {
	pool := openConnectionMigrationPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, migration := range []string{
		logicalCompanyBrainUpMigration,
		companyBrainPermissionScopeUpMigration,
		companyBrainRollbackUpMigration,
	} {
		if _, err := tx.Exec(ctx, readLogicalCompanyBrainMigration(t, migration)); err != nil {
			t.Fatalf("apply %s: %v", migration, err)
		}
	}
	if _, err := tx.Exec(
		ctx,
		readLogicalCompanyBrainMigration(t, companyBrainRollbackUpMigration),
	); err != nil {
		t.Fatalf("reapply idempotent rollback tombstone migration: %v", err)
	}

	workspaceID := insertMigrationWorkspace(t, tx, "company-brain-rollback")
	otherWorkspaceID := insertMigrationWorkspace(t, tx, "company-brain-rollback-other")
	agentID := insertMigrationAgent(t, tx, workspaceID)
	logicalConnectionID := insertMigrationConnection(t, tx, workspaceID, "company-brain")
	logicalID := insertLogicalCompanyBrainConnection(t, tx, workspaceID, logicalConnectionID)
	legacyConnectionID := insertMigrationConnection(t, tx, workspaceID, "company-brain-shared")
	otherLogicalConnectionID := insertMigrationConnection(t, tx, otherWorkspaceID, "company-brain")
	otherLogicalID := insertLogicalCompanyBrainConnection(t, tx, otherWorkspaceID, otherLogicalConnectionID)
	otherLegacyConnectionID := insertMigrationConnection(t, tx, otherWorkspaceID, "company-brain-shared")

	permissionID := insertMigrationLegacyConnectionPermission(
		t, tx, workspaceID, agentID, "connection:company-brain-shared",
	)
	permissionAuditID := insertMigrationPermissionAudit(
		t, tx, workspaceID, "connection:company-brain-shared", agentID,
	)
	otherPermissionID := insertMigrationLegacyConnectionPermission(
		t, tx, workspaceID, agentID, "connection:other-connection",
	)
	otherPermissionAuditID := insertMigrationPermissionAudit(
		t, tx, workspaceID, "connection:other-connection", agentID,
	)
	approvalID := insertMigrationApproval(
		t, tx, workspaceID, agentID, "connection:company-brain-shared",
	)
	approvalAuditID := insertMigrationApprovalAudit(t, tx, workspaceID, approvalID)
	otherApprovalID := insertMigrationApproval(
		t, tx, workspaceID, agentID, "connection:company-brain-shared",
	)
	otherApprovalAuditID := insertMigrationApprovalAudit(t, tx, workspaceID, otherApprovalID)
	foreignApprovalID := insertMigrationApproval(
		t, tx, workspaceID, agentID, "connection:other-connection",
	)
	aliasID, aliasCapabilityID := insertMigrationToolAlias(
		t, tx, "connection:company-brain-shared:mcp:search",
	)
	foreignAliasID, foreignAliasCapabilityID := insertMigrationToolAlias(
		t, tx, "connection:other-connection:mcp:search",
	)

	var seeded int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM cerebro_company_brain_rollback_window
	`).Scan(&seeded); err != nil {
		t.Fatal(err)
	}
	if seeded != 0 {
		t.Fatalf("migration seeded %d rollback windows, want 0", seeded)
	}

	var rollbackWindowID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO cerebro_company_brain_rollback_window (
			workspace_id, company_brain_connection_id,
			snapshot_sha256, starts_at, expires_at
		)
		VALUES ($1, $2, repeat('b', 64), now(), now() + interval '14 days')
		RETURNING id
	`, workspaceID, logicalID).Scan(&rollbackWindowID); err != nil {
		t.Fatalf("insert rollback window: %v", err)
	}

	expectMigrationConstraintFailure(t, tx, "permission without legacy Connection tombstone", `
		INSERT INTO cerebro_company_brain_permission_tombstone (
			workspace_id, rollback_window_id, legacy_connection_id,
			legacy_connection_name, permission_id, tool_key, layer,
			subject_id, resource_pattern, metadata_sha256
		)
		VALUES (
			$1, $2, $3, 'company-brain-shared', $4,
			'connection:company-brain-shared', 'agent', $5, '',
			repeat('a', 64)
		)
	`, workspaceID, rollbackWindowID, legacyConnectionID, permissionID, agentID)

	expectMigrationConstraintFailure(t, tx, "legacy Connection tombstone with wrong name", `
		INSERT INTO cerebro_company_brain_connection_tombstone (
			workspace_id, rollback_window_id, legacy_connection_id,
			legacy_connection_name, metadata_sha256
		)
		VALUES ($1, $2, $3, 'wrong-name', repeat('c', 64))
	`, workspaceID, rollbackWindowID, legacyConnectionID)

	if _, err := tx.Exec(ctx, `
		INSERT INTO cerebro_company_brain_connection_tombstone (
			workspace_id, rollback_window_id, legacy_connection_id,
			legacy_connection_name, metadata_sha256
		)
		VALUES ($1, $2, $3, 'company-brain-shared', repeat('c', 64))
	`, workspaceID, rollbackWindowID, legacyConnectionID); err != nil {
		t.Fatalf("insert legacy Connection tombstone: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO cerebro_company_brain_permission_tombstone (
			workspace_id, rollback_window_id, legacy_connection_id,
			legacy_connection_name, permission_id, tool_key, layer,
			subject_id, resource_pattern, metadata_sha256
		)
		VALUES (
			$1, $2, $3, 'company-brain-shared', $4,
			'connection:company-brain-shared', 'agent', $5, '',
			repeat('d', 64)
		)
	`, workspaceID, rollbackWindowID, legacyConnectionID, permissionID, agentID); err != nil {
		t.Fatalf("insert permission tombstone: %v", err)
	}
	expectMigrationConstraintFailure(t, tx, "permission linked to wrong legacy Connection", `
		INSERT INTO cerebro_company_brain_permission_tombstone (
			workspace_id, rollback_window_id, legacy_connection_id,
			legacy_connection_name, permission_id, tool_key, layer,
			subject_id, resource_pattern, metadata_sha256
		)
		VALUES (
			$1, $2, $3, 'company-brain-shared', $4,
			'connection:other-connection', 'agent', $5, '',
			repeat('d', 64)
		)
	`, workspaceID, rollbackWindowID, legacyConnectionID, otherPermissionID, agentID)
	if _, err := tx.Exec(ctx, `
		INSERT INTO cerebro_company_brain_approval_tombstone (
			workspace_id, rollback_window_id, legacy_connection_id,
			legacy_connection_name, approval_id, capability, resource,
			metadata_sha256
		)
		VALUES (
			$1, $2, $3, 'company-brain-shared', $4,
			'connection:company-brain-shared', 'search', repeat('e', 64)
		)
	`, workspaceID, rollbackWindowID, legacyConnectionID, approvalID); err != nil {
		t.Fatalf("insert approval tombstone: %v", err)
	}
	expectMigrationConstraintFailure(t, tx, "approval linked to wrong legacy Connection", `
		INSERT INTO cerebro_company_brain_approval_tombstone (
			workspace_id, rollback_window_id, legacy_connection_id,
			legacy_connection_name, approval_id, capability, resource,
			metadata_sha256
		)
		VALUES (
			$1, $2, $3, 'company-brain-shared', $4,
			'connection:other-connection', 'search', repeat('e', 64)
		)
	`, workspaceID, rollbackWindowID, legacyConnectionID, foreignApprovalID)
	if _, err := tx.Exec(ctx, `
		INSERT INTO cerebro_company_brain_approval_audit_tombstone (
			workspace_id, rollback_window_id, approval_id, approval_audit_id
		)
		VALUES ($1, $2, $3, $4)
	`, workspaceID, rollbackWindowID, approvalID, approvalAuditID); err != nil {
		t.Fatalf("insert approval audit tombstone: %v", err)
	}
	expectMigrationConstraintFailure(t, tx, "audit linked to wrong approval", `
		INSERT INTO cerebro_company_brain_approval_audit_tombstone (
			workspace_id, rollback_window_id, approval_id, approval_audit_id
		)
		VALUES ($1, $2, $3, $4)
	`, workspaceID, rollbackWindowID, approvalID, otherApprovalAuditID)
	if _, err := tx.Exec(ctx, `
		INSERT INTO cerebro_company_brain_permission_audit_tombstone (
			workspace_id, rollback_window_id, permission_id,
			permission_audit_id, tool_key, layer, subject_id, resource_pattern
		)
		VALUES (
			$1, $2, $3, $4,
			'connection:company-brain-shared', 'agent', $5, ''
		)
	`, workspaceID, rollbackWindowID, permissionID, permissionAuditID, agentID); err != nil {
		t.Fatalf("insert permission audit tombstone: %v", err)
	}
	expectMigrationConstraintFailure(t, tx, "audit linked to wrong permission", `
		INSERT INTO cerebro_company_brain_permission_audit_tombstone (
			workspace_id, rollback_window_id, permission_id,
			permission_audit_id, tool_key, layer, subject_id, resource_pattern
		)
		VALUES (
			$1, $2, $3, $4,
			'connection:company-brain-shared', 'agent', $5, ''
		)
	`, workspaceID, rollbackWindowID, permissionID, otherPermissionAuditID, agentID)
	if _, err := tx.Exec(ctx, `
		INSERT INTO cerebro_company_brain_tool_alias_tombstone (
			workspace_id, rollback_window_id, legacy_connection_id,
			legacy_connection_name, alias_id, capability_id, metadata_sha256
		)
		VALUES (
			$1, $2, $3, 'company-brain-shared', $4, $5, repeat('f', 64)
		)
	`, workspaceID, rollbackWindowID, legacyConnectionID, aliasID,
		aliasCapabilityID); err != nil {
		t.Fatalf("insert tool alias tombstone: %v", err)
	}
	expectMigrationConstraintFailure(t, tx, "tool alias linked to wrong legacy Connection", `
		INSERT INTO cerebro_company_brain_tool_alias_tombstone (
			workspace_id, rollback_window_id, legacy_connection_id,
			legacy_connection_name, alias_id, capability_id, metadata_sha256
		)
		VALUES (
			$1, $2, $3, 'company-brain-shared', $4, $5, repeat('f', 64)
		)
	`, workspaceID, rollbackWindowID, legacyConnectionID,
		foreignAliasID, foreignAliasCapabilityID)

	expectMigrationConstraintFailure(t, tx, "expired rollback window", `
		INSERT INTO cerebro_company_brain_rollback_window (
			workspace_id, company_brain_connection_id,
			snapshot_sha256, starts_at, expires_at
		)
		VALUES (
			$1, $2, repeat('a', 64),
			now() - interval '2 days', now() - interval '1 day'
		)
	`, otherWorkspaceID, otherLogicalID)
	expectMigrationConstraintFailure(t, tx, "future rollback window", `
		INSERT INTO cerebro_company_brain_rollback_window (
			workspace_id, company_brain_connection_id,
			snapshot_sha256, starts_at, expires_at
		)
		VALUES (
			$1, $2, repeat('a', 64),
			now() + interval '1 day', now() + interval '2 days'
		)
	`, otherWorkspaceID, otherLogicalID)
	expectMigrationConstraintFailure(t, tx, "rollback window longer than 14 days", `
		INSERT INTO cerebro_company_brain_rollback_window (
			workspace_id, company_brain_connection_id,
			snapshot_sha256, starts_at, expires_at
		)
		VALUES ($1, $2, repeat('a', 64), now(), now() + interval '15 days')
	`, otherWorkspaceID, otherLogicalID)
	expectMigrationConstraintFailure(t, tx, "invalid rollback snapshot hash", `
		INSERT INTO cerebro_company_brain_rollback_window (
			workspace_id, company_brain_connection_id,
			snapshot_sha256, starts_at, expires_at
		)
		VALUES ($1, $2, 'not-a-sha256', now(), now() + interval '14 days')
	`, otherWorkspaceID, otherLogicalID)

	expectMigrationConstraintFailure(t, tx, "cross-workspace legacy Connection", `
		INSERT INTO cerebro_company_brain_connection_tombstone (
			workspace_id, rollback_window_id, legacy_connection_id,
			legacy_connection_name, metadata_sha256
		)
		VALUES ($1, $2, $3, 'wrong-workspace', repeat('a', 64))
	`, workspaceID, rollbackWindowID, otherLegacyConnectionID)

	expectMigrationConstraintFailure(t, tx, "shorten active rollback window", `
		UPDATE cerebro_company_brain_rollback_window
		SET expires_at = now() - interval '1 second'
		WHERE id = $1
	`, rollbackWindowID)
	expectMigrationConstraintFailure(t, tx, "delete active rollback window", `
		DELETE FROM cerebro_company_brain_rollback_window WHERE id = $1
	`, rollbackWindowID)
	for _, protected := range []struct {
		name  string
		table string
	}{
		{name: "legacy Connection tombstone", table: "cerebro_company_brain_connection_tombstone"},
		{name: "permission tombstone", table: "cerebro_company_brain_permission_tombstone"},
		{name: "approval tombstone", table: "cerebro_company_brain_approval_tombstone"},
		{name: "approval audit tombstone", table: "cerebro_company_brain_approval_audit_tombstone"},
		{name: "permission audit tombstone", table: "cerebro_company_brain_permission_audit_tombstone"},
		{name: "tool alias tombstone", table: "cerebro_company_brain_tool_alias_tombstone"},
	} {
		expectMigrationConstraintFailure(
			t,
			tx,
			"delete active "+protected.name,
			"DELETE FROM "+protected.table+" WHERE rollback_window_id = $1",
			rollbackWindowID,
		)
		expectMigrationConstraintFailure(
			t,
			tx,
			"update active "+protected.name,
			"UPDATE "+protected.table+" SET created_at = created_at WHERE rollback_window_id = $1",
			rollbackWindowID,
		)
	}

	if _, err := tx.Exec(ctx, `
		ALTER TABLE cerebro_company_brain_rollback_window
		DISABLE TRIGGER protect_active_company_brain_rollback_window
	`); err != nil {
		t.Fatalf("disable rollback-window trigger for expired fixture: %v", err)
	}
	var expiredRollbackWindowID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO cerebro_company_brain_rollback_window (
			workspace_id, company_brain_connection_id,
			snapshot_sha256, starts_at, expires_at
		)
		VALUES (
			$1, $2, repeat('a', 64),
			now() - interval '2 days', now() - interval '1 day'
		)
		RETURNING id
	`, otherWorkspaceID, otherLogicalID).Scan(&expiredRollbackWindowID); err != nil {
		t.Fatalf("insert expired rollback-window fixture: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		ALTER TABLE cerebro_company_brain_rollback_window
		ENABLE TRIGGER protect_active_company_brain_rollback_window
	`); err != nil {
		t.Fatalf("enable rollback-window trigger after expired fixture: %v", err)
	}
	expectMigrationConstraintFailure(t, tx, "add tombstone after rollback expiry", `
		INSERT INTO cerebro_company_brain_connection_tombstone (
			workspace_id, rollback_window_id, legacy_connection_id,
			legacy_connection_name, metadata_sha256
		)
		VALUES ($1, $2, $3, 'company-brain-shared', repeat('a', 64))
	`, otherWorkspaceID, expiredRollbackWindowID, otherLegacyConnectionID)

	expectMigrationConstraintFailure(t, tx, "delete protected legacy Connection", `
		DELETE FROM workspace_connection WHERE id = $1
	`, legacyConnectionID)
	expectMigrationConstraintFailure(t, tx, "delete protected permission", `
		DELETE FROM cerebro_tool_policy WHERE id = $1
	`, permissionID)
	expectMigrationConstraintFailure(t, tx, "delete protected approval", `
		DELETE FROM cerebro_approval_request WHERE id = $1
	`, approvalID)
	expectMigrationConstraintFailure(t, tx, "delete protected approval audit", `
		DELETE FROM cerebro_approval_audit WHERE id = $1
	`, approvalAuditID)
	expectMigrationConstraintFailure(t, tx, "delete protected permission audit", `
		DELETE FROM cerebro_tool_policy_audit WHERE id = $1
	`, permissionAuditID)
	expectMigrationConstraintFailure(t, tx, "delete protected tool alias", `
		DELETE FROM cerebro_capability_alias WHERE id = $1
	`, aliasID)

	if _, err := tx.Exec(ctx, readLogicalCompanyBrainMigration(t, companyBrainRollbackDownMigration)); err != nil {
		t.Fatalf("apply rollback tombstone down migration: %v", err)
	}
	for _, table := range []string{
		"cerebro_company_brain_rollback_window",
		"cerebro_company_brain_connection_tombstone",
		"cerebro_company_brain_permission_tombstone",
		"cerebro_company_brain_approval_tombstone",
		"cerebro_company_brain_approval_audit_tombstone",
		"cerebro_company_brain_permission_audit_tombstone",
		"cerebro_company_brain_tool_alias_tombstone",
	} {
		var tableName *string
		if err := tx.QueryRow(ctx, `SELECT to_regclass($1)::text`, table).Scan(&tableName); err != nil {
			t.Fatal(err)
		}
		if tableName != nil {
			t.Fatalf("down migration left table %q", *tableName)
		}
	}

	var rollbackConstraints int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM pg_constraint
		WHERE conname IN (
			'workspace_connection_workspace_id_id_name_unique',
			'cerebro_tool_policy_workspace_id_id_unique',
			'cerebro_tool_policy_workspace_identity_unique',
			'cerebro_approval_request_workspace_id_id_unique',
			'cerebro_approval_request_workspace_identity_unique',
			'cerebro_approval_audit_workspace_approval_id_unique',
			'cerebro_tool_policy_audit_workspace_id_id_unique',
			'cerebro_tool_policy_audit_workspace_identity_unique',
			'cerebro_capability_alias_id_capability_unique'
		)
	`).Scan(&rollbackConstraints); err != nil {
		t.Fatal(err)
	}
	if rollbackConstraints != 0 {
		t.Fatalf("down migration left %d rollback-only constraints", rollbackConstraints)
	}

	for _, preserved := range []struct {
		name  string
		query string
		id    string
	}{
		{name: "legacy Connection", query: `SELECT COUNT(*) FROM workspace_connection WHERE id = $1`, id: legacyConnectionID},
		{name: "permission", query: `SELECT COUNT(*) FROM cerebro_tool_policy WHERE id = $1`, id: permissionID},
		{name: "approval", query: `SELECT COUNT(*) FROM cerebro_approval_request WHERE id = $1`, id: approvalID},
		{name: "approval audit", query: `SELECT COUNT(*) FROM cerebro_approval_audit WHERE id = $1`, id: approvalAuditID},
		{name: "permission audit", query: `SELECT COUNT(*) FROM cerebro_tool_policy_audit WHERE id = $1`, id: permissionAuditID},
		{name: "tool alias", query: `SELECT COUNT(*) FROM cerebro_capability_alias WHERE id = $1`, id: aliasID},
	} {
		var count int
		if err := tx.QueryRow(ctx, preserved.query, preserved.id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("down migration preserved %s rows = %d, want 1", preserved.name, count)
		}
	}
}

func TestCompanyBrainMigrationDecisionDefinesDurableOutcomes(t *testing.T) {
	up := readLogicalCompanyBrainMigration(t, companyBrainMigrationDecisionUpMigration)

	for _, contract := range []string{
		"CREATE TABLE IF NOT EXISTS cerebro_company_brain_migration_decision",
		"company_brain_connection_id",
		"agent_id",
		"census_version",
		"conflict_code",
		"affected_reference",
		"outcome",
		"status",
		"automatic",
		"owner_decision",
		"cannot_migrate",
		"do_not_migrate",
		"consequence",
		"recommended_choice",
		"safe_remediation",
		"observed_state",
		"expected_state",
		"owner_user_id",
		"saved_decision",
		"decided_by_user_id",
		"decided_at",
		"decision_note",
		"evidence_sha256",
		"evidence_at",
		"FOREIGN KEY (workspace_id, company_brain_connection_id)",
		"FOREIGN KEY (workspace_id, agent_id)",
		"FOREIGN KEY (workspace_id, owner_user_id)",
		"FOREIGN KEY (workspace_id, decided_by_user_id)",
	} {
		if !strings.Contains(up, contract) {
			t.Errorf("migration-decision migration missing %q", contract)
		}
	}

	upper := strings.ToUpper(up)
	for _, line := range strings.Split(upper, "\n") {
		statement := strings.TrimSpace(line)
		for _, mutation := range []string{"INSERT INTO", "UPDATE ", "DELETE FROM"} {
			if strings.HasPrefix(statement, mutation) {
				t.Errorf(
					"schema-only migration must not mutate existing rows: found %q in %q",
					mutation,
					statement,
				)
			}
		}
	}
}

func TestCompanyBrainMigrationDecisionConstraints(t *testing.T) {
	pool := openConnectionMigrationPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, migration := range []string{
		logicalCompanyBrainUpMigration,
		companyBrainMigrationDecisionUpMigration,
	} {
		if _, err := tx.Exec(ctx, readLogicalCompanyBrainMigration(t, migration)); err != nil {
			t.Fatalf("apply %s: %v", migration, err)
		}
	}
	if _, err := tx.Exec(
		ctx,
		readLogicalCompanyBrainMigration(t, companyBrainMigrationDecisionUpMigration),
	); err != nil {
		t.Fatalf("reapply idempotent migration-decision migration: %v", err)
	}

	workspaceID := insertMigrationWorkspace(t, tx, "company-brain-migration-decision")
	otherWorkspaceID := insertMigrationWorkspace(t, tx, "company-brain-migration-decision-other")
	agentID := insertMigrationAgent(t, tx, workspaceID)
	otherAgentID := insertMigrationAgent(t, tx, otherWorkspaceID)
	ownerUserID := insertMigrationMember(t, tx, workspaceID, "migration-owner")
	otherOwnerUserID := insertMigrationMember(t, tx, otherWorkspaceID, "migration-owner-other")
	connectionID := insertMigrationConnection(t, tx, workspaceID, "company-brain")
	logicalID := insertLogicalCompanyBrainConnection(t, tx, workspaceID, connectionID)
	otherConnectionID := insertMigrationConnection(t, tx, otherWorkspaceID, "company-brain")
	otherLogicalID := insertLogicalCompanyBrainConnection(t, tx, otherWorkspaceID, otherConnectionID)

	var seeded int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM cerebro_company_brain_migration_decision
	`).Scan(&seeded); err != nil {
		t.Fatal(err)
	}
	if seeded != 0 {
		t.Fatalf("migration seeded %d decisions, want 0", seeded)
	}

	insertDecision := func(
		conflictCode, outcome, status, owner, savedDecision, decidedBy, decidedAt, decisionNote string,
	) {
		t.Helper()
		if _, err := tx.Exec(ctx, `
			INSERT INTO cerebro_company_brain_migration_decision (
				workspace_id, company_brain_connection_id, agent_id,
				census_version, conflict_code, affected_reference,
				outcome, status, consequence, recommended_choice,
				safe_remediation, observed_state, expected_state,
				owner_user_id, saved_decision, decided_by_user_id,
				decided_at, decision_note, evidence_sha256, evidence_at
			)
			VALUES (
				$1::uuid, $2::uuid, $3::uuid, 1, $4, 'agent:' || $3::uuid::text,
				$5, $6, 'Migration consequence', 'Recommended choice',
				'Safe remediation', 'Observed state', 'Expected state',
				`+owner+`, `+savedDecision+`, `+decidedBy+`,
				`+decidedAt+`, `+decisionNote+`, repeat('a', 64), now()
			)
		`, workspaceID, logicalID, agentID, conflictCode, outcome, status); err != nil {
			t.Fatalf("insert %s migration decision: %v", outcome, err)
		}
	}

	insertDecision(
		"CB-AUTOMATIC-001", "automatic", "resolved",
		"NULL", "'migrate'", "NULL", "NULL", "NULL",
	)
	insertDecision(
		"CB-OWNER-001", "owner_decision", "pending",
		"'"+ownerUserID+"'", "NULL", "NULL", "NULL", "NULL",
	)
	insertDecision(
		"CB-OWNER-002", "owner_decision", "resolved",
		"'"+ownerUserID+"'", "'migrate'", "'"+ownerUserID+"'", "now()", "'Owner approved migration'",
	)
	insertDecision(
		"CB-CANNOT-001", "cannot_migrate", "blocked",
		"NULL", "NULL", "NULL", "NULL", "NULL",
	)
	insertDecision(
		"CB-EXCLUDED-001", "do_not_migrate", "resolved",
		"'"+ownerUserID+"'", "'do_not_migrate'", "'"+ownerUserID+"'", "now()", "'Owner excluded this agent'",
	)

	expectMigrationConstraintFailure(t, tx, "duplicate census conflict", `
		INSERT INTO cerebro_company_brain_migration_decision (
			workspace_id, company_brain_connection_id, agent_id,
			census_version, conflict_code, affected_reference,
			outcome, status, consequence, recommended_choice,
			safe_remediation, observed_state, expected_state,
			saved_decision, evidence_sha256, evidence_at
		)
		VALUES (
			$1::uuid, $2::uuid, $3::uuid, 1,
			'CB-AUTOMATIC-001', 'agent:' || $3::uuid::text,
			'automatic', 'resolved', 'Migration consequence', 'Recommended choice',
			'Safe remediation', 'Observed state', 'Expected state',
			'migrate', repeat('a', 64), now()
		)
	`, workspaceID, logicalID, agentID)

	for _, tc := range []struct {
		name           string
		censusVersion  string
		conflictCode   string
		reference      string
		outcome        string
		status         string
		consequence    string
		recommendation string
		remediation    string
		observed       string
		expected       string
		owner          string
		savedDecision  string
		decidedBy      string
		decidedAt      string
		decisionNote   string
		evidenceHash   string
	}{
		{name: "unknown outcome", censusVersion: "2", conflictCode: "'CB-INVALID-001'", reference: "'agent:invalid'", outcome: "'unknown'", status: "'blocked'", consequence: "'Consequence'", recommendation: "'Choice'", remediation: "'Fix'", observed: "'Observed'", expected: "'Expected'", owner: "NULL", savedDecision: "NULL", decidedBy: "NULL", decidedAt: "NULL", decisionNote: "NULL", evidenceHash: "repeat('a', 64)"},
		{name: "automatic left pending", censusVersion: "2", conflictCode: "'CB-INVALID-002'", reference: "'agent:invalid'", outcome: "'automatic'", status: "'pending'", consequence: "'Consequence'", recommendation: "'Choice'", remediation: "'Fix'", observed: "'Observed'", expected: "'Expected'", owner: "NULL", savedDecision: "'migrate'", decidedBy: "NULL", decidedAt: "NULL", decisionNote: "NULL", evidenceHash: "repeat('a', 64)"},
		{name: "automatic assigned to owner", censusVersion: "2", conflictCode: "'CB-INVALID-003'", reference: "'agent:invalid'", outcome: "'automatic'", status: "'resolved'", consequence: "'Consequence'", recommendation: "'Choice'", remediation: "'Fix'", observed: "'Observed'", expected: "'Expected'", owner: "'" + ownerUserID + "'", savedDecision: "'migrate'", decidedBy: "NULL", decidedAt: "NULL", decisionNote: "NULL", evidenceHash: "repeat('a', 64)"},
		{name: "owner decision without owner", censusVersion: "2", conflictCode: "'CB-INVALID-004'", reference: "'agent:invalid'", outcome: "'owner_decision'", status: "'pending'", consequence: "'Consequence'", recommendation: "'Choice'", remediation: "'Fix'", observed: "'Observed'", expected: "'Expected'", owner: "NULL", savedDecision: "NULL", decidedBy: "NULL", decidedAt: "NULL", decisionNote: "NULL", evidenceHash: "repeat('a', 64)"},
		{name: "pending owner decision already saved", censusVersion: "2", conflictCode: "'CB-INVALID-005'", reference: "'agent:invalid'", outcome: "'owner_decision'", status: "'pending'", consequence: "'Consequence'", recommendation: "'Choice'", remediation: "'Fix'", observed: "'Observed'", expected: "'Expected'", owner: "'" + ownerUserID + "'", savedDecision: "'migrate'", decidedBy: "'" + ownerUserID + "'", decidedAt: "now()", decisionNote: "'Too early'", evidenceHash: "repeat('a', 64)"},
		{name: "resolved owner decision missing decision", censusVersion: "2", conflictCode: "'CB-INVALID-006'", reference: "'agent:invalid'", outcome: "'owner_decision'", status: "'resolved'", consequence: "'Consequence'", recommendation: "'Choice'", remediation: "'Fix'", observed: "'Observed'", expected: "'Expected'", owner: "'" + ownerUserID + "'", savedDecision: "NULL", decidedBy: "NULL", decidedAt: "NULL", decisionNote: "NULL", evidenceHash: "repeat('a', 64)"},
		{name: "resolved owner decision missing note", censusVersion: "2", conflictCode: "'CB-INVALID-006-NOTE'", reference: "'agent:invalid'", outcome: "'owner_decision'", status: "'resolved'", consequence: "'Consequence'", recommendation: "'Choice'", remediation: "'Fix'", observed: "'Observed'", expected: "'Expected'", owner: "'" + ownerUserID + "'", savedDecision: "'migrate'", decidedBy: "'" + ownerUserID + "'", decidedAt: "now()", decisionNote: "NULL", evidenceHash: "repeat('a', 64)"},
		{name: "cannot migrate marked resolved", censusVersion: "2", conflictCode: "'CB-INVALID-007'", reference: "'agent:invalid'", outcome: "'cannot_migrate'", status: "'resolved'", consequence: "'Consequence'", recommendation: "'Choice'", remediation: "'Fix'", observed: "'Observed'", expected: "'Expected'", owner: "NULL", savedDecision: "NULL", decidedBy: "NULL", decidedAt: "NULL", decisionNote: "NULL", evidenceHash: "repeat('a', 64)"},
		{name: "do not migrate without owner decision", censusVersion: "2", conflictCode: "'CB-INVALID-008'", reference: "'agent:invalid'", outcome: "'do_not_migrate'", status: "'resolved'", consequence: "'Consequence'", recommendation: "'Choice'", remediation: "'Fix'", observed: "'Observed'", expected: "'Expected'", owner: "NULL", savedDecision: "'do_not_migrate'", decidedBy: "NULL", decidedAt: "NULL", decisionNote: "NULL", evidenceHash: "repeat('a', 64)"},
		{name: "do not migrate missing note", censusVersion: "2", conflictCode: "'CB-INVALID-008-NOTE'", reference: "'agent:invalid'", outcome: "'do_not_migrate'", status: "'resolved'", consequence: "'Consequence'", recommendation: "'Choice'", remediation: "'Fix'", observed: "'Observed'", expected: "'Expected'", owner: "'" + ownerUserID + "'", savedDecision: "'do_not_migrate'", decidedBy: "'" + ownerUserID + "'", decidedAt: "now()", decisionNote: "NULL", evidenceHash: "repeat('a', 64)"},
		{name: "zero census version", censusVersion: "0", conflictCode: "'CB-INVALID-009'", reference: "'agent:invalid'", outcome: "'automatic'", status: "'resolved'", consequence: "'Consequence'", recommendation: "'Choice'", remediation: "'Fix'", observed: "'Observed'", expected: "'Expected'", owner: "NULL", savedDecision: "'migrate'", decidedBy: "NULL", decidedAt: "NULL", decisionNote: "NULL", evidenceHash: "repeat('a', 64)"},
		{name: "blank conflict code", censusVersion: "2", conflictCode: "''", reference: "'agent:invalid'", outcome: "'automatic'", status: "'resolved'", consequence: "'Consequence'", recommendation: "'Choice'", remediation: "'Fix'", observed: "'Observed'", expected: "'Expected'", owner: "NULL", savedDecision: "'migrate'", decidedBy: "NULL", decidedAt: "NULL", decisionNote: "NULL", evidenceHash: "repeat('a', 64)"},
		{name: "blank affected reference", censusVersion: "2", conflictCode: "'CB-INVALID-010'", reference: "''", outcome: "'automatic'", status: "'resolved'", consequence: "'Consequence'", recommendation: "'Choice'", remediation: "'Fix'", observed: "'Observed'", expected: "'Expected'", owner: "NULL", savedDecision: "'migrate'", decidedBy: "NULL", decidedAt: "NULL", decisionNote: "NULL", evidenceHash: "repeat('a', 64)"},
		{name: "blank consequence", censusVersion: "2", conflictCode: "'CB-INVALID-011'", reference: "'agent:invalid'", outcome: "'automatic'", status: "'resolved'", consequence: "''", recommendation: "'Choice'", remediation: "'Fix'", observed: "'Observed'", expected: "'Expected'", owner: "NULL", savedDecision: "'migrate'", decidedBy: "NULL", decidedAt: "NULL", decisionNote: "NULL", evidenceHash: "repeat('a', 64)"},
		{name: "invalid evidence hash", censusVersion: "2", conflictCode: "'CB-INVALID-012'", reference: "'agent:invalid'", outcome: "'automatic'", status: "'resolved'", consequence: "'Consequence'", recommendation: "'Choice'", remediation: "'Fix'", observed: "'Observed'", expected: "'Expected'", owner: "NULL", savedDecision: "'migrate'", decidedBy: "NULL", decidedAt: "NULL", decisionNote: "NULL", evidenceHash: "'not-a-sha256'"},
	} {
		statement := `
			INSERT INTO cerebro_company_brain_migration_decision (
				workspace_id, company_brain_connection_id, agent_id,
				census_version, conflict_code, affected_reference,
				outcome, status, consequence, recommended_choice,
				safe_remediation, observed_state, expected_state,
				owner_user_id, saved_decision, decided_by_user_id,
				decided_at, decision_note, evidence_sha256, evidence_at
			)
			VALUES (
				$1, $2, $3, ` + tc.censusVersion + `, ` + tc.conflictCode + `, ` + tc.reference + `,
				` + tc.outcome + `, ` + tc.status + `, ` + tc.consequence + `, ` + tc.recommendation + `,
				` + tc.remediation + `, ` + tc.observed + `, ` + tc.expected + `,
				` + tc.owner + `, ` + tc.savedDecision + `, ` + tc.decidedBy + `,
				` + tc.decidedAt + `, ` + tc.decisionNote + `, ` + tc.evidenceHash + `, now()
			)
		`
		expectMigrationConstraintFailure(t, tx, tc.name, statement, workspaceID, logicalID, agentID)
	}

	expectMigrationConstraintFailure(t, tx, "cross-workspace logical connection", `
		INSERT INTO cerebro_company_brain_migration_decision (
			workspace_id, company_brain_connection_id, agent_id,
			census_version, conflict_code, affected_reference,
			outcome, status, consequence, recommended_choice,
			safe_remediation, observed_state, expected_state,
			saved_decision, evidence_sha256, evidence_at
		)
		VALUES (
			$1, $2, $3, 2, 'CB-CROSS-001', 'agent:cross',
			'automatic', 'resolved', 'Consequence', 'Choice',
			'Fix', 'Observed', 'Expected',
			'migrate', repeat('a', 64), now()
		)
	`, workspaceID, otherLogicalID, agentID)
	expectMigrationConstraintFailure(t, tx, "cross-workspace agent", `
		INSERT INTO cerebro_company_brain_migration_decision (
			workspace_id, company_brain_connection_id, agent_id,
			census_version, conflict_code, affected_reference,
			outcome, status, consequence, recommended_choice,
			safe_remediation, observed_state, expected_state,
			saved_decision, evidence_sha256, evidence_at
		)
		VALUES (
			$1, $2, $3, 2, 'CB-CROSS-002', 'agent:cross',
			'automatic', 'resolved', 'Consequence', 'Choice',
			'Fix', 'Observed', 'Expected',
			'migrate', repeat('a', 64), now()
		)
	`, workspaceID, logicalID, otherAgentID)
	expectMigrationConstraintFailure(t, tx, "cross-workspace owner", `
		INSERT INTO cerebro_company_brain_migration_decision (
			workspace_id, company_brain_connection_id, agent_id,
			census_version, conflict_code, affected_reference,
			outcome, status, consequence, recommended_choice,
			safe_remediation, observed_state, expected_state,
			owner_user_id, evidence_sha256, evidence_at
		)
		VALUES (
			$1, $2, $3, 2, 'CB-CROSS-003', 'agent:cross',
			'owner_decision', 'pending', 'Consequence', 'Choice',
			'Fix', 'Observed', 'Expected',
			$4, repeat('a', 64), now()
		)
	`, workspaceID, logicalID, agentID, otherOwnerUserID)

	decisionDown := readLogicalCompanyBrainMigration(t, companyBrainMigrationDecisionDownMigration)
	if _, err := tx.Exec(ctx, decisionDown); err != nil {
		t.Fatalf("apply migration-decision down migration: %v", err)
	}
	var tableName *string
	if err := tx.QueryRow(
		ctx,
		`SELECT to_regclass('cerebro_company_brain_migration_decision')::text`,
	).Scan(&tableName); err != nil {
		t.Fatal(err)
	}
	if tableName != nil {
		t.Fatalf("down migration left table %q", *tableName)
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

func insertMigrationMember(t *testing.T, tx pgx.Tx, workspaceID, emailPrefix string) string {
	t.Helper()
	var userID string
	if err := tx.QueryRow(context.Background(), `
		INSERT INTO "user" (name, email)
		VALUES ('Company Brain migration owner', $1 || '-' || gen_random_uuid() || '@example.test')
		RETURNING id
	`, emailPrefix).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(context.Background(), `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, userID); err != nil {
		t.Fatal(err)
	}
	return userID
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

func insertMigrationLegacyConnectionPermission(
	t *testing.T,
	tx pgx.Tx,
	workspaceID, agentID, toolKey string,
) string {
	t.Helper()
	var id string
	if err := tx.QueryRow(context.Background(), `
		INSERT INTO cerebro_tool_policy (
			workspace_id, tool_key, layer, subject_id, setting, resource_pattern
		)
		VALUES ($1, $2, 'agent', $3, 'allow', '')
		RETURNING id
	`, workspaceID, toolKey, agentID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertMigrationPermissionAudit(
	t *testing.T,
	tx pgx.Tx,
	workspaceID, toolKey, agentID string,
) string {
	t.Helper()
	var id string
	if err := tx.QueryRow(context.Background(), `
		INSERT INTO cerebro_tool_policy_audit (
			workspace_id, tool_key, layer, subject_id, resource_pattern,
			action, old_setting, new_setting, actor_type
		)
		VALUES ($1, $2, 'agent', $3, '', 'set', '', 'allow', 'system')
		RETURNING id
	`, workspaceID, toolKey, agentID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertMigrationApproval(
	t *testing.T,
	tx pgx.Tx,
	workspaceID, agentID, capability string,
) string {
	t.Helper()
	var id string
	if err := tx.QueryRow(context.Background(), `
		INSERT INTO cerebro_approval_request (
			workspace_id, requester_type, requester_id, agent_id,
			capability, resource, status
		)
		VALUES (
			$1, 'agent', $2, $2,
			$3, 'search', 'approved'
		)
		RETURNING id
	`, workspaceID, agentID, capability).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertMigrationApprovalAudit(
	t *testing.T,
	tx pgx.Tx,
	workspaceID, approvalID string,
) string {
	t.Helper()
	var id string
	if err := tx.QueryRow(context.Background(), `
		INSERT INTO cerebro_approval_audit (
			workspace_id, approval_id, action, actor_type, surface
		)
		VALUES ($1, $2, 'approved', 'system', 'system')
		RETURNING id
	`, workspaceID, approvalID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertMigrationToolAlias(
	t *testing.T,
	tx pgx.Tx,
	capabilityPrefix string,
) (string, string) {
	t.Helper()
	capabilityID := capabilityPrefix + ":" + strings.ToLower(t.Name())
	if _, err := tx.Exec(context.Background(), `
		INSERT INTO cerebro_canonical_capability (
			canonical_id, family, source_reference
		)
		VALUES ($1, 'mcp', 'FIR-3924 migration test')
	`, capabilityID); err != nil {
		t.Fatal(err)
	}

	var id string
	if err := tx.QueryRow(context.Background(), `
		INSERT INTO cerebro_capability_alias (
			capability_id, surface, provider, key_value,
			resource_pattern, key_source, relation, source_reference
		)
		VALUES (
			$1, 'mcp', '', $2, '', '', 'alias', 'FIR-3924 migration test'
		)
		RETURNING id
	`, capabilityID, strings.ReplaceAll(capabilityPrefix, ":", "__")+"-"+strings.ToLower(t.Name())).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id, capabilityID
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
