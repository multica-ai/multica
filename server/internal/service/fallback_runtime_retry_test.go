package service

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestProviderExhaustionWalksFallbackChainAndCoolsRuntimes(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}

	agent, err := q.GetAgent(ctx, util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}
	createRuntime := func(name, provider string) pgtype.UUID {
		t.Helper()
		var id pgtype.UUID
		if err := pool.QueryRow(ctx, `
			INSERT INTO agent_runtime (
				workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id
			) VALUES ($1, 'fallback-test-daemon', $2, 'cloud', $3, 'online', '', '{}'::jsonb, $4)
			RETURNING id
		`, workspaceID, name, provider, userID).Scan(&id); err != nil {
			t.Fatalf("create runtime %s: %v", name, err)
		}
		return id
	}
	fallbackOne := createRuntime("fallback-one", "claude")
	fallbackTwo := createRuntime("fallback-two", "hermes")
	for priority, runtimeID := range []pgtype.UUID{fallbackOne, fallbackTwo} {
		if err := q.AddAgentFallbackRuntime(ctx, db.AddAgentFallbackRuntimeParams{
			AgentID: agent.ID, RuntimeID: runtimeID, Priority: int32(priority),
		}); err != nil {
			t.Fatalf("add fallback runtime: %v", err)
		}
	}

	insertRunningTask := func(runtimeID pgtype.UUID, attempt, maxAttempts int32, parentID pgtype.UUID) pgtype.UUID {
		t.Helper()
		var id pgtype.UUID
		if err := pool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (
				agent_id, runtime_id, issue_id, status, priority, attempt, max_attempts,
				parent_task_id, session_id, work_dir, originator_user_id,
				accountable_user_id, originator_source, trigger_evidence_kind,
				trigger_evidence_ref_id
			) VALUES (
				$1, $2, $3, 'running', 0, $4, $5, $6, 'provider-session',
				'/tmp/fallback-chain', $7, $7, 'direct_human',
				'issue_assignment', $3
			) RETURNING id
		`, agent.ID, runtimeID, issueID, attempt, maxAttempts, parentID, userID).Scan(&id); err != nil {
			t.Fatalf("insert running task: %v", err)
		}
		return id
	}

	quotaReason := string(taskfailure.ReasonAgentProviderQuotaLimit)
	primaryTaskID := insertRunningTask(agent.RuntimeID, 1, 2, pgtype.UUID{})
	if _, err := svc.FailTask(ctx, primaryTaskID, "monthly usage limit reached", "provider-session", "/tmp/fallback-chain", quotaReason); err != nil {
		t.Fatalf("fail primary task: %v", err)
	}

	primaryCooldown, err := q.GetAgentRuntimeFallbackCooldown(ctx, db.GetAgentRuntimeFallbackCooldownParams{
		AgentID: agent.ID, RuntimeID: agent.RuntimeID,
	})
	if err != nil {
		t.Fatalf("load primary cooldown: %v", err)
	}
	if primaryCooldown.FailureReason != quotaReason || !primaryCooldown.CooldownUntil.Time.After(time.Now()) {
		t.Fatalf("primary cooldown = %#v", primaryCooldown)
	}

	firstChild := loadOnlyRetryChild(t, ctx, pool, primaryTaskID)
	if firstChild.RuntimeID != fallbackOne || firstChild.Attempt != 2 || firstChild.MaxAttempts != 3 {
		t.Fatalf("first child runtime/attempt = %s %d/%d", util.UUIDToString(firstChild.RuntimeID), firstChild.Attempt, firstChild.MaxAttempts)
	}
	if firstChild.SessionID.String != "provider-session" || firstChild.WorkDir.String != "/tmp/fallback-chain" {
		t.Fatalf("first child lost work context: session=%q workdir=%q", firstChild.SessionID.String, firstChild.WorkDir.String)
	}

	selected, err := svc.runtimeForNewTask(ctx, agent)
	if err != nil {
		t.Fatalf("select runtime during primary cooldown: %v", err)
	}
	if selected != fallbackOne {
		t.Fatalf("new task selected %s, want first fallback %s", util.UUIDToString(selected), util.UUIDToString(fallbackOne))
	}

	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1`, firstChild.ID); err != nil {
		t.Fatalf("start first child: %v", err)
	}
	if _, err := svc.FailTask(ctx, firstChild.ID, "HTTP 429 rate limit reached", "fallback-session-1", "/tmp/fallback-chain", string(taskfailure.ReasonAgentProviderCapacityOrRateLimit)); err != nil {
		t.Fatalf("fail first fallback: %v", err)
	}
	secondChild := loadOnlyRetryChild(t, ctx, pool, firstChild.ID)
	if secondChild.RuntimeID != fallbackTwo || secondChild.Attempt != 3 || secondChild.MaxAttempts != 3 {
		t.Fatalf("second child runtime/attempt = %s %d/%d", util.UUIDToString(secondChild.RuntimeID), secondChild.Attempt, secondChild.MaxAttempts)
	}

	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1`, secondChild.ID); err != nil {
		t.Fatalf("start second child: %v", err)
	}
	if _, err := svc.FailTask(ctx, secondChild.ID, "credits exhausted", "fallback-session-2", "/tmp/fallback-chain", quotaReason); err != nil {
		t.Fatalf("fail final fallback: %v", err)
	}
	var descendants int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE parent_task_id = $1`, secondChild.ID).Scan(&descendants); err != nil {
		t.Fatalf("count final descendants: %v", err)
	}
	if descendants != 0 {
		t.Fatalf("fallback chain cycled after exhaustion: %d descendants", descendants)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE agent_runtime_fallback_cooldown
		SET cooldown_until = now() - interval '1 second'
		WHERE agent_id = $1 AND runtime_id = $2
	`, agent.ID, agent.RuntimeID); err != nil {
		t.Fatalf("expire primary cooldown: %v", err)
	}
	selected, err = svc.runtimeForNewTask(ctx, agent)
	if err != nil {
		t.Fatalf("select runtime after cooldown expiry: %v", err)
	}
	if selected != agent.RuntimeID {
		t.Fatalf("expired primary cooldown selected %s, want %s", util.UUIDToString(selected), util.UUIDToString(agent.RuntimeID))
	}

	if _, err := pool.Exec(ctx, `DELETE FROM agent_runtime_fallback_cooldown WHERE agent_id = $1`, agent.ID); err != nil {
		t.Fatalf("clear chain cooldowns: %v", err)
	}
	genericTaskID := insertRunningTask(agent.RuntimeID, 1, 2, pgtype.UUID{})
	if _, err := svc.FailTask(ctx, genericTaskID, "agent process exited unexpectedly", "", "", "agent_error"); err != nil {
		t.Fatalf("fail generic agent task: %v", err)
	}
	var genericChildren int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE parent_task_id = $1`, genericTaskID).Scan(&genericChildren); err != nil {
		t.Fatalf("count generic failure children: %v", err)
	}
	if genericChildren != 0 {
		t.Fatalf("generic agent failure created %d fallback children", genericChildren)
	}
	var genericCooldowns int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_runtime_fallback_cooldown WHERE agent_id = $1`, agent.ID).Scan(&genericCooldowns); err != nil {
		t.Fatalf("count generic failure cooldowns: %v", err)
	}
	if genericCooldowns != 0 {
		t.Fatalf("generic agent failure created %d runtime cooldowns", genericCooldowns)
	}
}

func TestClearFallbackCooldownRejectsStaleTaskSuccess(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)
	agent, err := q.GetAgent(ctx, util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}

	var runtimeID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id
		) VALUES ($1, 'cooldown-race', 'cloud', 'codex', 'online', '', '{}'::jsonb, $2)
		RETURNING id
	`, workspaceID, userID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	cooldown, err := q.UpsertAgentRuntimeFallbackCooldown(ctx, db.UpsertAgentRuntimeFallbackCooldownParams{
		AgentID: agent.ID, RuntimeID: runtimeID,
		CooldownUntil: pgtype.Timestamptz{Time: time.Now().Add(15 * time.Minute), Valid: true},
		FailureReason: string(taskfailure.ReasonAgentProviderQuotaLimit),
		SourceTaskID:  pgtype.UUID{},
	})
	if err != nil {
		t.Fatalf("create cooldown: %v", err)
	}

	if err := q.ClearAgentRuntimeFallbackCooldown(ctx, db.ClearAgentRuntimeFallbackCooldownParams{
		AgentID: agent.ID, RuntimeID: runtimeID,
		SuccessfulTaskStartedAt: pgtype.Timestamptz{Time: cooldown.UpdatedAt.Time.Add(-time.Minute), Valid: true},
	}); err != nil {
		t.Fatalf("clear cooldown from stale success: %v", err)
	}
	if _, err := q.GetAgentRuntimeFallbackCooldown(ctx, db.GetAgentRuntimeFallbackCooldownParams{
		AgentID: agent.ID, RuntimeID: runtimeID,
	}); err != nil {
		t.Fatalf("stale success removed active cooldown: %v", err)
	}

	if err := q.ClearAgentRuntimeFallbackCooldown(ctx, db.ClearAgentRuntimeFallbackCooldownParams{
		AgentID: agent.ID, RuntimeID: runtimeID,
		SuccessfulTaskStartedAt: pgtype.Timestamptz{Time: cooldown.UpdatedAt.Time.Add(time.Minute), Valid: true},
	}); err != nil {
		t.Fatalf("clear cooldown from fresh success: %v", err)
	}
	if _, err := q.GetAgentRuntimeFallbackCooldown(ctx, db.GetAgentRuntimeFallbackCooldownParams{
		AgentID: agent.ID, RuntimeID: runtimeID,
	}); err == nil {
		t.Fatal("fresh success did not clear cooldown")
	}
}

func TestProviderFallbackDoesNotOfferHostLocalWorktreeToAnotherDaemon(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
	agent, err := q.GetAgent(ctx, util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}

	var fallbackID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id
		) VALUES ($1, 'another-daemon', 'remote-fallback', 'cloud', 'claude', 'online', '', '{}'::jsonb, $2)
		RETURNING id
	`, workspaceID, userID).Scan(&fallbackID); err != nil {
		t.Fatalf("create remote fallback: %v", err)
	}
	if err := q.AddAgentFallbackRuntime(ctx, db.AddAgentFallbackRuntimeParams{
		AgentID: agent.ID, RuntimeID: fallbackID, Priority: 0,
	}); err != nil {
		t.Fatalf("configure remote fallback: %v", err)
	}

	var taskID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, attempt, max_attempts, work_dir,
			originator_user_id, accountable_user_id, originator_source,
			trigger_evidence_kind, trigger_evidence_ref_id
		) VALUES ($1, $2, $3, 'running', 1, 2, '/tmp/local-only-worktree',
			$4, $4, 'direct_human', 'issue_assignment', $3)
		RETURNING id
	`, agent.ID, agent.RuntimeID, issueID, userID).Scan(&taskID); err != nil {
		t.Fatalf("create running task: %v", err)
	}

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	if _, err := svc.FailTask(ctx, taskID, "quota exceeded", "", "/tmp/local-only-worktree", string(taskfailure.ReasonAgentProviderQuotaLimit)); err != nil {
		t.Fatalf("fail task: %v", err)
	}
	var children int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE parent_task_id = $1`, taskID).Scan(&children); err != nil {
		t.Fatalf("count retry children: %v", err)
	}
	if children != 0 {
		t.Fatalf("created %d retry tasks on a daemon that cannot access the worktree", children)
	}
}

func TestProviderFallbackPreservesChatOrderingAndSquadRole(t *testing.T) {
	quotaReason := string(taskfailure.ReasonAgentProviderQuotaLimit)

	t.Run("chat retry keeps input order and worktree", func(t *testing.T) {
		pool := newResolveOriginatorPool(t)
		ctx := context.Background()
		q := db.New(pool)
		workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)
		agent, err := q.GetAgent(ctx, util.MustParseUUID(agentID))
		if err != nil {
			t.Fatalf("load agent: %v", err)
		}
		fallbackID := createFallbackTestRuntime(t, ctx, pool, workspaceID, userID, "chat-fallback")
		if err := q.AddAgentFallbackRuntime(ctx, db.AddAgentFallbackRuntimeParams{
			AgentID: agent.ID, RuntimeID: fallbackID, Priority: 0,
		}); err != nil {
			t.Fatalf("configure fallback: %v", err)
		}

		var chatID, taskID pgtype.UUID
		if err := pool.QueryRow(ctx, `
			INSERT INTO chat_session (workspace_id, agent_id, creator_id, title)
			VALUES ($1, $2, $3, 'fallback chat') RETURNING id
		`, workspaceID, agent.ID, userID).Scan(&chatID); err != nil {
			t.Fatalf("create chat: %v", err)
		}
		if err := pool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (
				agent_id, runtime_id, chat_session_id, status, priority, attempt, max_attempts,
				session_id, work_dir, originator_user_id, accountable_user_id,
				originator_source, trigger_evidence_kind, trigger_evidence_ref_id
			) VALUES (
				$1, $2, $3, 'running', 2, 1, 2, 'chat-provider-session',
				'/tmp/chat-fallback-worktree', $4, $4, 'direct_human',
				'chat', $3
			) RETURNING id
		`, agent.ID, agent.RuntimeID, chatID, userID).Scan(&taskID); err != nil {
			t.Fatalf("create chat task: %v", err)
		}

		svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
		if _, err := svc.FailTask(ctx, taskID, "credits exhausted", "chat-provider-session", "/tmp/chat-fallback-worktree", quotaReason); err != nil {
			t.Fatalf("fail chat task: %v", err)
		}
		child := loadOnlyRetryChild(t, ctx, pool, taskID)
		if child.RuntimeID != fallbackID || child.ChatSessionID != chatID || child.Priority != 3 {
			t.Fatalf("chat fallback routing/order = runtime %s chat %s priority %d", util.UUIDToString(child.RuntimeID), util.UUIDToString(child.ChatSessionID), child.Priority)
		}
		if child.WorkDir.String != "/tmp/chat-fallback-worktree" || child.SessionID.String != "chat-provider-session" || child.ForceFreshSession {
			t.Fatalf("chat fallback context = workdir %q session %q force_fresh %t", child.WorkDir.String, child.SessionID.String, child.ForceFreshSession)
		}
	})

	t.Run("squad leader retry keeps squad provenance", func(t *testing.T) {
		pool := newResolveOriginatorPool(t)
		ctx := context.Background()
		q := db.New(pool)
		workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
		agent, err := q.GetAgent(ctx, util.MustParseUUID(agentID))
		if err != nil {
			t.Fatalf("load agent: %v", err)
		}
		fallbackID := createFallbackTestRuntime(t, ctx, pool, workspaceID, userID, "squad-fallback")
		if err := q.AddAgentFallbackRuntime(ctx, db.AddAgentFallbackRuntimeParams{
			AgentID: agent.ID, RuntimeID: fallbackID, Priority: 0,
		}); err != nil {
			t.Fatalf("configure fallback: %v", err)
		}

		var squadID, taskID pgtype.UUID
		if err := pool.QueryRow(ctx, `
			INSERT INTO squad (workspace_id, name, leader_id, creator_id)
			VALUES ($1, 'fallback-squad-' || gen_random_uuid(), $2, $3) RETURNING id
		`, workspaceID, agent.ID, userID).Scan(&squadID); err != nil {
			t.Fatalf("create squad: %v", err)
		}
		if err := pool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (
				agent_id, runtime_id, issue_id, status, priority, attempt, max_attempts,
				is_leader_task, squad_id, originator_user_id, accountable_user_id,
				originator_source, trigger_evidence_kind, trigger_evidence_ref_id
			) VALUES (
				$1, $2, $3, 'running', 1, 1, 2, true, $4, $5, $5,
				'direct_human', 'issue_assignment', $3
			) RETURNING id
		`, agent.ID, agent.RuntimeID, issueID, squadID, userID).Scan(&taskID); err != nil {
			t.Fatalf("create squad task: %v", err)
		}

		svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
		if _, err := svc.FailTask(ctx, taskID, "monthly usage limit", "", "/tmp/squad-fallback", quotaReason); err != nil {
			t.Fatalf("fail squad task: %v", err)
		}
		child := loadOnlyRetryChild(t, ctx, pool, taskID)
		if child.RuntimeID != fallbackID || child.SquadID != squadID || !child.IsLeaderTask {
			t.Fatalf("squad fallback provenance = runtime %s squad %s leader %t", util.UUIDToString(child.RuntimeID), util.UUIDToString(child.SquadID), child.IsLeaderTask)
		}
	})
}

func createFallbackTestRuntime(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workspaceID, userID, name string) pgtype.UUID {
	t.Helper()
	var runtimeID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id
		) VALUES ($1, 'fallback-test-daemon', $2, 'cloud', 'claude', 'online', '', '{}'::jsonb, $3)
		RETURNING id
	`, workspaceID, name, userID).Scan(&runtimeID); err != nil {
		t.Fatalf("create fallback runtime: %v", err)
	}
	return runtimeID
}

func loadOnlyRetryChild(t *testing.T, ctx context.Context, pool *pgxpool.Pool, parentID pgtype.UUID) db.AgentTaskQueue {
	t.Helper()
	var childID pgtype.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM agent_task_queue WHERE parent_task_id = $1`, parentID).Scan(&childID); err != nil {
		t.Fatalf("load retry child id: %v", err)
	}
	child, err := db.New(pool).GetAgentTask(ctx, childID)
	if err != nil {
		t.Fatalf("load retry child: %v", err)
	}
	return child
}
