package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
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
