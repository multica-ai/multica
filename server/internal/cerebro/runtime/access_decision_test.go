package runtime

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/accessdecision"
	"github.com/multica-ai/multica/server/internal/cerebro/availabilityevidence"
	"github.com/multica-ai/multica/server/internal/cerebro/capabilitycatalog"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
)

type captureTaskMandates struct{ issued []string }

func (m *captureTaskMandates) Issue(_ context.Context, _, _, _ pgtype.UUID, tools []string, _ time.Time) error {
	m.issued = append([]string(nil), tools...)
	return nil
}

func (*captureTaskMandates) Authorize(context.Context, pgtype.UUID, pgtype.UUID, pgtype.UUID, string) error {
	return nil
}

type shadowEvidence map[string]availabilityevidence.Evidence

func (e shadowEvidence) Lookup(id string, runtimeType availabilityevidence.RuntimeType) availabilityevidence.Evidence {
	evidence := e[id]
	evidence.CapabilityID = id
	evidence.RuntimeType = runtimeType
	return evidence
}

type shadowLedgerWriter struct {
	entries []accessdecision.Entry
}

func (w *shadowLedgerWriter) Append(_ context.Context, entry accessdecision.Entry) error {
	w.entries = append(w.entries, entry)
	return nil
}

func TestCanonicalGatewayCapabilityIDUsesConcreteCallableIdentity(t *testing.T) {
	registry := NewRegistry(nil)
	registry.Register(stubTool{name: "get_issue"})
	registry.Register(stubTool{name: "mystery_tool"})
	registry.Register(stubTool{name: "memory_recall"})
	registry.Register(stubTool{name: "web_fetch"})
	registry.Register(&gatewayMCPTool{
		exposedName:    "mcp__customer_service__draft_reply",
		connectionName: "customer_service",
		toolName:       "draft_reply",
	})
	registry.Register(&APIConnectionTool{
		toolName: "infisical_admin__get_secrets",
		connName: "infisical-admin",
		method:   "GET",
		path:     "/secrets",
	})

	tests := []struct {
		name     string
		toolName string
		registry *Registry
		want     string
	}{
		{
			name:     "registered memory tool",
			toolName: "memory_recall",
			registry: registry,
			want:     capabilitycatalog.PlatformTool("memory_recall").ID,
		},
		{
			name:     "declared platform tool",
			toolName: "get_issue",
			registry: registry,
			want:     capabilitycatalog.PlatformTool("get_issue").ID,
		},
		{
			name:     "gateway native tool",
			toolName: "web_fetch",
			registry: registry,
			want:     capabilitycatalog.PlatformTool("web_fetch").ID,
		},
		{
			name:     "mcp connection tool",
			toolName: "mcp__customer_service__draft_reply",
			registry: registry,
			want:     capabilitycatalog.MCPConnectionTool("customer_service", "draft_reply").ID,
		},
		{
			name:     "api connection endpoint",
			toolName: "infisical_admin__get_secrets",
			registry: registry,
			want: capabilitycatalog.APIConnectionEndpoint(
				"infisical-admin", "GET", "/secrets", "infisical_admin__get_secrets",
			).ID,
		},
		{
			name:     "registered but undeclared tool fails closed",
			toolName: "mystery_tool",
			registry: registry,
		},
		{
			name:     "known fallback without registry",
			toolName: "get_issue",
			want:     capabilitycatalog.PlatformTool("get_issue").ID,
		},
		{
			name:     "unknown fallback without registry fails closed",
			toolName: "mystery_tool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canonicalGatewayCapabilityID(tt.toolName, tt.registry); got != tt.want {
				t.Fatalf("canonicalGatewayCapabilityID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGuardToolCallUsesCanonicalDecisionForEveryPlatformAction(t *testing.T) {
	executor, agentID := newToolPolicyGatedExecutor(t, &gateFakeApprovals{})
	const toolName = "get_issue"
	setAgentToolPolicy(t, agentID, toolName, toolpolicy.SettingAllow)
	setAgentToolPolicy(t, agentID, "create_issue", toolpolicy.SettingAllow)

	writer := &shadowLedgerWriter{}
	capabilityID := capabilitycatalog.PlatformTool(toolName).ID
	createIssueCapabilityID := capabilitycatalog.PlatformTool("create_issue").ID
	executor.SetAccessDecisionService(accessdecision.NewService(
		executor.toolPolicy,
		shadowEvidence{
			capabilityID:            {Level: availabilityevidence.LevelVerified},
			createIssueCapabilityID: {Level: availabilityevidence.LevelVerified},
		},
		writer,
	))

	allowed, reason := executor.guardToolCall(
		context.Background(),
		agentID,
		runtimeAccountTestWSID,
		toolName,
		nil,
		nil,
		GatewayRequestMeta{
			AgentID:       util.UUIDToString(agentID),
			WorkspaceID:   util.UUIDToString(runtimeAccountTestWSID),
			TriggerUserID: util.UUIDToString(runtimeAccountTestUserID),
		},
	)
	if !allowed || reason != "" {
		t.Fatalf("canonical decision = (%t, %q), want allow", allowed, reason)
	}
	if len(writer.entries) != 1 {
		t.Fatalf("ledger entries = %d, want 1", len(writer.entries))
	}
	entry := writer.entries[0]
	if entry.CanonicalCapabilityID != capabilityID || entry.AgentID != util.UUIDToString(agentID) {
		t.Fatalf("ledger identity = %+v, want agent and canonical capability", entry)
	}
	if entry.Decision != accessdecision.DecisionAllow {
		t.Fatalf("ledger decision = %+v, want canonical allow", entry)
	}

	allowed, reason = executor.guardToolCall(
		context.Background(),
		agentID,
		runtimeAccountTestWSID,
		"create_issue",
		nil,
		nil,
		GatewayRequestMeta{
			AgentID:       util.UUIDToString(agentID),
			WorkspaceID:   util.UUIDToString(runtimeAccountTestWSID),
			TriggerUserID: util.UUIDToString(runtimeAccountTestUserID),
		},
	)
	if !allowed || reason != "" {
		t.Fatalf("platform-action canonical decision = (%t, %q), want allow", allowed, reason)
	}
	if len(writer.entries) != 2 || writer.entries[1].Decision != accessdecision.DecisionAllow {
		t.Fatalf("platform-action ledger entries = %+v, want one additional canonical allow", writer.entries)
	}
}

// TestEveryRegisteredGatewayPermissionUsesThePolicyDecisionService exercises
// the actual registered Gateway universe. New built-in tools enter this test
// automatically, while the synthetic MCP and API tools cover dynamic
// connection families.
//
// For every registered permission it proves the complete agent contract:
// Allow and Ask are listed, Deny is hidden, Allow calls run, Deny and
// human-less Ask calls stop, and the model receives a valid tool definition.
func TestEveryRegisteredGatewayPermissionUsesThePolicyDecisionService(t *testing.T) {
	executor, allowAgent := newToolPolicyGatedExecutor(t, &gateFakeApprovals{})
	_, denyAgent := newToolPolicyGatedExecutor(t, &gateFakeApprovals{})
	askExecutor, askAgent := newToolPolicyGatedExecutor(t, &gateFakeApprovals{})
	executor.SetAccessDecisionService(accessdecision.NewService(executor.toolPolicy, nil, &shadowLedgerWriter{}))
	if _, err := runtimeAccountTestPool.Exec(context.Background(), `
		INSERT INTO workspace_connection
		  (workspace_id, name, display_name, type, url, endpoint_permissions, default_access, enabled)
		VALUES ($1, 'infisical-admin', 'Permission contract API', 'api', 'http://internal:3000',
		        '[{"path":"/secrets","methods":["GET"]}]'::jsonb, 'allow', true)
	`, runtimeAccountTestWSID); err != nil {
		t.Fatalf("seed permission contract API connection: %v", err)
	}
	t.Cleanup(func() {
		_, _ = runtimeAccountTestPool.Exec(context.Background(),
			`DELETE FROM workspace_connection WHERE workspace_id = $1 AND name = 'infisical-admin'`,
			runtimeAccountTestWSID)
	})

	registry := NewDefaultRegistry(nil, nil, ToolContext{CapabilitiesProvider: stubCapabilitiesProvider{}})
	registry.Register(&gatewayMCPTool{
		exposedName:    "mcp__customer_service__draft_reply",
		connectionName: "customer_service",
		toolName:       "draft_reply",
		description:    "Draft a customer reply.",
		inputSchema:    map[string]any{"type": "object", "properties": map[string]any{}},
	})
	registry.Register(&APIConnectionTool{
		toolName:    "infisical_admin__get_secrets",
		description: "Get secrets from Infisical.",
		schema:      map[string]any{"type": "object", "properties": map[string]any{}},
		connName:    "infisical-admin",
		method:      "GET",
		path:        "/secrets",
	})

	for _, name := range registry.Names() {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("registered tool %q disappeared during contract check", name)
		}
		if strings.TrimSpace(tool.Description()) == "" {
			t.Errorf("registered tool %q has no agent-facing description", name)
		}
		schema := tool.InputSchema()
		if schema == nil || schema["type"] != "object" {
			t.Errorf("registered tool %q has invalid agent-facing input schema: %#v", name, schema)
		}
		setAgentToolPolicy(t, allowAgent, name, toolpolicy.SettingAllow)
		setAgentToolPolicy(t, askAgent, name, toolpolicy.SettingAsk)
		setAgentToolPolicy(t, denyAgent, name, toolpolicy.SettingDeny)
	}

	want := registry.Names()
	sort.Strings(want)
	if len(want) == 0 {
		t.Fatal("dynamic Gateway permission contract found no registered tools")
	}
	allowNames := sortedToolNames(executor.policyDecisionTools(
		context.Background(), registry, allowAgent, runtimeAccountTestWSID, GatewayRequestMeta{},
	))
	if !equalStrings(allowNames, want) {
		t.Fatalf("allow agent tools = %v, want %v", allowNames, want)
	}
	askNames := sortedToolNames(askExecutor.policyDecisionTools(
		context.Background(), registry, askAgent, runtimeAccountTestWSID,
		GatewayRequestMeta{TriggerUserID: util.UUIDToString(runtimeAccountTestUserID)},
	))
	if !equalStrings(askNames, want) {
		t.Fatalf("ask agent tools = %v, want %v", askNames, want)
	}
	mandates := &captureTaskMandates{}
	executor.SetTaskMandates(mandates)
	executor.SetAccessDecisionService(accessdecision.NewService(executor.toolPolicy, nil, &shadowLedgerWriter{}).WithMandates(mandates))
	issuedTools := executor.policyDecisionTools(
		context.Background(), registry, allowAgent, runtimeAccountTestWSID,
		GatewayRequestMeta{TaskID: util.UUIDToString(gateTestUUID(7))},
	)
	if got := sortedToolNames(issuedTools); !equalStrings(got, mandates.issued) {
		t.Fatalf("task mandate = %v, want exact final allowed list %v", mandates.issued, got)
	}
	if denied := executor.policyDecisionTools(
		context.Background(), registry, denyAgent, runtimeAccountTestWSID, GatewayRequestMeta{},
	); len(denied) != 0 {
		t.Fatalf("deny agent tools = %v, want none", sortedToolNames(denied))
	}

	for _, name := range registry.Names() {
		if allowed, reason := executor.guardToolCall(
			context.Background(), allowAgent, runtimeAccountTestWSID, name, nil, registry, GatewayRequestMeta{TaskID: util.UUIDToString(gateTestUUID(7))},
		); !allowed {
			t.Errorf("allow agent call %q denied: %s", name, reason)
		}
		if allowed, _ := executor.guardToolCall(
			context.Background(), denyAgent, runtimeAccountTestWSID, name, nil, registry, GatewayRequestMeta{TaskID: util.UUIDToString(gateTestUUID(7))},
		); allowed {
			t.Errorf("deny agent call %q was allowed", name)
		}
		if allowed, _ := askExecutor.guardToolCall(
			context.Background(), askAgent, runtimeAccountTestWSID, name, nil, registry, GatewayRequestMeta{},
		); allowed {
			t.Errorf("human-less Ask agent call %q was allowed", name)
		}
	}
}
