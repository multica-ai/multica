package agent

import (
	"reflect"
	"testing"
)

func TestCodexRawPlanUpdatedEmitsCompletePlanCall(t *testing.T) {
	t.Parallel()

	c, _, _ := newTestCodexClient(t)
	c.notificationProtocol = "raw"
	var got []Message
	c.onMessage = func(msg Message) { got = append(got, msg) }

	c.handleLine(`{"jsonrpc":"2.0","method":"turn/plan/updated","params":{"threadId":"","turnId":"turn-1","explanation":"why","plan":[{"step":"Read code","status":"completed"},{"step":"Write fix","status":"inProgress"},{"step":"","status":"pending"}]}}`)
	c.handleLine(`{"jsonrpc":"2.0","method":"turn/plan/updated","params":{"turnId":"turn-1","plan":[{"step":"Read code","status":"completed"}]}}`)

	if len(got) != 4 {
		t.Fatalf("expected tool-use + tool-result per snapshot, got %d: %+v", len(got), got)
	}
	use, result := got[0], got[1]
	if use.Type != MessageToolUse || use.Tool != "update_plan" || result.Type != MessageToolResult || result.CallID != use.CallID {
		t.Fatalf("unexpected pair: %+v / %+v", use, result)
	}
	wantPlan := []any{
		map[string]any{"step": "Read code", "status": "completed"},
		map[string]any{"step": "Write fix", "status": "inProgress"},
	}
	if !reflect.DeepEqual(use.Input["plan"], wantPlan) || use.Input["explanation"] != "why" {
		t.Fatalf("unexpected input: %#v", use.Input)
	}
	if got[2].CallID == use.CallID {
		t.Fatalf("each snapshot needs its own call id, both %q", use.CallID)
	}
}

func TestCodexTurnGateDropsPlanUpdatesFromAnotherTurn(t *testing.T) {
	t.Parallel()

	gate := &codexTurnNotificationGate{}
	gate.arm()
	if !gate.accept("turn/started", map[string]any{"turn": map[string]any{"id": "turn-2"}}) {
		t.Fatal("turn/started must be accepted")
	}
	if gate.accept("turn/plan/updated", map[string]any{"turnId": "turn-1"}) {
		t.Fatal("a plan from an earlier turn must be dropped")
	}
	if !gate.accept("turn/plan/updated", map[string]any{"turnId": "turn-2"}) {
		t.Fatal("the current turn's plan must be accepted")
	}
}
