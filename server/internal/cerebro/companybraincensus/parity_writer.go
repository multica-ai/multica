package companybraincensus

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/util"
)

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ParityProofDatabase is the transaction boundary required by
// ParityProofWriter. A pgx pool satisfies it directly.
type ParityProofDatabase interface {
	Begin(context.Context) (pgx.Tx, error)
}

// ParityProofWriter persists a complete evaluator batch atomically. It has no
// caller by design: population remains a separate, explicitly gated slice.
type ParityProofWriter struct {
	db ParityProofDatabase
}

func NewParityProofWriter(db ParityProofDatabase) *ParityProofWriter {
	return &ParityProofWriter{db: db}
}

// Write validates that every item is an untampered, persistable result from
// one EvaluateParity call, then inserts or refreshes the batch in one
// transaction. Missing identities and older evidence fail closed.
func (w *ParityProofWriter) Write(
	ctx context.Context,
	workspaceID string,
	evaluations []ParityEvaluation,
) error {
	if w == nil || w.db == nil {
		return fmt.Errorf("parity proof writer database is required")
	}
	if err := validateParityProofBatch(workspaceID, evaluations); err != nil {
		return err
	}

	tx, err := w.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin parity proof transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, evaluation := range evaluations {
		if err := lockCurrentParityIdentity(ctx, tx, workspaceID, evaluation); err != nil {
			return err
		}
		blocker := any(nil)
		if evaluation.BlockerCode != "" {
			blocker = string(evaluation.BlockerCode)
		}
		tag, err := tx.Exec(ctx, writeParityProofSQL,
			workspaceID,
			evaluation.CompanyBrainConnectionID,
			evaluation.TargetPermissionID,
			evaluation.AgentID,
			evaluation.CensusVersion,
			evaluation.AccessVersion,
			evaluation.LegacyAccessSHA256,
			evaluation.TargetAccessSHA256,
			evaluation.LegacyApprovalSHA256,
			evaluation.TargetApprovalSHA256,
			evaluation.LegacyToolCallsSHA256,
			evaluation.TargetToolCallsSHA256,
			evaluation.LegacyToolCount,
			evaluation.TargetToolCount,
			evaluation.LegacyWriteSource,
			evaluation.TargetWriteSource,
			string(evaluation.Status),
			blocker,
			evaluation.EvidenceSHA256,
			evaluation.EvidenceAt,
		)
		if err != nil {
			return fmt.Errorf("write parity proof for agent %s: %w", evaluation.AgentID, err)
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf(
				"write parity proof for agent %s: stale or cross-workspace identity",
				evaluation.AgentID,
			)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit parity proof transaction: %w", err)
	}
	return nil
}

func lockCurrentParityIdentity(
	ctx context.Context,
	tx pgx.Tx,
	workspaceID string,
	evaluation ParityEvaluation,
) error {
	var allowedReadSources []string
	var writeSource string
	if err := tx.QueryRow(ctx, lockParityIdentitySQL,
		workspaceID,
		evaluation.CompanyBrainConnectionID,
		evaluation.TargetPermissionID,
		evaluation.AgentID,
		evaluation.AccessVersion,
	).Scan(&allowedReadSources, &writeSource); err != nil {
		return fmt.Errorf(
			"lock parity proof identity for agent %s: stale or cross-workspace identity: %w",
			evaluation.AgentID,
			err,
		)
	}
	canonicalReadSources, ok := canonicalStrings(allowedReadSources, sourceID.MatchString)
	if !ok ||
		canonicalHash(canonicalReadSources) != evaluation.TargetAccessSHA256 ||
		writeSource != evaluation.TargetWriteSource {
		return fmt.Errorf(
			"lock parity proof identity for agent %s: stale target access evidence",
			evaluation.AgentID,
		)
	}
	return nil
}

func validateParityProofBatch(workspaceID string, evaluations []ParityEvaluation) error {
	if _, err := util.ParseUUID(workspaceID); err != nil {
		return fmt.Errorf("invalid parity proof workspace identity: %w", err)
	}
	if len(evaluations) == 0 {
		return fmt.Errorf("parity proof batch is empty")
	}

	first := evaluations[0]
	if !sha256Hex.MatchString(first.evaluationBatchSHA256) {
		return fmt.Errorf("parity proof batch is not an EvaluateParity result")
	}
	seenAgents := make(map[string]struct{}, len(evaluations))
	for _, evaluation := range evaluations {
		if evaluation.CompanyBrainConnectionID != first.CompanyBrainConnectionID ||
			evaluation.CensusVersion != first.CensusVersion ||
			!evaluation.EvidenceAt.Equal(first.EvidenceAt) ||
			!evaluation.censusGeneratedAt.Equal(first.censusGeneratedAt) ||
			evaluation.evaluationBatchSHA256 != first.evaluationBatchSHA256 {
			return fmt.Errorf("parity proof batch combines different evaluator runs")
		}
		if _, duplicate := seenAgents[evaluation.AgentID]; duplicate {
			return fmt.Errorf("duplicate parity proof agent identity %s", evaluation.AgentID)
		}
		seenAgents[evaluation.AgentID] = struct{}{}
		if err := validateParityProofEvaluation(evaluation); err != nil {
			return fmt.Errorf("invalid parity proof for agent %s: %w", evaluation.AgentID, err)
		}
	}
	if canonicalHash(evaluations) != first.evaluationBatchSHA256 {
		return fmt.Errorf("parity proof batch differs from EvaluateParity output")
	}
	return nil
}

func validateParityProofEvaluation(evaluation ParityEvaluation) error {
	for name, value := range map[string]string{
		"logical connection": evaluation.CompanyBrainConnectionID,
		"target permission":  evaluation.TargetPermissionID,
		"agent":              evaluation.AgentID,
	} {
		if _, err := util.ParseUUID(value); err != nil {
			return fmt.Errorf("invalid %s identity: %w", name, err)
		}
	}
	if evaluation.CensusVersion <= 0 || evaluation.AccessVersion <= 0 {
		return fmt.Errorf("census and access versions must be positive")
	}
	if evaluation.censusGeneratedAt.IsZero() ||
		evaluation.EvidenceAt.IsZero() ||
		evaluation.censusGeneratedAt.After(evaluation.EvidenceAt) ||
		evaluation.EvidenceAt.After(time.Now().UTC()) {
		return fmt.Errorf("invalid evaluator evidence time")
	}
	for name, value := range map[string]string{
		"legacy access":     evaluation.LegacyAccessSHA256,
		"target access":     evaluation.TargetAccessSHA256,
		"legacy approval":   evaluation.LegacyApprovalSHA256,
		"target approval":   evaluation.TargetApprovalSHA256,
		"legacy tool calls": evaluation.LegacyToolCallsSHA256,
		"target tool calls": evaluation.TargetToolCallsSHA256,
		"complete evidence": evaluation.EvidenceSHA256,
	} {
		if !sha256Hex.MatchString(value) {
			return fmt.Errorf("invalid %s hash", name)
		}
	}
	if evaluation.LegacyToolCount <= 0 || evaluation.TargetToolCount <= 0 {
		return fmt.Errorf("tool counts must be positive")
	}
	if !sourceID.MatchString(evaluation.LegacyWriteSource) ||
		!sourceID.MatchString(evaluation.TargetWriteSource) {
		return fmt.Errorf("invalid write source evidence")
	}

	wantStatus := ParityMatched
	wantBlocker := ParityBlockerCode("")
	switch {
	case evaluation.LegacyAccessSHA256 != evaluation.TargetAccessSHA256:
		wantStatus, wantBlocker = ParityBlocked, BlockerAccessMismatch
	case evaluation.LegacyApprovalSHA256 != evaluation.TargetApprovalSHA256:
		wantStatus, wantBlocker = ParityBlocked, BlockerApprovalMismatch
	case evaluation.LegacyToolCount != evaluation.TargetToolCount:
		wantStatus, wantBlocker = ParityBlocked, BlockerToolCountMismatch
	case evaluation.LegacyToolCallsSHA256 != evaluation.TargetToolCallsSHA256:
		wantStatus, wantBlocker = ParityBlocked, BlockerToolCallMismatch
	case evaluation.LegacyWriteSource != evaluation.TargetWriteSource:
		wantStatus, wantBlocker = ParityBlocked, BlockerWriteDestinationMismatch
	}
	if evaluation.Status != wantStatus || evaluation.BlockerCode != wantBlocker {
		return fmt.Errorf("status or blocker does not match evaluator evidence")
	}
	if evaluation.EvidenceSHA256 != evaluationHash(
		evaluation.censusGeneratedAt,
		evaluation,
	) {
		return fmt.Errorf("deterministic evidence hash does not match evaluator result")
	}
	return nil
}

const writeParityProofSQL = `
	INSERT INTO cerebro_company_brain_parity_proof (
		workspace_id, company_brain_connection_id,
		target_permission_id, agent_id, census_version, access_version,
		legacy_access_sha256, target_access_sha256,
		legacy_approval_sha256, target_approval_sha256,
		legacy_tool_calls_sha256, target_tool_calls_sha256,
		legacy_tool_count, target_tool_count,
		legacy_write_source, target_write_source,
		status, blocker_code, evidence_sha256, evidence_at
	)
	SELECT
		$1, $2, $3, $4, $5, $6,
		$7, $8, $9, $10, $11, $12,
		$13, $14, $15, $16,
		$17, $18, $19, $20
	FROM cerebro_company_brain_connection AS logical_connection
	JOIN agent AS target_agent
	  ON target_agent.workspace_id = logical_connection.workspace_id
	 AND target_agent.id = $4
	JOIN cerebro_tool_policy AS target_permission
	  ON target_permission.workspace_id = logical_connection.workspace_id
	 AND target_permission.id = $3
	 AND target_permission.subject_id = target_agent.id
	 AND target_permission.company_brain_connection_id = logical_connection.id
	 AND target_permission.company_brain_access_version = $6
	WHERE logical_connection.workspace_id = $1
	  AND logical_connection.id = $2
	ON CONFLICT (
		workspace_id, company_brain_connection_id, agent_id, census_version
	) DO UPDATE SET
		target_permission_id = EXCLUDED.target_permission_id,
		access_version = EXCLUDED.access_version,
		legacy_access_sha256 = EXCLUDED.legacy_access_sha256,
		target_access_sha256 = EXCLUDED.target_access_sha256,
		legacy_approval_sha256 = EXCLUDED.legacy_approval_sha256,
		target_approval_sha256 = EXCLUDED.target_approval_sha256,
		legacy_tool_calls_sha256 = EXCLUDED.legacy_tool_calls_sha256,
		target_tool_calls_sha256 = EXCLUDED.target_tool_calls_sha256,
		legacy_tool_count = EXCLUDED.legacy_tool_count,
		target_tool_count = EXCLUDED.target_tool_count,
		legacy_write_source = EXCLUDED.legacy_write_source,
		target_write_source = EXCLUDED.target_write_source,
		status = EXCLUDED.status,
		blocker_code = EXCLUDED.blocker_code,
		evidence_sha256 = EXCLUDED.evidence_sha256,
		evidence_at = EXCLUDED.evidence_at,
		updated_at = now()
	WHERE cerebro_company_brain_parity_proof.evidence_at <= EXCLUDED.evidence_at
`

const lockParityIdentitySQL = `
	SELECT target_permission.company_brain_allowed_read_sources,
	       target_permission.company_brain_write_source
	FROM cerebro_company_brain_connection AS logical_connection
	JOIN agent AS target_agent
	  ON target_agent.workspace_id = logical_connection.workspace_id
	 AND target_agent.id = $4
	JOIN cerebro_tool_policy AS target_permission
	  ON target_permission.workspace_id = logical_connection.workspace_id
	 AND target_permission.id = $3
	 AND target_permission.subject_id = target_agent.id
	 AND target_permission.company_brain_connection_id = logical_connection.id
	 AND target_permission.company_brain_access_version = $5
	WHERE logical_connection.workspace_id = $1
	  AND logical_connection.id = $2
	  AND target_permission.company_brain_lifecycle_state IN ('draft', 'active')
	FOR SHARE OF logical_connection, target_agent, target_permission
`
