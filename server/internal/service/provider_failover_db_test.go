package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/featureflag"
	"github.com/multica-ai/multica/server/pkg/providerfailover"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// TestFailTaskQuotaCreatesExactlyOneAuthorizedContinuation is the regression
// test for the ordering bug found in PR review: FailTask used to write its own
// failure comment before EvaluateFailover, so the side-effect scan observed
// that platform-authored comment and declined every clean run. This exercises
// the real database transaction and terminal callback twice; the result must be
// exactly one owning ledger row and one queued cross-provider continuation.
func TestFailTaskQuotaCreatesExactlyOneAuthorizedContinuation(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()

	var failoverTable pgtype.Text
	if err := pool.QueryRow(ctx, `SELECT to_regclass('provider_failover_handoff')::text`).Scan(&failoverTable); err != nil ||
		!failoverTable.Valid {
		t.Skip("provider failover migrations are not installed in the test database")
	}

	var userID, workspaceID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Provider Failover Test', 'provider-failover-' || gen_random_uuid() || '@multica.test')
		RETURNING id
	`).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug)
		VALUES ('provider-failover-test', 'provider-failover-' || gen_random_uuid())
		RETURNING id
	`).Scan(&workspaceID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		pool.Exec(cleanupCtx, `DELETE FROM provider_failover_handoff WHERE workspace_id = $1`, workspaceID)
		pool.Exec(cleanupCtx, `
			DELETE FROM comment
			WHERE workspace_id = $1
		`, workspaceID)
		pool.Exec(cleanupCtx, `
			DELETE FROM agent_task_queue
			WHERE agent_id IN (SELECT id FROM agent WHERE workspace_id = $1)
		`, workspaceID)
		pool.Exec(cleanupCtx, `DELETE FROM issue WHERE workspace_id = $1`, workspaceID)
		pool.Exec(cleanupCtx, `
			DELETE FROM agent_invocation_target
			WHERE agent_id IN (SELECT id FROM agent WHERE workspace_id = $1)
		`, workspaceID)
		pool.Exec(cleanupCtx, `DELETE FROM agent WHERE workspace_id = $1`, workspaceID)
		pool.Exec(cleanupCtx, `DELETE FROM agent_runtime WHERE workspace_id = $1`, workspaceID)
		pool.Exec(cleanupCtx, `DELETE FROM member WHERE workspace_id = $1`, workspaceID)
		pool.Exec(cleanupCtx, `DELETE FROM workspace WHERE id = $1`, workspaceID)
		pool.Exec(cleanupCtx, `DELETE FROM "user" WHERE id = $1`, userID)
	})

	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, userID); err != nil {
		t.Fatalf("seed member: %v", err)
	}

	var codexRuntimeID, claudeRuntimeID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id
		) VALUES ($1, 'failover-codex', 'cloud', 'codex', 'online', '', '{}'::jsonb, $2)
		RETURNING id
	`, workspaceID, userID).Scan(&codexRuntimeID); err != nil {
		t.Fatalf("seed codex runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id
		) VALUES ($1, 'failover-claude', 'cloud', 'claude', 'online', '', '{}'::jsonb, $2)
		RETURNING id
	`, workspaceID, userID).Scan(&claudeRuntimeID); err != nil {
		t.Fatalf("seed claude runtime: %v", err)
	}

	var sourceAgentID, targetAgentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args
		)
		VALUES ($1, 'failover-source', 'cloud', '{}'::jsonb,
		        $2, 'private', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, workspaceID, codexRuntimeID, userID).Scan(&sourceAgentID); err != nil {
		t.Fatalf("seed source agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args
		)
		VALUES ($1, 'failover-target', 'cloud',
		        '{"provider_failover_target":true}'::jsonb,
		        $2, 'private', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, workspaceID, claudeRuntimeID, userID).Scan(&targetAgentID); err != nil {
		t.Fatalf("seed target agent: %v", err)
	}

	var issueID, taskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id)
		VALUES ($1, 'provider failover ordering', 'member', $2)
		RETURNING id
	`, workspaceID, userID).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, started_at,
			originator_user_id, accountable_user_id
		)
		VALUES ($1, $2, $3, 'running', 0, now(), $4, $4)
		RETURNING id
	`, sourceAgentID, codexRuntimeID, issueID, userID).Scan(&taskID); err != nil {
		t.Fatalf("seed running task: %v", err)
	}

	static := featureflag.NewStaticProvider()
	static.Set(featureflags.ProviderFailover, featureflag.Rule{Default: true})
	static.Set(featureflags.ProviderFailoverActive, featureflag.Rule{Default: true})
	queries := db.New(pool)
	svc := NewTaskService(queries, pool, nil, events.New())
	svc.FeatureFlags = featureflag.NewService(static)
	evidence := &providerfailover.SideEffectEvidence{Complete: true}

	taskUUID := util.MustParseUUID(taskID)
	for i := 0; i < 2; i++ {
		if _, err := svc.FailTask(
			ctx,
			taskUUID,
			"provider plan quota exhausted",
			"",
			"",
			string(taskfailure.ReasonAgentProviderQuotaLimit),
			false,
			evidence,
		); err != nil {
			t.Fatalf("FailTask call %d: %v", i+1, err)
		}
	}

	var handoffCount, childCount int
	var state, declineReason, actualTargetAgentID string
	if err := pool.QueryRow(ctx, `
		SELECT count(*), max(state), COALESCE(max(decline_reason), ''),
		       COALESCE(max(target_agent_id::text), '')
		FROM provider_failover_handoff
		WHERE original_task_id = $1
	`, taskID).Scan(&handoffCount, &state, &declineReason, &actualTargetAgentID); err != nil {
		t.Fatalf("read handoff: %v", err)
	}
	if handoffCount != 1 {
		t.Fatalf("handoff rows = %d, want exactly 1", handoffCount)
	}
	if state != string(providerfailover.StateDispatched) || declineReason != providerfailover.ReasonEligible {
		t.Fatalf("handoff state=%q reason=%q, want dispatched/%q",
			state, declineReason, providerfailover.ReasonEligible)
	}
	if actualTargetAgentID != targetAgentID {
		t.Fatalf("target agent = %s, want authorized target %s", actualTargetAgentID, targetAgentID)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM agent_task_queue
		WHERE parent_task_id = $1 AND agent_id = $2
	`, taskID, targetAgentID).Scan(&childCount); err != nil {
		t.Fatalf("count continuation tasks: %v", err)
	}
	if childCount != 1 {
		t.Fatalf("continuation tasks = %d, want exactly 1", childCount)
	}
}
