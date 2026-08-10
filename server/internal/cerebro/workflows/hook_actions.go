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

// Symbolic action targets. A hook is written once but runs against many events,
// so "the agent that triggered this hook" is the only way to instruct the agent
// that is actually running — picking one agent up front cannot express it.
const (
	EventTargetAgent = "event.agent"
	EventTargetTask  = "event.task"
)

// HookEventTargetLabels is the single source for how a symbolic target reads to
// a person. The editor renders these; nothing else may invent its own wording.
var HookEventTargetLabels = map[string]string{
	EventTargetAgent: "The agent that triggered this hook",
	EventTargetTask:  "The task that produced this event",
}

func isEventTarget(value string) bool {
	_, ok := HookEventTargetLabels[value]
	return ok
}

func resolveEventTarget(target string, event HookEvent) (string, error) {
	switch target {
	case EventTargetAgent:
		if event.AgentID == "" {
			return "", fmt.Errorf("this event has no agent, so %q cannot be resolved", HookEventTargetLabels[target])
		}
		return event.AgentID, nil
	case EventTargetTask:
		task, _ := event.Context["task"].(map[string]any)
		id, _ := task["id"].(string)
		if id == "" {
			return "", fmt.Errorf("this event has no task, so %q cannot be resolved", HookEventTargetLabels[target])
		}
		return id, nil
	default:
		return "", fmt.Errorf("unknown event target %q", target)
	}
}

// resolveActionEventTargets rewrites symbolic config values into concrete ids
// for this one event, so every downstream reader keeps parsing plain UUIDs.
func resolveActionEventTargets(action HookAction, event HookEvent) (HookAction, error) {
	definition, configured := hookActionManifestByType[action.Type]
	if !configured {
		return action, nil
	}
	// Clone before writing: the stored policy config is shared across every
	// event this hook sees and must never be mutated by one of them.
	config := make(map[string]any, len(action.Config))
	for key, value := range action.Config {
		config[key] = value
	}
	for _, field := range definition.Fields {
		if field.EventTarget == "" {
			continue
		}
		if value, _ := action.Config[field.Key].(string); value != field.EventTarget {
			continue
		}
		concrete, err := resolveEventTarget(field.EventTarget, event)
		if err != nil {
			return action, fmt.Errorf("%s %s: %w", action.Type, field.Key, err)
		}
		config[field.Key] = concrete
	}
	return HookAction{Type: action.Type, Config: config}, nil
}

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
	for _, field := range definition.Fields {
		text, ok := action.Config[field.Key].(string)
		if ok && isEventTarget(text) && field.EventTarget != text {
			return fmt.Errorf("%s %s does not accept %q", action.Type, field.Key, text)
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
