package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/pkg/protocol"
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

func TestDaemonRPCHandler_TasksClaimV2ReplaysSameAttempt(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	rt := createClaimReclaimRuntime(t, ctx, "WS claim v2 rt")
	a, i := createClaimReclaimAgentAndIssue(t, ctx, rt, "WS claim v2 agent")
	taskID := seedQueuedIssueTask(t, ctx, a, rt, i)
	attemptID := uuid.NewString()
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM daemon_claim_attempt WHERE id = $1`, attemptID)
	})
	identity := daemonws.ClientIdentity{
		DaemonID:     "ws-daemon-v2",
		UserID:       testUserID,
		WorkspaceID:  testWorkspaceID,
		WorkspaceIDs: []string{testWorkspaceID},
		RuntimeIDs:   []string{rt},
		Capabilities: protocol.DaemonCapabilityClaimReplayV1,
	}
	body, _ := json.Marshal(map[string]any{
		"claim_attempt_id": attemptID,
		"daemon_id":        "ws-daemon-v2",
		"runtime_ids":      []string{rt},
		"max_tasks":        1,
	})

	var ids []string
	for call := 0; call < 2; call++ {
		status, raw, err := testHandler.DaemonRPCHandler(ctx, identity, "tasks.claim.v2", body)
		if err != nil || status != http.StatusOK {
			t.Fatalf("call %d: status=%d err=%v body=%s", call, status, err, raw)
		}
		var resp batchClaimResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			t.Fatalf("call %d decode: %v", call, err)
		}
		if resp.ClaimAttemptID != attemptID || len(resp.Tasks) != 1 {
			t.Fatalf("call %d response = %+v", call, resp)
		}
		ids = append(ids, resp.Tasks[0].ID)
	}
	if ids[0] != taskID || ids[1] != taskID {
		t.Fatalf("v2 WS replay ids = %v, want %s twice", ids, taskID)
	}
}

// TestDaemonRPCHandler_UnknownMethod returns 404 for an unknown method.
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
