package runtime

import (
	"context"
	"testing"
	"time"

	googleuuid "github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/approvals"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/platformaction"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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

func TestGatewayCreateIssuePermission_AskDecisionControlsSingleMutation(t *testing.T) {
	for _, tc := range []struct {
		name       string
		decision   string
		wantIssues int
	}{
		{name: "approve once", decision: approvals.StatusApproved, wantIssues: 1},
		{name: "reject", decision: approvals.StatusRejected, wantIssues: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, agentID := newToolPolicyGatedExecutor(t, &gateFakeApprovals{})
			store := toolpolicy.NewStore(runtimeAccountTestPool)
			if _, err := store.Set(context.Background(), toolpolicy.SetParams{
				WorkspaceID: runtimeAccountTestWSID,
				ToolKey:     "create_issue",
				Layer:       toolpolicy.LayerWorkspace,
				SubjectID:   runtimeAccountTestWSID,
				Setting:     toolpolicy.SettingAsk,
			}); err != nil {
				t.Fatalf("set create_issue Workspace Ask: %v", err)
			}
			e.platformActionGate = platformaction.NewDefault(
				store,
				db.New(runtimeAccountTestPool),
				cerebrodb.New(runtimeAccountTestPool),
				runtimeAccountTestPool,
				nil,
			)

			title := "Gateway decided " + googleuuid.NewString()
			t.Cleanup(func() {
				_, _ = runtimeAccountTestPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1 AND title = $2`, runtimeAccountTestWSID, title)
			})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			type guardResult struct {
				allowed bool
				reason  string
			}
			resultCh := make(chan guardResult, 1)
			go func() {
				allowed, reason := e.guardPlatformAction(ctx, agentID, runtimeAccountTestWSID, "create_issue", map[string]any{"title": title}, GatewayRequestMeta{TriggerUserID: googleuuid.NewString()})
				resultCh <- guardResult{allowed: allowed, reason: reason}
			}()

			approvalID := waitForGatewayCreateIssueApproval(t, ctx, agentID)
			if _, err := runtimeAccountTestPool.Exec(ctx, `
				UPDATE cerebro_approval_request
				SET status = $2, expires_at = NOW() + INTERVAL '1 minute', updated_at = NOW()
				WHERE id = $1`, approvalID, tc.decision); err != nil {
				t.Fatalf("decide Gateway approval: %v", err)
			}

			var result guardResult
			select {
			case result = <-resultCh:
			case <-ctx.Done():
				t.Fatalf("Gateway did not resume after %s: %v", tc.decision, ctx.Err())
			}
			if result.allowed {
				tool := &FirtalCreateIssueTool{queries: e.queries, tctx: ToolContext{AgentID: agentID, WorkspaceID: runtimeAccountTestWSID}}
				if _, err := tool.Call(ctx, map[string]any{"title": title}); err != nil {
					t.Fatalf("approved Gateway create_issue failed: %v", err)
				}
			}
			var count int
			if err := runtimeAccountTestPool.QueryRow(ctx, `SELECT COUNT(*) FROM issue WHERE workspace_id = $1 AND title = $2`, runtimeAccountTestWSID, title).Scan(&count); err != nil {
				t.Fatalf("count Gateway-created issues: %v", err)
			}
			if count != tc.wantIssues {
				t.Fatalf("Gateway decision %s allowed=%v reason=%q mutated %d issue rows, want %d", tc.decision, result.allowed, result.reason, count, tc.wantIssues)
			}
		})
	}
}

func waitForGatewayCreateIssueApproval(t *testing.T, ctx context.Context, agentID pgtype.UUID) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var approvalID string
		err := runtimeAccountTestPool.QueryRow(ctx, `
			SELECT id::text
			FROM cerebro_approval_request
			WHERE requester_id = $1 AND capability = 'create_issue' AND status = 'pending'
			ORDER BY created_at DESC
			LIMIT 1`, agentID).Scan(&approvalID)
		if err == nil {
			return approvalID
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Gateway did not create a pending create_issue approval")
	return ""
}
