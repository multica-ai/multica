package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/tagaccess"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const cleanupFailureCode = "execution_cleanup_failed"

type cleanupLedgerRow struct {
	correlationID              string
	vibesWorkspaceID           pgtype.Text
	vibesUserID                pgtype.Text
	authorityVersion           pgtype.Int8
	identityRestrictionVersion pgtype.Int8
	accountEpoch               pgtype.Int8
	payloadDigest              []byte
	targetDigest               []byte
	state                      string
	receiptID                  string
	appliedAt                  pgtype.Timestamptz
}

type cleanupEffect struct {
	WorkspaceID      string                  `json:"workspaceId"`
	VIBESWorkspaceID string                  `json:"vibesWorkspaceId,omitempty"`
	UserID           string                  `json:"userId"`
	VIBESUserID      string                  `json:"vibesUserId"`
	Runtimes         []string                `json:"runtimeIds"`
	UnboundAgents    []string                `json:"unboundAgentIds"`
	PausedAutopilots []string                `json:"pausedAutopilotIds"`
	CancelledTasks   []cleanupTaskDependency `json:"cancelledTasks"`
	OfflineRuntimes  []string                `json:"offlineRuntimeIds"`
	DaemonTokenCount int                     `json:"daemonTokenCount"`
	revocation       revocationResult        `json:"-"`
}

type cleanupTaskDependency struct {
	TaskID         string `json:"taskId"`
	DependencyKind string `json:"dependencyKind"`
	DependencyID   string `json:"dependencyId"`
}

// Cleanup implements tagaccess.CleanupPort. VIBES remains the authority: this
// method never writes Multica membership or projection state. It records and
// retries only the execution-side consequence of an already-durable
// restriction.
func (h *Handler) Cleanup(ctx context.Context, command tagaccess.CleanupCommand) (tagaccess.CleanupReceipt, error) {
	payloadDigest, targetDigest, targetsJSON, err := validateCleanupCommand(command)
	if err != nil {
		return tagaccess.CleanupReceipt{}, err
	}
	if h == nil || h.DB == nil || h.TxStarter == nil || h.Queries == nil {
		return tagaccess.CleanupReceipt{}, errors.New("member execution cleanup is not configured")
	}

	if _, err := h.DB.Exec(ctx, `
		INSERT INTO tag_member_execution_cleanup (
			source, delivery_id, correlation_id, vibes_workspace_id, vibes_user_id,
			authority_version, identity_restriction_version, account_epoch,
			payload_digest, target_digest, targets
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (source, delivery_id) DO NOTHING
	`, command.Source, command.DeliveryID, command.CorrelationID, nullText(command.WorkspaceID),
		nullText(command.VIBESUserID), nullUint64(command.AuthorityVersion),
		nullUint64(command.IdentityRestrictionVersion), nullUint64(command.AccountEpoch),
		payloadDigest, targetDigest, targetsJSON); err != nil {
		return tagaccess.CleanupReceipt{}, fmt.Errorf("record member execution cleanup request: %w", err)
	}

	row, err := h.loadCleanupLedger(ctx, h.DB, command.Source, command.DeliveryID, false)
	if err != nil {
		return tagaccess.CleanupReceipt{}, err
	}
	if err := matchCleanupLedger(command, payloadDigest, targetDigest, row); err != nil {
		return tagaccess.CleanupReceipt{}, err
	}
	if row.state == "applied" {
		return cleanupReceipt(command, row), nil
	}
	if row.state == "failed" || row.state == "retry" {
		if _, err := h.DB.Exec(ctx, `
			UPDATE tag_member_execution_cleanup
			SET state = 'retry', attempt_count = attempt_count + 1, last_retry_at = now(),
			    failure_code = '', failed_at = NULL, updated_at = now()
			WHERE source = $1 AND delivery_id = $2 AND state <> 'applied'
		`, command.Source, command.DeliveryID); err != nil {
			return tagaccess.CleanupReceipt{}, fmt.Errorf("record member execution cleanup retry: %w", err)
		}
	}

	receipt, effects, err := h.applyCleanupCommand(ctx, command, payloadDigest, targetDigest)
	if err != nil {
		_, _ = h.DB.Exec(ctx, `
			UPDATE tag_member_execution_cleanup
			SET state = 'failed', failure_code = $3, failed_at = now(), updated_at = now()
			WHERE source = $1 AND delivery_id = $2 AND state <> 'applied'
		`, command.Source, command.DeliveryID, cleanupFailureCode)
		return tagaccess.CleanupReceipt{}, err
	}
	for _, effect := range effects {
		h.publishRevocation(ctx, effect.revocation, effect.WorkspaceID, "system", "vibes-authority")
		logRevocation(effect.revocation, effect.WorkspaceID, effect.UserID,
			"source", command.Source, "delivery_id", command.DeliveryID)
	}
	return receipt, nil
}

func (h *Handler) applyCleanupCommand(ctx context.Context, command tagaccess.CleanupCommand, payloadDigest, targetDigest []byte) (tagaccess.CleanupReceipt, []cleanupEffect, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return tagaccess.CleanupReceipt{}, nil, fmt.Errorf("begin member execution cleanup: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row, err := h.loadCleanupLedger(ctx, tx, command.Source, command.DeliveryID, true)
	if err != nil {
		return tagaccess.CleanupReceipt{}, nil, err
	}
	if err := matchCleanupLedger(command, payloadDigest, targetDigest, row); err != nil {
		return tagaccess.CleanupReceipt{}, nil, err
	}
	if row.state == "applied" {
		return cleanupReceipt(command, row), nil, nil
	}
	qtx := h.Queries.WithTx(tx)
	var effects []cleanupEffect
	var outcome string
	switch command.Source {
	case tagaccess.CleanupWorkspaceProjection:
		effects, outcome, err = h.applyWorkspaceCleanup(ctx, tx, qtx, command)
	case tagaccess.CleanupIdentityRestriction:
		effects, outcome, err = h.applyIdentityCleanup(ctx, tx, qtx, command)
	default:
		err = errors.New("unsupported member execution cleanup source")
	}
	if err != nil {
		return tagaccess.CleanupReceipt{}, nil, err
	}

	persistedEffects := append([]cleanupEffect(nil), effects...)
	for index := range persistedEffects {
		persistedEffects[index].revocation = revocationResult{}
	}
	effectsJSON, err := json.Marshal(persistedEffects)
	if err != nil {
		return tagaccess.CleanupReceipt{}, nil, fmt.Errorf("encode member execution cleanup effects: %w", err)
	}
	receiptID := cleanupReceiptID(command)
	var appliedAt time.Time
	if err := tx.QueryRow(ctx, `
		UPDATE tag_member_execution_cleanup
		SET state = 'applied', outcome = $3, effects = $4, receipt_id = $5,
		    applied_at = now(), failure_code = '', failed_at = NULL, updated_at = now()
		WHERE source = $1 AND delivery_id = $2
		RETURNING applied_at
	`, command.Source, command.DeliveryID, outcome, effectsJSON, receiptID).Scan(&appliedAt); err != nil {
		return tagaccess.CleanupReceipt{}, nil, fmt.Errorf("record member execution cleanup receipt: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return tagaccess.CleanupReceipt{}, nil, fmt.Errorf("commit member execution cleanup: %w", err)
	}
	return tagaccess.CleanupReceipt{
		ReceiptID: receiptID, Source: command.Source, DeliveryID: command.DeliveryID,
		CorrelationID: command.CorrelationID, WorkspaceID: command.WorkspaceID,
		VIBESUserID: command.VIBESUserID, AuthorityVersion: command.AuthorityVersion,
		IdentityRestrictionVersion: command.IdentityRestrictionVersion,
		AccountEpoch:               command.AccountEpoch, PayloadDigest: command.PayloadDigest,
		TargetDigest: command.TargetDigest, CompletedAt: appliedAt,
	}, effects, nil
}

func (h *Handler) applyWorkspaceCleanup(ctx context.Context, tx pgx.Tx, qtx *db.Queries, command tagaccess.CleanupCommand) ([]cleanupEffect, string, error) {
	// Lock order is cleanup-ledger row, then the same authority lock used by
	// tagaccess projection writes. Authority writers never lock the cleanup
	// ledger, so this order cannot cycle. Holding the authority lock through
	// cleanup makes the generation/status revalidation atomic with revocation:
	// a concurrent re-invite either wins first and supersedes this command, or
	// waits until this old generation has been fully revoked.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "tag-access-workspace:"+command.WorkspaceID); err != nil {
		return nil, "", fmt.Errorf("lock cleanup workspace authority: %w", err)
	}
	var workspaceID pgtype.UUID
	err := tx.QueryRow(ctx, `
		SELECT multica_workspace_id FROM vibes_workspace_mirror WHERE vibes_workspace_id = $1
	`, command.WorkspaceID).Scan(&workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "no_local_workspace", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("resolve cleanup workspace mirror: %w", err)
	}

	targets := make(map[string]tagaccess.CleanupTarget, len(command.Targets))
	for _, target := range command.Targets {
		var generation int64
		var status string
		err := tx.QueryRow(ctx, `
			SELECT membership_generation, status
			FROM tag_access_projection
			WHERE vibes_workspace_id = $1 AND vibes_user_id = $2
		`, command.WorkspaceID, target.VIBESUserID).Scan(&generation, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, "", fmt.Errorf("load cleanup projection: %w", err)
		}
		if generation == int64(target.MembershipGeneration) && status == string(target.Status) {
			targets[target.VIBESUserID] = target
		}
	}

	rows, err := tx.Query(ctx, `
		SELECT vibes_user_id, membership_generation, status
		FROM tag_access_projection
		WHERE vibes_workspace_id = $1 AND authority_version = $2
		  AND status IN ('removed', 'disabled')
	`, command.WorkspaceID, int64(command.AuthorityVersion))
	if err != nil {
		return nil, "", fmt.Errorf("load snapshot cleanup projections: %w", err)
	}
	for rows.Next() {
		var target tagaccess.CleanupTarget
		var generation int64
		var status string
		if err := rows.Scan(&target.VIBESUserID, &generation, &status); err != nil {
			rows.Close()
			return nil, "", err
		}
		target.MembershipGeneration = uint64(generation)
		target.Status = tagaccess.Status(status)
		targets[target.VIBESUserID] = target
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, "", err
	}
	rows.Close()

	ordered := make([]string, 0, len(targets))
	for vibesUserID := range targets {
		ordered = append(ordered, vibesUserID)
	}
	sort.Strings(ordered)
	effects := make([]cleanupEffect, 0, len(ordered))
	for _, vibesUserID := range ordered {
		var userID pgtype.UUID
		err := tx.QueryRow(ctx, `
			SELECT multica_user_id FROM vibes_user_mirror WHERE vibes_user_id = $1
		`, vibesUserID).Scan(&userID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, "", fmt.Errorf("resolve cleanup user mirror: %w", err)
		}
		effect, err := h.cleanupWorkspaceUser(ctx, qtx, workspaceID, userID, command.WorkspaceID, vibesUserID)
		if err != nil {
			return nil, "", err
		}
		effects = append(effects, effect)
	}
	if len(targets) == 0 {
		if len(command.Targets) == 0 {
			return nil, "no_restricted_target", nil
		}
		return nil, "superseded_generation", nil
	}
	if len(effects) == 0 {
		return nil, "no_local_user", nil
	}
	return effects, "applied", nil
}

func (h *Handler) applyIdentityCleanup(ctx context.Context, tx pgx.Tx, qtx *db.Queries, command tagaccess.CleanupCommand) ([]cleanupEffect, string, error) {
	// Match the identity authority writer's advisory lock before revalidating
	// its version/epoch. See the workspace lock-order note above.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "tag-access-identity:"+command.VIBESUserID); err != nil {
		return nil, "", fmt.Errorf("lock cleanup identity authority: %w", err)
	}
	var version, accountEpoch, revokedThrough int64
	err := tx.QueryRow(ctx, `
		SELECT identity_restriction_version, account_epoch, revoked_through_account_epoch
		FROM tag_access_identity_restriction_state
		WHERE vibes_user_id = $1
	`, command.VIBESUserID).Scan(&version, &accountEpoch, &revokedThrough)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "superseded_identity_restriction", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("load cleanup identity restriction: %w", err)
	}
	if version != int64(command.IdentityRestrictionVersion) || accountEpoch != int64(command.AccountEpoch) ||
		revokedThrough < int64(command.AccountEpoch) {
		return nil, "superseded_identity_restriction", nil
	}
	var userID pgtype.UUID
	if err := tx.QueryRow(ctx, `
		SELECT multica_user_id FROM vibes_user_mirror WHERE vibes_user_id = $1
	`, command.VIBESUserID).Scan(&userID); errors.Is(err, pgx.ErrNoRows) {
		return nil, "no_local_user", nil
	} else if err != nil {
		return nil, "", fmt.Errorf("resolve cleanup identity mirror: %w", err)
	}

	rows, err := tx.Query(ctx, `
		SELECT DISTINCT workspace_id FROM (
			SELECT workspace_id FROM member WHERE user_id = $1
			UNION SELECT workspace_id FROM agent_runtime WHERE owner_id = $1
			UNION SELECT workspace_id FROM channel_user_binding WHERE multica_user_id = $1
			UNION SELECT a.workspace_id
			      FROM agent_invocation_target t JOIN agent a ON a.id = t.agent_id
			      WHERE t.target_type = 'member' AND t.target_id = $1
		) affected
		ORDER BY workspace_id
	`, userID)
	if err != nil {
		return nil, "", fmt.Errorf("list account cleanup workspaces: %w", err)
	}
	var workspaceIDs []pgtype.UUID
	for rows.Next() {
		var workspaceID pgtype.UUID
		if err := rows.Scan(&workspaceID); err != nil {
			rows.Close()
			return nil, "", err
		}
		workspaceIDs = append(workspaceIDs, workspaceID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, "", err
	}
	rows.Close()

	effects := make([]cleanupEffect, 0, len(workspaceIDs))
	for _, workspaceID := range workspaceIDs {
		var vibesWorkspaceID pgtype.Text
		_ = tx.QueryRow(ctx, `
			SELECT vibes_workspace_id FROM vibes_workspace_mirror WHERE multica_workspace_id = $1
		`, workspaceID).Scan(&vibesWorkspaceID)
		effect, err := h.cleanupWorkspaceUser(ctx, qtx, workspaceID, userID, vibesWorkspaceID.String, command.VIBESUserID)
		if err != nil {
			return nil, "", err
		}
		effects = append(effects, effect)
	}
	if len(effects) == 0 {
		return nil, "no_local_execution_resources", nil
	}
	return effects, "applied", nil
}

func (h *Handler) cleanupWorkspaceUser(ctx context.Context, qtx *db.Queries, workspaceID, userID pgtype.UUID, vibesWorkspaceID, vibesUserID string) (cleanupEffect, error) {
	result, err := h.applyMemberExecutionCleanup(ctx, qtx, workspaceID, userID)
	if err != nil {
		return cleanupEffect{}, fmt.Errorf("revoke member execution dependencies: %w", err)
	}
	effect := cleanupEffect{
		WorkspaceID: uuidToString(workspaceID), VIBESWorkspaceID: vibesWorkspaceID,
		UserID: uuidToString(userID), VIBESUserID: vibesUserID, DaemonTokenCount: len(result.RevokedTokenHashes),
		revocation: result,
	}
	revokedRuntimeIDs := make(map[string]struct{}, len(result.Runtimes))
	for _, runtime := range result.Runtimes {
		id := uuidToString(runtime.ID)
		effect.Runtimes = append(effect.Runtimes, id)
		revokedRuntimeIDs[id] = struct{}{}
	}
	for _, runtime := range result.OfflineRuntimeIDs {
		effect.OfflineRuntimes = append(effect.OfflineRuntimes, uuidToString(runtime.ID))
	}
	for _, agent := range result.UnboundAgents {
		effect.UnboundAgents = append(effect.UnboundAgents, uuidToString(agent.ID))
	}
	for _, autopilot := range result.PausedAutopilots {
		effect.PausedAutopilots = append(effect.PausedAutopilots, uuidToString(autopilot.ID))
	}
	for _, task := range result.CancelledTasks {
		dependency := cleanupTaskDependency{TaskID: uuidToString(task.ID)}
		if runtimeID := uuidToString(task.RuntimeID); runtimeID != "" {
			if _, revoked := revokedRuntimeIDs[runtimeID]; revoked {
				dependency.DependencyKind, dependency.DependencyID = "runtime", runtimeID
			}
		}
		if dependency.DependencyID == "" {
			dependency.DependencyKind, dependency.DependencyID = "agent", uuidToString(task.AgentID)
		}
		effect.CancelledTasks = append(effect.CancelledTasks, dependency)
	}
	return effect, nil
}

func (h *Handler) loadCleanupLedger(ctx context.Context, executor interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, source tagaccess.CleanupSource, deliveryID string, forUpdate bool) (cleanupLedgerRow, error) {
	query := `
		SELECT correlation_id, vibes_workspace_id, vibes_user_id, authority_version,
		       identity_restriction_version, account_epoch, payload_digest, target_digest,
		       state, receipt_id, applied_at
		FROM tag_member_execution_cleanup
		WHERE source = $1 AND delivery_id = $2`
	if forUpdate {
		query += " FOR UPDATE"
	}
	var row cleanupLedgerRow
	if err := executor.QueryRow(ctx, query, source, deliveryID).Scan(
		&row.correlationID, &row.vibesWorkspaceID, &row.vibesUserID, &row.authorityVersion,
		&row.identityRestrictionVersion, &row.accountEpoch, &row.payloadDigest, &row.targetDigest,
		&row.state, &row.receiptID, &row.appliedAt,
	); err != nil {
		return cleanupLedgerRow{}, fmt.Errorf("load member execution cleanup: %w", err)
	}
	return row, nil
}

func validateCleanupCommand(command tagaccess.CleanupCommand) ([]byte, []byte, []byte, error) {
	stable := func(value string) bool {
		return value != "" && len(value) <= 255 && !strings.ContainsRune(value, '\x00')
	}
	if !stable(command.DeliveryID) || !stable(command.CorrelationID) {
		return nil, nil, nil, errors.New("invalid member execution cleanup correlation")
	}
	payloadDigest, err := hex.DecodeString(command.PayloadDigest)
	if err != nil || len(payloadDigest) != sha256.Size {
		return nil, nil, nil, errors.New("invalid member execution cleanup payload digest")
	}
	targetDigest, err := hex.DecodeString(command.TargetDigest)
	if err != nil || len(targetDigest) != sha256.Size {
		return nil, nil, nil, errors.New("invalid member execution cleanup target digest")
	}
	targets := append([]tagaccess.CleanupTarget(nil), command.Targets...)
	sort.Slice(targets, func(left, right int) bool { return targets[left].VIBESUserID < targets[right].VIBESUserID })
	for _, target := range targets {
		if !stable(target.VIBESUserID) || target.MembershipGeneration == 0 ||
			(target.Status != tagaccess.StatusRemoved && target.Status != tagaccess.StatusDisabled) {
			return nil, nil, nil, errors.New("invalid member execution cleanup target")
		}
	}
	targetsJSON, err := json.Marshal(targets)
	if err != nil {
		return nil, nil, nil, err
	}
	calculatedTargetDigest := sha256.Sum256(targetsJSON)
	if !bytes.Equal(targetDigest, calculatedTargetDigest[:]) {
		return nil, nil, nil, errors.New("member execution cleanup target digest mismatch")
	}
	switch command.Source {
	case tagaccess.CleanupWorkspaceProjection:
		if !stable(command.WorkspaceID) || command.AuthorityVersion == 0 || command.VIBESUserID != "" ||
			command.IdentityRestrictionVersion != 0 || command.AccountEpoch != 0 {
			return nil, nil, nil, errors.New("invalid workspace member execution cleanup command")
		}
	case tagaccess.CleanupIdentityRestriction:
		if !stable(command.VIBESUserID) || command.IdentityRestrictionVersion == 0 || command.AccountEpoch == 0 ||
			command.WorkspaceID != "" || command.AuthorityVersion != 0 || len(targets) != 0 {
			return nil, nil, nil, errors.New("invalid identity member execution cleanup command")
		}
	default:
		return nil, nil, nil, errors.New("invalid member execution cleanup source")
	}
	return payloadDigest, targetDigest, targetsJSON, nil
}

func matchCleanupLedger(command tagaccess.CleanupCommand, payloadDigest, targetDigest []byte, row cleanupLedgerRow) error {
	if row.correlationID != command.CorrelationID || row.vibesWorkspaceID.String != command.WorkspaceID ||
		row.vibesUserID.String != command.VIBESUserID || row.authorityVersion.Int64 != int64(command.AuthorityVersion) ||
		row.identityRestrictionVersion.Int64 != int64(command.IdentityRestrictionVersion) ||
		row.accountEpoch.Int64 != int64(command.AccountEpoch) || !bytes.Equal(row.payloadDigest, payloadDigest) ||
		!bytes.Equal(row.targetDigest, targetDigest) {
		return errors.New("member execution cleanup delivery correlation conflict")
	}
	return nil
}

func cleanupReceipt(command tagaccess.CleanupCommand, row cleanupLedgerRow) tagaccess.CleanupReceipt {
	return tagaccess.CleanupReceipt{
		ReceiptID: row.receiptID, Source: command.Source, DeliveryID: command.DeliveryID,
		CorrelationID: command.CorrelationID, WorkspaceID: command.WorkspaceID,
		VIBESUserID: command.VIBESUserID, AuthorityVersion: command.AuthorityVersion,
		IdentityRestrictionVersion: command.IdentityRestrictionVersion, AccountEpoch: command.AccountEpoch,
		PayloadDigest: command.PayloadDigest, TargetDigest: command.TargetDigest, CompletedAt: row.appliedAt.Time,
	}
}

func cleanupReceiptID(command tagaccess.CleanupCommand) string {
	digest := sha256.Sum256([]byte(string(command.Source) + "\x00" + command.DeliveryID + "\x00" + command.PayloadDigest + "\x00" + command.TargetDigest))
	return "cleanup-" + hex.EncodeToString(digest[:])
}

func nullText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullUint64(value uint64) any {
	if value == 0 {
		return nil
	}
	return int64(value)
}
