package companybraincensus

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestParityProofWriterPersistsMatchedAndBlockedEvidenceExactly(t *testing.T) {
	pool := openParityWriterPool(t)
	ctx := context.Background()
	workspaceID, logicalID := insertParityWriterWorkspace(t, pool, "exact")
	t.Cleanup(func() { deleteParityWriterWorkspace(t, pool, workspaceID) })

	matched := insertParityWriterIdentity(t, pool, workspaceID, logicalID, 7)
	blocked := insertParityWriterIdentity(t, pool, workspaceID, logicalID, 8)
	if _, err := pool.Exec(ctx, `
		UPDATE cerebro_tool_policy
		SET company_brain_allowed_read_sources = ARRAY['commercial']
		WHERE id = $1
	`, blocked.PermissionID); err != nil {
		t.Fatalf("align blocked target permission: %v", err)
	}
	now := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	evaluations := parityWriterEvaluations(t, logicalID, matched, blocked, now)

	writer := NewParityProofWriter(pool)
	if err := writer.Write(ctx, workspaceID, evaluations); err != nil {
		t.Fatalf("write parity proofs: %v", err)
	}

	for _, want := range evaluations {
		var got ParityEvaluation
		var status string
		var blocker *string
		if err := pool.QueryRow(ctx, `
			SELECT company_brain_connection_id::text,
			       target_permission_id::text, agent_id::text,
			       census_version, access_version,
			       legacy_access_sha256, target_access_sha256,
			       legacy_approval_sha256, target_approval_sha256,
			       legacy_tool_calls_sha256, target_tool_calls_sha256,
			       legacy_tool_count, target_tool_count,
			       legacy_write_source, target_write_source,
			       status, blocker_code, evidence_sha256, evidence_at
			FROM cerebro_company_brain_parity_proof
			WHERE workspace_id = $1 AND agent_id = $2 AND census_version = $3
		`, workspaceID, want.AgentID, want.CensusVersion).Scan(
			&got.CompanyBrainConnectionID,
			&got.TargetPermissionID, &got.AgentID,
			&got.CensusVersion, &got.AccessVersion,
			&got.LegacyAccessSHA256, &got.TargetAccessSHA256,
			&got.LegacyApprovalSHA256, &got.TargetApprovalSHA256,
			&got.LegacyToolCallsSHA256, &got.TargetToolCallsSHA256,
			&got.LegacyToolCount, &got.TargetToolCount,
			&got.LegacyWriteSource, &got.TargetWriteSource,
			&status, &blocker, &got.EvidenceSHA256, &got.EvidenceAt,
		); err != nil {
			t.Fatalf("read proof for %s: %v", want.AgentID, err)
		}
		got.Status = ParityStatus(status)
		if blocker != nil {
			got.BlockerCode = ParityBlockerCode(*blocker)
		}
		got.censusGeneratedAt = want.censusGeneratedAt
		got.evaluationBatchSHA256 = want.evaluationBatchSHA256
		if !got.EvidenceAt.Equal(want.EvidenceAt) {
			t.Fatalf("persisted evidence time = %s, want %s", got.EvidenceAt, want.EvidenceAt)
		}
		got.EvidenceAt = time.Time{}
		want.EvidenceAt = time.Time{}
		if got != want {
			t.Fatalf("persisted proof differs for %s:\n got: %#v\nwant: %#v", want.AgentID, got, want)
		}
	}
}

func TestParityProofWriterRejectsNonDeterministicStaleAndCrossWorkspaceResultsAtomically(t *testing.T) {
	pool := openParityWriterPool(t)
	ctx := context.Background()
	workspaceID, logicalID := insertParityWriterWorkspace(t, pool, "reject")
	otherWorkspaceID, otherLogicalID := insertParityWriterWorkspace(t, pool, "other")
	t.Cleanup(func() { deleteParityWriterWorkspace(t, pool, workspaceID) })
	t.Cleanup(func() { deleteParityWriterWorkspace(t, pool, otherWorkspaceID) })

	valid := insertParityWriterIdentity(t, pool, workspaceID, logicalID, 7)
	stale := insertParityWriterIdentity(t, pool, workspaceID, logicalID, 8)
	otherValid := insertParityWriterIdentity(t, pool, workspaceID, logicalID, 10)
	foreign := insertParityWriterIdentity(t, pool, otherWorkspaceID, otherLogicalID, 9)
	now := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	writer := NewParityProofWriter(pool)

	t.Run("tampered deterministic evidence", func(t *testing.T) {
		results := parityWriterEvaluations(t, logicalID, valid, stale, now)
		results[0].EvidenceSHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		if err := writer.Write(ctx, workspaceID, results[:1]); err == nil {
			t.Fatal("tampered EvaluateParity result unexpectedly persisted")
		}
		expectParityWriterProofCount(t, pool, workspaceID, 0)
	})

	t.Run("mixed evaluator batches", func(t *testing.T) {
		batch := []ParityEvaluation{
			singleParityWriterEvaluation(t, logicalID, valid, now),
			singleParityWriterEvaluation(t, logicalID, otherValid, now),
		}
		if err := writer.Write(ctx, workspaceID, batch); err == nil {
			t.Fatal("results from separate EvaluateParity calls unexpectedly persisted together")
		}
		expectParityWriterProofCount(t, pool, workspaceID, 0)
	})

	t.Run("stale permission identity", func(t *testing.T) {
		results := parityWriterEvaluations(t, logicalID, valid, stale, now)
		if _, err := pool.Exec(ctx, `
			UPDATE cerebro_tool_policy
			SET company_brain_allowed_read_sources = ARRAY['commercial', 'internal']
			WHERE id = $1
		`, stale.PermissionID); err != nil {
			t.Fatalf("advance permission identity: %v", err)
		}
		if err := writer.Write(ctx, workspaceID, results); err == nil {
			t.Fatal("stale permission identity unexpectedly persisted")
		}
		expectParityWriterProofCount(t, pool, workspaceID, 0)
	})

	t.Run("cross-workspace identity rolls back prior row", func(t *testing.T) {
		batch := parityWriterEvaluations(t, logicalID, valid, foreign, now.Add(10*time.Second))
		if err := writer.Write(ctx, workspaceID, batch); err == nil {
			t.Fatal("cross-workspace identity unexpectedly persisted")
		}
		expectParityWriterProofCount(t, pool, workspaceID, 0)
		expectParityWriterProofCount(t, pool, otherWorkspaceID, 0)
	})
}

func singleParityWriterEvaluation(
	t *testing.T,
	logicalID string,
	identity parityWriterIdentity,
	now time.Time,
) ParityEvaluation {
	t.Helper()
	report, target := parityFixture()
	report.Actors[0].AgentID = identity.AgentID
	target.AgentID = identity.AgentID
	target.PermissionID = identity.PermissionID
	target.CompanyBrainConnectionID = logicalID
	target.AccessVersion = identity.Version
	results := EvaluateParity(report, []TargetPermission{target}, 12, logicalID, now)
	if len(results) != 1 || results[0].Status != ParityMatched {
		t.Fatalf("unexpected single evaluator fixture: %#v", results)
	}
	return results[0]
}

type parityWriterIdentity struct {
	AgentID      string
	PermissionID string
	Version      int64
}

func parityWriterEvaluations(
	t *testing.T,
	logicalID string,
	matched parityWriterIdentity,
	blocked parityWriterIdentity,
	now time.Time,
) []ParityEvaluation {
	t.Helper()
	report, matchedTarget := parityFixture()
	report.Actors[0].AgentID = matched.AgentID
	matchedTarget.AgentID = matched.AgentID
	matchedTarget.PermissionID = matched.PermissionID
	matchedTarget.CompanyBrainConnectionID = logicalID
	matchedTarget.AccessVersion = matched.Version

	blockedActor := report.Actors[0]
	blockedActor.AgentID = blocked.AgentID
	blockedActor.Name = "Blocked"
	report.Actors = append(report.Actors, blockedActor)
	blockedTarget := matchedTarget
	blockedTarget.AgentID = blocked.AgentID
	blockedTarget.PermissionID = blocked.PermissionID
	blockedTarget.AccessVersion = blocked.Version
	blockedTarget.AllowedReadSources = []string{"commercial"}

	results := EvaluateParity(
		report,
		[]TargetPermission{matchedTarget, blockedTarget},
		12,
		logicalID,
		now,
	)
	if len(results) != 2 {
		t.Fatalf("unexpected evaluator fixture: %#v", results)
	}
	seenMatched := false
	seenBlocked := false
	for _, result := range results {
		switch result.AgentID {
		case matched.AgentID:
			seenMatched = result.Status == ParityMatched && result.BlockerCode == ""
		case blocked.AgentID:
			seenBlocked = result.Status == ParityBlocked && result.BlockerCode == BlockerAccessMismatch
		}
	}
	if !seenMatched || !seenBlocked {
		t.Fatalf("unexpected evaluator fixture: %#v", results)
	}
	return results
}

func openParityWriterPool(t *testing.T) *pgxpool.Pool {
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

func insertParityWriterWorkspace(t *testing.T, pool *pgxpool.Pool, label string) (string, string) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var workspaceID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Parity writer test', $1 || '-' || gen_random_uuid(), '', 'PWT')
		RETURNING id
	`, "parity-writer-"+label).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	var connectionID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO workspace_connection
			(workspace_id, name, display_name, type, url, tools, instructions)
		VALUES ($1, 'company-brain', 'company-brain', 'mcp_http',
		        'http://company-brain.invalid/mcp',
		        '[{"name":"search","description":"Search"}]'::jsonb,
		        'Parity writer test only')
		RETURNING id
	`, workspaceID).Scan(&connectionID); err != nil {
		t.Fatal(err)
	}
	var logicalID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO cerebro_company_brain_connection
			(workspace_id, connection_id, tool_contract_sha256)
		VALUES ($1, $2, repeat('a', 64))
		RETURNING id
	`, workspaceID, connectionID).Scan(&logicalID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return workspaceID, logicalID
}

func insertParityWriterIdentity(
	t *testing.T,
	pool *pgxpool.Pool,
	workspaceID string,
	logicalID string,
	version int64,
) parityWriterIdentity {
	t.Helper()
	ctx := context.Background()
	var identity parityWriterIdentity
	identity.Version = version
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode)
		VALUES ($1, $2, 'local')
		RETURNING id
	`, workspaceID, fmt.Sprintf("Parity writer agent %d", version)).Scan(&identity.AgentID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO cerebro_tool_policy (
			workspace_id, tool_key, layer, subject_id, setting, resource_pattern,
			company_brain_connection_id, company_brain_allowed_read_sources,
			company_brain_write_source, company_brain_access_version,
			company_brain_lifecycle_state
		)
		VALUES (
			$1, 'connection:company-brain', 'agent', $3, 'allow', '',
			$2, ARRAY['commercial', 'shared'], 'commercial', $4, 'draft'
		)
		RETURNING id
	`, workspaceID, logicalID, identity.AgentID, version).Scan(&identity.PermissionID); err != nil {
		t.Fatal(err)
	}
	return identity
}

func expectParityWriterProofCount(t *testing.T, pool *pgxpool.Pool, workspaceID string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM cerebro_company_brain_parity_proof WHERE workspace_id = $1
	`, workspaceID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("parity proof rows = %d, want %d", got, want)
	}
}

func deleteParityWriterWorkspace(t *testing.T, pool *pgxpool.Pool, workspaceID string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID); err != nil && err != pgx.ErrNoRows {
		t.Errorf("delete parity writer workspace: %v", err)
	}
}
