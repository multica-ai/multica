package workflows

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestHookEventCatalogContainsVersionOneParity(t *testing.T) {
	want := []HookEventType{
		HookBeforeSessionStart, HookAfterSessionStart,
		HookBeforeSessionEnd, HookAfterSessionEnd,
		HookBeforePromptAssemble,
		HookBeforeToolCall, HookAfterToolCall, HookOnToolFailure,
		HookBeforeTaskComplete, HookBeforeAgentStop,
		HookBeforeSubagentStart, HookAfterSubagentStop,
		HookOnError,
	}

	for _, eventType := range want {
		if _, ok := HookEventCatalog[eventType]; !ok {
			t.Fatalf("missing version one hook event %q", eventType)
		}
	}
}

func TestVersionOneEventMatrixUsesSharedMatcherDecisionActionAndAuditContract(t *testing.T) {
	events := []HookEventType{
		HookBeforeSessionStart, HookAfterSessionStart, HookBeforeSessionEnd, HookAfterSessionEnd,
		HookBeforePromptAssemble, HookBeforeToolCall, HookAfterToolCall, HookOnToolFailure,
		HookBeforeTaskComplete, HookBeforeAgentStop, HookBeforeSubagentStart, HookAfterSubagentStop, HookOnError,
	}
	for _, eventType := range events {
		t.Run(string(eventType), func(t *testing.T) {
			policy := newTestHookPolicy("parity", HookRequire, HookModeEnforce, HookBinding{Kind: HookScopeWorkspace, ID: "ws-1"})
			policy.Events = []HookEventType{eventType}
			policy.Conditions = []Condition{{Field: "event.type", Op: "eq", Value: string(eventType)}}
			policy.Handlers[0].Requirement = "Record a valid continuation"
			policy.Handlers[0].Actions = []HookAction{{Type: "audit.record", Config: map[string]any{"event": string(eventType)}}}
			store := NewMemoryHookStore([]HookPolicy{policy})
			actions := NewActionRegistry()
			actions.Register("audit.record", func(_ context.Context, in ActionInvocation) (map[string]any, error) {
				return map[string]any{"event": in.Action.Config["event"]}, nil
			})
			result, err := NewHookEngine(true, store).WithActionRegistry(actions).Evaluate(context.Background(), HookEvent{
				EventID: "event-" + string(eventType), Type: eventType, WorkspaceID: "ws-1",
				SessionID: "session-1", AgentID: "agent-1", IssueID: "issue-1",
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Decision != HookRequire || len(result.Matches) != 1 || len(result.ActionResults) != 1 || result.ActionResults[0].Status != HookActionSuccess {
				t.Fatalf("shared contract result = %#v", result)
			}
			if result.Matches[0].SourceScope.Kind != HookScopeWorkspace || result.Matches[0].PolicyID != "parity" || result.RunID == "" || store.RunCount() != 1 {
				t.Fatalf("audit contract result = %#v runs=%d", result, store.RunCount())
			}
		})
	}
}

func TestStandardCompiledIssueAndHooksShareConditionAndActionContracts(t *testing.T) {
	conditions := []Condition{{Field: "issue.id", Op: "eq", Value: "issue-1"}}
	trigger := TriggerEvent{IssueID: "issue-1", Raw: map[string]any{"issue": map[string]any{"id": "issue-1"}}}
	if !evaluate(conditions, trigger.Raw) {
		t.Fatal("standard/compiled Issue condition path did not match")
	}

	policy := newTestHookPolicy("shared-engine", HookAllow, HookModeEnforce, HookBinding{Kind: HookScopeIssue, ID: "issue-1"})
	policy.Conditions = conditions
	policy.Handlers[0].Actions = []HookAction{{Type: "shared.action"}}
	registry := NewActionRegistry()
	var standardCalls, hookCalls int
	registry.Register("standard.action", func(_ context.Context, in ActionInvocation) (map[string]any, error) {
		if in.Workflow == nil || in.Trigger.IssueID != "issue-1" {
			t.Fatalf("standard invocation = %#v", in)
		}
		standardCalls++
		return nil, nil
	})
	registry.Register("shared.action", func(_ context.Context, in ActionInvocation) (map[string]any, error) {
		if in.Policy == nil || in.Event.IssueID != "issue-1" {
			t.Fatalf("hook invocation = %#v", in)
		}
		hookCalls++
		return nil, nil
	})
	wf := workflow{actionType: "standard.action"}
	if _, err := registry.Execute(context.Background(), wf.actionType, ActionInvocation{Workflow: &wf, Trigger: trigger}); err != nil {
		t.Fatal(err)
	}
	result, err := NewHookEngine(true, NewMemoryHookStore([]HookPolicy{policy})).WithActionRegistry(registry).Evaluate(context.Background(), HookEvent{
		EventID: "shared-event", Type: HookBeforeTaskComplete, WorkspaceID: "ws-1", IssueID: "issue-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if standardCalls != 1 || hookCalls != 1 || len(result.MatchedConditions) != 1 || len(result.ActionResults) != 1 || result.ActionResults[0].Status != HookActionSuccess {
		t.Fatalf("shared contracts standard=%d hook=%d result=%#v", standardCalls, hookCalls, result)
	}
}

func TestHookScopePrecedenceCoversEveryVersionOneBinding(t *testing.T) {
	event := HookEvent{
		EventID: "precedence-event", Type: HookBeforeTaskComplete,
		WorkspaceID: "ws-1", ProjectID: "project-1", WorkflowID: "workflow-1",
		AgentID: "agent-1", Model: "claude-opus-4-6", IssueID: "issue-1", SessionID: "session-1",
	}
	bindings := []HookBinding{
		{Kind: HookScopeWorkspace, ID: event.WorkspaceID},
		{Kind: HookScopeProject, ID: event.ProjectID},
		{Kind: HookScopeWorkflow, ID: event.WorkflowID},
		{Kind: HookScopeAgent, ID: event.AgentID},
		{Kind: HookScopeModel, ID: "claude-opus"},
		{Kind: HookScopeIssue, ID: event.IssueID},
		{Kind: HookScopeSession, ID: event.SessionID, Priority: 1},
	}
	policies := make([]HookPolicy, 0, len(bindings))
	for index, binding := range bindings {
		decision := HookAllow
		mode := HookModeEnforce
		if binding.Kind == HookScopeWorkspace {
			decision = HookBlock
			mode = HookModeManaged
		}
		policies = append(policies, newTestHookPolicy(fmt.Sprintf("scope-%d", index), decision, mode, binding))
	}
	result, err := NewHookEngine(true, NewMemoryHookStore(policies)).Evaluate(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != HookBlock || len(result.Matches) != len(bindings) {
		t.Fatalf("precedence result = %#v", result)
	}
	for index, match := range result.Matches {
		if match.SourceScope != bindings[index] {
			t.Fatalf("match %d source = %#v, want %#v", index, match.SourceScope, bindings[index])
		}
	}
	if mostSpecific, ok := mostSpecificBinding(bindings, event); !ok || mostSpecific.Kind != HookScopeSession {
		t.Fatalf("most specific binding = %#v, ok=%v", mostSpecific, ok)
	}
}

func TestVersionOneTypedActionMatrixRecordsOutcomesAndNoProgressStops(t *testing.T) {
	policy := newTestHookPolicy("typed-actions", HookAllow, HookModeEnforce, HookBinding{Kind: HookScopeWorkspace, ID: "ws-1"})
	for _, actionType := range versionOneHookActionTypes {
		policy.Handlers[0].Actions = append(policy.Handlers[0].Actions, HookAction{Type: actionType})
	}
	registry := NewActionRegistry()
	registerVersionOneHookActions(registry, &fakeTypedActionExecutor{})
	result, err := NewHookEngine(true, NewMemoryHookStore([]HookPolicy{policy})).WithActionRegistry(registry).Evaluate(context.Background(), HookEvent{
		EventID: "typed-action-event", Type: HookBeforeTaskComplete, WorkspaceID: "ws-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ActionResults) != len(versionOneHookActionTypes) {
		t.Fatalf("action outcomes = %d, want %d", len(result.ActionResults), len(versionOneHookActionTypes))
	}
	for index, actionResult := range result.ActionResults {
		if actionResult.Type != versionOneHookActionTypes[index] || actionResult.Status != HookActionSuccess {
			t.Fatalf("action %d result = %#v", index, actionResult)
		}
	}

	stopped, err := NewHookEngine(true, NewMemoryHookStore([]HookPolicy{policy})).Evaluate(context.Background(), HookEvent{
		EventID: "no-progress-event", Type: HookBeforeTaskComplete, WorkspaceID: "ws-1", NoProgress: MaxHookNoProgress + 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Decision != HookBlock || stopped.Warning == "" {
		t.Fatalf("no-progress result = %#v", stopped)
	}
}

func TestHookEngineNoopsWhenDisabled(t *testing.T) {
	engine := NewHookEngine(false, nil)
	result, err := engine.Evaluate(context.Background(), HookEvent{Type: HookBeforeTaskComplete})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != HookAllow || len(result.Matches) != 0 {
		t.Fatalf("disabled engine returned %#v", result)
	}
}

func TestHookEngineBlockWinsAndRequirementsAreCombined(t *testing.T) {
	policies := []HookPolicy{
		newTestHookPolicy("require-a", HookRequire, HookModeEnforce, HookBinding{Kind: HookScopeWorkspace, ID: "ws-1"}),
		newTestHookPolicy("require-b", HookRequire, HookModeEnforce, HookBinding{Kind: HookScopeAgent, ID: "agent-1"}),
		newTestHookPolicy("block", HookBlock, HookModeEnforce, HookBinding{Kind: HookScopeIssue, ID: "issue-1"}),
	}
	policies[0].Handlers[0].Requirement = "Create a wakeup"
	policies[1].Handlers[0].Requirement = "Record a continuation"
	policies[0].Conditions = []Condition{{Field: "workspace.id", Op: "eq", Value: "ws-1"}}
	policies[1].Conditions = []Condition{{Field: "agent.id", Op: "eq", Value: "agent-1"}}
	policies[2].Conditions = []Condition{{Field: "issue.id", Op: "eq", Value: "issue-1"}}

	engine := NewHookEngine(true, NewMemoryHookStore(policies))
	result, err := engine.Evaluate(context.Background(), HookEvent{
		EventID: "event-1", Type: HookBeforeTaskComplete, WorkspaceID: "ws-1",
		AgentID: "agent-1", IssueID: "issue-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != HookBlock {
		t.Fatalf("decision = %q, want block", result.Decision)
	}
	if !reflect.DeepEqual(result.Requirements, []string{"Create a wakeup", "Record a continuation"}) {
		t.Fatalf("requirements = %#v", result.Requirements)
	}
	if !reflect.DeepEqual(result.MatchedConditions, []Condition{
		{Field: "workspace.id", Op: "eq", Value: "ws-1"},
		{Field: "agent.id", Op: "eq", Value: "agent-1"},
		{Field: "issue.id", Op: "eq", Value: "issue-1"},
	}) {
		t.Fatalf("matched conditions = %#v", result.MatchedConditions)
	}
}

func TestHookEngineDryRunReportsWouldDecisionWithoutEnforcing(t *testing.T) {
	policy := newTestHookPolicy("dry-run", HookBlock, HookModeDryRun, HookBinding{Kind: HookScopeWorkspace, ID: "ws-1"})
	engine := NewHookEngine(true, NewMemoryHookStore([]HookPolicy{policy}))

	result, err := engine.Evaluate(context.Background(), HookEvent{
		EventID: "event-1", Type: HookBeforeTaskComplete, WorkspaceID: "ws-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != HookAllow || result.WouldDecision != HookBlock {
		t.Fatalf("dry-run result = %#v", result)
	}
}

func TestHookEngineModifyOnlyAllowsDeclaredFields(t *testing.T) {
	policy := newTestHookPolicy("modify", HookModify, HookModeEnforce, HookBinding{Kind: HookScopeWorkspace, ID: "ws-1"})
	policy.Handlers[0].Modifications = map[string]any{"prompt": "safe", "workspace_id": "other"}
	engine := NewHookEngine(true, NewMemoryHookStore([]HookPolicy{policy}))

	result, err := engine.Evaluate(context.Background(), HookEvent{
		EventID: "event-1", Type: HookBeforePromptAssemble, WorkspaceID: "ws-1",
		MutableFields: []string{"prompt"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Modifications, map[string]any{"prompt": "safe"}) {
		t.Fatalf("modifications = %#v", result.Modifications)
	}
}

func TestHookEngineManagedPolicyCannotBeLoosened(t *testing.T) {
	managed := newTestHookPolicy("managed", HookBlock, HookModeManaged, HookBinding{Kind: HookScopeWorkspace, ID: "ws-1"})
	allow := newTestHookPolicy("issue-allow", HookAllow, HookModeEnforce, HookBinding{Kind: HookScopeIssue, ID: "issue-1"})
	engine := NewHookEngine(true, NewMemoryHookStore([]HookPolicy{managed, allow}))

	result, err := engine.Evaluate(context.Background(), HookEvent{
		EventID: "event-1", Type: HookBeforeTaskComplete, WorkspaceID: "ws-1", IssueID: "issue-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != HookBlock {
		t.Fatalf("decision = %q, want managed block", result.Decision)
	}
}

func TestHookEngineIsIdempotentAndStopsRecursiveDepth(t *testing.T) {
	policy := newTestHookPolicy("block", HookBlock, HookModeEnforce, HookBinding{Kind: HookScopeWorkspace, ID: "ws-1"})
	store := NewMemoryHookStore([]HookPolicy{policy})
	engine := NewHookEngine(true, store)
	event := HookEvent{EventID: "event-1", Type: HookBeforeTaskComplete, WorkspaceID: "ws-1"}

	first, err := engine.Evaluate(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	second, err := engine.Evaluate(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if first.RunID == "" || second.RunID != first.RunID || store.RunCount() != 1 {
		t.Fatalf("idempotency failed: first=%#v second=%#v runs=%d", first, second, store.RunCount())
	}

	_, err = engine.Evaluate(context.Background(), HookEvent{
		EventID: "event-2", Type: HookOnError, WorkspaceID: "ws-1", HookDepth: MaxHookDepth + 1,
	})
	if !errors.Is(err, ErrHookDepthExceeded) {
		t.Fatalf("depth error = %v", err)
	}
}

func TestHookEngineTimeoutUsesExplicitFailMode(t *testing.T) {
	policy := newTestHookPolicy("timeout", HookBlock, HookModeEnforce, HookBinding{Kind: HookScopeWorkspace, ID: "ws-1"})
	policy.FailMode = HookFailClosed
	engine := NewHookEngine(true, NewMemoryHookStore([]HookPolicy{policy}))
	engine.timeout = time.Nanosecond
	engine.beforeMatch = func() { time.Sleep(time.Millisecond) }

	result, err := engine.Evaluate(context.Background(), HookEvent{
		EventID: "event-1", Type: HookBeforeTaskComplete, WorkspaceID: "ws-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != HookBlock || !result.TimedOut {
		t.Fatalf("fail-closed timeout result = %#v", result)
	}
}

func newTestHookPolicy(id string, decision HookDecision, mode HookMode, binding HookBinding) HookPolicy {
	return HookPolicy{
		ID: id, Version: 1, Name: id, Mode: mode, FailMode: HookFailOpen,
		Events:   []HookEventType{HookBeforeTaskComplete, HookBeforePromptAssemble, HookOnError},
		Bindings: []HookBinding{binding},
		Handlers: []HookHandler{{ID: "handler-1", Decision: decision}},
	}
}
