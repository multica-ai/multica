package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemonws"
)

// TestDaemonRPCHandler_TasksClaim pins the WS-first claim binding (MUL-4257):
// a tasks.claim RPC, driven with the WS connection's identity, reuses the HTTP
// claim handler and claims a queued task for the daemon's runtime.
func TestDaemonRPCHandler_TasksClaim(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	rt := createClaimReclaimRuntime(t, ctx, "WS claim rt")
	a, i := createClaimReclaimAgentAndIssue(t, ctx, rt, "WS claim agent")
	taskID := seedQueuedIssueTask(t, ctx, a, rt, i)

	identity := daemonws.ClientIdentity{
		DaemonID:     "ws-daemon",
		UserID:       testUserID,
		WorkspaceID:  testWorkspaceID,
		WorkspaceIDs: []string{testWorkspaceID},
		RuntimeIDs:   []string{rt},
	}
	body, _ := json.Marshal(map[string]any{
		"daemon_id":   "ws-daemon",
		"runtime_ids": []string{rt},
		"max_tasks":   5,
	})

	status, respBody, err := testHandler.DaemonRPCHandler(ctx, identity, "tasks.claim", body)
	if err != nil {
		t.Fatalf("DaemonRPCHandler: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", status, string(respBody))
	}
	var resp batchClaimReceiptResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Tasks) != 1 || resp.Tasks[0].ID != taskID {
		t.Fatalf("claimed %+v, want the queued task %s", resp.Tasks, taskID)
	}
	if resp.Tasks[0].RuntimeID != rt {
		t.Fatalf("claimed runtime = %s, want %s", resp.Tasks[0].RuntimeID, rt)
	}

	var dbStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&dbStatus); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if dbStatus != "dispatched" {
		t.Fatalf("task status = %s, want dispatched", dbStatus)
	}
}

// TestDaemonRPCHandler_UnknownMethod returns 404 for an unknown method.
func TestDaemonRPCHandler_TasksClaimReturnsAuthoritativePausedFence(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "WS paused claim runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(
		t,
		ctx,
		runtimeID,
		"WS paused claim agent",
	)
	taskID := seedQueuedIssueTask(t, ctx, agentID, runtimeID, issueID)

	var actionID string
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace_claim_intake_control
		SET state = 'paused',
		    generation = 13,
		    authoritative_action_id = gen_random_uuid(),
		    effective_at = now(),
		    updated_at = now()
		WHERE workspace_id = $1
		RETURNING authoritative_action_id::text
	`, testWorkspaceID).Scan(&actionID); err != nil {
		t.Fatalf("pause claim intake: %v", err)
	}
	t.Cleanup(func() {
		resetWorkspaceClaimIntakeForTest(t, testWorkspaceID)
	})

	identity := daemonws.ClientIdentity{
		DaemonID:     "ws-paused-daemon",
		UserID:       testUserID,
		WorkspaceID:  testWorkspaceID,
		WorkspaceIDs: []string{testWorkspaceID},
		RuntimeIDs:   []string{runtimeID},
	}
	body, _ := json.Marshal(map[string]any{
		"daemon_id":   identity.DaemonID,
		"runtime_ids": []string{runtimeID},
		"max_tasks":   1,
	})

	status, responseBody, err := testHandler.DaemonRPCHandler(
		ctx,
		identity,
		"tasks.claim",
		body,
	)
	if err != nil {
		t.Fatalf("DaemonRPCHandler: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", status, string(responseBody))
	}
	var response struct {
		Tasks            []json.RawMessage `json:"tasks"`
		PausedWorkspaces []struct {
			WorkspaceID string `json:"workspace_id"`
			State       string `json:"state"`
			Generation  int64  `json:"generation"`
			ActionID    string `json:"action_id"`
		} `json:"paused_workspaces"`
	}
	if err := json.Unmarshal(responseBody, &response); err != nil {
		t.Fatalf("decode paused RPC response: %v", err)
	}
	if len(response.Tasks) != 0 || len(response.PausedWorkspaces) != 1 {
		t.Fatalf("paused RPC response = %+v", response)
	}
	fence := response.PausedWorkspaces[0]
	if fence.WorkspaceID != testWorkspaceID ||
		fence.State != "paused" ||
		fence.Generation != 13 ||
		fence.ActionID != actionID {
		t.Fatalf("paused RPC fence = %+v", fence)
	}

	var taskStatus string
	if err := testPool.QueryRow(
		ctx,
		`SELECT status FROM agent_task_queue WHERE id = $1`,
		taskID,
	).Scan(&taskStatus); err != nil {
		t.Fatalf("load paused RPC task: %v", err)
	}
	if taskStatus != "queued" {
		t.Fatalf("paused RPC task status = %q, want queued", taskStatus)
	}
}

func TestDaemonRPCHandler_UnknownMethod(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	status, _, err := testHandler.DaemonRPCHandler(context.Background(),
		daemonws.ClientIdentity{DaemonID: "ws-daemon"}, "does.not.exist", nil)
	if status != http.StatusNotFound || err == nil {
		t.Fatalf("status=%d err=%v, want 404 + error", status, err)
	}
}
