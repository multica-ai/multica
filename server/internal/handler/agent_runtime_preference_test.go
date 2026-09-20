package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Every row is owned by the official handler test fixture and removed in reverse order.
func personalRuntimeFixture(t *testing.T) (agentID, ownerID, defaultID, personalID string) {
	t.Helper()
	ownerID = dbfx.User(t, "Shared agent owner", uuid.NewString()+"@runtime.test")
	dbfx.Member(t, testWorkspaceID, ownerID, "member")
	defaultID = dbfx.Runtime(t, "Shared default", testutil.Cols{"owner_id": ownerID, "provider": "claude"})
	personalID = dbfx.Runtime(t, "Personal runtime", testutil.Cols{"provider": "codex", "metadata": testutil.Raw(`'{"cli_version":"999.0.0"}'::jsonb`)})
	agentID = dbfx.Agent(t, "Shared identity", defaultID, testutil.Cols{"owner_id": ownerID, "permission_mode": "public_to", "model": "claude-sonnet-4-6"})
	dbfx.Insert(t, "agent_invocation_target", testutil.Cols{"agent_id": agentID, "target_type": "workspace", "target_id": testWorkspaceID})
	dbfx.Cleanup(t, `DELETE FROM agent_runtime_preference WHERE agent_id=$1`, agentID)
	return
}

func preferenceRequest(t *testing.T, agentID, userID, method string, body any) *testutil.Response {
	t.Helper()
	req := withURLParam(newRequestAs(userID, method, "/api/agents/"+agentID+"/runtime-preference", body), "id", agentID)
	if method == http.MethodGet {
		return testutil.Call(t, testHandler.GetAgentRuntimePreference, req)
	}
	return testutil.Call(t, testHandler.UpdateAgentRuntimePreference, req)
}

func TestAgentRuntimePreference(t *testing.T) {
	agentID, ownerID, defaultID, personalID := personalRuntimeFixture(t)
	preferenceRequest(t, agentID, testUserID, "PUT", map[string]any{}).Want(http.StatusBadRequest)
	preferenceRequest(t, agentID, testUserID, "PUT", map[string]any{"runtime_id": "invalid"}).Want(http.StatusBadRequest)
	preferenceRequest(t, agentID, testUserID, "PUT", map[string]any{"runtime_id": defaultID}).Want(http.StatusForbidden)
	var saved AgentRuntimePreferenceResponse
	preferenceRequest(t, agentID, testUserID, "PUT", map[string]any{"runtime_id": personalID, "model_mode": "custom", "model": "private-model", "max_concurrent_tasks": 3}).Want(http.StatusOK).JSON(&saved)
	if saved.RuntimeID == nil || *saved.RuntimeID != personalID || saved.Model != "private-model" || saved.MaxConcurrentTasks == nil || *saved.MaxConcurrentTasks != 3 {
		t.Fatalf("saved preference = %+v", saved)
	}
	for _, limit := range []any{0, 101, 1.5, "3"} {
		preferenceRequest(t, agentID, testUserID, "PUT", map[string]any{"runtime_id": personalID, "max_concurrent_tasks": limit}).Want(http.StatusBadRequest)
	}
	// Preferences must not change the shared agent or another viewer's response.
	for _, viewer := range []string{ownerID, testUserID} {
		var out AgentResponse
		testutil.Call(t, testHandler.GetAgent, withURLParam(newRequestAs(viewer, "GET", "/api/agents/"+agentID, nil), "id", agentID)).Want(http.StatusOK).JSON(&out)
		want := ""
		if viewer == testUserID {
			want = personalID
		}
		if (want == "" && out.PersonalRuntimeAvailability != "") || (want != "" && out.PersonalRuntimeAvailability != "online") {
			t.Fatalf("viewer %s saw personal availability=%s", viewer, out.PersonalRuntimeAvailability)
		}
		if out.RuntimeID != defaultID || out.PersonalRuntimeID != want {
			t.Fatalf("viewer %s saw default=%s personal=%s", viewer, out.RuntimeID, out.PersonalRuntimeID)
		}
	}
	// A provider change clears the previous custom model while retaining the personal limit.
	other := dbfx.Runtime(t, "Another provider", testutil.Cols{"provider": "claude"})
	saved = AgentRuntimePreferenceResponse{}
	preferenceRequest(t, agentID, testUserID, "PUT", map[string]any{"runtime_id": other}).Want(http.StatusOK).JSON(&saved)
	if saved.ModelMode != "runtime_default" || saved.Model != "" || saved.MaxConcurrentTasks == nil || *saved.MaxConcurrentTasks != 3 {
		t.Fatalf("provider switch retained incompatible settings: %+v", saved)
	}
	preferenceRequest(t, agentID, testUserID, "PUT", map[string]any{"runtime_id": nil}).Want(http.StatusOK).JSON(&saved)
	if saved.RuntimeID != nil {
		t.Fatal("reset retained personal route")
	}
}

func TestAgentRuntimePreferenceRequiresHumanInvocationPermission(t *testing.T) {
	agentID, ownerID, _, personalID := personalRuntimeFixture(t)
	dbfx.Exec(t, `UPDATE agent SET permission_mode='private' WHERE id=$1`, agentID)
	preferenceRequest(t, agentID, testUserID, "GET", nil).Want(http.StatusForbidden)
	preferenceRequest(t, agentID, testUserID, "PUT", map[string]any{"runtime_id": personalID}).Want(http.StatusForbidden)
	req := withURLParam(newRequestAs(ownerID, "GET", "/api/agents/"+agentID+"/runtime-preference", nil), "id", agentID)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", agentID)
	testutil.Call(t, testHandler.GetAgentRuntimePreference, req).Want(http.StatusForbidden)
	preferenceRequest(t, agentID, ownerID, "GET", nil).Want(http.StatusOK)
}

func TestPersonalRuntimePreservesDeletedSelection(t *testing.T) {
	agentID, _, _, personalID := personalRuntimeFixture(t)
	preferenceRequest(t, agentID, testUserID, "PUT", map[string]any{"runtime_id": personalID}).Want(http.StatusOK)
	dbfx.Exec(t, `DELETE FROM agent_runtime WHERE id=$1`, personalID)
	var out AgentRuntimePreferenceResponse
	preferenceRequest(t, agentID, testUserID, "GET", nil).Want(http.StatusOK).JSON(&out)
	if out.RuntimeID == nil || *out.RuntimeID != personalID {
		t.Fatal("deleted preference silently fell back")
	}
	agent, err := testHandler.Queries.GetAgent(context.Background(), parseUUID(agentID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = testHandler.TaskService.EffectiveRuntimeForUser(context.Background(), agent, parseUUID(testUserID)); err == nil {
		t.Fatal("deleted personal runtime was accepted")
	}
}

func TestRuntimePreferencesDeletedWithWorkspace(t *testing.T) {
	wsID := dbfx.Workspace(t, "Runtime preference cleanup", "runtime-preference-"+uuid.NewString())
	fx := testutil.New(testPool, wsID, testUserID)
	fx.Member(t, wsID, testUserID, "owner")
	runtimeID := fx.Runtime(t, "Cleanup runtime")
	agentID := fx.Agent(t, "Cleanup agent", runtimeID)
	fx.Exec(t, `INSERT INTO agent_runtime_preference(workspace_id,user_id,agent_id,runtime_id) VALUES($1,$2,$3,$4)`, wsID, testUserID, agentID, runtimeID)
	fx.Cleanup(t, `DELETE FROM agent_runtime_preference WHERE workspace_id=$1`, wsID)
	req := withURLParam(newRequest("DELETE", "/api/workspaces/"+wsID, nil), "id", wsID)
	req.Header.Set("X-Workspace-ID", wsID)
	testutil.Call(t, testHandler.DeleteWorkspace, req).Want(http.StatusNoContent)
	var count int
	fx.QueryRow(t, `SELECT count(*) FROM agent_runtime_preference WHERE workspace_id=$1`, wsID).Scan(&count)
	if count != 0 {
		t.Fatal("workspace deletion orphaned personal preferences")
	}
}

func TestRuntimePreferencesDeletedOnlyForRevokedMembership(t *testing.T) {
	fx := setupRevocationFixture(t, "runtime-preference-revoke", "runtime-preference-revoke")
	agentID, _, _, runtimeID := personalRuntimeFixture(t)
	dbfx.Member(t, testWorkspaceID, fx.TargetUserID, "member")
	dbfx.Exec(t, `INSERT INTO agent_runtime_preference(workspace_id,user_id,agent_id,runtime_id) VALUES($1,$2,$3,$4),($5,$2,$6,$7)`, fx.WorkspaceID, fx.TargetUserID, fx.AgentID, fx.RuntimeID, testWorkspaceID, agentID, runtimeID)
	dbfx.Cleanup(t, `DELETE FROM agent_runtime_preference WHERE user_id=$1`, fx.TargetUserID)
	req := withURLParams(newRequest("DELETE", "/api/workspaces/"+fx.WorkspaceID+"/members/"+fx.MemberID, nil), "id", fx.WorkspaceID, "memberId", fx.MemberID)
	req.Header.Set("X-Workspace-ID", fx.WorkspaceID)
	testutil.Call(t, testHandler.DeleteMember, req).Want(http.StatusNoContent)
	var deletedCount, otherCount int
	dbfx.QueryRow(t, `SELECT count(*) FILTER(WHERE workspace_id=$1),count(*) FILTER(WHERE workspace_id=$2) FROM agent_runtime_preference WHERE user_id=$3`, fx.WorkspaceID, testWorkspaceID, fx.TargetUserID).Scan(&deletedCount, &otherCount)
	if deletedCount != 0 || otherCount != 1 {
		t.Fatalf("preference cleanup deleted=%d other=%d", deletedCount, otherCount)
	}
}

func TestAgentRuntimePreferenceRejectsSystemAgent(t *testing.T) {
	agentID, _, _, personalID := personalRuntimeFixture(t)
	dbfx.Exec(t, `UPDATE agent SET kind='system' WHERE id=$1`, agentID)
	preferenceRequest(t, agentID, testUserID, "GET", nil).Want(http.StatusNotFound)
	preferenceRequest(t, agentID, testUserID, "PUT", map[string]any{"runtime_id": personalID}).Want(http.StatusNotFound)
	var count int
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_runtime_preference WHERE agent_id=$1`, agentID).Scan(&count)
	if count != 0 {
		t.Fatal("system agent acquired a personal runtime preference")
	}
}
