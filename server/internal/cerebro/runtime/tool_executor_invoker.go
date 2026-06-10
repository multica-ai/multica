package runtime

// CEREBRO-PATCH(tool-executor-invoker): TECH-3226 concrete implementation of
// handler.ToolExecutorInvoker. Creates a per-request tool registry with the
// agent's ToolContext and dispatches the named tool server-side, giving
// external runtimes (firtal-local) identical capabilities to firtal-gateway
// including connections (web_fetch, firtal_registry, Google Sheets, etc.).

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/handler"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ToolExecutorInvoker implements handler.ToolExecutorInvoker. It creates a
// per-request Registry with the agent's ToolContext and calls the named tool.
type ToolExecutorInvoker struct {
	Queries        *db.Queries
	CerebroQueries *cerebrodb.Queries
}

// compile-time interface check
var _ handler.ToolExecutorInvoker = (*ToolExecutorInvoker)(nil)

// Invoke creates a per-request tool registry for the given agent/workspace,
// runs the same cascade permission check as firtal-gateway, then calls the
// named tool. userID drives authorship (member when set, agent when zero).
// cascadeUserID is passed to GetCascadeEnabledToolsForAgent for permission
// resolution; for task tokens this is task.OriginalUserID, for user tokens
// it equals userID.
func (e *ToolExecutorInvoker) Invoke(ctx context.Context, agentID, workspaceID, userID, cascadeUserID pgtype.UUID, toolName string, args map[string]any) (string, error) {
	tctx := ToolContext{AgentID: agentID, WorkspaceID: workspaceID, UserID: userID}
	reg := NewDefaultRegistry(nil, e.Queries, tctx, e.CerebroQueries)

	// CEREBRO-PATCH(invoke-cascade-check): TECH-3226 — same permission check as
	// firtal-gateway's agentHasCallableTools: cascade grants, not raw agent_tool_grant.
	enabledTools := reg.GetCascadeEnabledToolsForAgent(ctx, e.CerebroQueries, agentID, cascadeUserID)
	toolAllowed := false
	for _, t := range enabledTools {
		if t.Name() == toolName {
			toolAllowed = true
			break
		}
	}
	if !toolAllowed {
		return "", handler.ErrToolNotPermitted
	}

	result, err := reg.Call(ctx, toolName, args)
	if err != nil {
		return "", fmt.Errorf("tool %q: %w", toolName, err)
	}
	return result, nil
}
