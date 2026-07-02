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

	// 2. One check reports failure. The gate must still hold.
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

	// 3. Both checks report green. Only now does the gate advance.
	if err := eval.store.Report(ctx, issueID, gate, gateRound, test, 0); err != nil {
		t.Fatalf("report passing test: %v", err)
	}
	if err := eval.store.Report(ctx, issueID, gate, gateRound, vet, 0); err != nil {
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
