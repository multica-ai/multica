package workflows

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const MaxHookDepth = 4
const MaxHookNoProgress = 3

var ErrHookDepthExceeded = errors.New("workflow hook depth exceeded")

type HookStore interface {
	EffectivePolicies(context.Context, HookEvent) ([]HookPolicy, error)
	GetResult(context.Context, string) (HookResult, bool)
	SaveResult(context.Context, string, HookEvent, HookResult) error
}

type HookEngine struct {
	enabled     bool
	store       HookStore
	timeout     time.Duration
	now         func() time.Time
	beforeMatch func()
	actions     *ActionRegistry
	resolver    HookConditionResolver
}

func (e *HookEngine) WithActionRegistry(actions *ActionRegistry) *HookEngine {
	e.actions = actions
	return e
}

// WithConditionResolver injects the DB-backed resolver for deferred condition
// operators (eval_passed). Left nil, those conditions fail closed.
func (e *HookEngine) WithConditionResolver(resolver HookConditionResolver) *HookEngine {
	e.resolver = resolver
	return e
}

func NewHookEngine(enabled bool, store HookStore) *HookEngine {
	return &HookEngine{enabled: enabled, store: store, timeout: 250 * time.Millisecond, now: time.Now}
}

func (e *HookEngine) Evaluate(ctx context.Context, event HookEvent) (HookResult, error) {
	allow := HookResult{Decision: HookAllow}
	if !e.enabled || e.store == nil {
		return allow, nil
	}
	if event.HookDepth > MaxHookDepth {
		return allow, ErrHookDepthExceeded
	}
	if event.NoProgress > MaxHookNoProgress {
		return HookResult{Decision: HookBlock, Warning: "Hook stopped after repeated no-progress events"}, nil
	}
	key := event.EventID
	if key == "" {
		key = fmt.Sprintf("%s:%s:%s:%s", event.Type, event.WorkspaceID, event.IssueID, event.SessionID)
	}
	if previous, ok := e.store.GetResult(ctx, key); ok {
		return previous, nil
	}

	started := e.now()
	policies, err := e.store.EffectivePolicies(ctx, event)
	if err != nil {
		return allow, err
	}
	if e.beforeMatch != nil {
		e.beforeMatch()
	}
	if e.timeout > 0 && e.now().Sub(started) > e.timeout {
		result := timeoutResult(policies)
		result.RunID = key
		_ = e.store.SaveResult(ctx, key, event, result)
		return result, nil
	}

	result := HookResult{RunID: key, Decision: HookAllow, Modifications: map[string]any{}}
	for _, policy := range policies {
		if policy.Mode == HookModeOff || !eventListed(policy.Events, event.Type) || !policyMatches(policy, event) {
			continue
		}
		pure, deferred := splitHookConditions(policy.Conditions)
		if !evaluate(pure, hookConditionContext(event)) || !resolveHookConditions(ctx, e.resolver, policy, deferred, event) {
			continue
		}
		binding, ok := mostSpecificBinding(policy.Bindings, event)
		if !ok {
			continue
		}
		result.MatchedConditions = append(result.MatchedConditions, policy.Conditions...)
		for _, handler := range policy.Handlers {
			match := HookMatch{
				PolicyID: policy.ID, Version: policy.Version, HandlerID: handler.ID,
				SourceScope: binding, Decision: handler.Decision,
				DryRun:      policy.Mode == HookModeDryRun,
				Idempotency: fmt.Sprintf("%s:%d:%s", key, policy.Version, handler.ID), MatchedAt: e.now(),
			}
			result.Matches = append(result.Matches, match)
			if policy.Mode == HookModeDryRun {
				result.WouldDecision = strongerDecision(result.WouldDecision, handler.Decision)
				for index, action := range handler.Actions {
					result.ActionResults = append(result.ActionResults, HookActionResult{Type: action.Type, Status: HookActionWouldRun, IdempotencyKey: fmt.Sprintf("%s:%d:%s:%d", key, policy.Version, handler.ID, index), HandlerID: handler.ID, ActionIndex: len(result.ActionResults), Config: action.Config})
				}
				continue
			}
			result.Decision = strongerDecision(result.Decision, handler.Decision)
			if handler.Requirement != "" {
				result.Requirements = append(result.Requirements, handler.Requirement)
			}
			for field, value := range handler.Modifications {
				if containsString(event.MutableFields, field) {
					result.Modifications[field] = value
				}
			}
			for index, action := range handler.Actions {
				actionResult := HookActionResult{Type: action.Type, IdempotencyKey: fmt.Sprintf("%s:%d:%s:%d", key, policy.Version, handler.ID, index), HandlerID: handler.ID, ActionIndex: len(result.ActionResults), Config: action.Config}
				if e.actions == nil {
					actionResult.Status, actionResult.Error = HookActionFailed, ErrActionNotConfigured.Error()
				} else {
					output, actionErr := e.actions.Execute(ctx, action.Type, ActionInvocation{Policy: &policy, Event: event, Action: action})
					actionResult.Result = output
					if actionErr != nil {
						actionResult.Status, actionResult.Error = HookActionFailed, actionErr.Error()
						if errors.Is(actionErr, ErrHookActionPermissionDenied) {
							actionResult.Status = HookActionDenied
						}
					} else {
						actionResult.Status = HookActionSuccess
					}
				}
				result.ActionResults = append(result.ActionResults, actionResult)
				if actionResult.Status == HookActionFailed || actionResult.Status == HookActionDenied {
					switch policy.FailMode {
					case HookFailClosed:
						result.Decision = HookBlock
					case HookFailWarn:
						result.Warning = "A hook action failed"
					}
				}
			}
		}
	}
	if len(result.Modifications) == 0 {
		result.Modifications = nil
	}
	if err := e.store.SaveResult(ctx, key, event, result); err != nil {
		return allow, err
	}
	return result, nil
}

func timeoutResult(policies []HookPolicy) HookResult {
	for _, policy := range policies {
		if policy.Mode != HookModeOff && policy.FailMode == HookFailClosed {
			return HookResult{Decision: HookBlock, TimedOut: true, Warning: "Hook evaluation timed out and failed closed"}
		}
	}
	for _, policy := range policies {
		if policy.Mode != HookModeOff && policy.FailMode == HookFailWarn {
			return HookResult{Decision: HookAllow, TimedOut: true, Warning: "Hook evaluation timed out"}
		}
	}
	return HookResult{Decision: HookAllow, TimedOut: true}
}

func strongerDecision(current, next HookDecision) HookDecision {
	rank := map[HookDecision]int{HookAllow: 0, HookModify: 1, HookRequire: 2, HookBlock: 3}
	if current == "" || rank[next] > rank[current] {
		return next
	}
	return current
}

func eventListed(events []HookEventType, target HookEventType) bool {
	for _, event := range events {
		if event == target {
			return true
		}
	}
	return false
}

func policyMatches(policy HookPolicy, event HookEvent) bool {
	_, ok := mostSpecificBinding(policy.Bindings, event)
	return ok
}

func mostSpecificBinding(bindings []HookBinding, event HookEvent) (HookBinding, bool) {
	type candidate struct {
		binding HookBinding
		rank    int
	}
	var candidates []candidate
	for _, binding := range bindings {
		value, rank := bindingValue(binding.Kind, event)
		matched := binding.Kind == HookScopeModel && strings.HasPrefix(event.Model, binding.ID)
		if binding.Kind != HookScopeModel {
			matched = value != "" && value == binding.ID
		}
		if matched {
			candidates = append(candidates, candidate{binding: binding, rank: rank})
		}
	}
	if len(candidates) == 0 {
		return HookBinding{}, false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].rank == candidates[j].rank {
			return candidates[i].binding.Priority > candidates[j].binding.Priority
		}
		return candidates[i].rank > candidates[j].rank
	})
	return candidates[0].binding, true
}

func bindingValue(kind HookScopeKind, event HookEvent) (string, int) {
	switch kind {
	case HookScopeWorkspace:
		return event.WorkspaceID, 1
	case HookScopeProject:
		return event.ProjectID, 2
	case HookScopeWorkflow:
		return event.WorkflowID, 3
	case HookScopeAgent:
		return event.AgentID, 4
	case HookScopeModel:
		return event.Model, 4
	case HookScopeIssue:
		return event.IssueID, 5
	case HookScopeSession:
		return event.SessionID, 5
	default:
		return "", 0
	}
}

func hookConditionContext(event HookEvent) map[string]any {
	ctx := map[string]any{
		"event":     map[string]any{"type": event.Type},
		"workspace": map[string]any{"id": event.WorkspaceID},
		"project":   map[string]any{"id": event.ProjectID},
		"workflow":  map[string]any{"id": event.WorkflowID},
		"agent":     map[string]any{"id": event.AgentID, "model": event.Model},
		"issue":     map[string]any{"id": event.IssueID},
		"session":   map[string]any{"id": event.SessionID},
		"attempt":   event.Attempt, "no_progress": event.NoProgress, "hook_depth": event.HookDepth,
	}
	for key, value := range event.Context {
		ctx[key] = value
	}
	return ctx
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

type MemoryHookStore struct {
	mu       sync.Mutex
	policies []HookPolicy
	results  map[string]HookResult
}

func NewMemoryHookStore(policies []HookPolicy) *MemoryHookStore {
	return &MemoryHookStore{policies: append([]HookPolicy(nil), policies...), results: make(map[string]HookResult)}
}

func (s *MemoryHookStore) EffectivePolicies(_ context.Context, _ HookEvent) ([]HookPolicy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]HookPolicy(nil), s.policies...), nil
}

func (s *MemoryHookStore) GetResult(_ context.Context, key string) (HookResult, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, ok := s.results[key]
	return result, ok
}

func (s *MemoryHookStore) SaveResult(_ context.Context, key string, _ HookEvent, result HookResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.results[key]; !exists {
		s.results[key] = result
	}
	return nil
}

func (s *MemoryHookStore) RunCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.results)
}
