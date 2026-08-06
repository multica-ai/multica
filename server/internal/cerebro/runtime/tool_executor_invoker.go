package runtime

// CEREBRO-PATCH(tool-executor-invoker): TECH-3226 concrete implementation of
// handler.ToolExecutorInvoker. Creates a per-request tool registry with the
// agent's ToolContext and dispatches the named tool server-side, giving
// external runtimes (firtal-local) identical capabilities to firtal-gateway
// including connections (web_fetch, firtal_registry, Google Sheets, etc.).

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	cerebroloops "github.com/multica-ai/multica/server/internal/cerebro/loops"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/cerebro/workflows"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ToolExecutorInvoker implements handler.ToolExecutorInvoker. It creates a
// per-request Registry with the agent's ToolContext and calls the named tool.
type ToolExecutorInvoker struct {
	Queries        *db.Queries
	CerebroQueries *cerebrodb.Queries
	// Pool backs per-request registry DB lookups used by server-side tools.
	Pool      *pgxpool.Pool
	Policy    *toolpolicy.Store
	LoopStore *cerebroloops.Store
	Hooks     workflows.HookEvaluator
}

// compile-time interface check
var _ handler.ToolExecutorInvoker = (*ToolExecutorInvoker)(nil)

// Invoke creates a per-request tool registry for the given agent/workspace,
// resolves the same capability policy as firtal-gateway, then calls the
// named tool. userID drives authorship (member when set, agent when zero).
// cascadeUserID is retained for task-scoped adjunct tools; for task tokens it
// is task.OriginalUserID, and for user tokens it equals userID.
func (e *ToolExecutorInvoker) Invoke(ctx context.Context, agentID, workspaceID, userID, cascadeUserID, taskID pgtype.UUID, toolName string, args map[string]any) (string, error) {
	tctx := ToolContext{AgentID: agentID, WorkspaceID: workspaceID, UserID: userID, TaskID: taskID, LoopStore: e.LoopStore}
	var task db.AgentTaskQueue
	if taskID.Valid && e.Queries != nil {
		if loaded, err := e.Queries.GetAgentTask(ctx, taskID); err == nil && loaded.AgentID == agentID {
			task = loaded
			tctx.LoopStep = loopStepCapabilityFromTask(task)
		}
	}
	reg := NewDefaultRegistry(nil, e.Queries, tctx, e.CerebroQueries)
	// NewDefaultRegistry leaves the pool unset (it only registers tools); wire
	// it here so server-side tools can use their database-backed configuration
	// (mirrors how the firtal-gateway executor sets taskRegistry.db).
	reg.db = e.Pool

	toolAllowed := toolName == "open_loop_step" && tctx.LoopStep != nil
	enabledTools := []Tool{}
	if !toolAllowed {
		agent, err := e.Queries.GetAgent(ctx, agentID)
		if err != nil || e.Policy == nil {
			return "", handler.ErrToolNotPermitted
		}
		// The live registry is availability evidence; the unified policy resolver is
		// the access decision. A tool must exist in both surfaces to be callable.
		enabledTools = reg.GetToolPolicyExecutableToolsForAgent(
			ctx, e.Policy, workspaceID, agentID, agent.RuntimeID, agent.OwnerID, cascadeUserID,
		)
	}
	// CEREBRO-PATCH(memory-tools-offer): FIR-1794 — memory tools are additive,
	// offered by the three memory gates rather than the cascade, mirroring the
	// gateway executor. Each tool re-checks the gates at Call time, fail closed.
	for _, t := range CerebroMemoryToolsForTask(ctx, e.CerebroQueries, tctx, cascadeUserID) {
		reg.Register(t)
		enabledTools = append(enabledTools, t)
	}
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
	if err == nil {
		return result, nil
	}

	// Workflow, not this seam, decides what a failed tool call does next.
	decision := e.decideToolFailure(ctx, task, agentID, workspaceID, toolName, args, err, 1)
	if decision.Action == workflows.ToolFailureRetry {
		for retry := 1; retry <= decision.RetryLimit; retry++ {
			retried, retryErr := reg.Call(ctx, toolName, args)
			if retryErr == nil {
				return retried, nil
			}
			err = retryErr
			decision = e.decideToolFailure(ctx, task, agentID, workspaceID, toolName, args, err, retry+1)
			if decision.Action != workflows.ToolFailureRetry {
				break
			}
		}
	}
	if decision.Action == workflows.ToolFailureStop {
		return "", fmt.Errorf("tool %q: %w", toolName, decision.StopError())
	}
	return "", fmt.Errorf("tool %q: %w", toolName, err)
}

// decideToolFailure raises on.tool.failure and returns Workflow's decision.
// A missing evaluator or an evaluation error keeps today's behaviour: surface
// the raw tool error to the agent.
func (e *ToolExecutorInvoker) decideToolFailure(ctx context.Context, task db.AgentTaskQueue, agentID, workspaceID pgtype.UUID, toolName string, args map[string]any, toolErr error, call int) workflows.ToolFailureDecision {
	surface := workflows.ToolFailureDecision{Action: workflows.ToolFailureSurface}
	if e == nil || e.Hooks == nil {
		return surface
	}
	argumentKeys := make([]string, 0, len(args))
	for key := range args {
		argumentKeys = append(argumentKeys, key)
	}
	sort.Strings(argumentKeys)
	attempt := int(task.Attempt)
	result, err := e.Hooks.Evaluate(ctx, workflows.HookEvent{
		EventID:     fmt.Sprintf("%s:tool-failure:%s:%d:%d", util.UUIDToString(task.ID), toolName, attempt, call),
		Type:        workflows.HookOnToolFailure,
		WorkspaceID: util.UUIDToString(workspaceID),
		AgentID:     util.UUIDToString(agentID),
		IssueID:     util.UUIDToString(task.IssueID),
		SessionID:   task.SessionID.String,
		Attempt:     attempt,
		Context: map[string]any{
			"tool": map[string]any{
				"name": toolName, "argument_keys": argumentKeys, "error": toolErr.Error(),
				"call": call,
			},
			"task": map[string]any{"id": util.UUIDToString(task.ID), "status": task.Status},
		},
		MutableFields: workflows.ToolFailureMutableFields,
	})
	if err != nil {
		return surface
	}
	return workflows.ToolFailureDecisionFrom(result)
}
