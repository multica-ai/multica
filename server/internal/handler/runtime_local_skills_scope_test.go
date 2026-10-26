package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestLocalSkillAgentScopeAccessAndDispatch(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := dbfx.Runtime(t, "hermes catalog", testutil.Cols{"provider": "hermes", "runtime_mode": "local", "daemon_id": "scope-daemon", "visibility": "public", "owner_id": testUserID})
	memberID := createRuntimeLocalSkillTestMember(t, "member")
	agentID := dbfx.Agent(t, "private Hermes catalog", runtimeID, testutil.Cols{
		"owner_id": testUserID, "permission_mode": "private",
		"custom_args": `["-p","alpha"]`, "custom_env": `{"HERMES_HOME":"/synthetic/hermes","TEAM_SKILLS":"/synthetic/team"}`,
	})
	initiate := func(userID, id string) *http.Request {
		return withURLParams(newRequestAsUser(userID, http.MethodPost, "/api/runtimes/"+runtimeID+"/local-skills?agent_id="+id, nil), "runtimeId", runtimeID)
	}
	testutil.Call(t, testHandler.InitiateListLocalSkills, initiate(memberID, agentID)).Want(http.StatusForbidden)
	var req RuntimeLocalSkillListRequest
	testutil.Call(t, testHandler.InitiateListLocalSkills, initiate(testUserID, agentID)).Want(http.StatusOK).JSON(&req)
	if req.AgentID != agentID || req.AgentUpdatedAt == "" {
		t.Fatalf("missing scope: %+v", req)
	}
	raw, _ := json.Marshal(req)
	if strings.Contains(string(raw), "synthetic") || strings.Contains(string(raw), "custom_env") {
		t.Fatal("catalog exposed agent configuration")
	}
	pending, err := testHandler.pendingLocalSkillList(ctx, &req)
	if err != nil {
		t.Fatal(err)
	}
	if pending.AgentScope == nil || pending.AgentScope.AgentID != agentID || len(pending.AgentScope.CustomArgs) != 2 || pending.AgentScope.CustomArgs[1] != "alpha" || pending.AgentScope.CustomEnv["TEAM_SKILLS"] != "/synthetic/team" {
		t.Fatalf("scope not dispatched: %+v", pending)
	}
	poll := func(userID string) *http.Request {
		return withURLParams(newRequestAsUser(userID, http.MethodGet, "/api/runtimes/"+runtimeID+"/local-skills/"+req.ID, nil), "runtimeId", runtimeID, "requestId", req.ID)
	}
	testutil.Call(t, testHandler.GetLocalSkillListRequest, poll(memberID)).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.GetLocalSkillListRequest, poll(testUserID)).Want(http.StatusOK)
	otherRuntime := dbfx.Runtime(t, "different runtime")
	otherAgent := dbfx.Agent(t, "different runtime agent", otherRuntime)
	testutil.Call(t, testHandler.InitiateListLocalSkills, initiate(testUserID, otherAgent)).Want(http.StatusConflict)
	if _, err := testPool.Exec(ctx, `UPDATE agent SET custom_args = '["-p","beta"]', updated_at = updated_at + interval '1 second' WHERE id = $1`, agentID); err != nil {
		t.Fatal(err)
	}
	if _, err := testHandler.pendingLocalSkillList(ctx, &req); err == nil {
		t.Fatal("dispatched stale agent configuration")
	}
	testutil.Call(t, testHandler.GetLocalSkillListRequest, poll(testUserID)).Want(http.StatusConflict)
}

func TestLocalSkillListStoresKeepAgentScopesSeparate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		store func(*testing.T) LocalSkillListStore
	}{
		{"memory", func(t *testing.T) LocalSkillListStore { return NewInMemoryLocalSkillListStore() }},
		{"redis", func(t *testing.T) LocalSkillListStore { return NewRedisLocalSkillListStore(newRedisTestClient(t)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := tc.store(t)
			ctx := context.Background()
			for _, agentID := range []string{"alpha", "beta", ""} {
				req, err := store.Create(ctx, LocalSkillListRequestInput{RuntimeID: "same-runtime", AgentID: agentID, AgentUpdatedAt: "revision"})
				if err != nil {
					t.Fatal(err)
				}
				pending, err := store.PopPending(ctx, "same-runtime")
				if err != nil || pending == nil || pending.ID != req.ID || pending.AgentID != agentID || pending.AgentUpdatedAt != "revision" {
					t.Fatalf("lost scope: %+v %v", pending, err)
				}
				if err := store.Complete(ctx, req.ID, []RuntimeLocalSkillSummary{{Key: agentID}}, true, nil, false); err != nil {
					t.Fatal(err)
				}
				got, err := store.Get(ctx, req.ID)
				if err != nil || got.AgentID != agentID || got.Skills[0].Key != agentID {
					t.Fatalf("mixed catalogs: %+v %v", got, err)
				}
			}
		})
	}
}

func TestLocalSkillAgentScopeRejectsUnscopedDaemonResult(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := dbfx.Runtime(t, "scoped catalog report", testutil.Cols{"provider": "hermes", "runtime_mode": "local", "daemon_id": "scope-daemon"})
	for _, returnedAgent := range []string{"", "different-agent", "selected-agent"} {
		req, err := testHandler.LocalSkillListStore.Create(context.Background(), LocalSkillListRequestInput{RuntimeID: runtimeID, AgentID: "selected-agent"})
		if err != nil {
			t.Fatal(err)
		}
		r := withURLParams(newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/local-skills/"+req.ID+"/result", map[string]any{
			"status": "completed", "agent_id": returnedAgent, "supported": true, "skills": []RuntimeLocalSkillSummary{{Key: "private-skill"}},
		}, testWorkspaceID, "scope-daemon"), "runtimeId", runtimeID, "requestId", req.ID)
		testutil.Call(t, testHandler.ReportLocalSkillListResult, r).Want(http.StatusOK)
		result, err := testHandler.LocalSkillListStore.Get(context.Background(), req.ID)
		if err != nil {
			t.Fatal(err)
		}
		if returnedAgent == "selected-agent" {
			if result.Status != RuntimeLocalSkillCompleted || len(result.Skills) != 1 {
				t.Fatalf("scoped result rejected: %+v", result)
			}
		} else if result.Status != RuntimeLocalSkillFailed || len(result.Skills) != 0 {
			t.Fatalf("unscoped catalog accepted: %+v", result)
		}
	}
}
