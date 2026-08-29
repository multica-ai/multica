package migrations

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestOperationalControlsMigrationsAndQueries(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("integration test requires Postgres at DATABASE_URL")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to Postgres: %v", err)
	}
	defer pool.Close()

	schema := "operational_controls_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `
			DROP TABLE IF EXISTS `+quotedSchema+`.agent_tool_action_event,
				`+quotedSchema+`.agent_tool_approval_request,
				`+quotedSchema+`.agent_tool_policy_rule,
				`+quotedSchema+`.agent_tool_policy_revision,
				`+quotedSchema+`.agent_tool_policy,
				`+quotedSchema+`.agent
		`)
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+quotedSchema)
	})

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT set_config('search_path', $1, false)`, schema); err != nil {
		t.Fatalf("set search path: %v", err)
	}
	if _, err := conn.Exec(ctx, `CREATE TABLE agent (id UUID NOT NULL)`); err != nil {
		t.Fatalf("create pre-442 agent table: %v", err)
	}

	for _, stem := range operationalControlsMigrationStems {
		applyMigrationFile(t, ctx, conn.Conn(), stem+".up.sql")
	}

	assertOperatingModeContract(t, ctx, conn.Conn())
	assertMetadataOnlyOperationalColumns(t, ctx, conn.Conn(), schema)
	assertOperationalControlQueryContracts(t, ctx, conn.Conn())

	for i := len(operationalControlsMigrationStems) - 1; i >= 0; i-- {
		applyMigrationFile(t, ctx, conn.Conn(), operationalControlsMigrationStems[i]+".down.sql")
	}

	var operatingModeColumns int
	if err := conn.QueryRow(ctx, `
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema = $1
		  AND table_name = 'agent'
		  AND column_name = 'operating_mode'
	`, schema).Scan(&operatingModeColumns); err != nil {
		t.Fatalf("inspect operating_mode after rollback: %v", err)
	}
	if operatingModeColumns != 0 {
		t.Fatalf("operating_mode columns after rollback = %d, want 0", operatingModeColumns)
	}
}

func assertOperatingModeContract(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	id := operationalPGUUID()
	if _, err := conn.Exec(ctx, `INSERT INTO agent (id) VALUES ($1)`, id); err != nil {
		t.Fatalf("insert agent using operating mode default: %v", err)
	}
	var mode string
	if err := conn.QueryRow(ctx, `SELECT operating_mode FROM agent WHERE id = $1`, id).Scan(&mode); err != nil {
		t.Fatalf("read operating mode default: %v", err)
	}
	if mode != "coding" {
		t.Fatalf("operating mode default = %q, want coding", mode)
	}
	if _, err := conn.Exec(ctx, `UPDATE agent SET operating_mode = 'unbounded' WHERE id = $1`, id); !isCheckViolation(err) {
		t.Fatalf("invalid operating mode update: got %v, want check violation", err)
	}
}

func assertMetadataOnlyOperationalColumns(t *testing.T, ctx context.Context, conn *pgx.Conn, schema string) {
	t.Helper()
	forbidden := []string{
		"arguments", "argument_values", "result", "result_values", "headers",
		"url", "command", "command_line", "environment", "provider_error_body",
	}
	var count int
	if err := conn.QueryRow(ctx, `
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema = $1
		  AND table_name IN ('agent_tool_approval_request', 'agent_tool_action_event')
		  AND column_name = ANY($2::text[])
	`, schema, forbidden).Scan(&count); err != nil {
		t.Fatalf("inspect operational metadata columns: %v", err)
	}
	if count != 0 {
		t.Fatalf("operational tables expose %d raw-value columns", count)
	}
}

func assertOperationalControlQueryContracts(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	queries := db.New(conn)
	workspaceID := operationalPGUUID()
	otherWorkspaceID := operationalPGUUID()
	agentID := operationalPGUUID()
	taskID := operationalPGUUID()
	invocationID := operationalPGUUID()
	operatorID := operationalPGUUID()
	policyID := operationalPGUUID()
	base := time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
	digestA := "sha256:" + strings.Repeat("a", 64)
	digestB := "sha256:" + strings.Repeat("b", 64)

	if _, err := conn.Exec(ctx, `
		INSERT INTO agent_tool_policy (
			id, workspace_id, agent_id, revision, status, policy_digest,
			default_effect, created_by_user_id, updated_by_user_id
		) VALUES ($1, $2, $3, 1, 'active', $4, 'deny', $5, $5)
	`, policyID, workspaceID, agentID, digestA, operatorID); err != nil {
		t.Fatalf("insert active policy: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO agent_tool_policy_rule (
			workspace_id, agent_id, policy_id, transport_kind, server_key,
			tool_name, schema_digest, effect
		) VALUES ($1, $2, $3, 'managed_mcp', 'fixture', 'read', $4, 'require_approval')
	`, workspaceID, agentID, policyID, digestA); err != nil {
		t.Fatalf("insert exact policy rule: %v", err)
	}

	create := db.CreateOrGetAgentToolApprovalRequestParams{
		WorkspaceID:      workspaceID,
		AgentID:          agentID,
		TaskID:           taskID,
		InvocationID:     invocationID,
		IdempotencyKey:   "fixture-call-1",
		TransportKind:    "managed_mcp",
		ServerKey:        "fixture",
		ToolName:         "read",
		SchemaDigest:     digestA,
		PolicyRevision:   1,
		SchemaFieldNames: []string{"record_id"},
		ArgumentBytes:    24,
		RequestedAt:      operationalPGTime(base),
		ExpiresAt:        operationalPGTime(base.Add(30 * time.Minute)),
	}
	first, err := queries.CreateOrGetAgentToolApprovalRequest(ctx, create)
	if err != nil {
		t.Fatalf("create approval request: %v", err)
	}
	retry, err := queries.CreateOrGetAgentToolApprovalRequest(ctx, create)
	if err != nil {
		t.Fatalf("get matching approval request: %v", err)
	}
	if first.ID != retry.ID {
		t.Fatalf("matching retry returned id %v, want %v", retry.ID, first.ID)
	}

	mismatch := create
	mismatch.SchemaDigest = digestB
	if _, err := queries.CreateOrGetAgentToolApprovalRequest(ctx, mismatch); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("mismatched immutable identity: got %v, want no rows", err)
	}
	crossWorkspace := create
	crossWorkspace.WorkspaceID = otherWorkspaceID
	if _, err := queries.CreateOrGetAgentToolApprovalRequest(ctx, crossWorkspace); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross-workspace idempotency retry: got %v, want no rows", err)
	}

	second := create
	second.TaskID = operationalPGUUID()
	second.InvocationID = operationalPGUUID()
	second.IdempotencyKey = "fixture-call-2"
	second.RequestedAt = operationalPGTime(base.Add(time.Minute))
	second.ExpiresAt = operationalPGTime(base.Add(20 * time.Minute))
	secondRow, err := queries.CreateOrGetAgentToolApprovalRequest(ctx, second)
	if err != nil {
		t.Fatalf("create second approval request: %v", err)
	}
	third := create
	third.TaskID = operationalPGUUID()
	third.InvocationID = operationalPGUUID()
	third.IdempotencyKey = "fixture-call-3"
	third.RequestedAt = operationalPGTime(base.Add(2 * time.Minute))
	third.ExpiresAt = operationalPGTime(base.Add(40 * time.Minute))
	thirdRow, err := queries.CreateOrGetAgentToolApprovalRequest(ctx, third)
	if err != nil {
		t.Fatalf("create third approval request: %v", err)
	}

	page, err := queries.ListPendingAgentToolApprovalRequests(ctx, db.ListPendingAgentToolApprovalRequestsParams{
		WorkspaceID: workspaceID,
		AsOf:        operationalPGTime(base),
		PageSize:    2,
	})
	if err != nil {
		t.Fatalf("list first pending page: %v", err)
	}
	if len(page) != 2 || page[0].ID != first.ID || page[1].ID != secondRow.ID {
		t.Fatalf("first pending page ids = %v, want first then second", approvalIDs(page))
	}
	nextPage, err := queries.ListPendingAgentToolApprovalRequests(ctx, db.ListPendingAgentToolApprovalRequestsParams{
		WorkspaceID:       workspaceID,
		AsOf:              operationalPGTime(base),
		CursorRequestedAt: page[1].RequestedAt,
		CursorID:          page[1].ID,
		PageSize:          2,
	})
	if err != nil {
		t.Fatalf("list second pending page: %v", err)
	}
	if len(nextPage) != 1 || nextPage[0].ID != thirdRow.ID {
		t.Fatalf("second pending page ids = %v, want third", approvalIDs(nextPage))
	}

	expired, err := queries.ListExpiredAgentToolApprovalRequestsForUpdate(ctx, db.ListExpiredAgentToolApprovalRequestsForUpdateParams{
		WorkspaceID: workspaceID,
		AsOf:        operationalPGTime(base.Add(time.Hour)),
		BatchSize:   10,
	})
	if err != nil {
		t.Fatalf("list expired approval requests: %v", err)
	}
	if len(expired) != 3 || expired[0].ID != secondRow.ID || expired[1].ID != first.ID || expired[2].ID != thirdRow.ID {
		t.Fatalf("expiry order ids = %v, want second, first, third", approvalIDs(expired))
	}

	approved, err := queries.ApproveAgentToolApprovalRequest(ctx, db.ApproveAgentToolApprovalRequestParams{
		DecidedAt:       operationalPGTime(base.Add(5 * time.Minute)),
		DecidedByUserID: operatorID,
		WorkspaceID:     workspaceID,
		ID:              first.ID,
		AgentID:         agentID,
		TaskID:          taskID,
		InvocationID:    invocationID,
		TransportKind:   "managed_mcp",
		ServerKey:       "fixture",
		ToolName:        "read",
		SchemaDigest:    digestA,
		PolicyRevision:  1,
	})
	if err != nil || approved.Status != "approved" {
		t.Fatalf("approve exact request: status %q, err %v", approved.Status, err)
	}
	consumed, err := queries.ConsumeAgentToolApprovalRequest(ctx, db.ConsumeAgentToolApprovalRequestParams{
		ConsumedAt:     operationalPGTime(base.Add(6 * time.Minute)),
		WorkspaceID:    workspaceID,
		ID:             first.ID,
		AgentID:        agentID,
		TaskID:         taskID,
		InvocationID:   invocationID,
		TransportKind:  "managed_mcp",
		ServerKey:      "fixture",
		ToolName:       "read",
		SchemaDigest:   digestA,
		PolicyRevision: 1,
	})
	if err != nil || consumed.Status != "consumed" {
		t.Fatalf("consume exact request: status %q, err %v", consumed.Status, err)
	}
	if _, err := queries.ConsumeAgentToolApprovalRequest(ctx, db.ConsumeAgentToolApprovalRequestParams{
		ConsumedAt:     operationalPGTime(base.Add(7 * time.Minute)),
		WorkspaceID:    workspaceID,
		ID:             first.ID,
		AgentID:        agentID,
		TaskID:         taskID,
		InvocationID:   invocationID,
		TransportKind:  "managed_mcp",
		ServerKey:      "fixture",
		ToolName:       "read",
		SchemaDigest:   digestA,
		PolicyRevision: 1,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("duplicate consume: got %v, want no rows", err)
	}

	defaultDays, err := queries.GetOperationalControlsRetentionDefaultDays(ctx, workspaceID)
	if err != nil || defaultDays != 90 {
		t.Fatalf("retention default = %d, err %v, want 90", defaultDays, err)
	}
	deleted, err := queries.DeleteTerminalAgentToolApprovalRequestsByRetention(ctx, db.DeleteTerminalAgentToolApprovalRequestsByRetentionParams{
		WorkspaceID:     workspaceID,
		RetentionCutoff: operationalPGTime(base.Add(10 * time.Minute)),
		BatchSize:       10,
	})
	if err != nil {
		t.Fatalf("delete retained approvals: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != first.ID {
		t.Fatalf("retention deleted ids = %v, want consumed request", deleted)
	}
}

func operationalPGUUID() pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte(uuid.New()), Valid: true}
}

func operationalPGTime(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}

func approvalIDs(rows []db.AgentToolApprovalRequest) []pgtype.UUID {
	ids := make([]pgtype.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}
