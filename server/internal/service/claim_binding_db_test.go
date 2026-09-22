package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestClaimAgentTaskHonorsCurrentBindings pins invariant I4 against a real
// database: a task's persisted runtime is authority for claiming only while it
// is still one of the agent's current bindings. This is what lets a quota
// fallback pin a non-priority-0 runtime and still have the task claimed, while
// keeping a task pinned to a runtime the agent is no longer bound to orphaned.
//
// Both fences that gate delivery must agree, so the test drives each scenario
// through ListQueuedClaimCandidatesByRuntime (what the daemon polls) and
// ClaimAgentTask (what actually dispatches):
//
//   - a task pinned to a non-priority-0 binding is a candidate and is claimed;
//   - a task pinned to a runtime absent from the pool is neither;
//   - a legacy agent with no binding rows still claims on its agent.runtime_id.
func TestClaimAgentTaskHonorsCurrentBindings(t *testing.T) {
	pool := sharedTestPool(t)
	ctx := context.Background()
	q := db.New(pool)

	suffix := time.Now().UnixNano()
	userID := insertBindingTeardownRow(t, pool, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Claim Binding", fmt.Sprintf("claim-binding-%d@multica.ai", suffix))
	workspaceID := insertBindingTeardownRow(t, pool, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, '', 'CBD') RETURNING id`,
		"Claim Binding", fmt.Sprintf("claim-binding-%d", suffix))
	execBindingTeardown(t, pool, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, workspaceID, userID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM member WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, workspaceID)
		_, _ = pool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID)
	})

	newTask := func(agentID, runtimeID string) string {
		id := insertBindingTeardownRow(t, pool, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
			VALUES ($1, $2, NULL, 'queued', 0) RETURNING id`, agentID, runtimeID)
		t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, id) })
		return id
	}
	claim := func(agentID, runtimeID string) (db.AgentTaskQueue, error) {
		return q.ClaimAgentTask(ctx, db.ClaimAgentTaskParams{
			PrepareLeaseSecs: 60,
			AgentID:          util.MustParseUUID(agentID),
			RuntimeID:        util.MustParseUUID(runtimeID),
			RuntimeStaleSecs: 3600,
		})
	}
	candidateFor := func(runtimeID, taskID string) bool {
		rows, err := q.ListQueuedClaimCandidatesByRuntime(ctx, util.MustParseUUID(runtimeID))
		if err != nil {
			t.Fatalf("list claim candidates: %v", err)
		}
		for _, r := range rows {
			if util.UUIDToString(r.ID) == taskID {
				return true
			}
		}
		return false
	}

	// Scenario A: agent bound to an ordered pool [r0 (priority 0), r1]. A task
	// pinned to the non-priority-0 runtime r1 must be claimable — this is a
	// fallback pin.
	r0 := insertRuntimeForBindingTeardown(t, pool, workspaceID, userID, "A r0")
	r1 := insertRuntimeForBindingTeardown(t, pool, workspaceID, userID, "A r1")
	agentA := insertAgentForBindingTeardown(t, pool, workspaceID, userID, "agent A", r0)
	insertBinding(t, pool, workspaceID, agentA, r0, 0)
	insertBinding(t, pool, workspaceID, agentA, r1, 1)
	taskA := newTask(agentA, r1)

	if !candidateFor(r1, taskA) {
		t.Fatalf("fallback-pinned task %s not listed as a claim candidate for its bound runtime r1", taskA)
	}
	gotA, err := claim(agentA, r1)
	if err != nil {
		t.Fatalf("claim fallback-pinned task on non-priority-0 binding: %v", err)
	}
	if util.UUIDToString(gotA.ID) != taskA || gotA.Status != "dispatched" {
		t.Fatalf("claim A returned id=%s status=%q, want %s/dispatched", util.UUIDToString(gotA.ID), gotA.Status, taskA)
	}

	// Scenario B: agent bound only to rb0. A task pinned to an online runtime
	// that is NOT in the agent's pool (rx) must be neither a candidate nor
	// claimable — the rebind-orphan guard.
	rb0 := insertRuntimeForBindingTeardown(t, pool, workspaceID, userID, "B r0")
	rx := insertRuntimeForBindingTeardown(t, pool, workspaceID, userID, "B rx (unbound)")
	agentB := insertAgentForBindingTeardown(t, pool, workspaceID, userID, "agent B", rb0)
	insertBinding(t, pool, workspaceID, agentB, rb0, 0)
	taskB := newTask(agentB, rx)

	if candidateFor(rx, taskB) {
		t.Fatalf("task %s pinned to an unbound runtime rx was listed as a claim candidate", taskB)
	}
	if _, err := claim(agentB, rx); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("claim on unbound runtime rx returned err=%v, want pgx.ErrNoRows (no claim)", err)
	}

	// Scenario C: legacy agent with no binding rows. Claiming falls back to the
	// singleton pool implied by agent.runtime_id.
	rc0 := insertRuntimeForBindingTeardown(t, pool, workspaceID, userID, "C r0")
	agentC := insertAgentForBindingTeardown(t, pool, workspaceID, userID, "agent C", rc0)
	taskC := newTask(agentC, rc0)

	if !candidateFor(rc0, taskC) {
		t.Fatalf("legacy singleton task %s not listed as a claim candidate", taskC)
	}
	gotC, err := claim(agentC, rc0)
	if err != nil {
		t.Fatalf("claim legacy singleton task: %v", err)
	}
	if util.UUIDToString(gotC.ID) != taskC || gotC.Status != "dispatched" {
		t.Fatalf("claim C returned id=%s status=%q, want %s/dispatched", util.UUIDToString(gotC.ID), gotC.Status, taskC)
	}
}
