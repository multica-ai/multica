package runtime

// End-to-end proof for the FIR-2230 reviewer finding: the per-tool policy chain
// must actually gate real tool calls, not just render in the admin table. These
// tests drive the real executor path — guardToolCall → Policy Decision Service
// → real DB chain → permgate GuardDecision — and assert that an
// Allow/Ask/Deny row authored on the agent layer changes whether the tool runs.
//
// They reuse the shared pool + workspace/runtime/user fixture from
// account_test.go's TestMain, and skip cleanly when no DB is reachable. The
// approval inbox is faked (gateFakeApprovals) so the test does not depend on the
// approvals service tables — the point under test is the resolution + gating,
// not inbox persistence (that is covered in permgate's own tests).

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/accessdecision"
	"github.com/multica-ai/multica/server/internal/cerebro/approvals"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/permgate"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var toolPolicyGateProbeSequence atomic.Uint64

// seedConnectionAsk seeds an MCP connection with two tools and an agent-layer Ask
// on draft_reply, returning a cleanup. It skips the test if the workspace_connection
// table is absent. Shared by the TECH-3498 gateway connection-tool tests.
func seedConnectionAsk(t *testing.T, agentID pgtype.UUID, setting toolpolicy.Setting) {
	t.Helper()
	pool := runtimeAccountTestPool
	ctx := context.Background()
	const conn = "customer-service-mcp"
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspace_connection
		  (workspace_id, name, display_name, type, url, tools, enabled)
		VALUES ($1, $2, $3, 'mcp_http', 'http://internal:3000',
		        '[{"name":"draft_reply"},{"name":"lookup_order"}]'::jsonb, true)
	`, runtimeAccountTestWSID, conn, "Customer Service MCP"); err != nil {
		if strings.Contains(err.Error(), "workspace_connection") {
			t.Skip("workspace_connection table not present; skipping connection-tool gate test")
		}
		t.Fatalf("seed connection: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM workspace_connection WHERE workspace_id = $1`, runtimeAccountTestWSID)
	})
	store := toolpolicy.NewStore(pool)
	if _, err := store.Set(ctx, toolpolicy.SetParams{
		WorkspaceID:     runtimeAccountTestWSID,
		ToolKey:         "connection:" + conn,
		Layer:           toolpolicy.LayerAgent,
		SubjectID:       agentID,
		Setting:         setting,
		ResourcePattern: "draft_reply",
	}); err != nil {
		t.Fatalf("set connection %s: %v", setting, err)
	}
}

// TestGateConnectionTool_AskRoutesThroughInbox is the TECH-3498 gateway headline:
// an Ask authored on a workspace-connection MCP tool — resolved through the
// connection:<name> chain, which the generic tools: resolver never sees — routes
// through the same inbox + await and continues when approved. A connection tool
// left unset runs with no ask.
func TestGateConnectionTool_AskRoutesThroughInbox(t *testing.T) {
	ap := &gateFakeApprovals{status: approvals.StatusApproved}
	e, agentID := newToolPolicyGatedExecutor(t, ap)
	e.connDeny = toolpolicy.NewStore(runtimeAccountTestPool)
	seedConnectionAsk(t, agentID, toolpolicy.SettingAsk)
	ctx := context.Background()

	allowed, _ := e.guardToolCall(ctx, agentID, runtimeAccountTestWSID, "draft_reply", nil, policyTestRegistry("draft_reply"), GatewayRequestMeta{})
	if !allowed {
		t.Fatal("approved connection Ask must let the tool continue")
	}
	if ap.intakes != 1 {
		t.Fatalf("connection Ask must create exactly one inbox request, got %d", ap.intakes)
	}

	allowed, _ = e.guardToolCall(ctx, agentID, runtimeAccountTestWSID, "lookup_order", nil, policyTestRegistry("lookup_order"), GatewayRequestMeta{})
	if !allowed {
		t.Fatal("unset connection tool must run")
	}
	if ap.intakes != 1 {
		t.Fatalf("unset connection tool must not create another ask, got %d", ap.intakes)
	}
}

// TestGateConnectionTool_PeriodGrantSkipsSecondAsk is the TECH-3498 "approve for
// a period" gateway proof: once a connection-tool Ask is approved with a future
// expires_at, a SECOND guardToolCall for the same tool short-circuits to allowed
// via FindReusable — no new inbox ask (intakes stays 1). It models the still-valid
// grant by feeding the fake approvals an approved, unexpired row.
func TestGateConnectionTool_PeriodGrantSkipsSecondAsk(t *testing.T) {
	ap := &gateFakeApprovals{status: approvals.StatusApproved}
	e, agentID := newToolPolicyGatedExecutor(t, ap)
	e.connDeny = toolpolicy.NewStore(runtimeAccountTestPool)
	seedConnectionAsk(t, agentID, toolpolicy.SettingAsk)
	ctx := context.Background()

	// First call: no reusable grant yet, so it raises one ask and is approved.
	allowed, _ := e.guardToolCall(ctx, agentID, runtimeAccountTestWSID, "draft_reply", nil, policyTestRegistry("draft_reply"), GatewayRequestMeta{})
	if !allowed {
		t.Fatal("approved connection Ask must let the first call continue")
	}
	if ap.intakes != 1 {
		t.Fatalf("first call must create exactly one inbox request, got %d", ap.intakes)
	}

	// Model the period grant: an approved row whose expires_at is in the future.
	ap.reusable = &cerebrodb.CerebroApprovalRequest{
		ID:        gateTestUUID(7),
		Status:    approvals.StatusApproved,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}

	// Second call for the same tool: the still-valid grant short-circuits to
	// allowed with NO new ask.
	allowed, _ = e.guardToolCall(ctx, agentID, runtimeAccountTestWSID, "draft_reply", nil, policyTestRegistry("draft_reply"), GatewayRequestMeta{})
	if !allowed {
		t.Fatal("a still-valid period grant must let the second call continue")
	}
	if ap.intakes != 1 {
		t.Fatalf("second call must reuse the grant, not create a new ask; intakes=%d", ap.intakes)
	}
}

// TestGateConnectionTool_DenyBlocksAlwaysOn proves the TECH-3174 guarantee is
// preserved through the TECH-3498 refactor: a connection-tool Deny blocks even
// with the approval gate OFF (e.gate == nil) and never creates an inbox ask.
func TestGateConnectionTool_DenyBlocksAlwaysOn(t *testing.T) {
	ap := &gateFakeApprovals{}
	e, agentID := newToolPolicyGatedExecutor(t, ap)
	e.connDeny = toolpolicy.NewStore(runtimeAccountTestPool)
	e.gate = nil // approval gate OFF — Deny must still hold
	seedConnectionAsk(t, agentID, toolpolicy.SettingDeny)

	allowed, reason := e.guardToolCall(context.Background(), agentID, runtimeAccountTestWSID, "draft_reply", nil, policyTestRegistry("draft_reply"), GatewayRequestMeta{})
	if allowed {
		t.Fatal("connection Deny must block even with the approval gate off")
	}
	if reason == "" {
		t.Fatal("blocked call must carry a reason")
	}
	if ap.intakes != 0 {
		t.Fatalf("Deny must not create an approval ask, got %d", ap.intakes)
	}
}

// newToolPolicyGatedExecutor builds an executor wired exactly like the
// toolpolicy gate mode: real queries + real tool-policy store + a gate whose
// inbox is faked. It also creates a real agent (owner = test user, runtime =
// test runtime) and returns its id, so the canonical decision service resolves
// the owner + runtime that anchor the chain.
func newToolPolicyGatedExecutor(t *testing.T, ap *gateFakeApprovals) (*FirtalGatewayExecutor, pgtype.UUID) {
	t.Helper()
	if runtimeAccountTestPool == nil {
		t.Skip("DATABASE_URL not configured; skipping tool-policy gate integration test")
	}
	pool := runtimeAccountTestPool
	ctx := context.Background()

	var agentID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id, owner_id)
		VALUES ($1, $2, 'local', $3, $4)
		RETURNING id
	`, runtimeAccountTestWSID, fmt.Sprintf("tool-policy-gate-probe-%d", toolPolicyGateProbeSequence.Add(1)), runtimeAccountTestRuntimeID, runtimeAccountTestUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM cerebro_tool_policy WHERE workspace_id = $1 AND layer = 'agent' AND subject_id = $2`, runtimeAccountTestWSID, agentID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	e := &FirtalGatewayExecutor{
		queries:    db.New(pool),
		cerebro:    cerebrodb.New(pool),
		logger:     slog.Default(),
		toolPolicy: toolpolicy.NewStore(pool),
	}
	e.connDeny = e.toolPolicy
	e.SetAccessDecisionService(accessdecision.NewService(e.toolPolicy, nil, &shadowLedgerWriter{}))
	gate := &permgate.Gate{Approvals: ap, PollInterval: time.Millisecond, WaitTimeout: time.Second}
	e.EnableApprovalGate(gate)
	return e, agentID
}

func policyTestRegistry(toolName string) *Registry {
	reg := NewRegistry(nil)
	switch toolName {
	case "draft_reply", "lookup_order":
		reg.Register(&gatewayMCPTool{
			exposedName:    toolName,
			connectionName: "customer-service-mcp",
			toolName:       toolName,
		})
	default:
		reg.Register(stubTool{name: toolName})
	}
	return reg
}

func setAgentToolPolicy(t *testing.T, agentID pgtype.UUID, tool string, setting toolpolicy.Setting) {
	t.Helper()
	store := toolpolicy.NewStore(runtimeAccountTestPool)
	if _, err := store.Set(context.Background(), toolpolicy.SetParams{
		WorkspaceID: runtimeAccountTestWSID,
		ToolKey:     tool,
		Layer:       toolpolicy.LayerAgent,
		SubjectID:   agentID,
		Setting:     setting,
	}); err != nil {
		t.Fatalf("set agent policy %s=%s: %v", tool, setting, err)
	}
}

// TestGateToolPolicy_DenyBlocksRealToolCall is the headline check: a Deny row on
// the agent layer stops the tool through the live executor path.
func TestGateToolPolicy_DenyBlocksRealToolCall(t *testing.T) {
	ap := &gateFakeApprovals{}
	e, agentID := newToolPolicyGatedExecutor(t, ap)
	const tool = "web_fetch"
	setAgentToolPolicy(t, agentID, tool, toolpolicy.SettingDeny)

	allowed, reason := e.guardToolCall(context.Background(), agentID, runtimeAccountTestWSID, tool, nil, policyTestRegistry(tool), GatewayRequestMeta{})
	if allowed {
		t.Fatal("a Deny row on the agent layer must block the tool")
	}
	if reason == "" {
		t.Fatal("blocked call must carry a reason")
	}
	if ap.intakes != 0 {
		t.Fatalf("deny must not create an approval ask, got %d", ap.intakes)
	}
}

func TestGateToolPolicy_MemberOverrideProductionSemantics(t *testing.T) {
	ctx := context.Background()
	const tool = "web_fetch"

	setMemberOverride := func(t *testing.T, e *FirtalGatewayExecutor) {
		t.Helper()
		if err := e.cerebro.UpsertCerebroWorkspaceFeatureFlag(ctx, cerebrodb.UpsertCerebroWorkspaceFeatureFlagParams{
			WorkspaceID: runtimeAccountTestWSID,
			FlagKey:     toolpolicy.FlagMemberOverride,
			Enabled:     true,
		}); err != nil {
			t.Fatalf("enable member override: %v", err)
		}
		t.Cleanup(func() {
			_, _ = runtimeAccountTestPool.Exec(context.Background(),
				`DELETE FROM cerebro_feature_flags WHERE workspace_id = $1 AND flag_key = $2`,
				runtimeAccountTestWSID, toolpolicy.FlagMemberOverride)
			_, _ = runtimeAccountTestPool.Exec(context.Background(),
				`DELETE FROM cerebro_tool_policy WHERE workspace_id = $1 AND tool_key = $2`,
				runtimeAccountTestWSID, tool)
		})
	}

	t.Run("workspace deny is opened by explicit agent allow", func(t *testing.T) {
		e, agentID := newToolPolicyGatedExecutor(t, &gateFakeApprovals{})
		setMemberOverride(t, e)
		store := toolpolicy.NewStore(runtimeAccountTestPool)
		if _, err := store.Set(ctx, toolpolicy.SetParams{
			WorkspaceID: runtimeAccountTestWSID,
			ToolKey:     tool,
			Layer:       toolpolicy.LayerWorkspace,
			SubjectID:   runtimeAccountTestWSID,
			Setting:     toolpolicy.SettingDeny,
		}); err != nil {
			t.Fatalf("set workspace deny: %v", err)
		}
		setAgentToolPolicy(t, agentID, tool, toolpolicy.SettingAllow)

		allowed, reason := e.guardToolCall(ctx, agentID, runtimeAccountTestWSID, tool, nil, policyTestRegistry(tool), GatewayRequestMeta{
			TriggerUserID: util.UUIDToString(runtimeAccountTestUserID),
		})
		if !allowed {
			t.Fatalf("agent Allow did not open workspace-default Deny: %s", reason)
		}
	})

	t.Run("member deny blocks explicit agent allow", func(t *testing.T) {
		e, agentID := newToolPolicyGatedExecutor(t, &gateFakeApprovals{})
		setMemberOverride(t, e)
		store := toolpolicy.NewStore(runtimeAccountTestPool)
		setAgentToolPolicy(t, agentID, tool, toolpolicy.SettingAllow)
		if _, err := store.Set(ctx, toolpolicy.SetParams{
			WorkspaceID: runtimeAccountTestWSID,
			ToolKey:     tool,
			Layer:       toolpolicy.LayerUser,
			SubjectID:   runtimeAccountTestUserID,
			Setting:     toolpolicy.SettingDeny,
		}); err != nil {
			t.Fatalf("set member deny: %v", err)
		}

		allowed, reason := e.guardToolCall(ctx, agentID, runtimeAccountTestWSID, tool, nil, policyTestRegistry(tool), GatewayRequestMeta{
			TriggerUserID: util.UUIDToString(runtimeAccountTestUserID),
		})
		if allowed {
			t.Fatal("member Deny must block even when the agent has Allow")
		}
		if reason == "" {
			t.Fatal("blocked call must explain the member ceiling")
		}
	})
}

// TestGateToolPolicy_ExpiredRoleBindingIsRejectedAtCallTime proves the live
// Gateway resolve does not keep using a role after its binding expires. The
// same role denies while active, then disappears from the very next call after
// its database expiry is moved into the past.
func TestGateToolPolicy_ExpiredRoleBindingIsRejectedAtCallTime(t *testing.T) {
	e, agentID := newToolPolicyGatedExecutor(t, &gateFakeApprovals{})
	ctx := context.Background()
	const tool = "web_fetch"

	var roleID pgtype.UUID
	if err := runtimeAccountTestPool.QueryRow(ctx, `
		INSERT INTO cerebro_role (workspace_id, name, permissions)
		VALUES ($1, $2, jsonb_build_object($3::text, jsonb_build_object('setting', 'deny')))
		RETURNING id`, runtimeAccountTestWSID, fmt.Sprintf("expiring-gateway-role-%d", toolPolicyGateProbeSequence.Add(1)), tool).Scan(&roleID); err != nil {
		if strings.Contains(err.Error(), `column "permissions"`) {
			t.Skip("FIR-3402 role migration not present in test database")
		}
		t.Fatalf("create expiring role: %v", err)
	}
	t.Cleanup(func() {
		_, _ = runtimeAccountTestPool.Exec(context.Background(), `DELETE FROM cerebro_role WHERE id=$1`, roleID)
	})
	if _, err := runtimeAccountTestPool.Exec(ctx, `
		INSERT INTO cerebro_role_assignment (role_id, subject_type, subject_id, expires_at)
		VALUES ($1, 'agent', $2, now() + interval '1 hour')`, roleID, agentID); err != nil {
		t.Fatalf("assign expiring role: %v", err)
	}

	if allowed, _ := e.guardToolCall(ctx, agentID, runtimeAccountTestWSID, tool, nil, policyTestRegistry(tool), GatewayRequestMeta{}); allowed {
		t.Fatal("active deny role must block the live Gateway call")
	}
	if _, err := runtimeAccountTestPool.Exec(ctx, `UPDATE cerebro_role_assignment SET expires_at=now() - interval '1 second' WHERE role_id=$1 AND subject_id=$2`, roleID, agentID); err != nil {
		t.Fatalf("expire role binding: %v", err)
	}
	if allowed, reason := e.guardToolCall(ctx, agentID, runtimeAccountTestWSID, tool, nil, policyTestRegistry(tool), GatewayRequestMeta{}); !allowed {
		t.Fatalf("expired role binding still affected live Gateway call: %s", reason)
	}
}

// TestGateToolPolicy_AskApprovedRuns proves a human-triggered Ask reaches the
// approval service and an approved decision allows the call.
func TestGateToolPolicy_AskApprovedRuns(t *testing.T) {
	ap := &gateFakeApprovals{status: approvals.StatusApproved} // human already approved
	e, agentID := newToolPolicyGatedExecutor(t, ap)
	const tool = "web_fetch"
	setAgentToolPolicy(t, agentID, tool, toolpolicy.SettingAsk)

	// A human triggered this run, so the Ask has someone to answer it.
	allowed, reason := e.guardToolCall(context.Background(), agentID, runtimeAccountTestWSID, tool, nil, policyTestRegistry(tool), GatewayRequestMeta{TriggerUserID: "11111111-1111-1111-1111-111111111111"})
	if !allowed {
		t.Fatalf("approved Ask must allow the tool; reason=%q", reason)
	}
	if ap.intakes != 1 {
		t.Fatalf("approved Ask must reach approval intake once, got %d", ap.intakes)
	}
}

// TestGateToolPolicy_AskRejectedBlocks proves the same Ask row remains denied
// after the approval service rejects it.
func TestGateToolPolicy_AskRejectedBlocks(t *testing.T) {
	ap := &gateFakeApprovals{status: approvals.StatusRejected}
	e, agentID := newToolPolicyGatedExecutor(t, ap)
	const tool = "web_fetch"
	setAgentToolPolicy(t, agentID, tool, toolpolicy.SettingAsk)

	// A human trigger sends Ask to the approval service.
	allowed, reason := e.guardToolCall(context.Background(), agentID, runtimeAccountTestWSID, tool, nil, policyTestRegistry(tool), GatewayRequestMeta{TriggerUserID: "11111111-1111-1111-1111-111111111111"})
	if allowed {
		t.Fatal("rejected Ask must block the tool")
	}
	if reason == "" {
		t.Fatal("rejected call must carry a reason")
	}
	if ap.intakes != 1 {
		t.Fatalf("rejected Ask must reach approval intake once, got %d", ap.intakes)
	}
}

// TestGateToolPolicy_SystemRunAskBecomesDeny proves the FIR-1609 resolution-context
// fail-safe through the live executor path: the same Ask row that a human run sends
// to the inbox is instead denied outright on a human-less System run (empty
// TriggerUserID) — there is no one to answer, so the gate must not create an inbox
// request and must block the tool.
func TestGateToolPolicy_SystemRunAskBecomesDeny(t *testing.T) {
	ap := &gateFakeApprovals{status: approvals.StatusApproved} // would approve if asked
	e, agentID := newToolPolicyGatedExecutor(t, ap)
	const tool = "web_fetch"
	setAgentToolPolicy(t, agentID, tool, toolpolicy.SettingAsk)

	// No triggering human → System run. The Ask must collapse to Deny.
	allowed, reason := e.guardToolCall(context.Background(), agentID, runtimeAccountTestWSID, tool, nil, policyTestRegistry(tool), GatewayRequestMeta{})
	if allowed {
		t.Fatal("a System run (no human) must not let an Ask tool through")
	}
	if reason == "" {
		t.Fatal("blocked system call must carry a reason")
	}
	if ap.intakes != 0 {
		t.Fatalf("a System run must not create an inbox request, got %d", ap.intakes)
	}
}

// TestGateToolPolicy_WorkspaceApprovalFlagCannotBypassPolicy proves the approval
// inbox feature flag never disables the authoritative Policy Decision Service.
func TestGateToolPolicy_WorkspaceApprovalFlagCannotBypassPolicy(t *testing.T) {
	ap := &gateFakeApprovals{}
	e, agentID := newToolPolicyGatedExecutor(t, ap)
	const tool = "web_fetch"
	setAgentToolPolicy(t, agentID, tool, toolpolicy.SettingDeny)

	// Turn the workspace flag OFF (all-zero sentinel user_id = workspace-level row).
	ctx := context.Background()
	if err := e.cerebro.UpsertCerebroFeatureFlag(ctx, cerebrodb.UpsertCerebroFeatureFlagParams{
		WorkspaceID: runtimeAccountTestWSID,
		UserID:      pgtype.UUID{Valid: true},
		FlagKey:     "cerebro_approval_gate",
		Enabled:     false,
	}); err != nil {
		t.Fatalf("seed cerebro_approval_gate=false: %v", err)
	}
	t.Cleanup(func() {
		_, _ = runtimeAccountTestPool.Exec(context.Background(),
			`DELETE FROM cerebro_feature_flags WHERE workspace_id = $1 AND flag_key = 'cerebro_approval_gate'`,
			runtimeAccountTestWSID)
	})

	allowed, _ := e.guardToolCall(ctx, agentID, runtimeAccountTestWSID, tool, nil, policyTestRegistry(tool), GatewayRequestMeta{})
	if allowed {
		t.Fatal("cerebro_approval_gate OFF must not bypass a Policy Decision Service Deny")
	}
	if ap.intakes != 0 {
		t.Fatalf("a bypassed gate must not create an inbox request, got %d", ap.intakes)
	}
}

// TestGateToolPolicy_UnconfiguredToolAllowed proves the blast radius: a tool
// with no policy row resolves to the Base default (Allow) and runs without an
// ask, so enabling the gate does not brick unconfigured tools.
func TestGateToolPolicy_UnconfiguredToolAllowed(t *testing.T) {
	ap := &gateFakeApprovals{}
	e, agentID := newToolPolicyGatedExecutor(t, ap)

	allowed, reason := e.guardToolCall(context.Background(), agentID, runtimeAccountTestWSID, "add_comment", nil, policyTestRegistry("add_comment"), GatewayRequestMeta{})
	if !allowed || reason != "" {
		t.Fatalf("unconfigured tool must be allowed; got allowed=%v reason=%q", allowed, reason)
	}
	if ap.intakes != 0 {
		t.Fatalf("allowed tool must not create an ask, got %d", ap.intakes)
	}
}
