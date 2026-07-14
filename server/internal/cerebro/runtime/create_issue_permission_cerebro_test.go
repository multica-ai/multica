package runtime

import (
	"context"
	"testing"

	googleuuid "github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

func TestGatewayCreateIssuePermission_DenyBlocksWhenRuntimeGateOff(t *testing.T) {
	e, agentID := newToolPolicyGatedExecutor(t, &gateFakeApprovals{})
	e.gate = nil // The platform-action floor must not depend on the runtime gate.
	store := toolpolicy.NewStore(runtimeAccountTestPool)
	if _, err := store.Set(context.Background(), toolpolicy.SetParams{
		WorkspaceID: runtimeAccountTestWSID,
		ToolKey:     "create_issue",
		Layer:       toolpolicy.LayerWorkspace,
		SubjectID:   runtimeAccountTestWSID,
		Setting:     toolpolicy.SettingDeny,
	}); err != nil {
		t.Fatalf("set create_issue Workspace Deny: %v", err)
	}

	title := "Gateway denied " + googleuuid.NewString()
	t.Cleanup(func() {
		_, _ = runtimeAccountTestPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1 AND title = $2`, runtimeAccountTestWSID, title)
	})

	allowed, _ := e.guardToolCall(context.Background(), agentID, runtimeAccountTestWSID, "create_issue", map[string]any{"title": title}, nil, GatewayRequestMeta{})
	if allowed {
		tool := &FirtalCreateIssueTool{queries: e.queries, tctx: ToolContext{AgentID: agentID, WorkspaceID: runtimeAccountTestWSID}}
		if _, err := tool.Call(context.Background(), map[string]any{"title": title}); err != nil {
			t.Fatalf("current Gateway create_issue call failed before permission assertion: %v", err)
		}
	}

	var count int
	if err := runtimeAccountTestPool.QueryRow(context.Background(), `SELECT COUNT(*) FROM issue WHERE workspace_id = $1 AND title = $2`, runtimeAccountTestWSID, title).Scan(&count); err != nil {
		t.Fatalf("count Gateway-created issues: %v", err)
	}
	if allowed {
		t.Fatalf("Gateway create_issue was allowed with runtime gate off under Workspace Deny; mutated issue rows=%d, want 0", count)
	}
	if count != 0 {
		t.Fatalf("Gateway Workspace Deny mutated %d issue rows, want 0", count)
	}
}
