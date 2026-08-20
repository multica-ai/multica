package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/tagaccess"
)

func TestMemberExecutionCleanupCancelsOnlyRevokedDependenciesAndPreservesHistory(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var removedUserID, survivorUserID, workspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ('cleanup removed', $1) RETURNING id
	`, "cleanup-removed-"+suffix+"@example.test").Scan(&removedUserID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ('cleanup survivor', $1) RETURNING id
	`, "cleanup-survivor-"+suffix+"@example.test").Scan(&survivorUserID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, issue_prefix) VALUES ('cleanup workspace', $1, 'CLN') RETURNING id
	`, "cleanup-"+suffix).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	vibesUserID := "vibes-cleanup-user-" + suffix
	vibesWorkspaceID := "vibes-cleanup-workspace-" + suffix
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM tag_member_execution_cleanup WHERE delivery_id = $1`, "cleanup-delivery-"+suffix)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM tag_access_projection WHERE vibes_workspace_id = $1`, vibesWorkspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM vibes_workspace_mirror WHERE vibes_workspace_id = $1`, vibesWorkspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM vibes_user_mirror WHERE vibes_user_id = $1`, vibesUserID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = ANY($1::uuid[])`, []string{removedUserID, survivorUserID})
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member'), ($1, $3, 'owner');
		INSERT INTO vibes_user_mirror (vibes_user_id, multica_user_id, profile_email) VALUES ($4, $2, 'removed@example.test');
		INSERT INTO vibes_workspace_mirror (vibes_workspace_id, multica_workspace_id) VALUES ($5, $1);
		INSERT INTO tag_access_projection (
			vibes_user_id, vibes_workspace_id, role, status, account_epoch,
			membership_generation, authority_version, last_event_id, last_payload_digest
		) VALUES ($4, $5, 'member', 'removed', 7, 4, 1, $6, decode($7, 'hex'))
	`, workspaceID, removedUserID, survivorUserID, vibesUserID, vibesWorkspaceID, "cleanup-event-"+suffix, fmt.Sprintf("%064d", 1)); err != nil {
		t.Fatal(err)
	}

	var revokedRuntimeID, safeRuntimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at
		) VALUES ($1, $2, 'revoked runtime', 'local', 'codex', 'online', 'device', '{}'::jsonb, $3, now()) RETURNING id
	`, workspaceID, "cleanup-daemon-revoked-"+suffix, removedUserID).Scan(&revokedRuntimeID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at
		) VALUES ($1, $2, 'safe runtime', 'local', 'codex', 'online', 'device', '{}'::jsonb, $3, now()) RETURNING id
	`, workspaceID, "cleanup-daemon-safe-"+suffix, survivorUserID).Scan(&safeRuntimeID); err != nil {
		t.Fatal(err)
	}
	var dependentAgentID, safeAgentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility,
			permission_mode, max_concurrent_tasks, owner_id
		) VALUES ($1, 'shared dependent agent', 'local', '{}'::jsonb, $2, 'workspace', 'public_to', 1, $3) RETURNING id
	`, workspaceID, revokedRuntimeID, survivorUserID).Scan(&dependentAgentID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility,
			permission_mode, max_concurrent_tasks, owner_id
		) VALUES ($1, 'safe agent', 'local', '{}'::jsonb, $2, 'workspace', 'public_to', 1, $3) RETURNING id
	`, workspaceID, safeRuntimeID, survivorUserID).Scan(&safeAgentID); err != nil {
		t.Fatal(err)
	}
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id)
		VALUES ($1, 'cleanup task history', 'member', $2) RETURNING id
	`, workspaceID, survivorUserID).Scan(&issueID); err != nil {
		t.Fatal(err)
	}
	var dependentTaskID, safeTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'running', 0) RETURNING id
	`, dependentAgentID, revokedRuntimeID, issueID).Scan(&dependentTaskID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0) RETURNING id
	`, safeAgentID, safeRuntimeID, issueID).Scan(&safeTaskID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO daemon_token (token_hash, workspace_id, daemon_id, expires_at)
		VALUES ($1, $2, $3, now() + interval '1 hour');
		INSERT INTO task_token (token_hash, task_id, agent_id, workspace_id, user_id, expires_at)
		VALUES ($4, $5, $6, $2, $7, now() + interval '1 hour'),
		       ($8, $9, $10, $2, $11, now() + interval '1 hour')
	`, "cleanup-daemon-token-"+suffix, workspaceID, "cleanup-daemon-revoked-"+suffix,
		"cleanup-dependent-task-token-"+suffix, dependentTaskID, dependentAgentID, removedUserID,
		"cleanup-safe-task-token-"+suffix, safeTaskID, safeAgentID, survivorUserID); err != nil {
		t.Fatal(err)
	}

	targets := []tagaccess.CleanupTarget{{
		VIBESUserID: vibesUserID, MembershipGeneration: 4, Status: tagaccess.StatusRemoved,
	}}
	targetPayload, err := json.Marshal(targets)
	if err != nil {
		t.Fatal(err)
	}
	targetDigest := sha256.Sum256(targetPayload)
	command := tagaccess.CleanupCommand{
		Source: tagaccess.CleanupWorkspaceProjection, DeliveryID: "cleanup-delivery-" + suffix,
		CorrelationID: "cleanup-correlation-" + suffix, WorkspaceID: vibesWorkspaceID,
		AuthorityVersion: 1, PayloadDigest: fmt.Sprintf("%064d", 1), TargetDigest: hex.EncodeToString(targetDigest[:]),
		Targets: targets,
	}
	failingHandler := &Handler{
		Queries: testHandler.Queries, DB: testPool,
		TxStarter: &failNthBegin{delegate: testPool, failAt: 1},
	}
	if failedReceipt, err := failingHandler.Cleanup(ctx, command); err == nil || failedReceipt.ReceiptID != "" {
		t.Fatalf("injected first attempt = %#v, %v; want durable failure without receipt", failedReceipt, err)
	}
	var failedState, failureCode string
	if err := testPool.QueryRow(ctx, `
		SELECT state, failure_code FROM tag_member_execution_cleanup WHERE source = $1 AND delivery_id = $2
	`, command.Source, command.DeliveryID).Scan(&failedState, &failureCode); err != nil {
		t.Fatal(err)
	}
	if failedState != "failed" || failureCode != cleanupFailureCode {
		t.Fatalf("first attempt state=%q failure=%q", failedState, failureCode)
	}
	// Two workers may retry the same durable delivery after a process/store
	// failure. The cleanup ledger row serializes them, so both must observe the
	// same exact receipt while the deep cleanup runs only once.
	type cleanupResult struct {
		receipt tagaccess.CleanupReceipt
		err     error
	}
	results := make(chan cleanupResult, 2)
	for range 2 {
		go func() {
			receipt, err := testHandler.Cleanup(ctx, command)
			results <- cleanupResult{receipt: receipt, err: err}
		}()
	}
	first, second := <-results, <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("concurrent retry errors: first=%v second=%v", first.err, second.err)
	}
	receipt := first.receipt
	if second.receipt.ReceiptID != receipt.ReceiptID || !second.receipt.CompletedAt.Equal(receipt.CompletedAt) {
		t.Fatalf("concurrent retry receipts differ: first=%#v second=%#v", receipt, second.receipt)
	}
	if receipt.ReceiptID == "" || receipt.DeliveryID != command.DeliveryID || receipt.TargetDigest != command.TargetDigest || receipt.CompletedAt.IsZero() {
		t.Fatalf("cleanup receipt = %#v", receipt)
	}

	var runtimeStatus, dependentStatus, safeStatus, dependentFailure string
	var dependentRuntimeBound, dependentArchived bool
	var dependentHistoryCount, memberCount, revokedDaemonTokenCount, dependentTaskTokenCount, safeTaskTokenCount int
	var runtimeFenced bool
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_runtime WHERE id = $1`, revokedRuntimeID).Scan(&runtimeStatus); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `SELECT runtime_id IS NOT NULL, archived_at IS NOT NULL FROM agent WHERE id = $1`, dependentAgentID).Scan(&dependentRuntimeBound, &dependentArchived); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `SELECT status, failure_reason FROM agent_task_queue WHERE id = $1`, dependentTaskID).Scan(&dependentStatus, &dependentFailure); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, safeTaskID).Scan(&safeStatus); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE id = $1`, dependentTaskID).Scan(&dependentHistoryCount); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM member WHERE workspace_id = $1 AND user_id = $2`, workspaceID, removedUserID).Scan(&memberCount); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM daemon_token WHERE token_hash = $1`, "cleanup-daemon-token-"+suffix).Scan(&revokedDaemonTokenCount); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM task_token WHERE token_hash = $1`, "cleanup-dependent-task-token-"+suffix).Scan(&dependentTaskTokenCount); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM task_token WHERE token_hash = $1`, "cleanup-safe-task-token-"+suffix).Scan(&safeTaskTokenCount); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `SELECT metadata @> '{"member_execution_revoked": true}'::jsonb FROM agent_runtime WHERE id = $1`, revokedRuntimeID).Scan(&runtimeFenced); err != nil {
		t.Fatal(err)
	}
	if runtimeStatus != "offline" || dependentRuntimeBound || dependentArchived || dependentStatus != "cancelled" ||
		dependentFailure != "member_execution_dependency_revoked" || safeStatus != "queued" || dependentHistoryCount != 1 || memberCount != 1 ||
		revokedDaemonTokenCount != 0 || dependentTaskTokenCount != 0 || safeTaskTokenCount != 1 || !runtimeFenced {
		t.Fatalf("cleanup state runtime=%q bound=%v archived=%v dependent=%q/%q safe=%q history=%d member=%d daemon_token=%d dependent_task_token=%d safe_task_token=%d runtime_fenced=%v",
			runtimeStatus, dependentRuntimeBound, dependentArchived, dependentStatus, dependentFailure, safeStatus, dependentHistoryCount, memberCount,
			revokedDaemonTokenCount, dependentTaskTokenCount, safeTaskTokenCount, runtimeFenced)
	}
	var ledgerState, ledgerOutcome, ledgerReceipt string
	var attemptCount int
	var effectsJSON []byte
	if err := testPool.QueryRow(ctx, `
		SELECT state, outcome, attempt_count, receipt_id, effects
		FROM tag_member_execution_cleanup WHERE source = $1 AND delivery_id = $2
	`, command.Source, command.DeliveryID).Scan(&ledgerState, &ledgerOutcome, &attemptCount, &ledgerReceipt, &effectsJSON); err != nil {
		t.Fatal(err)
	}
	if ledgerState != "applied" || ledgerOutcome != "applied" || attemptCount < 2 || ledgerReceipt != receipt.ReceiptID ||
		!json.Valid(effectsJSON) || !bytes.Contains(effectsJSON, []byte(dependentTaskID)) || !bytes.Contains(effectsJSON, []byte(revokedRuntimeID)) {
		t.Fatalf("cleanup ledger state=%q outcome=%q attempts=%d receipt=%q effects=%s",
			ledgerState, ledgerOutcome, attemptCount, ledgerReceipt, effectsJSON)
	}

	duplicate, err := testHandler.Cleanup(ctx, command)
	if err != nil || duplicate.ReceiptID != receipt.ReceiptID || !duplicate.CompletedAt.Equal(receipt.CompletedAt) {
		t.Fatalf("duplicate cleanup = %#v, %v; first = %#v", duplicate, err, receipt)
	}
}

func TestMemberExecutionCleanupOldRemovalCannotRevokeReinvitedGeneration(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var userID, workspaceID, runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ('cleanup reinvited', $1) RETURNING id
	`, "cleanup-reinvited-"+suffix+"@example.test").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, issue_prefix) VALUES ('cleanup reinvited', $1, 'CLR') RETURNING id
	`, "cleanup-reinvited-"+suffix).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	vibesUserID := "vibes-cleanup-reinvited-user-" + suffix
	vibesWorkspaceID := "vibes-cleanup-reinvited-workspace-" + suffix
	deliveryID := "cleanup-reinvited-delivery-" + suffix
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM tag_member_execution_cleanup WHERE delivery_id = $1`, deliveryID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM tag_access_projection WHERE vibes_workspace_id = $1`, vibesWorkspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM vibes_workspace_mirror WHERE vibes_workspace_id = $1`, vibesWorkspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM vibes_user_mirror WHERE vibes_user_id = $1`, vibesUserID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'admin');
		INSERT INTO vibes_user_mirror (vibes_user_id, multica_user_id, profile_email) VALUES ($3, $2, 'reinvited@example.test');
		INSERT INTO vibes_workspace_mirror (vibes_workspace_id, multica_workspace_id) VALUES ($4, $1);
		INSERT INTO tag_access_projection (
			vibes_user_id, vibes_workspace_id, role, status, account_epoch,
			membership_generation, authority_version, last_event_id, last_payload_digest
		) VALUES ($3, $4, 'admin', 'removed', 8, 4, 1, $5, decode($6, 'hex'))
	`, workspaceID, userID, vibesUserID, vibesWorkspaceID, "cleanup-reinvited-event-"+suffix, fmt.Sprintf("%064d", 3)); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at
		) VALUES ($1, $2, 'reinvited runtime', 'local', 'codex', 'online', 'device', '{}'::jsonb, $3, now()) RETURNING id
	`, workspaceID, "cleanup-reinvited-daemon-"+suffix, userID).Scan(&runtimeID); err != nil {
		t.Fatal(err)
	}
	targets := []tagaccess.CleanupTarget{{
		VIBESUserID: vibesUserID, MembershipGeneration: 4, Status: tagaccess.StatusRemoved,
	}}
	targetPayload, err := json.Marshal(targets)
	if err != nil {
		t.Fatal(err)
	}
	targetDigest := sha256.Sum256(targetPayload)
	command := tagaccess.CleanupCommand{
		Source: tagaccess.CleanupWorkspaceProjection, DeliveryID: deliveryID,
		CorrelationID: "cleanup-reinvited-correlation-" + suffix, WorkspaceID: vibesWorkspaceID,
		AuthorityVersion: 1, PayloadDigest: fmt.Sprintf("%064d", 4),
		TargetDigest: hex.EncodeToString(targetDigest[:]), Targets: targets,
	}
	// Hold the same authority lock while committing a higher-generation
	// re-invite. Cleanup must wait, then re-read the active generation rather
	// than revoke resources based on the earlier removed row.
	authorityTx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer authorityTx.Rollback(ctx)
	if _, err := authorityTx.Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		"tag-access-workspace:"+vibesWorkspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := authorityTx.Exec(ctx, `
		UPDATE tag_access_projection
		SET status = 'active', membership_generation = 5, authority_version = 2,
		    last_event_id = $3, last_payload_digest = decode($4, 'hex'), updated_at = now()
		WHERE vibes_workspace_id = $1 AND vibes_user_id = $2
	`, vibesWorkspaceID, vibesUserID, "cleanup-reinvite-committed-"+suffix, fmt.Sprintf("%064d", 6)); err != nil {
		t.Fatal(err)
	}
	type asyncCleanupResult struct {
		receipt tagaccess.CleanupReceipt
		err     error
	}
	resultCh := make(chan asyncCleanupResult, 1)
	go func() {
		receipt, err := testHandler.Cleanup(ctx, command)
		resultCh <- asyncCleanupResult{receipt: receipt, err: err}
	}()
	select {
	case result := <-resultCh:
		t.Fatalf("cleanup bypassed authority lock: receipt=%#v err=%v", result.receipt, result.err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := authorityTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	result := <-resultCh
	if result.err != nil || result.receipt.ReceiptID == "" {
		t.Fatalf("superseded cleanup receipt=%#v err=%v", result.receipt, result.err)
	}
	var runtimeStatus, outcome string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&runtimeStatus); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT outcome FROM tag_member_execution_cleanup WHERE source = $1 AND delivery_id = $2
	`, command.Source, command.DeliveryID).Scan(&outcome); err != nil {
		t.Fatal(err)
	}
	if runtimeStatus != "online" || outcome != "superseded_generation" {
		t.Fatalf("old removal changed reinvited generation: runtime=%q outcome=%q", runtimeStatus, outcome)
	}
}

func TestMemberExecutionCleanupAccountBanRevokesAcrossWorkspaceWithoutDeletingMembership(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var userID, workspaceID, runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ('cleanup banned', $1) RETURNING id
	`, "cleanup-banned-"+suffix+"@example.test").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, issue_prefix) VALUES ('cleanup banned', $1, 'CLB') RETURNING id
	`, "cleanup-banned-"+suffix).Scan(&workspaceID); err != nil {
		t.Fatal(err)
	}
	vibesUserID := "vibes-cleanup-banned-user-" + suffix
	deliveryID := "cleanup-banned-delivery-" + suffix
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM tag_member_execution_cleanup WHERE delivery_id = $1`, deliveryID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM tag_access_identity_restriction_state WHERE vibes_user_id = $1`, vibesUserID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM vibes_user_mirror WHERE vibes_user_id = $1`, vibesUserID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner');
		INSERT INTO vibes_user_mirror (vibes_user_id, multica_user_id, profile_email) VALUES ($3, $2, 'banned@example.test');
		INSERT INTO tag_access_identity_restriction_state (
			vibes_user_id, identity_restriction_version, observed_identity_restriction_version,
			account_epoch, revoked_through_account_epoch, integrity_state
		) VALUES ($3, 1, 1, 7, 7, 'healthy')
	`, workspaceID, userID, vibesUserID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at
		) VALUES ($1, $2, 'banned offline runtime', 'local', 'codex', 'offline', 'device', '{}'::jsonb, $3, now()) RETURNING id
	`, workspaceID, "cleanup-banned-daemon-"+suffix, userID).Scan(&runtimeID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO daemon_token (token_hash, workspace_id, daemon_id, expires_at)
		VALUES ($1, $2, $3, now() + interval '1 hour')
	`, "cleanup-banned-token-"+suffix, workspaceID, "cleanup-banned-daemon-"+suffix); err != nil {
		t.Fatal(err)
	}
	nilTargetsJSON, err := json.Marshal([]tagaccess.CleanupTarget(nil))
	if err != nil {
		t.Fatal(err)
	}
	targetDigest := sha256.Sum256(nilTargetsJSON)
	command := tagaccess.CleanupCommand{
		Source: tagaccess.CleanupIdentityRestriction, DeliveryID: deliveryID,
		CorrelationID: "cleanup-banned-correlation-" + suffix, VIBESUserID: vibesUserID,
		IdentityRestrictionVersion: 1, AccountEpoch: 7, PayloadDigest: fmt.Sprintf("%064d", 5),
		TargetDigest: hex.EncodeToString(targetDigest[:]),
	}
	if receipt, err := testHandler.Cleanup(ctx, command); err != nil || receipt.ReceiptID == "" {
		t.Fatalf("account-ban cleanup receipt=%#v err=%v", receipt, err)
	}
	var memberCount, tokenCount int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM member WHERE workspace_id = $1 AND user_id = $2`, workspaceID, userID).Scan(&memberCount); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM daemon_token WHERE token_hash = $1`, "cleanup-banned-token-"+suffix).Scan(&tokenCount); err != nil {
		t.Fatal(err)
	}
	if memberCount != 1 || tokenCount != 0 {
		t.Fatalf("account-ban cleanup member=%d daemon_token=%d", memberCount, tokenCount)
	}
}
