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
	"github.com/multica-ai/multica/server/internal/handler"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
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
// then calls the named tool with the provided arguments.
func (e *ToolExecutorInvoker) Invoke(ctx context.Context, agentID, workspaceID pgtype.UUID, toolName string, args map[string]any) (string, error) {
	tctx := ToolContext{AgentID: agentID, WorkspaceID: workspaceID}
	reg := NewDefaultRegistry(nil, e.Queries, tctx, e.CerebroQueries)

	result, err := reg.Call(ctx, toolName, args)
	if err != nil {
		return "", fmt.Errorf("tool %q: %w", toolName, err)
	}
	return result, nil
}
