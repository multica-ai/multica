package workflows

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	"skill.run", "judge.gate", "eval.gate", "eval.run",
	"issue.comment", "issue.status",
}

var issueStatusActionValues = []string{"backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"}

func validateTypedHookAction(action HookAction) error {
	known := false
	for _, actionType := range versionOneHookActionTypes {
		if action.Type == actionType {
			known = true
			break
		}
	}
	if !known {
		// Preserve forward compatibility for actions registered by another
		// workflow module. This validator only owns the typed actions above.
		return nil
	}
	requireString := func(key string) error {
		if strings.TrimSpace(hookConfigString(action.Config, key, "")) == "" {
			return fmt.Errorf("%s %s is required", action.Type, key)
		}
		return nil
	}
	switch action.Type {
	case "member.notify":
		for _, key := range []string{"member_id", "title", "message"} {
			if err := requireString(key); err != nil {
				return err
			}
		}
		return nil
	case "wakeup.create":
		if err := requireString("fire_at"); err != nil {
			return err
		}
		return requireString("prompt")
	case "wakeup.cancel":
		return requireString("wakeup_id")
	case "task.retry", "task.cancel":
		return requireString("task_id")
	case "artifact.create_or_update":
		for _, key := range []string{"title", "kind", "body"} {
			if err := requireString(key); err != nil {
				return err
			}
		}
		return nil
	case "approval.require":
		for _, key := range []string{"capability", "resource", "reason"} {
			if err := requireString(key); err != nil {
				return err
			}
		}
		return nil
	case "audit.record":
		return requireString("event")
	case "metric.increment":
		if err := requireString("name"); err != nil {
			return err
		}
		if _, ok := action.Config["amount"]; !ok {
			return fmt.Errorf("%s amount is required", action.Type)
		}
		return nil
	case "skill.run":
		return requireString("skill_name")
	case "judge.gate":
		if err := requireString("agent_id"); err != nil {
			return err
		}
		return requireString("rubric")
	case "eval.gate":
		if _, err := hookConfigUUID(action.Config, "eval_id"); err != nil {
			return fmt.Errorf("eval.gate requires eval_id: %w", err)
		}
		return nil
	case "eval.run":
		if _, err := hookConfigUUID(action.Config, "eval_id"); err != nil {
			return fmt.Errorf("eval.run requires eval_id: %w", err)
		}
		return nil
	case "issue.comment":
		return requireString("body")
	case "issue.status":
		if err := requireString("status"); err != nil {
			return err
		}
		status := hookConfigString(action.Config, "status", "")
		for _, allowed := range issueStatusActionValues {
			if status == allowed {
				return nil
			}
		}
		return fmt.Errorf("issue.status status %q is not a valid issue status", status)
	case "agent.dispatch":
		return requireString("agent_id")
	case "squad.dispatch":
		if err := requireString("squad_id"); err != nil {
			return err
		}
		return requireString("agent_id")
	case "session.handoff":
		_, err := validateHandoffAction(HookEvent{}, action.Config)
		return err
	case "workflow.activate", "workflow.pause", "workflow.resume", "workflow.stop":
		return requireString("workflow_id")
	default:
		return nil
	}
}

func validateTypedHookActions(policy HookPolicy) error {
	for _, handler := range policy.Handlers {
		for _, action := range handler.Actions {
			if err := validateTypedHookAction(action); err != nil {
				return err
			}
		}
	}
	return nil
}

func hookActionCapability(actionType string) string {
	switch actionType {
	case "member.notify", "issue.comment":
		return "add_comment"
	case "issue.status":
		return "update_issue"
	case "agent.dispatch", "squad.dispatch", "skill.run", "judge.gate", "eval.gate", "eval.run":
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
