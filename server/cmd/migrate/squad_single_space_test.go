package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/migrations"
)

const syntheticPre173SquadSchema = `
CREATE TABLE workspace_space (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    is_default BOOLEAN NOT NULL DEFAULT false,
    UNIQUE (workspace_id, id)
);
CREATE TABLE squad (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    leader_id UUID NOT NULL,
    creator_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    archived_at TIMESTAMPTZ,
    archived_by UUID,
    avatar_url TEXT,
    instructions TEXT NOT NULL DEFAULT ''
);
CREATE TABLE squad_member (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    squad_id UUID NOT NULL,
    member_type TEXT NOT NULL,
    member_id UUID NOT NULL,
    role TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE issue (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    space_id UUID NOT NULL,
    assignee_type TEXT,
    assignee_id UUID
);
CREATE TABLE autopilot (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    space_id UUID NOT NULL,
    assignee_type TEXT NOT NULL,
    assignee_id UUID NOT NULL
);
CREATE TABLE autopilot_run (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    autopilot_id UUID NOT NULL,
    squad_id UUID
);
CREATE TABLE agent_task_queue (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id UUID,
    autopilot_run_id UUID,
    squad_id UUID,
    context JSONB NOT NULL DEFAULT '{}'::jsonb
);
`

func TestMigration173SplitsCrossSpaceSquadWithoutMovingWork(t *testing.T) {
	pool, schema := newCutoverSchema(t)
	execCutover(t, pool, syntheticPre173SquadSchema)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var workspaceID, spaceAID, spaceBID, squadID, agentID, userID string
	if err := pool.QueryRow(ctx, `SELECT gen_random_uuid(), gen_random_uuid(), gen_random_uuid()`).Scan(&workspaceID, &agentID, &userID); err != nil {
		t.Fatalf("generate fixture IDs: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace_space (workspace_id, is_default)
		VALUES ($1, true) RETURNING id
	`, workspaceID).Scan(&spaceAID); err != nil {
		t.Fatalf("insert default Space: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace_space (workspace_id, is_default)
		VALUES ($1, false) RETURNING id
	`, workspaceID).Scan(&spaceBID); err != nil {
		t.Fatalf("insert second Space: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id, instructions)
		VALUES ($1, 'Cross-space Squad', 'kept', $2, $3, 'same roster')
		RETURNING id
	`, workspaceID, agentID, userID).Scan(&squadID); err != nil {
		t.Fatalf("insert legacy Squad: %v", err)
	}
	execCutover(t, pool, `
		INSERT INTO squad_member (squad_id, member_type, member_id, role)
		VALUES ($1, 'agent', $2, 'leader'), ($1, 'member', $3, 'member')
	`, squadID, agentID, userID)

	var issueAID, issueBID, autopilotAID, autopilotBID, runAID, runBID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, space_id, assignee_type, assignee_id)
		VALUES ($1, $2, 'squad', $3) RETURNING id
	`, workspaceID, spaceAID, squadID).Scan(&issueAID); err != nil {
		t.Fatalf("insert Space A issue: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, space_id, assignee_type, assignee_id)
		VALUES ($1, $2, 'squad', $3) RETURNING id
	`, workspaceID, spaceBID, squadID).Scan(&issueBID); err != nil {
		t.Fatalf("insert Space B issue: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot (workspace_id, space_id, assignee_type, assignee_id)
		VALUES ($1, $2, 'squad', $3) RETURNING id
	`, workspaceID, spaceAID, squadID).Scan(&autopilotAID); err != nil {
		t.Fatalf("insert Space A Autopilot: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot (workspace_id, space_id, assignee_type, assignee_id)
		VALUES ($1, $2, 'squad', $3) RETURNING id
	`, workspaceID, spaceBID, squadID).Scan(&autopilotBID); err != nil {
		t.Fatalf("insert Space B Autopilot: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO autopilot_run (autopilot_id, squad_id) VALUES ($1, $2) RETURNING id`, autopilotAID, squadID).Scan(&runAID); err != nil {
		t.Fatalf("insert Space A run: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO autopilot_run (autopilot_id, squad_id) VALUES ($1, $2) RETURNING id`, autopilotBID, squadID).Scan(&runBID); err != nil {
		t.Fatalf("insert Space B run: %v", err)
	}
	execCutover(t, pool, `
		INSERT INTO agent_task_queue (issue_id, squad_id) VALUES ($1, $3), ($2, $3)
	`, issueAID, issueBID, squadID)
	execCutover(t, pool, `
		INSERT INTO agent_task_queue (autopilot_run_id, squad_id) VALUES ($1, $3), ($2, $3)
	`, runAID, runBID, squadID)
	var quickCreateTaskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (squad_id, context)
		VALUES ($1::uuid, jsonb_build_object(
			'type', 'quick_create',
			'workspace_id', ($2::uuid)::text,
			'space_id', ($3::uuid)::text,
			'squad_id', ($1::uuid)::text
		)) RETURNING id
	`, squadID, workspaceID, spaceBID).Scan(&quickCreateTaskID); err != nil {
		t.Fatalf("insert quick-create task: %v", err)
	}

	dir, err := migrations.ResolveDir()
	if err != nil {
		t.Fatalf("resolve migrations dir: %v", err)
	}
	if err := applyCutoverMigration(pool, schema, filepath.Join(dir, "173_squad_single_space.up.sql")); err != nil {
		t.Fatalf("apply migration 173: %v", err)
	}

	var squadCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM squad WHERE workspace_id = $1`, workspaceID).Scan(&squadCount); err != nil {
		t.Fatalf("count split Squads: %v", err)
	}
	if squadCount != 2 {
		t.Fatalf("Squad count = %d, want one per Space (2)", squadCount)
	}
	var badRosterCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM (
			SELECT s.id FROM squad s
			LEFT JOIN squad_member sm ON sm.squad_id = s.id
			WHERE s.workspace_id = $1
			GROUP BY s.id
			HAVING count(sm.id) <> 2
		) bad
	`, workspaceID).Scan(&badRosterCount); err != nil {
		t.Fatalf("validate cloned rosters: %v", err)
	}
	if badRosterCount != 0 {
		t.Fatalf("found %d split Squads without the original two-member roster", badRosterCount)
	}

	var crossSpaceReferences int
	if err := pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM issue i JOIN squad s ON s.id = i.assignee_id
		   WHERE i.workspace_id = $1 AND (i.space_id <> s.space_id OR i.workspace_id <> s.workspace_id))
		+ (SELECT count(*) FROM autopilot a JOIN squad s ON s.id = a.assignee_id
		   WHERE a.workspace_id = $1 AND (a.space_id <> s.space_id OR a.workspace_id <> s.workspace_id))
	`, workspaceID).Scan(&crossSpaceReferences); err != nil {
		t.Fatalf("validate work-to-Squad Space references: %v", err)
	}
	if crossSpaceReferences != 0 {
		t.Fatalf("found %d cross-Space Issue/Autopilot Squad references", crossSpaceReferences)
	}

	var issueASpaceAfter, issueBSpaceAfter string
	if err := pool.QueryRow(ctx, `SELECT space_id FROM issue WHERE id = $1`, issueAID).Scan(&issueASpaceAfter); err != nil {
		t.Fatalf("load Space A issue after migration: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT space_id FROM issue WHERE id = $1`, issueBID).Scan(&issueBSpaceAfter); err != nil {
		t.Fatalf("load Space B issue after migration: %v", err)
	}
	if issueASpaceAfter != spaceAID || issueBSpaceAfter != spaceBID {
		t.Fatalf("migration moved work: issue Spaces = %s/%s, want %s/%s", issueASpaceAfter, issueBSpaceAfter, spaceAID, spaceBID)
	}

	var quickSquadID, contextSquadID, quickSquadSpace string
	if err := pool.QueryRow(ctx, `
		SELECT t.squad_id, t.context->>'squad_id', s.space_id
		FROM agent_task_queue t JOIN squad s ON s.id = t.squad_id
		WHERE t.id = $1
	`, quickCreateTaskID).Scan(&quickSquadID, &contextSquadID, &quickSquadSpace); err != nil {
		t.Fatalf("load quick-create task after migration: %v", err)
	}
	if quickSquadID != contextSquadID || quickSquadSpace != spaceBID {
		t.Fatalf("quick-create Squad rewrite = id %s context %s Space %s, want matching id in Space %s", quickSquadID, contextSquadID, quickSquadSpace, spaceBID)
	}
}
