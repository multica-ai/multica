package workflows

import (
	"context"
)

// WorkspaceHookEvaluator is the one production entry point for hook events.
// The server environment switch is the emergency kill switch; the
// cerebro_workflow_hooks flag lets a single workspace switch the engine off
// without a deploy, and is resolved per event because one process serves
// every workspace.
type WorkspaceHookEvaluator struct {
	serverEnabled bool
	evaluator     HookEvaluator
	flags         WorkspaceFlagResolver
}

func NewWorkspaceHookEvaluator(serverEnabled bool, evaluator HookEvaluator) *WorkspaceHookEvaluator {
	return &WorkspaceHookEvaluator{serverEnabled: serverEnabled, evaluator: evaluator}
}

// WithWorkspaceFlags attaches the per-workspace cerebro_workflow_hooks lookup.
// Without it the evaluator keeps the registry default, which is ON.
func (e *WorkspaceHookEvaluator) WithWorkspaceFlags(flags WorkspaceFlagResolver) *WorkspaceHookEvaluator {
	if e != nil {
		e.flags = flags
	}
	return e
}

func (e *WorkspaceHookEvaluator) Evaluate(ctx context.Context, event HookEvent) (HookResult, error) {
	allow := HookResult{Decision: HookAllow}
	if e == nil || !e.serverEnabled || e.evaluator == nil || event.WorkspaceID == "" {
		return allow, nil
	}
	if e.flags != nil {
		enabled, err := e.flags.WorkflowHooksEnabledForWorkspace(ctx, event.WorkspaceID)
		if err != nil {
			return allow, err
		}
		if !enabled {
			return allow, nil
		}
	}
	return e.evaluator.Evaluate(ctx, event)
}
