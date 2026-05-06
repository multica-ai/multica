package handler

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/localmode"
	"github.com/multica-ai/multica/server/internal/util"
)

// These tests exercise TaskService.ClaimTask through the handler package's
// DB fixture (the service package has no real-DB harness of its own). They
// pin the workspace-wide concurrency cap that fires only when local product
// mode is enabled.

// claimTestSetup creates three agents in the test workspace and returns
// their IDs as strings. Three agents are enough to host two unrelated
// "active" tasks plus a clean queued task on a third agent that has no
// active task at all — necessary because ClaimAgentTask groups same-agent
// + same-discriminator tasks and would otherwise refuse to claim a
// queued task while another active one shares the (NULL, NULL, NULL)
// discriminator on the same agent.
func claimTestSetup(t *testing.T, prefix string) (agentA, agentB, agentC string) {
	t.Helper()
	agentA = createHandlerTestAgent(t, prefix+"_a", []byte(`{}`))
	agentB = createHandlerTestAgent(t, prefix+"_b", []byte(`{}`))
	agentC = createHandlerTestAgent(t, prefix+"_c", []byte(`{}`))
	// Bump max_concurrent_tasks on each agent so the per-agent cap (set to
	// 1 by createHandlerTestAgent) doesn't fire before the workspace-wide
	// cap. The local-mode cap is the only thing under test here.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent SET max_concurrent_tasks = 10 WHERE id = ANY($1)`,
		[]string{agentA, agentB, agentC},
	); err != nil {
		t.Fatalf("bump agent caps: %v", err)
	}
	return agentA, agentB, agentC
}

// insertTask inserts an agent_task_queue row with the given status and
// returns the ID. The row is registered for cleanup via t.Cleanup.
func insertTask(t *testing.T, agentID, status string) string {
	t.Helper()
	ctx := context.Background()
	var id string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority)
		 VALUES ($1, $2, $3, 0)
		 RETURNING id`,
		agentID, testRuntimeID, status,
	).Scan(&id); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, id)
	})
	return id
}

// TestLocalConcurrencyCapBlocksClaim verifies the workspace-wide cap: with
// LocalMode enabled and 2 active tasks already running, a third claim
// returns nil/nil so the daemon will simply poll again later.
func TestLocalConcurrencyCapBlocksClaim(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentA, agentB, agentC := claimTestSetup(t, "local_concurrency_block")

	// Two active tasks (one each on agentA and agentB) — meets the cap.
	insertTask(t, agentA, "running")
	insertTask(t, agentB, "dispatched")

	// One queued task on a fresh agent that would otherwise be claimable
	// (no per-agent active conflict), but the workspace-wide cap must
	// block it.
	queued := insertTask(t, agentC, "queued")

	// Snapshot + restore LocalMode on the shared TaskService so the gate
	// fires for this test only.
	prev := testHandler.TaskService.LocalMode
	testHandler.TaskService.LocalMode = localmode.Config{ProductMode: "local"}
	t.Cleanup(func() { testHandler.TaskService.LocalMode = prev })

	agentUUID, err := util.ParseUUID(agentC)
	if err != nil {
		t.Fatalf("parse agent uuid: %v", err)
	}

	got, err := testHandler.TaskService.ClaimTask(context.Background(), agentUUID)
	if err != nil {
		t.Fatalf("ClaimTask returned error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected ClaimTask to return nil when local cap reached, got task %s",
			util.UUIDToString(got.ID))
	}

	// And the queued row must remain queued (not flipped to dispatched).
	var status string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status FROM agent_task_queue WHERE id = $1`, queued,
	).Scan(&status); err != nil {
		t.Fatalf("query queued status: %v", err)
	}
	if status != "queued" {
		t.Fatalf("queued task status changed to %q; expected to remain queued", status)
	}
}

// TestLocalConcurrencyAllowsClaimWhenUnderCap verifies the cap doesn't
// over-trigger: with one active task on a different agent and a queued
// task ready to claim, the queued task is claimed normally. The two
// tasks live on different agents to avoid the per-agent active-grouping
// rule in ClaimAgentTask (same-agent + same NULL discriminators).
func TestLocalConcurrencyAllowsClaimWhenUnderCap(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentA, agentB, _ := claimTestSetup(t, "local_concurrency_allow")

	insertTask(t, agentB, "running")
	queued := insertTask(t, agentA, "queued")

	prev := testHandler.TaskService.LocalMode
	testHandler.TaskService.LocalMode = localmode.Config{ProductMode: "local"}
	t.Cleanup(func() { testHandler.TaskService.LocalMode = prev })

	agentUUID, err := util.ParseUUID(agentA)
	if err != nil {
		t.Fatalf("parse agent uuid: %v", err)
	}

	got, err := testHandler.TaskService.ClaimTask(context.Background(), agentUUID)
	if err != nil {
		t.Fatalf("ClaimTask returned error: %v", err)
	}
	if got == nil {
		t.Fatalf("expected ClaimTask to claim queued task under cap, got nil")
	}
	if util.UUIDToString(got.ID) != queued {
		t.Fatalf("claimed unexpected task: got %s want %s",
			util.UUIDToString(got.ID), queued)
	}
}

// TestLocalConcurrencyDisabledByDefault verifies that with LocalMode off
// (cloud product mode), the workspace-wide cap does NOT fire. Two active
// tasks elsewhere in the workspace must NOT block a claim on a different
// agent. The fixture deliberately matches the cap-blocked count (2
// actives) so the only thing distinguishing the two cases is the
// LocalMode flag.
func TestLocalConcurrencyDisabledByDefault(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentA, agentB, agentC := claimTestSetup(t, "local_concurrency_disabled")

	// Match the cap-blocked fixture: two actives on different agents
	// plus a queued task on a fresh agent. With LocalMode disabled the
	// workspace count is irrelevant and the queued task must claim.
	insertTask(t, agentA, "running")
	insertTask(t, agentB, "dispatched")
	queued := insertTask(t, agentC, "queued")

	// Default test handler has empty LocalMode (cloud / non-local). Be
	// defensive — snapshot and restore in case earlier test runs leaked.
	prev := testHandler.TaskService.LocalMode
	testHandler.TaskService.LocalMode = localmode.Config{}
	t.Cleanup(func() { testHandler.TaskService.LocalMode = prev })

	agentUUID, err := util.ParseUUID(agentC)
	if err != nil {
		t.Fatalf("parse agent uuid: %v", err)
	}

	got, err := testHandler.TaskService.ClaimTask(context.Background(), agentUUID)
	if err != nil {
		t.Fatalf("ClaimTask returned error: %v", err)
	}
	if got == nil {
		t.Fatalf("expected ClaimTask to claim queued task in non-local mode, got nil")
	}
	if util.UUIDToString(got.ID) != queued {
		t.Fatalf("claimed unexpected task: got %s want %s",
			util.UUIDToString(got.ID), queued)
	}
}
