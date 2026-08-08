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
	var response struct {
		Task *struct {
			Agent *TaskAgentData `json:"agent"`
		} `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Task == nil || response.Task.Agent == nil {
		t.Fatalf("missing task agent: %s", w.Body.String())
	}
	var got, want any
	if err := json.Unmarshal(response.Task.Agent.RuntimeConfig, &got); err != nil {
		t.Fatalf("claim runtime_config is invalid: %v", err)
	}
	if err := json.Unmarshal(wantRaw, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("claim runtime_config = %#v, want %#v", got, want)
	}
}
