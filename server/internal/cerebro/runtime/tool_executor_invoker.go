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
	"github.com/jackc/pgx/v5/pgxpool"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/handler"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ToolExecutorInvoker implements handler.ToolExecutorInvoker. It creates a
// per-request Registry with the agent's ToolContext and calls the named tool.
type ToolExecutorInvoker struct {
	Queries        *db.Queries
	CerebroQueries *cerebrodb.Queries
	// Pool backs the per-request registry's DB-dependent lookups — chiefly
	// GetGrantConfig, which reads agent_tool_grant.config_json for tools like
	// firtal_registry (data-source allowlist, allowed_apps, allow_write).
	// Without it the registry is db-less and every grant config reads empty,
	// silently denying config-gated actions through this server-side path.
	Pool *pgxpool.Pool
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
	// NewDefaultRegistry leaves the pool unset (it only registers tools); wire
	// it here so tools can read their per-agent grant config (mirrors how the
	// firtal-gateway executor sets taskRegistry.db).
	reg.db = e.Pool

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
