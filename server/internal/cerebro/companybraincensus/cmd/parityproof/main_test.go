package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/cerebro/companybraincensus"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

func TestValidateOptionsKeepsDryRunReadOnlyAndRequiresApprovalForApply(t *testing.T) {
	base := options{
		databaseURL:    "postgres://database",
		workspaceID:    "11111111-1111-4111-8111-111111111111",
		snapshotFile:   "frozen-census.json",
		snapshotSHA256: strings.Repeat("a", 64),
	}
	if err := validateOptions(base); err != nil {
		t.Fatalf("validate dry-run options: %v", err)
	}

	apply := base
	apply.apply = true
	if err := validateOptions(apply); err == nil ||
		!strings.Contains(err.Error(), "approval-id") {
		t.Fatalf("apply validation error = %v, want approval requirement", err)
	}
	apply.approvalID = "22222222-2222-4222-8222-222222222222"
	if err := validateOptions(apply); err != nil {
		t.Fatalf("validate approved apply options: %v", err)
	}
}

func TestParityPopulationResultRequiresEveryEligibleAgentCovered(t *testing.T) {
	tests := []struct {
		name      string
		result    parityPopulationResult
		wantReady bool
	}{
		{
			name: "all matched",
			result: parityPopulationResult{
				ExpectedEligibleAgentCount: 49,
				ProofCount:                 49,
				MatchedCount:               49,
				ConnectionsUnchanged:       true,
				PermissionsUnchanged:       true,
			},
			wantReady: true,
		},
		{
			name: "blocked agents have saved do not migrate decisions",
			result: parityPopulationResult{
				ExpectedEligibleAgentCount: 49,
				ProofCount:                 49,
				MatchedCount:               47,
				DoNotMigrateCount:          2,
				ConnectionsUnchanged:       true,
				PermissionsUnchanged:       true,
			},
			wantReady: true,
		},
		{
			name: "missing proof",
			result: parityPopulationResult{
				ExpectedEligibleAgentCount: 49,
				ProofCount:                 48,
				MatchedCount:               48,
				ConnectionsUnchanged:       true,
				PermissionsUnchanged:       true,
			},
			wantReady: false,
		},
		{
			name: "unresolved blocker",
			result: parityPopulationResult{
				ExpectedEligibleAgentCount: 49,
				ProofCount:                 49,
				MatchedCount:               48,
				ConnectionsUnchanged:       true,
				PermissionsUnchanged:       true,
			},
			wantReady: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.result.finalize()
			if test.result.CutoverReady != test.wantReady {
				t.Fatalf(
					"CutoverReady = %t, want %t",
					test.result.CutoverReady,
					test.wantReady,
				)
			}
		})
	}
}

func TestParityPopulationResultFailsWhenConnectionsOrPermissionsChanged(t *testing.T) {
	result := parityPopulationResult{
		ExpectedEligibleAgentCount: 49,
		ProofCount:                 49,
		MatchedCount:               49,
		ConnectionsUnchanged:       true,
		PermissionsUnchanged:       false,
	}
	result.finalize()
	if result.CutoverReady {
		t.Fatal("permission drift must keep cutover blocked")
	}
}

func TestStateFingerprintsDetectConnectionAndPermissionChanges(t *testing.T) {
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

	var workspaceID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES (
			'Parity proof fingerprint test',
			'parity-proof-fingerprint-' || gen_random_uuid(),
			'',
			'PPF'
		)
		RETURNING id
	`).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, workspaceID); err != nil {
			t.Errorf("delete fingerprint test workspace: %v", err)
		}
	})

	initial, err := loadStateFingerprints(ctx, pool, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspace_connection (
			workspace_id, name, display_name, type, url
		) VALUES (
			$1, 'company-brain', 'Company Brain', 'mcp_http',
			'http://company-brain.invalid/mcp'
		)
	`, workspaceID); err != nil {
		t.Fatal(err)
	}
	afterConnection, err := loadStateFingerprints(ctx, pool, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if afterConnection.connections == initial.connections {
		t.Fatal("Connection mutation did not change Connection fingerprint")
	}
	if afterConnection.permissions != initial.permissions {
		t.Fatal("Connection mutation changed permission fingerprint")
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO cerebro_tool_policy (
			workspace_id, tool_key, layer, subject_id, setting
		) VALUES ($1, 'connection:company-brain', 'workspace', $1, 'deny')
	`, workspaceID); err != nil {
		t.Fatal(err)
	}
	afterPermission, err := loadStateFingerprints(ctx, pool, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if afterPermission.connections != afterConnection.connections {
		t.Fatal("permission mutation changed Connection fingerprint")
	}
	if afterPermission.permissions == afterConnection.permissions {
		t.Fatal("permission mutation did not change permission fingerprint")
	}
}

func TestParityProofCommandDryRunAndApprovedApplyKeepLiveStateUnchanged(t *testing.T) {
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

	var workspaceID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES (
			'Parity proof command test',
			'parity-proof-command-' || gen_random_uuid(),
			'',
			'PPC'
		)
		RETURNING id
	`).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	var ownerID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES (
			'Parity proof owner',
			'parity-proof-' || gen_random_uuid() || '@test.local'
		)
		RETURNING id
	`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, workspaceID); err != nil {
			t.Errorf("delete parity proof command workspace: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, ownerID); err != nil {
			t.Errorf("delete parity proof command owner: %v", err)
		}
	})
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, ownerID); err != nil {
		t.Fatal(err)
	}

	var targetConnectionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace_connection (
			workspace_id, name, display_name, type, url, tools, default_access
		) VALUES (
			$1, 'company-brain', 'Company Brain', 'mcp_http',
			'http://company-brain.invalid/mcp',
			'[{"name":"search","description":"Search"}]'::jsonb,
			'deny'
		)
		RETURNING id
	`, workspaceID).Scan(&targetConnectionID); err != nil {
		t.Fatal(err)
	}
	var logicalConnectionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO cerebro_company_brain_connection (
			workspace_id, connection_id, tool_contract_sha256
		) VALUES ($1, $2, repeat('a', 64))
		RETURNING id
	`, workspaceID, targetConnectionID).Scan(&logicalConnectionID); err != nil {
		t.Fatal(err)
	}
	var agentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, owner_id)
		VALUES ($1, 'Parity proof agent', 'local', $2)
		RETURNING id
	`, workspaceID, ownerID).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO cerebro_tool_policy (
			workspace_id, tool_key, layer, subject_id, setting,
			resource_pattern, updated_by,
			company_brain_connection_id,
			company_brain_allowed_read_sources,
			company_brain_write_source,
			company_brain_access_version,
			company_brain_lifecycle_state
		) VALUES (
			$1, 'connection:company-brain', 'agent', $2, 'allow',
			'', $3, $4, ARRAY['commercial'], 'commercial', 1, 'draft'
		)
	`, workspaceID, agentID, ownerID, logicalConnectionID); err != nil {
		t.Fatal(err)
	}

	generatedAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	legacyConnection := map[string]any{
		"connection_id":   "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		"connection_name": "company-brain-commercial",
		"claim": map[string]any{
			"write_source":         "commercial",
			"allowed_read_sources": []string{"commercial"},
		},
		"tool_access": []map[string]string{{
			"tool": "search", "decision": "allow",
		}},
		"status": "verified",
	}
	snapshot, err := json.Marshal(map[string]any{
		"workspace_id":                workspaceID,
		"census_version":              1,
		"company_brain_connection_id": logicalConnectionID,
		"report": map[string]any{
			"generated_at": generatedAt,
			"actors": []map[string]any{{
				"agent_id": agentID,
				"name":     "Parity proof agent",
				"status":   "online",
				"sources":  []map[string]any{legacyConnection},
			}},
			"automations": []any{},
			"connections": []map[string]any{legacyConnection},
			"references":  []any{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(snapshot)
	snapshotSHA256 := hex.EncodeToString(sum[:])
	snapshotFile := t.TempDir() + "/frozen-census.json"
	if err := os.WriteFile(snapshotFile, snapshot, 0o600); err != nil {
		t.Fatal(err)
	}
	opts := options{
		databaseURL:    databaseURL,
		workspaceID:    workspaceID,
		snapshotFile:   snapshotFile,
		snapshotSHA256: snapshotSHA256,
	}

	beforeDryRun, err := loadStateFingerprints(ctx, pool, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if err := run(ctx, []string{
		"--database-url", opts.databaseURL,
		"--workspace-id", opts.workspaceID,
		"--snapshot-file", opts.snapshotFile,
		"--snapshot-sha256", opts.snapshotSHA256,
	}); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	afterDryRun, err := loadStateFingerprints(ctx, pool, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if beforeDryRun != afterDryRun {
		t.Fatal("dry-run changed a Connection or permission fingerprint")
	}
	var proofCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM cerebro_company_brain_parity_proof
		WHERE workspace_id = $1
	`, workspaceID).Scan(&proofCount); err != nil {
		t.Fatal(err)
	}
	if proofCount != 0 {
		t.Fatalf("dry-run wrote %d parity proofs", proofCount)
	}

	frozen := companybraincensus.NewFrozenCensusSnapshotLoader(
		snapshot,
		snapshotSHA256,
	)
	current := companybraincensus.NewDatabaseCurrentTargetPermissionLoader(
		pool,
		toolpolicy.NewStore(pool),
	)
	inputs, err := companybraincensus.NewParityPopulationPreparer(
		frozen,
		current,
	).Prepare(ctx, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	var approvalID string
	if err := pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&approvalID); err != nil {
		t.Fatal(err)
	}
	request := companybraincensus.ParityPopulationRequest{
		AuthorizationID:                 approvalID,
		WorkspaceID:                     inputs.WorkspaceID,
		FrozenCensusSHA256:              inputs.FrozenCensusSHA256,
		CensusVersion:                   inputs.CensusVersion,
		CompanyBrainConnectionID:        inputs.CompanyBrainConnectionID,
		ExpectedEligibleAgentCount:      inputs.ExpectedEligibleAgentCount,
		ExpectedTargetPermissionsSHA256: inputs.ExpectedTargetPermissionsSHA256,
	}
	contextJSON, err := json.Marshal(map[string]any{
		"issue_id":                    companybraincensus.ParityPopulationApprovalIssueID,
		"approval_boundary":           companybraincensus.ParityPopulationApprovalBoundary,
		"frozen_census_sha256":        request.FrozenCensusSHA256,
		"census_version":              request.CensusVersion,
		"company_brain_connection_id": request.CompanyBrainConnectionID,
		"eligible_agent_count":        request.ExpectedEligibleAgentCount,
		"target_permissions_sha256":   request.ExpectedTargetPermissionsSHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO cerebro_approval_request (
			id, workspace_id, requester_type, requester_id,
			capability, resource, context, status,
			decided_by_id, decided_at, single_use, expires_at
		) VALUES (
			$1, $2, 'member', $3, $4, $5, $6,
			'approved', $3, now(), true, now() + interval '10 minutes'
		)
	`,
		approvalID,
		workspaceID,
		ownerID,
		companybraincensus.ParityPopulationApprovalCapability,
		companybraincensus.ParityPopulationApprovalResource(request),
		contextJSON,
	); err != nil {
		t.Fatal(err)
	}

	beforeApply, err := loadStateFingerprints(ctx, pool, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if err := run(ctx, []string{
		"--database-url", opts.databaseURL,
		"--workspace-id", opts.workspaceID,
		"--snapshot-file", opts.snapshotFile,
		"--snapshot-sha256", opts.snapshotSHA256,
		"--apply",
		"--approval-id", approvalID,
	}); err != nil {
		t.Fatalf("approved apply: %v", err)
	}
	afterApply, err := loadStateFingerprints(ctx, pool, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if beforeApply != afterApply {
		t.Fatal("approved apply changed a Connection or permission fingerprint")
	}

	var proofStatus string
	var consumed bool
	if err := pool.QueryRow(ctx, `
		SELECT status
		FROM cerebro_company_brain_parity_proof
		WHERE workspace_id = $1
		  AND company_brain_connection_id = $2
		  AND agent_id = $3
		  AND census_version = 1
	`, workspaceID, logicalConnectionID, agentID).Scan(&proofStatus); err != nil {
		t.Fatal(err)
	}
	if proofStatus != "matched" {
		t.Fatalf("proof status = %q, want matched", proofStatus)
	}
	if err := pool.QueryRow(ctx, `
		SELECT consumed_at IS NOT NULL
		FROM cerebro_approval_request
		WHERE id = $1
	`, approvalID).Scan(&consumed); err != nil {
		t.Fatal(err)
	}
	if !consumed {
		t.Fatal("approved apply did not consume approval")
	}
	if err := run(ctx, []string{
		"--database-url", opts.databaseURL,
		"--workspace-id", opts.workspaceID,
		"--snapshot-file", opts.snapshotFile,
		"--snapshot-sha256", opts.snapshotSHA256,
		"--apply",
		"--approval-id", approvalID,
	}); err == nil || !strings.Contains(err.Error(), "live, unused, single-use") {
		t.Fatalf("replayed apply error = %v, want fail-closed denial", err)
	}
}
