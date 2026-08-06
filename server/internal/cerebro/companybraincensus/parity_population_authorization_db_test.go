package companybraincensus

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOwnerAdminParityPopulationAuthorizationGateConsumesDatabaseApprovalOnce(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	if err := admin.Ping(ctx); err != nil {
		admin.Close()
		t.Skipf("database unreachable: %v", err)
	}
	defer admin.Close()

	schema := fmt.Sprintf("company_brain_parity_approval_%d", time.Now().UnixNano())
	if _, err := admin.Exec(
		ctx,
		"CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize(),
	); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.Exec(
			ctx,
			"DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE",
		)
	}()

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = map[string]string{}
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `
		CREATE TABLE member (
			user_id uuid NOT NULL,
			workspace_id uuid NOT NULL,
			role text NOT NULL
		);
		CREATE TABLE cerebro_approval_request (
			id uuid PRIMARY KEY,
			workspace_id uuid NOT NULL,
			agent_id uuid,
			capability text NOT NULL,
			resource text NOT NULL,
			context jsonb NOT NULL,
			status text NOT NULL,
			single_use boolean NOT NULL,
			consumed_at timestamptz,
			expires_at timestamptz,
			decided_by_id uuid,
			updated_at timestamptz NOT NULL
		);
	`); err != nil {
		t.Fatal(err)
	}

	request := validParityPopulationRequest(t)
	const ownerID = "44444444-4444-4444-8444-444444444444"
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO member VALUES ($1, $2, 'owner')`,
		ownerID,
		request.WorkspaceID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO cerebro_approval_request (
			id, workspace_id, capability, resource, context, status,
			single_use, expires_at, decided_by_id, updated_at
		) VALUES (
			$1, $2, $3, $4,
			jsonb_build_object(
				'issue_id', $5::text,
				'approval_boundary', $6::text,
				'frozen_census_sha256', $7::text,
				'census_version', $8::bigint,
				'company_brain_connection_id', $9::text,
				'eligible_agent_count', $10::integer,
				'target_permissions_sha256', $11::text
			),
			'approved', true, now() + interval '10 minutes', $12, now()
		)
	`,
		request.AuthorizationID,
		request.WorkspaceID,
		ParityPopulationApprovalCapability,
		ParityPopulationApprovalResource(request),
		ParityPopulationApprovalIssueID,
		ParityPopulationApprovalBoundary,
		request.FrozenCensusSHA256,
		request.CensusVersion,
		request.CompanyBrainConnectionID,
		request.ExpectedEligibleAgentCount,
		request.ExpectedTargetPermissionsSHA256,
		ownerID,
	); err != nil {
		t.Fatal(err)
	}

	gate := NewOwnerAdminParityPopulationAuthorizationGate(pool)
	if err := gate.AuthorizeOnce(ctx, request); err != nil {
		t.Fatalf("consume exact owner approval: %v", err)
	}
	var consumed bool
	if err := pool.QueryRow(
		ctx,
		`SELECT consumed_at IS NOT NULL FROM cerebro_approval_request WHERE id = $1`,
		request.AuthorizationID,
	).Scan(&consumed); err != nil {
		t.Fatal(err)
	}
	if !consumed {
		t.Fatal("exact owner approval was not consumed")
	}
	if err := gate.AuthorizeOnce(ctx, request); err == nil ||
		!strings.Contains(err.Error(), "live, unused, single-use") {
		t.Fatalf("replayed database approval error = %v, want fail-closed denial", err)
	}
}
