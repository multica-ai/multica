package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TestClaimTaskByRuntimeForwardsPlatformRuntimeConfigRaw is a regression guard
// for the existing claim wire. The Platform integration must reuse this field,
// not introduce a second Extension-specific payload channel.
func TestClaimTaskByRuntimeForwardsPlatformRuntimeConfigRaw(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Platform runtime config claim")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Platform runtime config agent")
	wantRaw := json.RawMessage(`{
  "platform_agent": {
    "schema_version": "platform-agent.runtime-context/v1",
    "extension": {"key":"research-team","version":"1.0.0","release_id":"release-1","digest":"sha256:abc"},
    "agent": {"source_key":"lead-researcher"},
    "commands": [{"name":"summarize","description":"Summary command.","content":"Summarize findings.","metadata":{"owner":"platform"}}]
  }
}`)
	if _, err := testPool.Exec(ctx, `UPDATE agent SET runtime_config = $2::jsonb WHERE id = $1`, agentID, wantRaw); err != nil {
		t.Fatalf("set runtime_config: %v", err)
	}
	seedQueuedIssueTask(t, ctx, agentID, runtimeID, issueID)

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "platform-runtime-config-claim")
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: %d %s", w.Code, w.Body.String())
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var taskWire map[string]json.RawMessage
	if err := json.Unmarshal(envelope["task"], &taskWire); err != nil {
		t.Fatalf("decode task map: %v: %s", err, w.Body.String())
	}
	var agentWire map[string]json.RawMessage
	if err := json.Unmarshal(taskWire["agent"], &agentWire); err != nil {
		t.Fatalf("decode agent map: %v: %s", err, w.Body.String())
	}
	allowedAgentKeys := map[string]struct{}{
		"id": {}, "name": {}, "instructions": {}, "skills": {}, "skill_refs": {},
		"custom_env": {}, "custom_args": {}, "mcp_config": {}, "model": {},
		"thinking_level": {}, "service_tier": {}, "disabled_runtime_skills": {},
		"runtime_config": {},
	}
	for key := range agentWire {
		if _, ok := allowedAgentKeys[key]; !ok {
			t.Fatalf("claim introduced platform runtime sibling %q beside runtime_config: %s", key, w.Body.String())
		}
	}
	runtimeConfigRaw, ok := agentWire["runtime_config"]
	if !ok {
		t.Fatalf("claim omitted existing agent.runtime_config wire: %s", w.Body.String())
	}
	var got, want any
	if err := json.Unmarshal(runtimeConfigRaw, &got); err != nil {
		t.Fatalf("claim runtime_config is invalid: %v", err)
	}
	if err := json.Unmarshal(wantRaw, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("claim runtime_config = %#v, want %#v", got, want)
	}
}

func TestClaimTaskByRuntimePlatformPreservesExactBoundSkills(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	for _, test := range []struct {
		name                  string
		provider              string
		wantPlatformExactOnly bool
	}{
		{name: "platform", provider: platformExtensionProvider, wantPlatformExactOnly: true},
		{name: "platform-prefix-is-legacy", provider: platformExtensionProvider + "-v2"},
		{name: "legacy", provider: "handler_test_runtime"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			runtimeID := createClaimReclaimRuntime(t, ctx, "Claim skill contract "+test.name)
			if _, err := testPool.Exec(ctx, `UPDATE agent_runtime SET provider = $2 WHERE id = $1`, runtimeID, test.provider); err != nil {
				t.Fatalf("set runtime provider: %v", err)
			}
			agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Claim skill contract "+test.name)

			var skillID string
			boundName := "claim-bound-" + test.name
			if err := testPool.QueryRow(ctx, `
				INSERT INTO skill (workspace_id, name, description, content, config, created_by)
				VALUES ($1, $2, 'bound skill', 'bound content', '{}'::jsonb, $3)
				RETURNING id
			`, testWorkspaceID, boundName, testUserID).Scan(&skillID); err != nil {
				t.Fatalf("create bound skill: %v", err)
			}
			if _, err := testPool.Exec(ctx, `INSERT INTO agent_skill (agent_id, skill_id) VALUES ($1, $2)`, agentID, skillID); err != nil {
				t.Fatalf("bind skill: %v", err)
			}
			t.Cleanup(func() {
				testPool.Exec(ctx, `DELETE FROM agent_skill WHERE agent_id = $1 AND skill_id = $2`, agentID, skillID)
				testPool.Exec(ctx, `DELETE FROM skill WHERE id = $1`, skillID)
			})
			seedQueuedIssueTask(t, ctx, agentID, runtimeID, issueID)

			w := httptest.NewRecorder()
			req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "claim-skill-contract-"+test.name)
			req = withURLParam(req, "runtimeId", runtimeID)
			testHandler.ClaimTaskByRuntime(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("ClaimTaskByRuntime: %d %s", w.Code, w.Body.String())
			}
			var response struct {
				Task *AgentTaskResponse `json:"task"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode claim: %v", err)
			}
			if response.Task == nil || response.Task.Agent == nil {
				t.Fatalf("claim omitted task agent: %s", w.Body.String())
			}
			got := response.Task.Agent.Skills
			if len(got) == 0 || got[0].ID != skillID || got[0].Name != boundName || got[0].Content != "bound content" {
				t.Fatalf("bound skill changed or reordered: %+v", got)
			}
			if test.wantPlatformExactOnly {
				if len(got) != 1 {
					t.Fatalf("Platform claim skills = %+v, want exactly the bound Extension skill", got)
				}
				return
			}

			wantBuiltins := testHandler.TaskService.BuiltinSkills()
			if len(wantBuiltins) == 0 {
				t.Fatal("legacy regression fixture requires built-in skills")
			}
			if !reflect.DeepEqual(got[1:], wantBuiltins) {
				t.Fatalf("legacy built-ins changed:\ngot=%+v\nwant=%+v", got[1:], wantBuiltins)
			}
		})
	}
}

func TestNegotiatedSkillBundleClaimsPreservePlatformBoundSkills(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	for _, test := range []struct {
		name                  string
		provider              string
		batch                 bool
		wantPlatformExactOnly bool
	}{
		{name: "platform-single", provider: platformExtensionProvider, wantPlatformExactOnly: true},
		{name: "platform-batch", provider: platformExtensionProvider, batch: true, wantPlatformExactOnly: true},
		{name: "platform-prefix-is-legacy", provider: platformExtensionProvider + "-v2"},
		{name: "legacy-single", provider: "handler_test_runtime"},
		{name: "legacy-batch", provider: "handler_test_runtime", batch: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			runtimeID := createClaimReclaimRuntime(t, ctx, "Negotiated skill contract "+test.name)
			if _, err := testPool.Exec(ctx, `UPDATE agent_runtime SET provider = $2 WHERE id = $1`, runtimeID, test.provider); err != nil {
				t.Fatalf("set runtime provider: %v", err)
			}
			agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Negotiated skill contract "+test.name)

			var skillID string
			boundName := "negotiated-bound-" + test.name
			if err := testPool.QueryRow(ctx, `
				INSERT INTO skill (workspace_id, name, description, content, config, created_by)
				VALUES ($1, $2, 'bound skill', 'bound content', '{}'::jsonb, $3)
				RETURNING id
			`, testWorkspaceID, boundName, testUserID).Scan(&skillID); err != nil {
				t.Fatalf("create bound skill: %v", err)
			}
			if _, err := testPool.Exec(ctx, `INSERT INTO agent_skill (agent_id, skill_id) VALUES ($1, $2)`, agentID, skillID); err != nil {
				t.Fatalf("bind skill: %v", err)
			}
			t.Cleanup(func() {
				testPool.Exec(ctx, `DELETE FROM agent_skill WHERE agent_id = $1 AND skill_id = $2`, agentID, skillID)
				testPool.Exec(ctx, `DELETE FROM skill WHERE id = $1`, skillID)
			})
			seedQueuedIssueTask(t, ctx, agentID, runtimeID, issueID)

			var got []service.AgentSkillRefData
			if test.batch {
				w := httptest.NewRecorder()
				req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/claim",
					map[string]any{"daemon_id": batchClaimTestDaemonID, "runtime_ids": []string{runtimeID}, "max_tasks": 1},
					testWorkspaceID, batchClaimTestDaemonID)
				req.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilitySkillBundlesV1)
				testHandler.ClaimTasksByRuntime(w, req)
				if w.Code != http.StatusOK {
					t.Fatalf("ClaimTasksByRuntime: %d %s", w.Code, w.Body.String())
				}
				var response struct {
					Tasks []AgentTaskResponse `json:"tasks"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatalf("decode batch claim: %v", err)
				}
				if len(response.Tasks) != 1 || response.Tasks[0].Agent == nil {
					t.Fatalf("batch claim tasks = %+v", response.Tasks)
				}
				got = response.Tasks[0].Agent.SkillRefs
			} else {
				w := httptest.NewRecorder()
				req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "negotiated-skill-contract-"+test.name)
				req.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilitySkillBundlesV1)
				req = withURLParam(req, "runtimeId", runtimeID)
				testHandler.ClaimTaskByRuntime(w, req)
				if w.Code != http.StatusOK {
					t.Fatalf("ClaimTaskByRuntime: %d %s", w.Code, w.Body.String())
				}
				var response struct {
					Task *AgentTaskResponse `json:"task"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatalf("decode claim: %v", err)
				}
				if response.Task == nil || response.Task.Agent == nil {
					t.Fatalf("claim omitted task agent: %s", w.Body.String())
				}
				got = response.Task.Agent.SkillRefs
			}

			if len(got) == 0 || got[0].ID != skillID || got[0].Name != boundName || got[0].Source != "workspace" {
				t.Fatalf("bound skill ref changed or reordered: %+v", got)
			}
			if test.wantPlatformExactOnly {
				if len(got) != 1 {
					t.Fatalf("Platform negotiated refs = %+v, want exactly the bound Extension skill", got)
				}
				return
			}
			_, wantBuiltins := service.BuildAgentSkillBundles(testHandler.TaskService.BuiltinSkills())
			gotWire, err := json.Marshal(got[1:])
			if err != nil {
				t.Fatal(err)
			}
			wantWire, err := json.Marshal(wantBuiltins)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(gotWire, wantWire) {
				t.Fatalf("legacy negotiated built-ins changed:\ngot=%s\nwant=%s", gotWire, wantWire)
			}
		})
	}
}
