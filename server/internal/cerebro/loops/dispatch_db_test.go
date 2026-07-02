package loops

// FIR-2283 integration tests for the dispatch egress against a real DB. Proves
// the once-only dispatch contract: the store surfaces a fresh check for
// dispatch exactly once, and the gate evaluator sends each check to the runtime
// exactly once even when it is re-evaluated while checks are in flight. Skips
// cleanly when no test DB is reachable (shares loopTestPool / seedIssue with
// store_db_test.go).

import (
	"context"
	"testing"
)

// fakeDispatcher records every check it is asked to dispatch.
type fakeDispatcher struct {
	calls []CheckDispatch
}

func (f *fakeDispatcher) DispatchCheck(ctx context.Context, d CheckDispatch) error {
	f.calls = append(f.calls, d)
	return nil
}

// TestStore_DispatchOnce proves the dispatch dedup at the store layer: a fresh
// check is pending-dispatch once, leaves the pending set after MarkDispatched,
// and a re-enqueue never re-surfaces it — so the engine never re-sends a check
// that is already in flight.
func TestStore_DispatchOnce(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	issueID := seedIssue(t)
	store := NewStore(loopTestPool)

	gate := "delivery"
	round := int32(1)
	test := []string{"go", "test", "./..."}
	vet := []string{"go", "vet", "./..."}

	if err := store.Enqueue(ctx, issueID, gate, round, [][]string{test, vet}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	pending, err := store.PendingDispatch(ctx, issueID, gate, round)
	if err != nil {
		t.Fatalf("pending dispatch: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("both fresh checks should be pending dispatch, got %d", len(pending))
	}

	// Dispatch one. It leaves the pending set; the other stays.
	if err := store.MarkDispatched(ctx, issueID, gate, round, test); err != nil {
		t.Fatalf("mark dispatched: %v", err)
	}
	pending, _ = store.PendingDispatch(ctx, issueID, gate, round)
	if len(pending) != 1 {
		t.Fatalf("one dispatched should leave one pending, got %d", len(pending))
	}

	// Dispatch the second, then a re-enqueue must not re-surface either.
	if err := store.MarkDispatched(ctx, issueID, gate, round, vet); err != nil {
		t.Fatalf("mark dispatched vet: %v", err)
	}
	if err := store.Enqueue(ctx, issueID, gate, round, [][]string{test, vet}); err != nil {
		t.Fatalf("re-enqueue: %v", err)
	}
	pending, _ = store.PendingDispatch(ctx, issueID, gate, round)
	if len(pending) != 0 {
		t.Fatalf("dispatched checks must not re-surface after re-enqueue, got %d", len(pending))
	}
}

// TestGateEvaluator_DispatchesEachCheckOnce proves the egress wiring end to end:
// the first evaluation of a fresh gate dispatches every required check exactly
// once, a re-evaluation while the checks are in flight dispatches nothing more,
// and the gate still advances only once every check reports green.
func TestGateEvaluator_DispatchesEachCheckOnce(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	issueID := seedIssue(t)
	disp := &fakeDispatcher{}
	eval := NewGateEvaluator(loopTestPool).WithDispatcher(disp)

	gate := "dispatch-gate"
	agentID := "11111111-1111-1111-1111-111111111111"
	test := []string{"go", "test", "./..."}
	vet := []string{"go", "vet", "./..."}
	value := CheckGateConfig{Checks: [][]string{test, vet}, AgentID: agentID}

	// First evaluation: both checks dispatched once, gate holds.
	advance, err := eval.EvaluateCheckGate(ctx, uuidToString(issueID), gate, value)
	if err != nil {
		t.Fatalf("first eval: %v", err)
	}
	if advance {
		t.Fatal("gate advanced before any check reported")
	}
	if len(disp.calls) != 2 {
		t.Fatalf("first eval should dispatch 2 checks, got %d", len(disp.calls))
	}
	if disp.calls[0].AgentID != agentID {
		t.Fatalf("dispatch missing agent id: %+v", disp.calls[0])
	}
	if disp.calls[0].Gate != gate {
		t.Fatalf("dispatch missing gate: %+v", disp.calls[0])
	}

	// Second evaluation while in flight: nothing re-dispatched.
	if _, err := eval.EvaluateCheckGate(ctx, uuidToString(issueID), gate, value); err != nil {
		t.Fatalf("second eval: %v", err)
	}
	if len(disp.calls) != 2 {
		t.Fatalf("in-flight re-eval must not re-dispatch, got %d", len(disp.calls))
	}

	// All checks report green: the gate advances, with no extra dispatch.
	if err := eval.store.Report(ctx, issueID, gate, gateRound, test, 0); err != nil {
		t.Fatalf("report test: %v", err)
	}
	if err := eval.store.Report(ctx, issueID, gate, gateRound, vet, 0); err != nil {
		t.Fatalf("report vet: %v", err)
	}
	advance, err = eval.EvaluateCheckGate(ctx, uuidToString(issueID), gate, value)
	if err != nil {
		t.Fatalf("eval all green: %v", err)
	}
	if !advance {
		t.Fatal("gate did not advance with all checks green")
	}
	if len(disp.calls) != 2 {
		t.Fatalf("advancing must not dispatch more, got %d", len(disp.calls))
	}
}
