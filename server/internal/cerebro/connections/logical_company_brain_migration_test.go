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
	logicalCompanyBrainUpMigration   = "../../../migrations/9163_cerebro_company_brain_connection.up.sql"
	logicalCompanyBrainDownMigration = "../../../migrations/9163_cerebro_company_brain_connection.down.sql"
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
