package handler

import (
	"context"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"reflect"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/connectedapps"
)

func TestManagedMcpClaimOverlayAuthorizationFailureMatchesDeliveredConfig(t *testing.T) {
	withComposioMCPAppsFlag(t, testHandler, true)
	runtimeID := setupWorkspaceMcpClaimFixture(t, context.Background(), "overlay authorization failure", `{"mcpServers":{"own":{}}}`)
	// Invalid gateway URL triggers AuthorizeOverlay's decoder error. The
	// existing claim contract still attempts the original overlay merge.
	dbfx.Exec(t, `UPDATE agent_task_queue SET runtime_mcp_overlay='{"mcpServers":{"multica_apps":{"url":123}}}' WHERE runtime_id=$1`, runtimeID)
	h := *testHandler
	h.ConnectedApps = &connectedapps.Service{}
	response := claimMcpHistoryForTest(t, &h, runtimeID)
	want := []ManagedMcpSource{{Name: "multica_apps", Source: "task"}, {Name: "own", Source: "agent"}}
	if response.ManagedMcpSources == nil || !reflect.DeepEqual(*response.ManagedMcpSources, want) {
		t.Fatal("snapshot does not reflect actual overlay fallback")
	}
	if string(response.Agent.McpConfig) == "" {
		t.Fatal("existing runtime payload changed")
	}
}

func TestManagedMcpFailedClaimDoesNotPersistSources(t *testing.T) {
	ctx := context.Background()
	runtimeID := setupWorkspaceMcpClaimFixture(t, ctx, "failed delivery snapshot", `{"mcpServers":{"never-delivered":{}}}`)
	task, err := testHandler.TaskService.ClaimTaskForRuntime(ctx, parseUUID(runtimeID))
	if err != nil || task == nil {
		t.Fatalf("claim=%v %v", task, err)
	}
	foreign := dbfx.User(t, "Changed runtime owner", "changed-mcp-runtime-owner@example.test")
	dbfx.Exec(t, `UPDATE agent_runtime SET owner_id=$1 WHERE id=$2`, foreign, runtimeID)
	req := newOwnerDaemonRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil)
	_, _, _, _, failure, err := testHandler.finalizeClaimDeliveryForTest(req, task, runtimeID, testWorkspaceID)
	if err != nil || failure == nil || !failure.settled {
		t.Fatalf("expected settled authorization failure: %v %v", failure, err)
	}
	var snapshot []byte
	dbfx.QueryRow(t, `SELECT managed_mcp_snapshot FROM agent_task_queue WHERE id=$1`, task.ID).Scan(&snapshot)
	if len(snapshot) != 0 {
		t.Fatal("failed delivery persisted undelivered provenance")
	}
}

func TestManagedMcpSnapshotRollsBackWithClaimToken(t *testing.T) {
	ctx := context.Background()
	runtimeID := setupWorkspaceMcpClaimFixture(t, ctx, "token rollback snapshot", `{"mcpServers":{"rollback":{}}}`)
	first := claimMcpHistoryForTest(t, testHandler, runtimeID)
	old, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(first.ID))
	if err != nil {
		t.Fatal(err)
	}
	var tokenID, tokenHash string
	dbfx.QueryRow(t, `SELECT id,token_hash FROM task_token WHERE task_id=$1 LIMIT 1`, first.ID).Scan(&tokenID, &tokenHash)
	if _, err := testHandler.TaskService.RequeueTaskAfterClaimFailure(ctx, old); err != nil {
		t.Fatal(err)
	}
	task, err := testHandler.TaskService.ClaimTaskForRuntime(ctx, parseUUID(runtimeID))
	if err != nil || task == nil {
		t.Fatalf("claim=%v %v", task, err)
	}
	runtime, err := testHandler.Queries.GetAgentRuntimeForWorkspace(ctx, db.GetAgentRuntimeForWorkspaceParams{ID: parseUUID(runtimeID), WorkspaceID: parseUUID(testWorkspaceID)})
	if err != nil {
		t.Fatal(err)
	}
	req := newOwnerDaemonRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil)
	response, _, snapshot, _, _, failure := testHandler.buildClaimedTaskResponse(req, task, runtime, runtimeID, testWorkspaceID)
	if failure != nil {
		t.Fatalf("build failed: %+v", failure)
	}
	// Reusing the previous token primary key fails after the snapshot write
	// inside the same transaction, exercising rollback after partial progress.
	_, _, err = testHandler.finalizeClaimDelivery(ctx, task, runtime, runtimeID, testWorkspaceID, &response, db.CreateTaskTokenParams{
		ID: parseUUID(tokenID), TokenHash: tokenHash, TaskID: task.ID, AgentID: task.AgentID,
		WorkspaceID: parseUUID(testWorkspaceID), UserID: runtime.OwnerID,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}, nil, false, snapshot)
	if err == nil {
		t.Fatal("expected token insert failure")
	}
	fresh, err := testHandler.Queries.GetAgentTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh.ManagedMcpSnapshot) != 0 {
		t.Fatal("source snapshot survived failed token transaction")
	}
}
