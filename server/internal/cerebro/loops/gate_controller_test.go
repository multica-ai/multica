package loops

import "testing"

func TestReconcile(t *testing.T) {
	cfg := CheckGateConfig{Checks: [][]string{
		{"pytest"},
		{"go", "test", "./..."},
	}}

	t.Run("nothing reported -> enqueue all", func(t *testing.T) {
		d := Reconcile(cfg, nil)
		if d.Action != GateEnqueue {
			t.Fatalf("want enqueue, got %s", d.Action)
		}
		if len(d.Enqueue) != 2 {
			t.Fatalf("want 2 checks to enqueue, got %d", len(d.Enqueue))
		}
	})

	t.Run("one missing -> enqueue only the missing one", func(t *testing.T) {
		d := Reconcile(cfg, []CheckOutcome{{Argv: []string{"pytest"}, Ran: true, ExitCode: 0}})
		if d.Action != GateEnqueue || len(d.Enqueue) != 1 || d.Enqueue[0][0] != "go" {
			t.Fatalf("want enqueue of the go check, got %s %+v", d.Action, d.Enqueue)
		}
	})

	t.Run("all in flight -> wait", func(t *testing.T) {
		d := Reconcile(cfg, []CheckOutcome{
			{Argv: []string{"pytest"}, Ran: false},
			{Argv: []string{"go", "test", "./..."}, Ran: false},
		})
		if d.Action != GateWait {
			t.Fatalf("want wait, got %s", d.Action)
		}
	})

	t.Run("all passed -> advance", func(t *testing.T) {
		d := Reconcile(cfg, []CheckOutcome{
			{Argv: []string{"pytest"}, Ran: true, ExitCode: 0},
			{Argv: []string{"go", "test", "./..."}, Ran: true, ExitCode: 0},
		})
		if d.Action != GateAdvance {
			t.Fatalf("want advance, got %s", d.Action)
		}
	})

	t.Run("one failed -> revise even with another pending", func(t *testing.T) {
		d := Reconcile(cfg, []CheckOutcome{
			{Argv: []string{"pytest"}, Ran: true, ExitCode: 1},
		})
		if d.Action != GateRevise {
			t.Fatalf("want revise, got %s", d.Action)
		}
	})

	t.Run("idempotent", func(t *testing.T) {
		outcomes := []CheckOutcome{{Argv: []string{"pytest"}, Ran: true, ExitCode: 0}}
		a := Reconcile(cfg, outcomes)
		b := Reconcile(cfg, outcomes)
		if a.Action != b.Action || len(a.Enqueue) != len(b.Enqueue) {
			t.Fatalf("not idempotent: %+v vs %+v", a, b)
		}
	})
}

func TestReconcile_EmptyConfigWaits(t *testing.T) {
	if d := Reconcile(CheckGateConfig{}, nil); d.Action != GateWait {
		t.Fatalf("empty config should wait, got %s", d.Action)
	}
}
