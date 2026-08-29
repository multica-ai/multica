package handler

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

const workspaceDeleteAgentToolDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"

func TestDeleteWorkspace_CleansAgentToolSafetyData(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	targetWorkspaceID := dbfx.Workspace(t, "Agent Tool Delete Target", "agent-tool-delete-target")
	neighborWorkspaceID := dbfx.Workspace(t, "Agent Tool Delete Neighbor", "agent-tool-delete-neighbor")
	target := testutil.New(testPool, targetWorkspaceID, testUserID)
	neighbor := testutil.New(testPool, neighborWorkspaceID, testUserID)

	target.Member(t, targetWorkspaceID, testUserID, "owner")
	neighbor.Member(t, neighborWorkspaceID, testUserID, "owner")
	insertWorkspaceAgentToolSafetyData(t, target, "target")
	insertWorkspaceAgentToolSafetyData(t, neighbor, "neighbor")

	request := newRequest(http.MethodDelete, "/api/workspaces/"+targetWorkspaceID, nil)
	request = withURLParam(request, "id", targetWorkspaceID)
	testutil.Call(t, testHandler.DeleteWorkspace, request).Want(http.StatusNoContent)

	tables := []string{
		"agent_tool_action_event",
		"agent_tool_approval_request",
		"agent_tool_policy_rule",
		"agent_tool_policy_revision",
		"agent_tool_policy",
	}
	for _, table := range tables {
		t.Run(table, func(t *testing.T) {
			query := fmt.Sprintf("SELECT count(*) FROM %s WHERE workspace_id = $1", table)
			if got := dbfx.Count(t, query, targetWorkspaceID); got != 0 {
				t.Fatalf("target workspace kept %d %s rows after deletion", got, table)
			}
			if got := dbfx.Count(t, query, neighborWorkspaceID); got != 1 {
				t.Fatalf("neighbor workspace has %d %s rows after target deletion, want 1", got, table)
			}
		})
	}
}

func insertWorkspaceAgentToolSafetyData(t *testing.T, fixture *testutil.Fixture, key string) {
	t.Helper()

	runtimeID := fixture.Runtime(t, "Agent Tool Delete "+key)
	agentID := fixture.Agent(t, "Agent Tool Delete "+key, runtimeID)
	taskID := fixture.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID})
	policyID := fixture.Insert(t, "agent_tool_policy", testutil.Cols{
		"workspace_id":       fixture.WorkspaceID,
		"agent_id":           agentID,
		"revision":           1,
		"status":             "active",
		"policy_digest":      workspaceDeleteAgentToolDigest,
		"created_by_user_id": fixture.UserID,
		"updated_by_user_id": fixture.UserID,
	})
	fixture.Insert(t, "agent_tool_policy_revision", testutil.Cols{
		"workspace_id":    fixture.WorkspaceID,
		"agent_id":        agentID,
		"revision":        1,
		"status":          "active",
		"policy_digest":   workspaceDeleteAgentToolDigest,
		"actor_user_id":   fixture.UserID,
		"rule_identities": testutil.Raw("'[]'::jsonb"),
	})
	fixture.Insert(t, "agent_tool_policy_rule", testutil.Cols{
		"workspace_id":   fixture.WorkspaceID,
		"agent_id":       agentID,
		"policy_id":      policyID,
		"transport_kind": "managed_mcp",
		"server_key":     "workspace-delete",
		"tool_name":      "run",
		"schema_digest":  workspaceDeleteAgentToolDigest,
		"effect":         "require_approval",
	})
	approvalID := fixture.Insert(t, "agent_tool_approval_request", testutil.Cols{
		"workspace_id":    fixture.WorkspaceID,
		"agent_id":        agentID,
		"task_id":         taskID,
		"invocation_id":   testutil.Raw("gen_random_uuid()"),
		"idempotency_key": key + "-workspace-delete",
		"transport_kind":  "managed_mcp",
		"server_key":      "workspace-delete",
		"tool_name":       "run",
		"schema_digest":   workspaceDeleteAgentToolDigest,
		"policy_revision": 1,
		"expires_at":      testutil.Raw("now() + interval '1 hour'"),
	})
	fixture.Insert(t, "agent_tool_action_event", testutil.Cols{
		"workspace_id":        fixture.WorkspaceID,
		"agent_id":            agentID,
		"task_id":             taskID,
		"invocation_id":       testutil.Raw("gen_random_uuid()"),
		"approval_request_id": approvalID,
		"transport_kind":      "managed_mcp",
		"server_key":          "workspace-delete",
		"tool_name":           "run",
		"schema_digest":       workspaceDeleteAgentToolDigest,
		"coverage_kind":       "managed_mcp",
		"event_type":          "approval_requested",
		"outcome_code":        "approval_required",
	})
}
