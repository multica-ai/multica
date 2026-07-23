package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/cerebro/grantrecovery"
)

func TestApplyRollsBackEveryWriteWhenALaterPolicyFails(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not configured; skipping recovery database integration test")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()

	schema := fmt.Sprintf("grant_recovery_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = map[string]string{}
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	target, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()

	const (
		workspaceID = "00000000-0000-0000-0000-000000000001"
		agentID     = "00000000-0000-0000-0000-000000000002"
		approvalID  = "00000000-0000-0000-0000-000000000003"
		ownerID     = "00000000-0000-0000-0000-000000000004"
	)
	setup := `
		CREATE TABLE agent (id uuid PRIMARY KEY, workspace_id uuid NOT NULL);
		CREATE TABLE member (user_id uuid NOT NULL, workspace_id uuid NOT NULL, role text NOT NULL);
		CREATE TABLE cerebro_tool_policy (
			workspace_id uuid NOT NULL, tool_key text NOT NULL, layer text NOT NULL,
			subject_id uuid NOT NULL, setting text NOT NULL, resource_pattern text NOT NULL DEFAULT '',
			conditions jsonb, updated_by text,
			UNIQUE (workspace_id, tool_key, layer, subject_id, resource_pattern)
		);
		CREATE TABLE cerebro_approval_request (
			id uuid PRIMARY KEY, workspace_id uuid NOT NULL, agent_id uuid,
			capability text NOT NULL, resource text NOT NULL, context jsonb NOT NULL,
			status text NOT NULL, single_use boolean NOT NULL, consumed_at timestamptz,
			expires_at timestamptz, decided_by_id uuid, updated_at timestamptz
		);
		CREATE TABLE cerebro_permission_recovery_audit (
			workspace_id uuid NOT NULL, source_fingerprint text NOT NULL,
			approval_id uuid NOT NULL, approved_by text NOT NULL,
			imported_count integer NOT NULL, already_present_count integer NOT NULL,
			imported_identities jsonb NOT NULL,
			UNIQUE (workspace_id, source_fingerprint)
		);
		CREATE FUNCTION fail_second_policy() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.tool_key = 'second' THEN
				RAISE EXCEPTION 'injected partial failure';
			END IF;
			RETURN NEW;
		END $$;
		CREATE TRIGGER fail_second_policy
			BEFORE INSERT ON cerebro_tool_policy
			FOR EACH ROW EXECUTE FUNCTION fail_second_policy();
	`
	if _, err := target.Exec(ctx, setup); err != nil {
		t.Fatal(err)
	}

	grants := []grantrecovery.LegacyGrant{
		{WorkspaceID: workspaceID, AgentID: agentID, ToolName: "first", Enabled: true},
		{WorkspaceID: workspaceID, AgentID: agentID, ToolName: "second", Enabled: true},
	}
	fingerprint := grantrecovery.Fingerprint(grants)
	if _, err := target.Exec(ctx, `INSERT INTO agent VALUES ($1, $2)`, agentID, workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := target.Exec(ctx, `INSERT INTO member VALUES ($1, $2, 'owner')`, ownerID, workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := target.Exec(ctx, `
		INSERT INTO cerebro_approval_request (
			id, workspace_id, capability, resource, context, status,
			single_use, decided_by_id, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, 'approved', true, $6, now()
		)
	`, approvalID, workspaceID, grantrecovery.ApprovalCapability,
		grantrecovery.ApprovalResource(workspaceID, fingerprint),
		fmt.Sprintf(`{"issue_id":%q,"approval_boundary":%q}`, grantrecovery.ApprovalIssueID, grantrecovery.ApprovalBoundary),
		ownerID); err != nil {
		t.Fatal(err)
	}

	err = apply(ctx, target, options{workspaceID: workspaceID, approvalID: approvalID}, grants)
	if err == nil || !strings.Contains(err.Error(), "injected partial failure") {
		t.Fatalf("apply error = %v, want injected partial failure", err)
	}

	for table, predicate := range map[string]string{
		"cerebro_tool_policy":               "TRUE",
		"cerebro_permission_recovery_audit": "TRUE",
		"cerebro_approval_request":          "consumed_at IS NOT NULL",
	} {
		var count int
		if err := target.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE "+predicate).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s retained %d partial writes after rollback", table, count)
		}
	}
}
