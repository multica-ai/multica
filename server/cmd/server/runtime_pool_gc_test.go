package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// A Pool Runtime has no bound Agent by design, so the legacy GC predicate
// treated it as dependency-free and deleted it directly. This fixture locks in
// the same removal/history contract as the interactive Runtime delete paths.
func TestRuntimePoolGCUsesAffinityTeardown(t *testing.T) {
	if testPool == nil {
		t.Fatal("database not available")
	}
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	h := handler.New(
		queries,
		testPool,
		nil,
		bus,
		service.NewEmailService(),
		nil,
		nil,
		analytics.NoopClient{},
		handler.Config{},
	)

	seed := time.Now().UnixNano()
	var agentID, issueID, runtimeID, waitingTaskID, terminalTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks,
			owner_id, runtime_binding_mode, runtime_requirements
		) VALUES (
			$1, $2, '', 'pool', '{}'::jsonb, NULL, 'private', 'private', 1,
			$3, 'pool', '{}'::jsonb
		)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Pool GC Agent %d", seed), testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create Pool GC Agent: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_type, creator_id,
			number, position, assignee_type, assignee_id
		)
		SELECT $1, $2, 'in_progress', 'none', 'member', $3,
		       COALESCE(MAX(number), 0) + 1, 0, 'agent', $4
		FROM issue WHERE workspace_id = $1
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Pool GC Issue %d", seed), testUserID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("create Pool GC Issue: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, visibility, capabilities, last_seen_at
		) VALUES (
			$1, $2, $3, 'local', 'pool-gc-test', 'offline', '', '{}'::jsonb,
			$4, 'private', '{}'::text[], now() - interval '1000 years'
		)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("pool-gc-%d", seed), fmt.Sprintf("Pool GC Runtime %d", seed), testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("create stale Pool Runtime: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = testPool.Exec(cleanupCtx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		_, _ = testPool.Exec(cleanupCtx, `DELETE FROM issue WHERE id = $1`, issueID)
		_, _ = testPool.Exec(cleanupCtx, `DELETE FROM agent WHERE id = $1`, agentID)
		_, _ = testPool.Exec(cleanupCtx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, issue_id, runtime_id, status, runtime_binding_mode,
			runtime_requirements, placement_workspace_id, runtime_requester_user_id,
			session_affinity_state, session_affinity_runtime_id, wait_reason
		) VALUES (
			$1, $2, NULL, 'waiting_runtime', 'pool', '{}'::jsonb, $3, $4,
			'pinned', $5, 'session_runtime_offline'
		)
		RETURNING id
	`, agentID, issueID, testWorkspaceID, testUserID, runtimeID).Scan(&waitingTaskID); err != nil {
		t.Fatalf("create waiting pinned Pool Task: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, issue_id, runtime_id, status, completed_at,
			runtime_binding_mode, runtime_requirements, placement_workspace_id,
			runtime_requester_user_id, session_affinity_state,
			session_affinity_runtime_id, session_id, work_dir
		) VALUES (
			$1, $2, $3, 'completed', now(), 'pool', '{}'::jsonb, $4, $5,
			'pinned', $3, 'pool-gc-session', '/tmp/pool-gc-workdir'
		)
		RETURNING id
	`, agentID, issueID, runtimeID, testWorkspaceID, testUserID).Scan(&terminalTaskID); err != nil {
		t.Fatalf("create terminal pinned Pool Task: %v", err)
	}

	var cancelledEvents, runtimeGCEvents int
	bus.Subscribe(protocol.EventTaskCancelled, func(events.Event) { cancelledEvents++ })
	bus.Subscribe(protocol.EventDaemonRegister, func(event events.Event) {
		payload, _ := event.Payload.(map[string]any)
		if payload["action"] == "runtime_gc" {
			runtimeGCEvents++
		}
	})

	gcRuntimes(ctx, h)

	var runtimeCount int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&runtimeCount); err != nil {
		t.Fatalf("count stale Runtime: %v", err)
	}
	if runtimeCount != 0 {
		t.Fatalf("stale Runtime count = %d, want 0", runtimeCount)
	}

	var waitingStatus, waitingAffinity string
	var waitingCompleted, waitingRuntime, waitingAffinityRuntime bool
	var waitingReason *string
	if err := testPool.QueryRow(ctx, `
		SELECT status, completed_at IS NOT NULL, runtime_id IS NOT NULL,
		       session_affinity_state, session_affinity_runtime_id IS NOT NULL, wait_reason
		FROM agent_task_queue WHERE id = $1
	`, waitingTaskID).Scan(
		&waitingStatus, &waitingCompleted, &waitingRuntime,
		&waitingAffinity, &waitingAffinityRuntime, &waitingReason,
	); err != nil {
		t.Fatalf("read waiting pinned Pool Task: %v", err)
	}
	if waitingStatus != "cancelled" || !waitingCompleted || waitingRuntime ||
		waitingAffinity != "removed" || waitingAffinityRuntime ||
		waitingReason == nil || *waitingReason != "session_runtime_removed" {
		t.Fatalf("waiting Task after GC = status=%q completed=%v runtime=%v affinity=%q affinity_runtime=%v reason=%v",
			waitingStatus, waitingCompleted, waitingRuntime, waitingAffinity, waitingAffinityRuntime, waitingReason)
	}

	var terminalStatus, terminalAffinity string
	var terminalRuntime bool
	var terminalAffinityRuntime pgtype.UUID
	if err := testPool.QueryRow(ctx, `
		SELECT status, runtime_id IS NOT NULL, session_affinity_state, session_affinity_runtime_id
		FROM agent_task_queue WHERE id = $1
	`, terminalTaskID).Scan(&terminalStatus, &terminalRuntime, &terminalAffinity, &terminalAffinityRuntime); err != nil {
		t.Fatalf("read terminal Pool history: %v", err)
	}
	if terminalStatus != "completed" || terminalRuntime || terminalAffinity != "pinned" || !terminalAffinityRuntime.Valid {
		t.Fatalf("terminal Task after GC = status=%q runtime=%v affinity=%q affinity_runtime=%v; want completed/null/pinned/tombstone",
			terminalStatus, terminalRuntime, terminalAffinity, terminalAffinityRuntime)
	}
	if cancelledEvents != 1 || runtimeGCEvents != 1 {
		t.Fatalf("post-commit events = cancelled:%d runtime_gc:%d, want 1/1", cancelledEvents, runtimeGCEvents)
	}
}
