package loops

import "testing"

func TestReconcile(t *testing.T) {
	cfg := CheckGateConfig{Checks: [][]string{
		{"pytest"},
		{"go", "test", "./..."},
	}}

	t.Run("nothing reported -> enqueue all", func(t *testing.T) {
		d := Reconcile(cfg, nil, nil, nil)
		if d.Action != GateEnqueue {
			t.Fatalf("want enqueue, got %s", d.Action)
		}
		if len(d.Enqueue) != 2 {
			t.Fatalf("want 2 checks to enqueue, got %d", len(d.Enqueue))
		}
	})

	t.Run("one missing -> enqueue only the missing one", func(t *testing.T) {
		d := Reconcile(cfg, []CheckOutcome{{Argv: []string{"pytest"}, Ran: true, ExitCode: 0}}, nil, nil)
		if d.Action != GateEnqueue || len(d.Enqueue) != 1 || d.Enqueue[0][0] != "go" {
			t.Fatalf("want enqueue of the go check, got %s %+v", d.Action, d.Enqueue)
		}
	})

	t.Run("all in flight -> wait", func(t *testing.T) {
		d := Reconcile(cfg, []CheckOutcome{
			{Argv: []string{"pytest"}, Ran: false},
			{Argv: []string{"go", "test", "./..."}, Ran: false},
		}, nil, nil)
		if d.Action != GateWait {
			t.Fatalf("want wait, got %s", d.Action)
		}
	})

	t.Run("all passed -> advance", func(t *testing.T) {
		d := Reconcile(cfg, []CheckOutcome{
			{Argv: []string{"pytest"}, Ran: true, ExitCode: 0},
			{Argv: []string{"go", "test", "./..."}, Ran: true, ExitCode: 0},
		}, nil, nil)
		if d.Action != GateAdvance {
			t.Fatalf("want advance, got %s", d.Action)
		}
	})

	t.Run("one failed -> revise even with another pending", func(t *testing.T) {
		d := Reconcile(cfg, []CheckOutcome{
			{Argv: []string{"pytest"}, Ran: true, ExitCode: 1},
		}, nil, nil)
		if d.Action != GateRevise {
			t.Fatalf("want revise, got %s", d.Action)
		}
	})

	t.Run("idempotent", func(t *testing.T) {
		outcomes := []CheckOutcome{{Argv: []string{"pytest"}, Ran: true, ExitCode: 0}}
		a := Reconcile(cfg, outcomes, nil, nil)
		b := Reconcile(cfg, outcomes, nil, nil)
		if a.Action != b.Action || len(a.Enqueue) != len(b.Enqueue) {
			t.Fatalf("not idempotent: %+v vs %+v", a, b)
		}
	})
}

func TestReconcile_EmptyConfigWaits(t *testing.T) {
	if d := Reconcile(CheckGateConfig{}, nil, nil, nil); d.Action != GateWait {
		t.Fatalf("empty config should wait, got %s", d.Action)
	}
}

// TestReconcile_JudgeChecks proves a judge check is decided exactly like a
// programmatic one — missing enqueues, in-flight waits, a failed verdict
// revises, and the gate only advances once every judge (and programmatic)
// check has reported a passing verdict.
func TestReconcile_JudgeChecks(t *testing.T) {
	cfg := CheckGateConfig{
		Checks:      [][]string{{"go", "test", "./..."}},
		JudgeChecks: []JudgeCheck{{ID: "ux-quality", Rubric: "the UI must not regress"}},
	}
	passingCheck := []CheckOutcome{{Argv: []string{"go", "test", "./..."}, Ran: true, ExitCode: 0}}

	t.Run("judge missing -> enqueued alongside the programmatic check", func(t *testing.T) {
		d := Reconcile(cfg, nil, nil, nil)
		if d.Action != GateEnqueue {
			t.Fatalf("want enqueue, got %s", d.Action)
		}
		if len(d.Enqueue) != 1 || len(d.EnqueueJudge) != 1 || d.EnqueueJudge[0].ID != "ux-quality" {
			t.Fatalf("want both the check and the judge enqueued, got %+v", d)
		}
	})

	t.Run("judge in flight, check passed -> wait", func(t *testing.T) {
		d := Reconcile(cfg, passingCheck, []JudgeOutcome{{ID: "ux-quality", Ran: false}}, nil)
		if d.Action != GateWait {
			t.Fatalf("want wait, got %s", d.Action)
		}
	})

	t.Run("judge revised -> GateRevise even with a passing programmatic check", func(t *testing.T) {
		d := Reconcile(cfg, passingCheck, []JudgeOutcome{{ID: "ux-quality", Ran: true, Pass: false, Blocking: []string{"button misaligned"}}}, nil)
		if d.Action != GateRevise {
			t.Fatalf("want revise, got %s", d.Action)
		}
	})

	t.Run("judge passed and check passed -> advance", func(t *testing.T) {
		d := Reconcile(cfg, passingCheck, []JudgeOutcome{{ID: "ux-quality", Ran: true, Pass: true}}, nil)
		if d.Action != GateAdvance {
			t.Fatalf("want advance, got %s", d.Action)
		}
	})

	t.Run("judge passed but programmatic check still missing -> enqueue only the check", func(t *testing.T) {
		d := Reconcile(cfg, nil, []JudgeOutcome{{ID: "ux-quality", Ran: true, Pass: true}}, nil)
		if d.Action != GateEnqueue || len(d.Enqueue) != 1 || len(d.EnqueueJudge) != 0 {
			t.Fatalf("want only the check enqueued, got %+v", d)
		}
	})
}
