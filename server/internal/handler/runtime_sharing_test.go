package handler

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func runtimeSharingPatch(t *testing.T, runtimeID, actorID string, body map[string]any) *testutil.Response {
	t.Helper()
	req := newRequestAs(actorID, http.MethodPatch, "/api/runtimes/"+runtimeID, body)
	return testutil.Call(t, testHandler.UpdateAgentRuntime, testutil.WithURLParams(req, "runtimeId", runtimeID))
}

func TestRuntimeSharingAccess(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database-backed fixture required")
	}
	rtID, ownerID, memberID := runtimeVisibilityFixture(t)
	var runtime AgentRuntimeResponse
	runtimeSharingPatch(t, rtID, ownerID, map[string]any{"shared_with": []string{memberID, memberID, ownerID}}).Want(http.StatusOK).JSON(&runtime)
	if runtime.Visibility != "private" || len(runtime.SharedWith) != 1 || runtime.SharedWith[0] != memberID {
		t.Fatalf("unexpected grant: %+v", runtime)
	}
	var visible []AgentRuntimeResponse
	testutil.Call(t, testHandler.ListAgentRuntimes, newRequestAs(memberID, http.MethodGet, "/api/runtimes", nil)).Want(http.StatusOK).JSON(&visible)
	found := false
	for _, rt := range visible {
		found = found || rt.ID == rtID
	}
	if !found {
		t.Fatal("shared runtime missing from recipient list")
	}
	for _, tc := range []struct {
		actor  string
		status int
	}{{ownerID, 200}, {memberID, 200}, {testUserID, 404}} {
		req := newRequestAs(tc.actor, http.MethodGet, "/api/runtimes/"+rtID+"/usage", nil)
		testutil.Call(t, testHandler.GetRuntimeUsage, testutil.WithURLParams(req, "runtimeId", rtID)).Want(tc.status)
	}
	for _, actor := range []string{memberID, testUserID} {
		runtimeSharingPatch(t, rtID, actor, map[string]any{"shared_with": []string{testUserID}}).Want(http.StatusForbidden)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE runtime_id = $1`, rtID) })
	testutil.Call(t, testHandler.CreateAgent, newRequestAs(memberID, http.MethodPost, "/api/agents", map[string]any{
		"name": "Shared runtime recipient", "runtime_id": rtID,
	})).Want(http.StatusCreated)
	testutil.Call(t, testHandler.CreateAgent, newRequestAs(testUserID, http.MethodPost, "/api/agents", map[string]any{
		"name": "Unselected runtime admin", "runtime_id": rtID,
	})).Want(http.StatusForbidden)
}

func TestRuntimeSharingRejectsInvalidRecipients(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database-backed fixture required")
	}
	rtID, ownerID, _ := runtimeVisibilityFixture(t)
	outsider := dbfx.Insert(t, "user", testutil.Cols{"name": "Outside workspace", "email": "runtime-sharing-outside@multica.test"})
	for _, recipients := range [][]string{{"bad-uuid"}, {outsider}} {
		runtimeSharingPatch(t, rtID, ownerID, map[string]any{"shared_with": recipients, "custom_name": "must not save"}).Want(http.StatusBadRequest)
	}
	req := newRequestAs(outsider, http.MethodPatch, "/api/runtimes/"+rtID, map[string]any{"shared_with": []string{outsider}})
	testutil.Call(t, testHandler.UpdateAgentRuntime, testutil.WithURLParams(req, "runtimeId", rtID)).Want(http.StatusNotFound)
	rt, err := testHandler.Queries.GetAgentRuntime(context.Background(), mustTestUUID(t, rtID))
	if err != nil || len(rt.SharedWith) != 0 || rt.CustomName.Valid {
		t.Fatalf("invalid input partially saved: %+v, %v", rt, err)
	}
}

func TestRuntimeSharingRevokesOnlyRemovedMembers(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database-backed fixture required")
	}
	for _, visibility := range []string{"public", "private"} {
		t.Run(visibility, func(t *testing.T) {
			rtID, ownerID, removedID := runtimeVisibilityFixture(t)
			dbfx.Exec(t, `UPDATE agent_runtime SET visibility = $2, shared_with = ARRAY[$3::uuid, $4::uuid] WHERE id = $1`, rtID, visibility, removedID, testUserID)
			removedAgent := createRuntimePrivacyAgent(t, rtID, removedID, "Removed member agent")
			keptAgent := createRuntimePrivacyAgent(t, rtID, testUserID, "Kept member agent")
			ownAgent := createRuntimePrivacyAgent(t, rtID, ownerID, "Runtime owner agent")
			removedTask := dbfx.Task(t, removedAgent, testutil.Cols{"runtime_id": rtID, "status": "running"})
			keptTask := dbfx.Task(t, keptAgent, testutil.Cols{"runtime_id": rtID, "status": "running"})
			var plan struct {
				Agents []struct {
					ID string `json:"id"`
				} `json:"nonowner_agents"`
				Tasks []string `json:"active_task_ids"`
			}
			runtimeSharingPatch(t, rtID, ownerID, map[string]any{"visibility": "private", "shared_with": []string{testUserID}}).Want(http.StatusConflict).JSON(&plan)
			if len(plan.Agents) != 1 || plan.Agents[0].ID != removedAgent || len(plan.Tasks) != 1 || plan.Tasks[0] != removedTask {
				t.Fatalf("wrong revocation plan: %+v", plan)
			}
			runtimePrivacyRequest(t, rtID, ownerID, map[string]any{
				"shared_with": []string{testUserID}, "expected_nonowner_agent_ids": []string{removedAgent}, "expected_task_ids": []string{},
			}).Want(http.StatusConflict)
			runtimePrivacyRequest(t, rtID, ownerID, map[string]any{
				"shared_with": []string{testUserID}, "expected_nonowner_agent_ids": []string{removedAgent}, "expected_task_ids": []string{removedTask},
			}).Want(http.StatusOK)
			for _, agentID := range []string{keptAgent, ownAgent} {
				a, err := testHandler.Queries.GetAgent(context.Background(), mustTestUUID(t, agentID))
				if err != nil || a.RuntimeID != mustTestUUID(t, rtID) {
					t.Fatalf("kept agent lost binding: %+v %v", a, err)
				}
			}
			for _, tc := range []struct{ id, status string }{{removedTask, "cancelled"}, {keptTask, "running"}} {
				task, err := testHandler.Queries.GetAgentTask(context.Background(), mustTestUUID(t, tc.id))
				if err != nil || task.Status != tc.status {
					t.Fatalf("task status=%s, want %s, err=%v", task.Status, tc.status, err)
				}
			}
			a, _ := testHandler.Queries.GetAgent(context.Background(), mustTestUUID(t, removedAgent))
			if a.RuntimeID.Valid {
				t.Fatal("removed agent remains bound")
			}
			req := newRequestAs(removedID, http.MethodGet, "/api/runtimes/"+rtID+"/usage", nil)
			testutil.Call(t, testHandler.GetRuntimeUsage, testutil.WithURLParams(req, "runtimeId", rtID)).Want(http.StatusNotFound)
		})
	}
}

func TestRuntimeSharingOldClientPrivateClearsGrants(t *testing.T) {
	rtID, ownerID, memberID := runtimeVisibilityFixture(t)
	runtimeSharingPatch(t, rtID, ownerID, map[string]any{"shared_with": []string{memberID}}).Want(http.StatusOK)
	var rt AgentRuntimeResponse
	runtimeSharingPatch(t, rtID, ownerID, map[string]any{"visibility": "private"}).Want(http.StatusOK).JSON(&rt)
	if len(rt.SharedWith) != 0 {
		t.Fatal("explicit old-client private request must clear grants")
	}
}

func TestRuntimeSharingMemberRemovalClearsGrant(t *testing.T) {
	rtID, ownerID, memberID := runtimeVisibilityFixture(t)
	runtimeSharingPatch(t, rtID, ownerID, map[string]any{"shared_with": []string{memberID}}).Want(http.StatusOK)
	agentID := createRuntimePrivacyAgent(t, rtID, memberID, "Departing member agent")
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": rtID, "status": "running"})

	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{
		WorkspaceID: mustTestUUID(t, testWorkspaceID), UserID: mustTestUUID(t, memberID),
	})
	if err != nil {
		t.Fatal(err)
	}
	req := newRequest(http.MethodDelete, "/api/workspaces/"+testWorkspaceID+"/members/"+uuidToString(member.ID), nil)
	testutil.Call(t, testHandler.DeleteMember, testutil.WithURLParams(req, "id", testWorkspaceID, "memberId", uuidToString(member.ID))).Want(http.StatusNoContent)
	rt, err := testHandler.Queries.GetAgentRuntime(context.Background(), mustTestUUID(t, rtID))
	if err != nil || len(rt.SharedWith) != 0 {
		t.Fatalf("departed member retained grant: %+v %v", rt.SharedWith, err)
	}
	task, err := testHandler.Queries.GetAgentTask(context.Background(), mustTestUUID(t, taskID))
	if err != nil || task.Status != "cancelled" {
		t.Fatalf("departed member's run survived: %+v %v", task, err)
	}
	a, err := testHandler.Queries.GetAgent(context.Background(), mustTestUUID(t, agentID))
	if err != nil || a.RuntimeID.Valid {
		t.Fatalf("departed member's agent remains bound: %+v %v", a, err)
	}

}

func TestRuntimeSharingClaimAndReclaim(t *testing.T) {
	for _, reclaim := range []bool{false, true} {
		t.Run(fmt.Sprint("reclaim=", reclaim), func(t *testing.T) {
			ctx := context.Background()
			memberID := dbfx.User(t, "Shared claim member", "shared-claim-"+uuid.NewString()+"@example.com")
			dbfx.Member(t, testWorkspaceID, memberID, "member")
			runtimeID := createClaimReclaimRuntime(t, ctx, "Shared claim runtime")
			agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Shared claim agent")
			dbfx.Exec(t, `UPDATE agent SET owner_id = $1 WHERE id = $2`, memberID, agentID)
			runtimeSharingPatch(t, runtimeID, testUserID, map[string]any{"shared_with": []string{memberID}}).Want(http.StatusOK)
			taskID := seedQueuedIssueTask(t, ctx, agentID, runtimeID, issueID)
			if reclaim {
				dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now() - interval '10 minutes', prepare_lease_expires_at = NULL WHERE id = $1`, taskID)
			}
			req := newOwnerDaemonRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil)
			var body struct {
				Task *struct {
					ID string `json:"id"`
				} `json:"task"`
			}
			testutil.Call(t, testHandler.ClaimTaskByRuntime, withURLParam(req, "runtimeId", runtimeID)).Want(http.StatusOK).JSON(&body)
			if body.Task == nil || body.Task.ID != taskID {
				t.Fatalf("shared member task not delivered: %+v", body.Task)
			}
		})
	}
}

func TestRuntimeSharingMemberRemovalContention(t *testing.T) {
	rtID, ownerID, memberID := runtimeVisibilityFixture(t)
	tx, err := testPool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(context.Background(), `SELECT id FROM member WHERE workspace_id = $1 AND user_id = $2 FOR UPDATE`, testWorkspaceID, memberID); err != nil {
		t.Fatal(err)
	}
	runtimeSharingPatch(t, rtID, ownerID, map[string]any{"shared_with": []string{memberID}}).Want(http.StatusConflict)
	rt, err := testHandler.Queries.GetAgentRuntime(context.Background(), mustTestUUID(t, rtID))
	if err != nil || len(rt.SharedWith) != 0 {
		t.Fatal("busy grant partially persisted")
	}
}
