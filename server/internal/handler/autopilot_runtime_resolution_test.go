package handler

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestDispatchAutopilotRunOnlyUsesCreatorRuntimeNotAgentLegacyRuntime(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var agentOwnerID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Autopilot Agent Owner', 'ap-agent-owner-runtime@multica.test')
		RETURNING id
	`).Scan(&agentOwnerID); err != nil {
		t.Fatalf("create agent owner: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, agentOwnerID)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, agentOwnerID); err != nil {
		t.Fatalf("add agent owner member: %v", err)
	}

	var creatorID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Autopilot Runtime Owner', 'ap-creator-runtime@multica.test')
		RETURNING id
	`).Scan(&creatorID); err != nil {
		t.Fatalf("create autopilot creator: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, creatorID)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, creatorID); err != nil {
		t.Fatalf("add autopilot creator member: %v", err)
	}

	var agentOwnerRuntimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, visibility, last_seen_at
		)
		VALUES ($1, 'ap-agent-owner-daemon', 'Agent owner runtime', 'local',
		        'codex', 'online', 'agent-owner', '{}'::jsonb, $2, 'private', now())
		RETURNING id
	`, testWorkspaceID, agentOwnerID).Scan(&agentOwnerRuntimeID); err != nil {
		t.Fatalf("create agent owner runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, agentOwnerRuntimeID)
	})

	var creatorRuntimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, visibility, last_seen_at
		)
		VALUES ($1, 'ap-creator-daemon', 'Creator runtime', 'local',
		        'codex', 'online', 'creator', '{}'::jsonb, $2, 'private', now())
		RETURNING id
	`, testWorkspaceID, creatorID).Scan(&creatorRuntimeID); err != nil {
		t.Fatalf("create creator runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, creatorRuntimeID)
	})

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, runtime_provider, visibility, max_concurrent_tasks,
			owner_id, instructions, custom_env, custom_args
		)
		VALUES ($1, 'ap runtime resolution agent', '', 'local', '{}'::jsonb,
		        $2, 'codex', 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, testWorkspaceID, agentOwnerRuntimeID, agentOwnerID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	var apID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot (
			workspace_id, title, assignee_type, assignee_id,
			execution_mode, created_by_type, created_by_id, status
		)
		VALUES ($1, 'creator runtime dispatch', 'agent', $2,
		        'run_only', 'member', $3, 'active')
		RETURNING id
	`, testWorkspaceID, agentID, creatorID).Scan(&apID); err != nil {
		t.Fatalf("create autopilot: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM autopilot_run WHERE autopilot_id = $1`, apID)
		testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, apID)
	})

	ap, err := testHandler.Queries.GetAutopilot(ctx, parseUUID(apID))
	if err != nil {
		t.Fatalf("load autopilot: %v", err)
	}
	run, err := testHandler.AutopilotService.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "manual", nil)
	if err != nil {
		t.Fatalf("DispatchAutopilot: %v", err)
	}
	if !run.TaskID.Valid {
		t.Fatalf("expected run task id")
	}

	var taskRuntimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT runtime_id::text FROM agent_task_queue WHERE id = $1
	`, run.TaskID).Scan(&taskRuntimeID); err != nil {
		t.Fatalf("read task runtime: %v", err)
	}
	if taskRuntimeID != creatorRuntimeID {
		t.Fatalf("task runtime_id = %s, want creator runtime %s; must not use agent legacy runtime %s",
			taskRuntimeID, creatorRuntimeID, agentOwnerRuntimeID)
	}
}
