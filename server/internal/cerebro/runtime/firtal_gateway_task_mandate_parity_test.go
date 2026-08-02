package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/connections"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
)

func TestRunToolLoopFinalizesTaskMandateAfterAddingMemoryTools(t *testing.T) {
	probe := newGatewayMandateProbe(t, false)
	setGatewayWorkspaceFlag(t, probe.executor, cerebroMemoryFlagKey, true)

	probe.run(t)

	assertOfferedAndMandatedTool(t, probe, "memory_recall")
}

func TestRunToolLoopFinalizesTaskMandateAfterAddingAPIConnectionTools(t *testing.T) {
	probe := newGatewayMandateProbe(t, false)
	setGatewayWorkspaceFlag(t, probe.executor, flagAPIConnectionTools, true)
	seedGatewayAPIConnection(t, "task-mandate-api", 1)
	probe.wireConnections()

	probe.run(t)

	assertOfferedAndMandatedTool(t, probe, "task_mandate_api__get_items_000")
}

func TestRunToolLoopFinalizesTaskMandateAfterAddingMCPTools(t *testing.T) {
	fake := httptest.NewServer((&fakeMCPServer{sessionID: "task-mandate-mcp"}).handler())
	t.Cleanup(fake.Close)

	probe := newGatewayMandateProbe(t, false)
	setGatewayWorkspaceFlag(t, probe.executor, flagAPIConnectionTools, true)
	seedGatewayMCPConnection(t, "task-mandate-mcp", fake.URL)
	probe.wireConnections()

	probe.run(t)

	assertOfferedAndMandatedTool(t, probe, "mcp__task-mandate-mcp__get_secret")
}

func TestRunToolLoopFinalizesTaskMandateAfterMalformedRequestFallback(t *testing.T) {
	probe := newGatewayMandateProbe(t, true)

	probe.run(t)

	if len(probe.offered) != 2 {
		t.Fatalf("gateway requests = %d, want malformed full list followed by core fallback", len(probe.offered))
	}
	if len(probe.offered[0]) <= len(probe.offered[1]) {
		t.Fatalf("gateway tool counts = %d then %d, want a smaller successful fallback", len(probe.offered[0]), len(probe.offered[1]))
	}
	if !equalStrings(probe.mandates.issued, probe.offered[1]) {
		t.Fatalf("Task Mandate = %v, want successful fallback tools %v", probe.mandates.issued, probe.offered[1])
	}
}

func TestRunToolLoopFinalizesTaskMandateFromLimitFirtalGatewayToolsResult(t *testing.T) {
	probe := newGatewayMandateProbe(t, false)
	setGatewayWorkspaceFlag(t, probe.executor, flagAPIConnectionTools, true)
	seedGatewayAPIConnection(t, "task-mandate-limit", firtalGatewayMaxToolDefs+25)
	probe.wireConnections()

	probe.run(t)

	if len(probe.offered) != 1 {
		t.Fatalf("gateway requests = %d, want one successful request", len(probe.offered))
	}
	if len(probe.offered[0]) != firtalGatewayMaxToolDefs {
		t.Fatalf("offered tools = %d, want provider-limited result of %d", len(probe.offered[0]), firtalGatewayMaxToolDefs)
	}
	if !equalStrings(probe.mandates.issued, probe.offered[0]) {
		t.Fatalf("Task Mandate = %v, want provider-limited tools %v", probe.mandates.issued, probe.offered[0])
	}
}

type gatewayMandateProbe struct {
	executor *FirtalGatewayExecutor
	agentID  pgtype.UUID
	mandates *captureTaskMandates
	offered  [][]string
}

func newGatewayMandateProbe(t *testing.T, malformedFirst bool) *gatewayMandateProbe {
	t.Helper()
	probe := &gatewayMandateProbe{mandates: &captureTaskMandates{}}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Tools []GatewayToolDef `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode gateway request: %v", err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		names := make([]string, 0, len(request.Tools))
		for _, tool := range request.Tools {
			names = append(names, tool.Function.Name)
		}
		probe.offered = append(probe.offered, names)
		w.Header().Set("Content-Type", "application/json")
		if malformedFirst && len(probe.offered) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"upstream_rejected_request","message":"request malformed"}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"done"}}]}`))
	}))
	t.Cleanup(server.Close)

	probe.executor, probe.agentID = newToolPolicyGatedExecutor(t, &gateFakeApprovals{})
	setAgentToolPolicy(t, probe.agentID, "get_issue", toolpolicy.SettingAllow)
	probe.executor.gateway = NewGatewayClient(FirtalGatewayRuntimeConfig{
		BaseURL: server.URL, APIKey: "rk", Model: "claude-sonnet-4-6", MaxTokens: 4096,
	}, server.Client())
	probe.executor.logger = testLogger()
	probe.executor.registry = NewRegistry(runtimeAccountTestPool)
	probe.executor.SetTaskMandates(probe.mandates)
	return probe
}

func (p *gatewayMandateProbe) wireConnections() {
	p.executor.SetAPIConnectionStore(connections.New(runtimeAccountTestPool))
	p.executor.SetConnectionDenyStore(toolpolicy.NewStore(runtimeAccountTestPool))
}

func (p *gatewayMandateProbe) run(t *testing.T) {
	t.Helper()
	agent, err := p.executor.queries.GetAgent(context.Background(), p.agentID)
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}
	if _, err := p.executor.runToolLoop(
		context.Background(),
		FirtalGatewayRuntimeConfig{Model: "claude-sonnet-4-6", MaxTokens: 4096},
		agent,
		[]GatewayMessage{{Role: "user", Content: "continue"}},
		GatewayRequestMeta{TaskID: util.UUIDToString(gateTestUUID(91))},
		p.agentID,
		runtimeAccountTestWSID,
		runtimeAccountTestUserID,
		false,
	); err != nil {
		t.Fatalf("run tool loop: %v", err)
	}
}

func assertOfferedAndMandatedTool(t *testing.T, probe *gatewayMandateProbe, name string) {
	t.Helper()
	if len(probe.offered) != 1 {
		t.Fatalf("gateway requests = %d, want one successful request", len(probe.offered))
	}
	if !containsString(probe.offered[0], name) {
		t.Fatalf("offered tools = %v, want %s", probe.offered[0], name)
	}
	if !containsString(probe.mandates.issued, name) {
		t.Fatalf("Task Mandate = %v, want offered %s", probe.mandates.issued, name)
	}
	if !equalStrings(probe.mandates.issued, probe.offered[0]) {
		t.Fatalf("Task Mandate = %v, want exact offered tools %v", probe.mandates.issued, probe.offered[0])
	}
}

func setGatewayWorkspaceFlag(t *testing.T, executor *FirtalGatewayExecutor, key string, enabled bool) {
	t.Helper()
	ctx := context.Background()
	flags, err := executor.cerebro.ListCerebroWorkspaceFeatureFlags(ctx, runtimeAccountTestWSID)
	if err != nil {
		t.Fatalf("list workspace flags: %v", err)
	}
	var previous *cerebrodb.ListCerebroWorkspaceFeatureFlagsRow
	for i := range flags {
		if flags[i].FlagKey == key {
			copy := flags[i]
			previous = &copy
			break
		}
	}
	if err := executor.cerebro.UpsertCerebroWorkspaceFeatureFlag(ctx, cerebrodb.UpsertCerebroWorkspaceFeatureFlagParams{
		WorkspaceID: runtimeAccountTestWSID,
		FlagKey:     key,
		Enabled:     enabled,
		Locked:      false,
	}); err != nil {
		t.Fatalf("set workspace flag %s: %v", key, err)
	}
	t.Cleanup(func() {
		if previous != nil {
			_ = executor.cerebro.UpsertCerebroWorkspaceFeatureFlag(context.Background(), cerebrodb.UpsertCerebroWorkspaceFeatureFlagParams{
				WorkspaceID: runtimeAccountTestWSID,
				FlagKey:     key,
				Enabled:     previous.Enabled,
				Locked:      previous.Locked,
			})
			return
		}
		_ = executor.cerebro.DeleteCerebroWorkspaceFeatureFlag(context.Background(), cerebrodb.DeleteCerebroWorkspaceFeatureFlagParams{
			WorkspaceID: runtimeAccountTestWSID,
			FlagKey:     key,
		})
	})
}

func seedGatewayAPIConnection(t *testing.T, name string, endpointCount int) {
	t.Helper()
	endpoints := make([]connections.EndpointPermission, 0, endpointCount)
	for i := 0; i < endpointCount; i++ {
		endpoints = append(endpoints, connections.EndpointPermission{
			Path:    fmt.Sprintf("/items/%03d", i),
			Methods: []string{http.MethodGet},
		})
	}
	raw, err := json.Marshal(endpoints)
	if err != nil {
		t.Fatalf("marshal API endpoints: %v", err)
	}
	if _, err := runtimeAccountTestPool.Exec(context.Background(), `
		INSERT INTO workspace_connection
		  (workspace_id, name, display_name, type, url, endpoint_permissions, default_access, enabled)
		VALUES ($1, $2, $2, 'api', 'http://api.internal', $3::jsonb, 'allow', true)
	`, runtimeAccountTestWSID, name, raw); err != nil {
		t.Fatalf("seed API connection: %v", err)
	}
	t.Cleanup(func() {
		_, _ = runtimeAccountTestPool.Exec(context.Background(),
			`DELETE FROM workspace_connection WHERE workspace_id = $1 AND name = $2`, runtimeAccountTestWSID, name)
	})
}

func seedGatewayMCPConnection(t *testing.T, name, url string) {
	t.Helper()
	if _, err := runtimeAccountTestPool.Exec(context.Background(), `
		INSERT INTO workspace_connection
		  (workspace_id, name, display_name, type, url, tools, enabled)
		VALUES ($1, $2, $2, 'mcp_http', $3, '[{"name":"get_secret"}]'::jsonb, true)
	`, runtimeAccountTestWSID, name, url); err != nil {
		t.Fatalf("seed MCP connection: %v", err)
	}
	t.Cleanup(func() {
		_, _ = runtimeAccountTestPool.Exec(context.Background(),
			`DELETE FROM workspace_connection WHERE workspace_id = $1 AND name = $2`, runtimeAccountTestWSID, name)
	})
}
