package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestAutopilotDispatchHonorsAgentSelectedSpaceAvailability(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin fixture tx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	var userID, workspaceID, spaceAID, spaceBID, runtimeID, agentID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Availability Dispatch Owner', 'availability-dispatch-' || gen_random_uuid() || '@multica.test')
		RETURNING id
	`).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO workspace (name, slug)
		VALUES ('availability-dispatch', 'availability-dispatch-' || gen_random_uuid())
		RETURNING id
	`).Scan(&workspaceID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
	`, workspaceID, userID); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO workspace_space (workspace_id, name, key, visibility, created_by)
		VALUES ($1, 'Space A', 'AVA', 'open', $2) RETURNING id
	`, workspaceID, userID).Scan(&spaceAID); err != nil {
		t.Fatalf("seed Space A: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO workspace_space (workspace_id, name, key, visibility, created_by)
		VALUES ($1, 'Space B', 'AVB', 'open', $2) RETURNING id
	`, workspaceID, userID).Scan(&spaceBID); err != nil {
		t.Fatalf("seed Space B: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id
		) VALUES ($1, 'availability-runtime', 'cloud', 'codex', 'online', '', '{}'::jsonb, $2)
		RETURNING id
	`, workspaceID, userID).Scan(&runtimeID); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, runtime_mode, runtime_config, runtime_id,
			visibility, permission_mode, availability_mode,
			max_concurrent_tasks, owner_id, instructions, custom_env, custom_args
		) VALUES (
			$1, 'availability-dispatch-agent', 'cloud', '{}'::jsonb, $2,
			'workspace', 'public_to', 'selected_spaces',
			1, $3, '', '{}'::jsonb, '[]'::jsonb
		) RETURNING id
	`, workspaceID, runtimeID, userID).Scan(&agentID); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id, created_by)
		VALUES ($1, 'workspace', $2, $3)
	`, agentID, workspaceID, userID); err != nil {
		t.Fatalf("seed invocation audience: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO agent_available_space (agent_id, workspace_id, space_id, created_by)
		VALUES ($1, $2, $3, $4)
	`, agentID, workspaceID, spaceAID, userID); err != nil {
		t.Fatalf("seed selected Space: %v", err)
	}

	q := db.New(tx)
	service := &AutopilotService{Queries: q}
	agent, err := q.GetAgent(ctx, util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}
	base := db.Autopilot{
		WorkspaceID:   util.MustParseUUID(workspaceID),
		AssigneeType:  "agent",
		AssigneeID:    util.MustParseUUID(agentID),
		CreatedByType: "member",
		CreatedByID:   util.MustParseUUID(userID),
		ExecutionMode: "run_only",
	}

	selected := base
	selected.SpaceID = util.MustParseUUID(spaceAID)
	if !service.canCreatorInvokeAgent(ctx, selected, agent) {
		t.Fatal("selected-space Autopilot should pass dispatch gate")
	}
	if reason, _, skip := service.shouldSkipDispatch(ctx, selected, pgtype.UUID{}); skip {
		t.Fatalf("selected-space dispatch unexpectedly skipped: %s", reason)
	}

	unselected := base
	unselected.SpaceID = util.MustParseUUID(spaceBID)
	if service.canCreatorInvokeAgent(ctx, unselected, agent) {
		t.Fatal("unselected-space Autopilot must fail dispatch gate")
	}
	if reason, _, skip := service.shouldSkipDispatch(ctx, unselected, pgtype.UUID{}); !skip || reason != "autopilot creator cannot invoke assignee agent in target Space" {
		t.Fatalf("unselected dispatch = skip %v reason %q", skip, reason)
	}

	// Archiving a previously selected Space invalidates the gate at dispatch
	// time; save-time validation alone is not sufficient for scheduled runs.
	if _, err := tx.Exec(ctx, `UPDATE workspace_space SET archived_at = now() WHERE id = $1`, spaceAID); err != nil {
		t.Fatalf("archive selected Space: %v", err)
	}
	if service.canCreatorInvokeAgent(ctx, selected, agent) {
		t.Fatal("archived selected Space must fail dispatch gate")
	}

	// Keep pgtype imported explicitly: this assertion also documents that a
	// missing Space is invalid for every Autopilot dispatch.
	missing := base
	missing.SpaceID = pgtype.UUID{}
	if service.canCreatorInvokeAgent(ctx, missing, agent) {
		t.Fatal("Autopilot without Space context must fail dispatch gate")
	}
}
