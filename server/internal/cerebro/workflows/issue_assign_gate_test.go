package workflows

import (
	"context"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func assignment() IssueAssignment {
	return IssueAssignment{
		WorkspaceID: "ws-1",
		ProjectID:   "proj-1",
		IssueID:     "issue-1",
		AgentID:     "agent-1",
		ActorType:   "member",
		ActorID:     "member-1",
		Nonce:       "n1",
	}
}

func TestIssueAssignGateIsNilSafe(t *testing.T) {
	var gate *IssueAssignGate
	decision, err := gate.CheckIssueAssignment(context.Background(), assignment())
	if err != nil || !decision.Allowed {
		t.Fatalf("a nil gate must allow the hand-over, got %+v err=%v", decision, err)
	}
	if decision, err := NewIssueAssignGate(nil).CheckIssueAssignment(context.Background(), assignment()); err != nil || !decision.Allowed {
		t.Fatalf("a gate without an engine must allow the hand-over, got %+v err=%v", decision, err)
	}
}

func TestIssueAssignGateSkipsHandOversWithoutAnAgent(t *testing.T) {
	evaluator := &fakeHookEvaluator{result: HookResult{Decision: HookBlock}}
	gate := NewIssueAssignGate(evaluator)

	change := assignment()
	change.AgentID = ""
	decision, err := gate.CheckIssueAssignment(context.Background(), change)
	if err != nil || !decision.Allowed {
		t.Fatalf("expected allow, got %+v err=%v", decision, err)
	}
	if evaluator.calls != 0 {
		t.Fatalf("an issue with no agent must not reach the engine, got %d calls", evaluator.calls)
	}
}

func TestIssueAssignGateBuildsTheEvent(t *testing.T) {
	evaluator := &fakeHookEvaluator{result: HookResult{Decision: HookAllow}}
	gate := NewIssueAssignGate(evaluator)

	change := assignment()
	change.Reason = "comment"
	if _, err := gate.CheckIssueAssignment(context.Background(), change); err != nil {
		t.Fatal(err)
	}
	if len(evaluator.events) != 1 {
		t.Fatalf("expected one event, got %d", len(evaluator.events))
	}
	event := evaluator.events[0]
	if event.Type != HookBeforeIssueAssigned {
		t.Fatalf("wrong event type: %s", event.Type)
	}
	if event.IssueID != "issue-1" || event.AgentID != "agent-1" || event.WorkspaceID != "ws-1" {
		t.Fatalf("event lost its subject: %+v", event)
	}
	if !strings.Contains(event.EventID, "issue-1") || !strings.Contains(event.EventID, "n1") {
		t.Fatalf("the idempotency key must carry the issue and the nonce, got %q", event.EventID)
	}
	assign, _ := event.Context["assignment"].(map[string]any)
	if assign == nil || assign["reason"] != "comment" {
		t.Fatalf("expected the hand-over reason in the event context, got %+v", event.Context)
	}
}

func TestIssueAssignGateDefaultsTheReason(t *testing.T) {
	evaluator := &fakeHookEvaluator{result: HookResult{Decision: HookAllow}}
	if _, err := NewIssueAssignGate(evaluator).CheckIssueAssignment(context.Background(), assignment()); err != nil {
		t.Fatal(err)
	}
	assign, _ := evaluator.events[0].Context["assignment"].(map[string]any)
	if assign["reason"] != "assignment" {
		t.Fatalf("expected the default reason, got %+v", assign)
	}
}

func TestIssueAssignGateStopsOnADuplicateFoundByTheAction(t *testing.T) {
	// The policy handler itself says allow — only the action's finding stops
	// this particular hand-over.
	evaluator := &fakeHookEvaluator{result: HookResult{
		Decision: HookAllow,
		ActionResults: []HookActionResult{{
			Type:   CheckRelatedActionType,
			Status: HookActionSuccess,
			Result: map[string]any{ResultKeyBlocked: true, "duplicate": true},
		}},
	}}

	decision, err := NewIssueAssignGate(evaluator).CheckIssueAssignment(context.Background(), assignment())
	if err != nil {
		t.Fatal(err)
	}
	if decision.Allowed || !decision.Blocked {
		t.Fatalf("expected the hand-over to stop, got %+v", decision)
	}
	if !strings.Contains(decision.Reason, "same matter") {
		t.Fatalf("expected a human-readable reason, got %q", decision.Reason)
	}
}

func TestIssueAssignGateContinuesWhenTheCheckFoundNoDuplicate(t *testing.T) {
	evaluator := &fakeHookEvaluator{result: HookResult{
		Decision: HookAllow,
		ActionResults: []HookActionResult{{
			Type:   CheckRelatedActionType,
			Status: HookActionSuccess,
			Result: map[string]any{ResultKeyBlocked: false, "duplicate": true, "new_links": 1},
		}},
	}}

	decision, err := NewIssueAssignGate(evaluator).CheckIssueAssignment(context.Background(), assignment())
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Allowed {
		t.Fatalf("a duplicate without block_on_duplicate must still start the agent, got %+v", decision)
	}
}

func TestIssueAssignGateIgnoresAFailedCheck(t *testing.T) {
	evaluator := &fakeHookEvaluator{result: HookResult{
		Decision: HookAllow,
		ActionResults: []HookActionResult{{
			Type:   CheckRelatedActionType,
			Status: HookActionFailed,
			Result: map[string]any{ResultKeyBlocked: true},
		}},
	}}

	decision, err := NewIssueAssignGate(evaluator).CheckIssueAssignment(context.Background(), assignment())
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Allowed {
		t.Fatalf("a failed check must never stop work, got %+v", decision)
	}
}

func TestIssueAssignGateHonoursAPolicyBlock(t *testing.T) {
	evaluator := &fakeHookEvaluator{result: HookResult{
		Decision:     HookRequire,
		Requirements: []string{"Link the duplicate first."},
	}}

	decision, err := NewIssueAssignGate(evaluator).CheckIssueAssignment(context.Background(), assignment())
	if err != nil {
		t.Fatal(err)
	}
	if decision.Allowed || decision.Reason != "Link the duplicate first." {
		t.Fatalf("expected the policy requirement to reach the caller, got %+v", decision)
	}
}

// assigneeUpdate builds the issue:updated payload the platform broadcasts when
// an assignee is written, so the bus path is exercised against the real shape.
func assigneeUpdate(assigneeType, assigneeID string) events.Event {
	return events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: "ws-1",
		Payload: map[string]any{
			"assignee_changed": true,
			"issue": map[string]any{
				"id":            "issue-1",
				"project_id":    "proj-1",
				"assignee_type": assigneeType,
				"assignee_id":   assigneeID,
			},
		},
	}
}

func TestIssueAssignGateFiresOnAgentAssignmentFromTheBus(t *testing.T) {
	evaluator := &fakeHookEvaluator{result: HookResult{Decision: HookAllow}}
	gate := NewIssueAssignGate(evaluator)

	gate.onIssueUpdated(assigneeUpdate("agent", "agent-1"))

	if evaluator.calls != 1 {
		t.Fatalf("an agent hand-over must reach the engine once, got %d calls", evaluator.calls)
	}
	if evaluator.events[0].IssueID != "issue-1" || evaluator.events[0].AgentID != "agent-1" {
		t.Fatalf("the event must name the issue and the agent, got %+v", evaluator.events[0])
	}
	if evaluator.events[0].Type != HookBeforeIssueAssigned {
		t.Fatalf("expected %s, got %s", HookBeforeIssueAssigned, evaluator.events[0].Type)
	}
}

func TestIssueAssignGateIgnoresUpdatesThatAreNotAgentHandOvers(t *testing.T) {
	for name, e := range map[string]events.Event{
		"handed to a person": assigneeUpdate("member", "member-1"),
		"assignee cleared":   assigneeUpdate("", ""),
		"assignee untouched": {Type: protocol.EventIssueUpdated, WorkspaceID: "ws-1", Payload: map[string]any{"issue": map[string]any{"id": "issue-1"}}},
	} {
		t.Run(name, func(t *testing.T) {
			evaluator := &fakeHookEvaluator{result: HookResult{Decision: HookAllow}}
			NewIssueAssignGate(evaluator).onIssueUpdated(e)
			if evaluator.calls != 0 {
				t.Fatalf("expected no engine call, got %d", evaluator.calls)
			}
		})
	}
}
