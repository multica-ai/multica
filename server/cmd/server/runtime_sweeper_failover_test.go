package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/dwickyfp/wallts/server/pkg/db/generated"
)

// TestReRouteTasksForOfflineRuntime tests the failover re-routing logic:
// 1. Runtime in a failover group goes offline
// 2. Tasks should be re-routed to the highest-priority online fallback
// 3. Runtime with no failover group should return nil (no re-routing)
func TestReRouteTasksForOfflineRuntime(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)

	// Create a failover group
	var groupID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO runtime_failover_group (workspace_id, name, strategy)
		SELECT id, 'test-failover-group', 'priority'
		FROM workspace LIMIT 1
		RETURNING id
	`).Scan(&groupID)
	if err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM runtime_failover_group WHERE id = $1`, groupID)
	})

	// Find a runtime to use as the offline one
	var offlineRuntimeID string
	err = testPool.QueryRow(ctx, `
		SELECT id FROM agent_runtime WHERE status = 'online' LIMIT 1
	`).Scan(&offlineRuntimeID)
	if err != nil {
		t.Fatalf("failed to find a runtime: %v", err)
	}

	// Set it to the failover group with priority 0
	_, err = testPool.Exec(ctx, `
		UPDATE agent_runtime SET failover_group_id = $1, priority = 0
		WHERE id = $2
	`, groupID, offlineRuntimeID)
	if err != nil {
		t.Fatalf("failed to set failover group: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `UPDATE agent_runtime SET failover_group_id = NULL, priority = 0 WHERE id = $1`, offlineRuntimeID)
	})

	// Mark the runtime offline
	_, err = testPool.Exec(ctx, `
		UPDATE agent_runtime SET status = 'offline' WHERE id = $1
	`, offlineRuntimeID)
	if err != nil {
		t.Fatalf("failed to mark runtime offline: %v", err)
	}

	// Try re-routing — should return nil since there's no other online runtime in the group
	offlineUUID := parseUUID(offlineRuntimeID)
	rerouted, err := reRouteTasksForOfflineRuntime(ctx, queries, offlineUUID)
	if err != nil {
		t.Fatalf("reRouteTasksForOfflineRuntime failed: %v", err)
	}
	if len(rerouted) != 0 {
		t.Errorf("expected 0 rerouted tasks (no fallback), got %d", len(rerouted))
	}

	// Restore runtime status
	testPool.Exec(ctx, `UPDATE agent_runtime SET status = 'online' WHERE id = $1`, offlineRuntimeID)
}

// TestReRouteTasksNoFailoverGroup verifies that runtimes without a failover
// group are skipped (nil return, no error).
func TestReRouteTasksNoFailoverGroup(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)

	var runtimeID string
	err := testPool.QueryRow(ctx, `
		SELECT id FROM agent_runtime WHERE failover_group_id IS NULL LIMIT 1
	`).Scan(&runtimeID)
	if err != nil {
		t.Skip("no runtime without failover group available")
	}

	rerouted, err := reRouteTasksForOfflineRuntime(ctx, queries, parseUUID(runtimeID))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rerouted != nil {
		t.Errorf("expected nil for runtime without failover group, got %d tasks", len(rerouted))
	}
}

// TestReRouteTasksWithFallback tests the full failover scenario:
// 1. Two runtimes in the same failover group (priority 10 and 0)
// 2. Higher priority runtime goes offline with queued tasks
// 3. Tasks should be re-routed to the lower-priority runtime
func TestReRouteTasksWithFallback(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)

	// Create a failover group
	var groupID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO runtime_failover_group (workspace_id, name, strategy)
		SELECT id, 'test-failover-fallback', 'priority'
		FROM workspace LIMIT 1
		RETURNING id
	`).Scan(&groupID)
	if err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM runtime_failover_group WHERE id = $1`, groupID)
	})

	// Find two runtimes
	var primaryID, fallbackID string
	rows, err := testPool.Query(ctx, `SELECT id FROM agent_runtime WHERE status = 'online' LIMIT 2`)
	if err != nil {
		t.Fatalf("failed to query runtimes: %v", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	if len(ids) < 2 {
		t.Skip("need at least 2 runtimes for this test")
	}
	primaryID, fallbackID = ids[0], ids[1]

	// Assign both to the failover group with different priorities
	testPool.Exec(ctx, `UPDATE agent_runtime SET failover_group_id = $1, priority = 10 WHERE id = $2`, groupID, primaryID)
	testPool.Exec(ctx, `UPDATE agent_runtime SET failover_group_id = $1, priority = 0 WHERE id = $2`, groupID, fallbackID)
	t.Cleanup(func() {
		testPool.Exec(ctx, `UPDATE agent_runtime SET failover_group_id = NULL, priority = 0 WHERE id = $1`, primaryID)
		testPool.Exec(ctx, `UPDATE agent_runtime SET failover_group_id = NULL, priority = 0 WHERE id = $1`, fallbackID)
	})

	// Create a test issue and queued task on the primary runtime
	var agentID string
	err = testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE runtime_id = $1 AND archived_at IS NULL LIMIT 1
	`, primaryID).Scan(&agentID)
	if err != nil {
		t.Skip("no agent on primary runtime")
	}

	var issueID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id)
		SELECT a.workspace_id, 'Failover test issue', 'todo', 'none', 'member', m.user_id, 'agent', $1
		FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		WHERE a.id = $1
		LIMIT 1
		RETURNING id
	`, agentID).Scan(&issueID)
	if err != nil {
		t.Fatalf("failed to create test issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	var taskID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0)
		RETURNING id
	`, agentID, primaryID, issueID).Scan(&taskID)
	if err != nil {
		t.Fatalf("failed to create queued task: %v", err)
	}

	// Mark primary offline
	testPool.Exec(ctx, `UPDATE agent_runtime SET status = 'offline' WHERE id = $1`, primaryID)
	t.Cleanup(func() {
		testPool.Exec(ctx, `UPDATE agent_runtime SET status = 'online' WHERE id = $1`, primaryID)
	})

	// Attempt re-routing
	rerouted, err := reRouteTasksForOfflineRuntime(ctx, queries, parseUUID(primaryID))
	if err != nil {
		t.Fatalf("reRouteTasksForOfflineRuntime failed: %v", err)
	}
	if len(rerouted) == 0 {
		t.Fatal("expected task to be re-routed, got 0")
	}

	// Verify the task was moved to the fallback runtime
	var newRuntimeID string
	err = testPool.QueryRow(ctx, `SELECT runtime_id::text FROM agent_task_queue WHERE id = $1`, taskID).Scan(&newRuntimeID)
	if err != nil {
		t.Fatalf("failed to read re-routed task: %v", err)
	}
	if newRuntimeID != fallbackID {
		t.Errorf("expected task on fallback runtime %s, got %s", fallbackID, newRuntimeID)
	}

	// Verify task status was reset to queued
	var status string
	testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status)
	if status != "queued" {
		t.Errorf("expected status 'queued' after re-route, got '%s'", status)
	}
}

func parseUUID(s string) pgtype.UUID {
	var u pgtype.UUID
	u.Scan(s)
	return u
}
