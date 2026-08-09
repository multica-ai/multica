package workflows

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
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

var versionOneHookActionTypes = manifestHookActionTypes()

var issueStatusActionValues = []string{"backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"}

func validateTypedHookAction(action HookAction) error {
	definition, configured := hookActionManifestByType[action.Type]
	if !configured {
		// Preserve forward compatibility for actions registered by another
		// workflow module. Structural Draft validation owns rejecting unknown
		// action types at the HTTP boundary.
		return nil
	}
	for _, field := range definition.Fields {
		if !field.Required {
			continue
		}
		value, exists := action.Config[field.Key]
		missing := !exists || value == nil
		if text, ok := value.(string); ok && strings.TrimSpace(text) == "" {
			missing = true
		}
		if missing {
			return fmt.Errorf("%s %s is required", action.Type, field.Key)
		}
	}
	switch action.Type {
	case "wakeup.create":
		if _, err := time.Parse(time.RFC3339, hookConfigString(action.Config, "fire_at", "")); err != nil {
			return fmt.Errorf("wakeup.create requires fire_at: %w", err)
		}
		return nil
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
	case "issue.status":
		status := hookConfigString(action.Config, "status", "")
		for _, allowed := range issueStatusActionValues {
			if status == allowed {
				return nil
			}
		}
		return fmt.Errorf("issue.status status %q is not a valid issue status", status)
	case "session.handoff":
		_, err := validateHandoffAction(HookEvent{}, action.Config)
		return err
	default:
		return nil
	}
}

func isVersionOneHookActionType(target string) bool {
	for _, actionType := range versionOneHookActionTypes {
		if target == actionType {
			return true
		}
	}
	return false
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
	return hookActionManifestByType[actionType].Capability
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
