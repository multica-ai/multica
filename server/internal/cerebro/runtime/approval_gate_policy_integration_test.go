package runtime

// End-to-end proof for the FIR-2230 reviewer finding: the per-tool policy chain
// must actually gate real tool calls, not just render in the admin table. These
// tests drive the real executor path — guardToolCall → guardToolCallViaPolicy →
// GetAgent (real DB) → toolpolicy.Store.Resolve (real DB chain) → permgate
// GuardDecision — and assert that an Allow/Ask/Deny row authored on the agent
// layer changes whether the tool runs.
//
// They reuse the shared pool + workspace/runtime/user fixture from
// account_test.go's TestMain, and skip cleanly when no DB is reachable. The
// approval inbox is faked (gateFakeApprovals) so the test does not depend on the
// approvals service tables — the point under test is the resolution + gating,
// not inbox persistence (that is covered in permgate's own tests).

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/approvals"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/permgate"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

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

	allowed, _ := e.guardToolCall(ctx, agentID, runtimeAccountTestWSID, "draft_reply", nil, GatewayRequestMeta{})
	if !allowed {
		t.Fatal("approved connection Ask must let the tool continue")
	}
	if ap.intakes != 1 {
		t.Fatalf("connection Ask must create exactly one inbox request, got %d", ap.intakes)
	}

	allowed, _ = e.guardToolCall(ctx, agentID, runtimeAccountTestWSID, "lookup_order", nil, GatewayRequestMeta{})
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
	allowed, _ := e.guardToolCall(ctx, agentID, runtimeAccountTestWSID, "draft_reply", nil, GatewayRequestMeta{})
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
	allowed, _ = e.guardToolCall(ctx, agentID, runtimeAccountTestWSID, "draft_reply", nil, GatewayRequestMeta{})
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

	allowed, reason := e.guardToolCall(context.Background(), agentID, runtimeAccountTestWSID, "draft_reply", nil, GatewayRequestMeta{})
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
// test runtime) and returns its id, so guardToolCallViaPolicy's GetAgent lookup
// resolves the owner + runtime that anchor the chain.
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
	`, runtimeAccountTestWSID, "tool-policy-gate-probe", runtimeAccountTestRuntimeID, runtimeAccountTestUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM cerebro_tool_policy WHERE workspace_id = $1`, runtimeAccountTestWSID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	e := &FirtalGatewayExecutor{
		queries:    db.New(pool),
		cerebro:    cerebrodb.New(pool),
		logger:     slog.Default(),
		toolPolicy: toolpolicy.NewStore(pool),
	}
	gate := &permgate.Gate{Approvals: ap, PollInterval: time.Millisecond, WaitTimeout: time.Second}
	e.EnableApprovalGate(gate, nil) // empty allowlist = all agents gated
	return e, agentID
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
	const tool = "deploy_restart"
	setAgentToolPolicy(t, agentID, tool, toolpolicy.SettingDeny)

	allowed, reason := e.guardToolCall(context.Background(), agentID, runtimeAccountTestWSID, tool, nil, GatewayRequestMeta{})
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

// TestGateToolPolicy_AskCreatesInboxAndAwaits proves an Ask row routes through
// the inbox + await machinery and continues when a human approves.
func TestGateToolPolicy_AskCreatesInboxAndAwaits(t *testing.T) {
	ap := &gateFakeApprovals{status: approvals.StatusApproved} // human already approved
	e, agentID := newToolPolicyGatedExecutor(t, ap)
	const tool = "web_fetch"
	setAgentToolPolicy(t, agentID, tool, toolpolicy.SettingAsk)

	allowed, _ := e.guardToolCall(context.Background(), agentID, runtimeAccountTestWSID, tool, nil, GatewayRequestMeta{})
	if !allowed {
		t.Fatal("approved Ask must let the tool continue")
	}
	if ap.intakes != 1 {
		t.Fatalf("Ask must create exactly one inbox request, got %d", ap.intakes)
	}
}

// TestGateToolPolicy_AskRejectedBlocks proves the same Ask row stops the tool
// when the human rejects.
func TestGateToolPolicy_AskRejectedBlocks(t *testing.T) {
	ap := &gateFakeApprovals{status: approvals.StatusRejected}
	e, agentID := newToolPolicyGatedExecutor(t, ap)
	const tool = "web_fetch"
	setAgentToolPolicy(t, agentID, tool, toolpolicy.SettingAsk)

	allowed, reason := e.guardToolCall(context.Background(), agentID, runtimeAccountTestWSID, tool, nil, GatewayRequestMeta{})
	if allowed {
		t.Fatal("rejected Ask must block the tool")
	}
	if reason == "" {
		t.Fatal("rejected call must carry a reason")
	}
	if ap.intakes != 1 {
		t.Fatalf("Ask must create exactly one inbox request, got %d", ap.intakes)
	}
}

// TestGateToolPolicy_UnconfiguredToolAllowed proves the blast radius: a tool
// with no policy row resolves to the Base default (Allow) and runs without an
// ask, so enabling the gate does not brick unconfigured tools.
func TestGateToolPolicy_UnconfiguredToolAllowed(t *testing.T) {
	ap := &gateFakeApprovals{}
	e, agentID := newToolPolicyGatedExecutor(t, ap)

	allowed, reason := e.guardToolCall(context.Background(), agentID, runtimeAccountTestWSID, "add_comment", nil, GatewayRequestMeta{})
	if !allowed || reason != "" {
		t.Fatalf("unconfigured tool must be allowed; got allowed=%v reason=%q", allowed, reason)
	}
	if ap.intakes != 0 {
		t.Fatalf("allowed tool must not create an ask, got %d", ap.intakes)
	}
}
