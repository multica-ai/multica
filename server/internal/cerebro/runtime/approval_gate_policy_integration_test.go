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
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/approvals"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/permgate"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

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
