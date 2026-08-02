package handler

// CEREBRO-PATCH(agent-tools-policy-read-test): FIR-3403 regression guard for the
// external-runtime tool listing (GET /api/agents/{id}/tools).
//
// A prior review flagged a suspected key mismatch in ListAgentTools — that the
// policyEnabled map is keyed by row.Descriptor.ToolKey while read by item.Name,
// and proposed switching the enabled signal to ExposureEffective. Both were
// wrong for this endpoint's real consumer (cloud / firtal-gateway runtimes):
//
//   1. cloudtoolscan records built-in capabilities under the BARE tool name
//      (capability_key = "web_fetch"), exactly what item.Name carries, so the
//      keys already match. Rewriting the read side through PolicyToolKey
//      ("tools:web_fetch") would MISS every builtin and disable all tools.
//   2. Cloud-scanned builtins are classified source="scan"→"mcp", so their
//      required protocols (mcp_stdio/mcp_http_sse) never match a gateway's
//      native_tool_loop protocol → ExposureEffective is false for every one.
//      Keying enabled off ExposureEffective would disable all tools too.
//
// The correct signal is the policy verdict (allow/ask ≠ deny). This test seeds a
// cloud runtime the production way and pins that a policy-allowed builtin is
// Enabled and a runtime-denied builtin is not, through the real toolaccess
// service — so neither proposed "fix" can regress cloud runtimes unnoticed.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/capabilityregistry"
	"github.com/multica-ai/multica/server/internal/cerebro/cloudtoolscan"
	"github.com/multica-ai/multica/server/internal/cerebro/toolaccess"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
)

// realToolAccessAdapter mirrors cmd/server's runtimeToolAccessAdapter so the
// handler test drives the genuine toolaccess.Service (capability register +
// tool-policy chain), not a hand-built fake.
type realToolAccessAdapter struct{ svc *toolaccess.Service }

func (a realToolAccessAdapter) ListEffectiveTools(ctx context.Context, q RuntimeToolAccessQuery) ([]RuntimeToolEffectiveAccessView, error) {
	rows, err := a.svc.ListEffectiveTools(ctx, toolaccess.Query{
		WorkspaceID:         q.WorkspaceID,
		RuntimeID:           q.RuntimeID,
		RuntimeMode:         q.RuntimeMode,
		RuntimeProvider:     q.RuntimeProvider,
		RuntimeCapabilities: q.RuntimeCapabilities,
		AgentID:             q.AgentID,
		UserID:              q.UserID,
		OnBehalfOfID:        q.OnBehalfOfID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]RuntimeToolEffectiveAccessView, 0, len(rows))
	for _, r := range rows {
		out = append(out, RuntimeToolEffectiveAccessView{
			Descriptor:        RuntimeToolDescriptorView{ToolKey: r.Descriptor.ToolKey, Source: r.Descriptor.Source},
			Policy:            RuntimeToolPolicyStateView{Effective: r.Policy.Effective},
			ExposureEffective: RuntimeToolExposureEffectiveView{Effective: r.ExposureEffective.Effective},
		})
	}
	return out, nil
}

func TestListAgentToolsMarksPolicyAllowedBuiltinEnabledForCloudRuntime(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// A cloud / firtal-gateway runtime is the real consumer of this endpoint.
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
		VALUES ($1, 'fir3403-tools-runtime', 'cloud', 'firtal-gateway', 'online', '', '{}'::jsonb, now())
		RETURNING id
	`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert runtime: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id=$1`, runtimeID) })

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, mcp_config
		)
		VALUES ($1, 'fir3403-tools-agent', '', 'cloud', '{}'::jsonb, $2, 'private', 1, $3, '', '{}'::jsonb, '[]'::jsonb, '[]'::jsonb)
		RETURNING id
	`, testWorkspaceID, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent WHERE id=$1`, agentID) })

	rtUUID, _ := util.ParseUUID(runtimeID)
	wsUUID, _ := util.ParseUUID(testWorkspaceID)
	actorUUID, _ := util.ParseUUID(testUserID)

	// Seed the capability register the production way (cloud "Scan now").
	capReg := capabilityregistry.New(testPool)
	scanner := cloudtoolscan.New(capReg, []cloudtoolscan.ToolMeta{
		{Name: "web_fetch", Description: "Fetch a URL"},
		{Name: "schedule_wakeup", Description: "Schedule a wakeup"},
	})
	if err := scanner.Scan(ctx, rtUUID, wsUUID); err != nil {
		t.Fatalf("scan: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM cerebro_capability_subject WHERE capability_id IN (SELECT id FROM cerebro_capability WHERE workspace_id=$1 AND capability_key IN ('web_fetch','schedule_wakeup'))`, wsUUID)
		testPool.Exec(ctx, `DELETE FROM cerebro_capability WHERE workspace_id=$1 AND capability_key IN ('web_fetch','schedule_wakeup')`, wsUUID)
	})

	// Deny the canonical schedule_agent_wakeup platform family at the runtime layer;
	// its exact schedule_wakeup binding must inherit that verdict while web_fetch
	// stays allow (base).
	policyStore := toolpolicy.NewStore(testPool)
	if _, err := policyStore.Set(ctx, toolpolicy.SetParams{
		WorkspaceID: wsUUID, ToolKey: "schedule_agent_wakeup", Layer: toolpolicy.LayerRuntime,
		SubjectID: rtUUID, Setting: toolpolicy.SettingDeny,
	}); err != nil {
		t.Fatalf("set deny policy: %v", err)
	}
	t.Cleanup(func() {
		policyStore.Clear(ctx, wsUUID, "schedule_agent_wakeup", toolpolicy.LayerRuntime, rtUUID, "", actorUUID)
	})

	// Wire the real access service into the shared test handler for this test.
	prevAccess := testHandler.runtimeToolAccess
	testHandler.runtimeToolAccess = realToolAccessAdapter{svc: toolaccess.New(capReg, policyStore)}
	prevItems, prevDesc, prevStatus := testHandler.cerebroToolItems, testHandler.cerebroToolDesc, testHandler.cerebroToolStatus
	testHandler.SetCerebroToolMeta([]CerebroToolItem{
		{Name: "web_fetch", Description: "Fetch a URL", Status: "implemented"},
		{Name: "schedule_wakeup", Description: "Schedule a wakeup", Status: "implemented"},
	})
	t.Cleanup(func() {
		testHandler.runtimeToolAccess = prevAccess
		testHandler.cerebroToolItems, testHandler.cerebroToolDesc, testHandler.cerebroToolStatus = prevItems, prevDesc, prevStatus
	})

	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodGet, "/api/agents/"+agentID+"/tools", nil), "id", agentID)
	testHandler.ListAgentTools(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListAgentTools: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var tools []AgentToolResponse
	if err := json.NewDecoder(w.Body).Decode(&tools); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	byName := map[string]AgentToolResponse{}
	for _, tool := range tools {
		byName[tool.Name] = tool
	}

	webFetch, ok := byName["web_fetch"]
	if !ok {
		t.Fatalf("web_fetch missing from response: %+v", tools)
	}
	if !webFetch.Enabled {
		t.Fatalf("policy-allowed builtin web_fetch must be Enabled=true for a cloud runtime; got %+v", webFetch)
	}
	if wakeup := byName["schedule_wakeup"]; wakeup.Enabled {
		t.Fatalf("runtime-denied builtin schedule_wakeup must be Enabled=false; got %+v", wakeup)
	}
}
