package workflows

import (
	"context"
	"errors"
	"sync"
)

var ErrActionNotConfigured = errors.New("workflow action is not configured")

type ActionInvocation struct {
	Workflow *workflow
	Trigger  TriggerEvent
	Policy   *HookPolicy
	Event    HookEvent
	Action   HookAction
}

type ActionHandler func(context.Context, ActionInvocation) (map[string]any, error)

type ActionRegistry struct {
	mu       sync.RWMutex
	handlers map[string]ActionHandler
}

func NewActionRegistry() *ActionRegistry {
	return &ActionRegistry{handlers: make(map[string]ActionHandler)}
}

func (r *ActionRegistry) Register(name string, handler ActionHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[name] = handler
}

func (r *ActionRegistry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.handlers[name]
	return ok
}

func (r *ActionRegistry) Execute(ctx context.Context, name string, in ActionInvocation) (map[string]any, error) {
	r.mu.RLock()
	handler, ok := r.handlers[name]
	r.mu.RUnlock()
	if !ok || handler == nil {
		return nil, ErrActionNotConfigured
	}
	return handler(ctx, in)
}

type TypedActionExecutor interface {
	ExecuteHookAction(context.Context, HookPolicy, HookEvent, HookAction) (map[string]any, error)
}

var versionOneHookActionTypes = []string{
	"member.notify", "agent.dispatch", "squad.dispatch", "wakeup.create", "wakeup.cancel",
	"session.handoff", "task.retry", "task.cancel", "artifact.create_or_update",
	"workflow.activate", "workflow.pause", "workflow.resume", "workflow.stop",
	"approval.require", "audit.record", "metric.increment",
}

func hookActionCapability(actionType string) string {
	switch actionType {
	case "member.notify":
		return "add_comment"
	case "agent.dispatch", "squad.dispatch":
		return "trigger_other_agent"
	case "wakeup.create", "wakeup.cancel":
		return "schedule_agent_wakeup"
	case "session.handoff":
		return "manage_sessions"
	case "task.retry", "task.cancel":
		return "manage_work_sessions"
	case "artifact.create_or_update":
		return "manage_artifacts"
	case "workflow.activate", "workflow.pause", "workflow.resume", "workflow.stop":
		return "manage_workflows"
	case "approval.require":
		return "decide_approval"
	default:
		return ""
	}
}

func registerVersionOneHookActions(registry *ActionRegistry, executor TypedActionExecutor) {
	for _, actionType := range versionOneHookActionTypes {
		actionType := actionType
		registry.Register(actionType, func(ctx context.Context, in ActionInvocation) (map[string]any, error) {
			if executor == nil || in.Policy == nil {
				return nil, ErrActionNotConfigured
			}
			return executor.ExecuteHookAction(ctx, *in.Policy, in.Event, HookAction{Type: actionType, Config: in.Action.Config})
		})
	}
}

func registerLegacyActionNames(registry *ActionRegistry) {
	for _, name := range []string{
		ActionSetStatus, ActionCreateSubIssue, ActionSendReminder, ActionRunSkill,
		ActionCommentOnIssue, ActionRouteByDomain, ActionEscalateToOwner,
		ActionReassignIssue, ActionWebhookOutbound,
	} {
		registry.Register(name, nil)
	}
}
