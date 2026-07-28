package providerfailover

import "testing"

// Orchestrator-tier coverage (td-836aa9). Actor-tier runs are unaffected by the
// new gate; orchestrator-tier runs are now COVERED (shadow records them) but a
// real active handoff is held closed until control-plane effects are proven
// idempotent.

// An orchestrator-tier run with idempotency proven proceeds exactly like an
// actor: extending coverage must not block the safe case.
func TestDecide_Orchestrator_IdempotentProceeds(t *testing.T) {
	t.Parallel()
	in := baseEligible()
	in.OrchestratorTier = true
	in.ControlPlaneIdempotent = true
	d := Decide(in)
	if d.Outcome != OutcomeProceed || !d.WouldFailOver {
		t.Fatalf("idempotent orchestrator should proceed, got %+v", d)
	}
}

// An orchestrator-tier run whose control-plane effects are NOT proven
// idempotent is held closed in active mode with the specific reason — this is
// the double-dispatch guard.
func TestDecide_Orchestrator_ActiveHoldsWithoutIdempotency(t *testing.T) {
	t.Parallel()
	in := baseEligible()
	in.OrchestratorTier = true
	in.ControlPlaneIdempotent = false
	d := Decide(in)
	if d.Outcome != OutcomeDeclined || d.State != StateDeclined {
		t.Fatalf("non-idempotent orchestrator should decline in active, got %+v", d)
	}
	if d.WouldFailOver || d.Reason != ReasonOrchestratorIdempotencyUnproven {
		t.Fatalf("reason should be orchestrator_idempotency_unproven, got %+v", d)
	}
}

// Shadow mode records orchestrator coverage regardless of idempotency: the
// point of coverage is that operators can SEE would-fail-over for orchestrator
// runs before enabling active handoffs.
func TestDecide_Orchestrator_ShadowRecordsCoverage(t *testing.T) {
	t.Parallel()
	in := baseEligible()
	in.Mode = ModeShadow
	in.OrchestratorTier = true
	in.ControlPlaneIdempotent = false
	d := Decide(in)
	if d.Outcome != OutcomeShadow || !d.WouldFailOver || d.Reason != ReasonEligible {
		t.Fatalf("shadow should record orchestrator would-fail-over, got %+v", d)
	}
}

// The actor-tier common case is untouched by the orchestrator gate even when
// idempotency is unproven (it simply does not apply).
func TestDecide_Actor_UnaffectedByOrchestratorGate(t *testing.T) {
	t.Parallel()
	in := baseEligible() // OrchestratorTier defaults false
	in.ControlPlaneIdempotent = false
	if d := Decide(in); d.Outcome != OutcomeProceed {
		t.Fatalf("actor-tier run must ignore the orchestrator gate, got %+v", d)
	}
}

// Direction is recorded on every decision, including declines, so the ledger
// always shows which way a handoff would have gone.
func TestDecide_RecordsTargetProvider(t *testing.T) {
	t.Parallel()
	if d := Decide(baseEligible()); d.TargetProvider != "claude" {
		t.Errorf("codex source should record target claude, got %q", d.TargetProvider)
	}
	// A decline still records the direction.
	in := baseEligible()
	in.Cancelled = true
	if d := Decide(in); d.TargetProvider != "claude" {
		t.Errorf("declined codex decision should still record target claude, got %q", d.TargetProvider)
	}
	// An ineligible source has no direction.
	in2 := baseEligible()
	in2.SourceProvider = "grok"
	if d := Decide(in2); d.TargetProvider != "" {
		t.Errorf("ineligible source should record empty target, got %q", d.TargetProvider)
	}
}
