package workflows

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func statusGatePolicy(ws uuid.UUID, decision HookDecision, requirement string) HookPolicy {
	return HookPolicy{
		ID: "workpad-start-gate", Version: 1, Name: "Workpad start gate",
		Mode: HookModeEnforce, FailMode: HookFailClosed,
		Events:   []HookEventType{HookBeforeIssueStatus},
		Bindings: []HookBinding{{Kind: HookScopeWorkspace, ID: ws.String()}},
		Conditions: []Condition{
			{Field: "status.to", Op: "eq", Value: "in_progress"},
			{Field: "actor.type", Op: "eq", Value: "agent"},
		},
		Handlers:      []HookHandler{{ID: "h1", Decision: decision, Requirement: requirement}},
		CreatedByID:   uuid.New().String(),
		CreatedByType: "member",
	}
}

func TestIssueStatusGateBlocksAgentInProgress(t *testing.T) {
	ws, issue, agent := uuid.New(), uuid.New(), uuid.New()
	policy := statusGatePolicy(ws, HookBlock, "Prepend a `## Workpad` checklist to the issue description first.")
	gate := NewIssueStatusGate(NewHookEngine(true, NewMemoryHookStore([]HookPolicy{policy})))

	decision, err := gate.CheckIssueStatusChange(context.Background(), IssueStatusChange{
		WorkspaceID: ws.String(), IssueID: issue.String(),
		ActorType: "agent", ActorID: agent.String(),
		FromStatus: "todo", ToStatus: "in_progress", Nonce: "n1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Allowed {
		t.Fatal("agent in_progress without workpad must be blocked")
	}
	if decision.Requirement != "Prepend a `## Workpad` checklist to the issue description first." {
		t.Fatalf("requirement = %q", decision.Requirement)
	}
}

func TestIssueStatusGateNeverBlocksMembers(t *testing.T) {
	ws, issue, member := uuid.New(), uuid.New(), uuid.New()
	policy := statusGatePolicy(ws, HookBlock, "blocked")
	gate := NewIssueStatusGate(NewHookEngine(true, NewMemoryHookStore([]HookPolicy{policy})))

	decision, err := gate.CheckIssueStatusChange(context.Background(), IssueStatusChange{
		WorkspaceID: ws.String(), IssueID: issue.String(),
		ActorType: "member", ActorID: member.String(),
		FromStatus: "todo", ToStatus: "in_progress", Nonce: "n1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Allowed {
		t.Fatal("member status changes must never be gated")
	}
}

func TestIssueStatusGateAllowsOtherTransitions(t *testing.T) {
	ws, issue, agent := uuid.New(), uuid.New(), uuid.New()
	policy := statusGatePolicy(ws, HookBlock, "blocked")
	gate := NewIssueStatusGate(NewHookEngine(true, NewMemoryHookStore([]HookPolicy{policy})))

	decision, err := gate.CheckIssueStatusChange(context.Background(), IssueStatusChange{
		WorkspaceID: ws.String(), IssueID: issue.String(),
		ActorType: "agent", ActorID: agent.String(),
		FromStatus: "in_progress", ToStatus: "in_review", Nonce: "n1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Allowed {
		t.Fatal("transition not matching the policy condition must pass")
	}
}

func TestIssueStatusGateRetryReEvaluates(t *testing.T) {
	ws, issue, agent := uuid.New(), uuid.New(), uuid.New()
	evalID := uuid.New()
	policy := statusGatePolicy(ws, HookBlock, "add workpad")
	policy.Conditions = append(policy.Conditions, Condition{Field: "eval", Op: OpEvalFailed, Value: evalID.String()})
	resolver := &fakeFreshRunner{runStatus: "failed"}
	engine := NewHookEngine(true, NewMemoryHookStore([]HookPolicy{policy})).WithConditionResolver(resolver)
	gate := NewIssueStatusGate(engine)

	change := IssueStatusChange{
		WorkspaceID: ws.String(), IssueID: issue.String(),
		ActorType: "agent", ActorID: agent.String(),
		FromStatus: "todo", ToStatus: "in_progress",
	}
	change.Nonce = "attempt-1"
	first, err := gate.CheckIssueStatusChange(context.Background(), change)
	if err != nil {
		t.Fatal(err)
	}
	if first.Allowed {
		t.Fatal("first attempt with failing eval must block")
	}

	// The agent adds the workpad; the fresh run now passes. A new nonce must
	// bypass the engine's idempotency cache and re-evaluate.
	resolver.runStatus = "passed"
	change.Nonce = "attempt-2"
	second, err := gate.CheckIssueStatusChange(context.Background(), change)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Allowed {
		t.Fatal("retry after fixing the workpad must pass")
	}
}

func TestIssueStatusGateNilSafeAndDisabled(t *testing.T) {
	var nilGate *IssueStatusGate
	decision, err := nilGate.CheckIssueStatusChange(context.Background(), IssueStatusChange{ActorType: "agent", FromStatus: "todo", ToStatus: "in_progress"})
	if err != nil || !decision.Allowed {
		t.Fatalf("nil gate must allow: %v %v", decision, err)
	}

	disabled := NewIssueStatusGate(NewHookEngine(false, NewMemoryHookStore([]HookPolicy{statusGatePolicy(uuid.New(), HookBlock, "x")})))
	decision, err = disabled.CheckIssueStatusChange(context.Background(), IssueStatusChange{
		WorkspaceID: uuid.New().String(), IssueID: uuid.New().String(),
		ActorType: "agent", ActorID: uuid.New().String(),
		FromStatus: "todo", ToStatus: "in_progress", Nonce: "n1",
	})
	if err != nil || !decision.Allowed {
		t.Fatalf("disabled engine must allow: %v %v", decision, err)
	}
}
