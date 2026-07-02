package loops

// FIR-2283 integration test for the engine-facing gate evaluator. Proves the
// decision the workflow engine sees as it drives an OpCheckPasses condition:
// a fresh gate registers its checks and holds, a failed check holds, and only
// an all-green set advances. Skips cleanly when no test DB is reachable
// (shares loopTestPool / TestMain / seedIssue with store_db_test.go).

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// uuidToString renders a pgtype.UUID in canonical 8-4-4-4-12 form, the string
// shape EvaluateCheckGate (via util.ParseUUID) expects.
func uuidToString(u pgtype.UUID) string {
	b := u.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// TestGateEvaluator_AdvancesOnlyWhenAllChecksGreen walks the gate exactly as
// the engine does: evaluate on each event and act on the boolean it returns.
func TestGateEvaluator_AdvancesOnlyWhenAllChecksGreen(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	issueID := seedIssue(t)
	eval := NewGateEvaluator(loopTestPool)

	gate := "gate-1" // any stable per-gate key
	test := []string{"go", "test", "./..."}
	vet := []string{"go", "vet", "./..."}
	value := CheckGateConfig{Checks: [][]string{test, vet}}

	// 1. First evaluation: nothing reported. The gate must NOT advance, and it
	//    must have registered both checks as pending so the runtime can run
	//    them.
	advance, err := eval.EvaluateCheckGate(ctx, uuidToString(issueID), gate, value)
	if err != nil {
		t.Fatalf("first eval: %v", err)
	}
	if advance {
		t.Fatal("gate advanced before any check reported")
	}
	outcomes, err := eval.store.Outcomes(ctx, issueID, gate, gateRound)
	if err != nil {
		t.Fatalf("outcomes: %v", err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("first eval should register 2 pending checks, got %d", len(outcomes))
	}

	// 2. One check reports failure. The gate must revise: it records the
	//    revision against the (unconfigured, so never-tripping) caps and
	//    advances to a fresh round, but still holds.
	if err := eval.store.Report(ctx, issueID, gate, gateRound, test, 1); err != nil {
		t.Fatalf("report failing: %v", err)
	}
	advance, err = eval.EvaluateCheckGate(ctx, uuidToString(issueID), gate, value)
	if err != nil {
		t.Fatalf("eval after failure: %v", err)
	}
	if advance {
		t.Fatal("gate advanced with a failed check")
	}
	state, err := eval.store.LoadGateState(ctx, issueID, gate)
	if err != nil {
		t.Fatalf("load gate state: %v", err)
	}
	if state.Round != 2 {
		t.Fatalf("a failed check should advance the round, got round %d", state.Round)
	}
	if state.Revisions != 1 {
		t.Fatalf("want 1 revision recorded, got %d", state.Revisions)
	}
	if state.Stopped {
		t.Fatal("gate should not stop: value carries no caps")
	}

	// 3. Re-evaluating enqueues the fresh round's checks (mirroring the
	//    runtime re-running the build after a revision). Once both report
	//    green, the gate advances.
	advance, err = eval.EvaluateCheckGate(ctx, uuidToString(issueID), gate, value)
	if err != nil {
		t.Fatalf("eval enqueues round 2: %v", err)
	}
	if advance {
		t.Fatal("gate advanced before round 2 checks reported")
	}
	if err := eval.store.Report(ctx, issueID, gate, state.Round, test, 0); err != nil {
		t.Fatalf("report passing test: %v", err)
	}
	if err := eval.store.Report(ctx, issueID, gate, state.Round, vet, 0); err != nil {
		t.Fatalf("report passing vet: %v", err)
	}
	advance, err = eval.EvaluateCheckGate(ctx, uuidToString(issueID), gate, value)
	if err != nil {
		t.Fatalf("eval all green: %v", err)
	}
	if !advance {
		t.Fatal("gate did not advance with all checks green")
	}
}

// TestGateEvaluator_StopsOnMaxRevisions proves the caps tracker actually
// freezes the gate: once MaxRevisions is reached, further failures stop
// advancing the round and the gate never enqueues, dispatches, or advances
// again — it stays exactly where the cap tripped until a human intervenes.
func TestGateEvaluator_StopsOnMaxRevisions(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	issueID := seedIssue(t)
	eval := NewGateEvaluator(loopTestPool)

	gate := "capped-gate"
	check := []string{"go", "test", "./..."}
	value := CheckGateConfig{
		Checks: [][]string{check},
		Caps:   Caps{MaxIterations: 10, MaxRevisions: 2, NoProgressStalls: 10},
	}

	// Round 1 fails -> revision 1, round advances to 2, not stopped yet.
	if _, err := eval.EvaluateCheckGate(ctx, uuidToString(issueID), gate, value); err != nil {
		t.Fatalf("eval enqueue round 1: %v", err)
	}
	if err := eval.store.Report(ctx, issueID, gate, 1, check, 1); err != nil {
		t.Fatalf("report round 1 failure: %v", err)
	}
	if _, err := eval.EvaluateCheckGate(ctx, uuidToString(issueID), gate, value); err != nil {
		t.Fatalf("eval revise round 1: %v", err)
	}
	state, err := eval.store.LoadGateState(ctx, issueID, gate)
	if err != nil {
		t.Fatalf("load state after revision 1: %v", err)
	}
	if state.Stopped || state.Round != 2 || state.Revisions != 1 {
		t.Fatalf("unexpected state after revision 1: %+v", state)
	}

	// Round 2 also fails -> revision 2 hits MaxRevisions=2 and stops.
	if _, err := eval.EvaluateCheckGate(ctx, uuidToString(issueID), gate, value); err != nil {
		t.Fatalf("eval enqueue round 2: %v", err)
	}
	if err := eval.store.Report(ctx, issueID, gate, 2, check, 1); err != nil {
		t.Fatalf("report round 2 failure: %v", err)
	}
	if _, err := eval.EvaluateCheckGate(ctx, uuidToString(issueID), gate, value); err != nil {
		t.Fatalf("eval revise round 2: %v", err)
	}
	state, err = eval.store.LoadGateState(ctx, issueID, gate)
	if err != nil {
		t.Fatalf("load state after revision 2: %v", err)
	}
	if !state.Stopped {
		t.Fatalf("gate should stop after MaxRevisions=2, got: %+v", state)
	}
	if state.StopReason == "" {
		t.Fatal("stopped gate must carry a stop reason")
	}

	// Even if round 3's checks somehow report green, a stopped gate never
	// advances again — it is frozen for a human to look at, not auto-cleared.
	if err := eval.store.Report(ctx, issueID, gate, 3, check, 0); err != nil {
		t.Fatalf("report round 3: %v", err)
	}
	advance, err := eval.EvaluateCheckGate(ctx, uuidToString(issueID), gate, value)
	if err != nil {
		t.Fatalf("eval after stop: %v", err)
	}
	if advance {
		t.Fatal("a stopped gate must never advance")
	}
}

// TestGateEvaluator_AdvancesFromJSONValue proves the evaluator parses the
// Condition.Value in its stored JSON form (how it arrives from a workflow row),
// not only as an already-typed CheckGateConfig.
func TestGateEvaluator_AdvancesFromJSONValue(t *testing.T) {
	if loopTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	issueID := seedIssue(t)
	eval := NewGateEvaluator(loopTestPool)

	gate := "json-gate"
	check := []string{"make", "check"}
	// The JSON shape a Condition.Value deserializes to: a map, not the Go type.
	value := map[string]any{"checks": []any{[]any{"make", "check"}}}

	if _, err := eval.EvaluateCheckGate(ctx, uuidToString(issueID), gate, value); err != nil {
		t.Fatalf("eval (enqueue) from json: %v", err)
	}
	if err := eval.store.Report(ctx, issueID, gate, gateRound, check, 0); err != nil {
		t.Fatalf("report: %v", err)
	}
	advance, err := eval.EvaluateCheckGate(ctx, uuidToString(issueID), gate, value)
	if err != nil {
		t.Fatalf("eval green from json: %v", err)
	}
	if !advance {
		t.Fatal("gate did not advance from a JSON-shaped config")
	}
}
