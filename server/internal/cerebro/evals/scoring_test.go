package evals

import "testing"

func TestScorePassesWhenThresholdAndCriticalRuleMet(t *testing.T) {
	cases := []CaseOutcome{
		{Passed: true, Critical: true},
		{Passed: true, Critical: false},
		{Passed: false, Critical: false},
	}
	policy := ThresholdPolicy{MinPassRate: 0.6, RequireAllCritical: true}
	out := Score(cases, policy)
	if out.Status != RunStatusPassed {
		t.Fatalf("expected passed, got %s (rate %.2f)", out.Status, out.PassRate)
	}
	if out.Passed != 2 || out.Total != 3 {
		t.Fatalf("unexpected tally: %+v", out)
	}
}

func TestScoreFailsWhenPassRateBelowThreshold(t *testing.T) {
	cases := []CaseOutcome{
		{Passed: true},
		{Passed: false},
		{Passed: false},
	}
	out := Score(cases, ThresholdPolicy{MinPassRate: 0.8})
	if out.Status != RunStatusFailed {
		t.Fatalf("expected failed, got %s", out.Status)
	}
	if out.ThresholdMet {
		t.Fatal("threshold should not be met")
	}
}

func TestScoreFailsWhenCriticalTaskFailsDespiteHighPassRate(t *testing.T) {
	cases := []CaseOutcome{
		{Passed: true, Critical: false},
		{Passed: true, Critical: false},
		{Passed: true, Critical: false},
		{Passed: false, Critical: true},
	}
	// 75% pass rate clears a 0.5 threshold, but the one critical task failed.
	out := Score(cases, ThresholdPolicy{MinPassRate: 0.5, RequireAllCritical: true})
	if out.Status != RunStatusFailed {
		t.Fatalf("expected failed on critical, got %s", out.Status)
	}
	if out.CriticalFailed != 1 || out.CriticalRuleMet {
		t.Fatalf("critical accounting wrong: %+v", out)
	}
}

func TestScoreCriticalFailureIgnoredWhenRuleOff(t *testing.T) {
	cases := []CaseOutcome{
		{Passed: true, Critical: false},
		{Passed: false, Critical: true},
	}
	out := Score(cases, ThresholdPolicy{MinPassRate: 0.5, RequireAllCritical: false})
	if out.Status != RunStatusPassed {
		t.Fatalf("expected passed when critical rule off, got %s", out.Status)
	}
}

func TestScoreEmptyRunAlwaysFails(t *testing.T) {
	out := Score(nil, ThresholdPolicy{MinPassRate: 0})
	if out.Status != RunStatusFailed {
		t.Fatalf("empty run must fail closed, got %s", out.Status)
	}
	if out.ThresholdMet {
		t.Fatal("empty run cannot meet threshold")
	}
}

func TestScoreExactThresholdBoundaryPasses(t *testing.T) {
	cases := []CaseOutcome{{Passed: true}, {Passed: false}}
	out := Score(cases, ThresholdPolicy{MinPassRate: 0.5})
	if out.Status != RunStatusPassed {
		t.Fatalf("0.5 rate should meet 0.5 threshold, got %s", out.Status)
	}
}
