package workflows

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	// Catalog load for every active policy is on the critical path of every
	// lifecycle event (including before.session.start). 250ms was too tight
	// under normal DB load and killed agent runs. 2s leaves headroom, and
	// exceeding it never blocks — see timeoutResult.
	return &HookEngine{enabled: enabled, store: store, timeout: 2 * time.Second, now: time.Now}
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
		return HookResult{Evaluated: true, Decision: HookBlock, Warning: "Hook stopped after repeated no-progress events"}, nil
	}
	key := event.EventID
	if key == "" {
		key = fmt.Sprintf("%s:%s:%s:%s", event.Type, event.WorkspaceID, event.IssueID, event.SessionID)
	}
	if previous, ok := e.store.GetResult(ctx, key); ok {
		return previous, nil
	}

	started := e.now()
	loadCtx := ctx
	if e.timeout > 0 {
		// A real deadline, not a post-hoc "was that slow?" check: without it a
		// hung catalog query holds the lifecycle event open indefinitely.
		var cancel context.CancelFunc
		loadCtx, cancel = context.WithTimeout(ctx, e.timeout)
		defer cancel()
	}
	policies, err := e.store.EffectivePolicies(loadCtx, event)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			// No SaveResult: the store is the thing that is hung. The log line
			// is the only trace, so it must always be written — a timeout means
			// hooks are degraded to allow-all and someone has to see that.
			logHookLoadTimeout(event, "catalog load exceeded the deadline")
			result := timeoutResult()
			result.RunID = key
			return result, nil
		}
		return allow, err
	}
	if e.beforeMatch != nil {
		e.beforeMatch()
	}
	if e.timeout > 0 && e.now().Sub(started) > e.timeout {
		logHookLoadTimeout(event, "catalog load exceeded the budget")
		result := timeoutResult()
		result.RunID = key
		_ = e.store.SaveResult(ctx, key, event, result)
		return result, nil
	}

	result := HookResult{RunID: key, Evaluated: true, Decision: HookAllow, Modifications: map[string]any{}}
policyLoop:
	for _, policy := range policies {
		if policy.Mode == HookModeOff || !eventListed(policy.Events, event.Type) || !policyMatches(policy, event) {
			continue
		}
		conditionsMatch, matchedConditions := evaluateHookPolicyConditions(ctx, e.resolver, policy, event)
		if !conditionsMatch {
			continue
		}
		binding, ok := mostSpecificBinding(policy.Bindings, event)
		if !ok {
			continue
		}
		result.MatchedConditions = append(result.MatchedConditions, matchedConditions...)
		for _, handler := range policy.Handlers {
			match := HookMatch{
				PolicyID: policy.ID, PolicyName: policy.Name, Version: policy.Version, HandlerID: handler.ID,
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
				result.Requirements = append(result.Requirements, policy.Name+": "+handler.Requirement)
			}
			for field, value := range handler.Modifications {
				if containsString(event.MutableFields, field) {
					if resolved, ok := resolveHookModification(value, event); ok {
						result.Modifications[field] = resolved
					}
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
				// A gate action (quality.gate) that ran successfully can still
				// REJECT the send: it reports a decision + requirement in its
				// output. That verdict is honored unconditionally — unlike an
				// action *failure* below, it does NOT depend on fail_mode, so a
				// bad comment is blocked even under fail_mode: warn.
				if actionResult.Status == HookActionSuccess {
					if decision, requirement, ok := actionGateVerdict(actionResult.Result); ok {
						result.Decision = strongerDecision(result.Decision, decision)
						// The match was recorded with the handler's own decision
						// ("allow"); this verdict came from the action. Raise it
						// so the stopped user is told which hook judged them.
						last := &result.Matches[len(result.Matches)-1]
						last.Decision = strongerDecision(last.Decision, decision)
						if requirement != "" {
							result.Requirements = append(result.Requirements, policy.Name+": "+requirement)
						}
					}
				}
				if actionResult.Status == HookActionFailed || actionResult.Status == HookActionDenied {
					// fail_mode governs only action FAILURES (e.g. the judge
					// gateway is unreachable): closed blocks, warn lets through.
					switch policy.FailMode {
					case HookFailClosed:
						result.Decision = HookBlock
						// Name the hook on the match too: without this the stop
						// carries no author at all, which is the "mystery hook
						// blocked my run" report this engine keeps producing.
						last := &result.Matches[len(result.Matches)-1]
						last.Decision = HookBlock
						result.Warning = fmt.Sprintf("Hook %q stopped this because one of its actions failed: %s", policy.Name, actionResult.Error)
						break policyLoop
					case HookFailWarn:
						result.Warning = fmt.Sprintf("An action in hook %q failed and was allowed through: %s", policy.Name, actionResult.Error)
					}
				}
			}
		}
	}
	if len(result.Modifications) == 0 {
		result.Modifications = nil
	}
	// One place decides who gets named. Every gate downstream reads BlockedBy
	// instead of inventing its own anonymous "blocked by a workflow hook" text.
	if result.Decision == HookBlock || result.Decision == HookRequire {
		result.BlockedBy = result.blockingHook()
	}
	if err := e.store.SaveResult(ctx, key, event, result); err != nil {
		return allow, err
	}
	return result, nil
}

// timeoutResult is what happens when loading the hook catalog exceeds the
// engine budget: always allow, with a named warning.
//
// fail_mode governs action FAILURES (the judge gateway is unreachable, see the
// action loop above) — it is a verdict about the guarded content. A slow
// catalog load says nothing about the content: no policy has been matched yet,
// let alone evaluated. Blocking there turns a database hiccup into a dead agent
// run, which is exactly the failure this engine must never cause.
func timeoutResult() HookResult {
	return HookResult{Evaluated: true, Decision: HookAllow, TimedOut: true, Warning: "Hook evaluation timed out; the event was allowed through"}
}

// logHookLoadTimeout makes the allow-all fallback visible. Without it the
// degraded state is silent: every hook is effectively off and nothing says so.
func logHookLoadTimeout(event HookEvent, reason string) {
	slog.Warn("workflow hook catalog load timed out; event allowed through",
		"reason", reason, "event_type", event.Type, "workspace_id", event.WorkspaceID,
		"issue_id", event.IssueID, "agent_id", event.AgentID)
}

// actionGateVerdict reads a decision an action reports on success. A quality
// gate that judged the content bad returns {"decision":"require"|"block",
// "requirement":"…"}; this raises the send decision independent of fail_mode.
// Any other output (e.g. {"pass":true}) yields ok=false and changes nothing.
func actionGateVerdict(out map[string]any) (HookDecision, string, bool) {
	raw, _ := out["decision"].(string)
	decision := HookDecision(raw)
	if decision != HookBlock && decision != HookRequire {
		return "", "", false
	}
	requirement, _ := out["requirement"].(string)
	return decision, strings.TrimSpace(requirement), true
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
		"actor":     map[string]any{"type": event.Actor.Type, "id": event.Actor.ID},
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

func resolveHookModification(value any, event HookEvent) (any, bool) {
	path, dynamic := value.(string)
	if !dynamic || !strings.HasPrefix(path, "$event.") {
		return value, true
	}
	return lookup(strings.TrimPrefix(path, "$event."), hookConditionContext(event))
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
