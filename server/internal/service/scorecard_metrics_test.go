package service

import (
	"math"
	"testing"
	"time"
)

func scoreApprox(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: no value, want %v", name, want)
	}
	if math.Abs(*got-want) > 1e-3 {
		t.Fatalf("%s = %v, want %v", name, *got, want)
	}
}

func TestComputeScorecardRunFailuresAndInsufficient(t *testing.T) {
	// Runtime-offline failures are reported as uncharged and stay out of the
	// success rate; a manual end is uncharged too.
	in := ScorecardInputs{Runs: ScorecardRunInputs{
		Completed: 6, ChargedFailed: 2, UnchargedFailed: 3,
		FailuresByCause: map[string]int64{"agent_error": 2, "runtime_offline": 3},
	}}
	res := ComputeScorecard(in)
	if res.Status != ScorecardStatusOK || res.SampleSize != 8 {
		t.Fatalf("status %s sample %d, want ok 8", res.Status, res.SampleSize)
	}
	scoreApprox(t, "success_rate", res.Metrics[MetricSuccessRate].Value, 0.75)
	if res.Metrics[MetricRunsUncharged].Sample != 3 {
		t.Fatalf("uncharged runs = %d, want 3", res.Metrics[MetricRunsUncharged].Sample)
	}
	if res.FailuresByCause["runtime_offline"] != 3 {
		t.Fatalf("failures by cause not carried through: %v", res.FailuresByCause)
	}

	// Only runtime failures: no scored run, so insufficient and no rate.
	onlyRuntime := ComputeScorecard(ScorecardInputs{Runs: ScorecardRunInputs{UnchargedFailed: 9}})
	if onlyRuntime.Status != ScorecardStatusInsufficient {
		t.Fatalf("status %s, want insufficient", onlyRuntime.Status)
	}
	if onlyRuntime.Metrics[MetricSuccessRate].Value != nil {
		t.Fatalf("success rate should have no value without scored runs")
	}

	// Four scored runs is below the minimum of five.
	four := ComputeScorecard(ScorecardInputs{Runs: ScorecardRunInputs{Completed: 4}})
	if four.Status != ScorecardStatusInsufficient {
		t.Fatalf("4 runs: status %s, want insufficient", four.Status)
	}
	five := ComputeScorecard(ScorecardInputs{Runs: ScorecardRunInputs{Completed: 5}})
	if five.Status != ScorecardStatusOK {
		t.Fatalf("5 runs: status %s, want ok", five.Status)
	}
}

func TestComputeScorecardIssueMetrics(t *testing.T) {
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	// First pass is "done without being reopened": row 5 finished in two runs with
	// no reopen, which counts; row 2 was reopened, which does not.
	rows := []ScorecardIssueRow{
		{CreatedAt: base, DoneAt: base.Add(48 * time.Hour), CompletedRuns: 1},                   // first pass
		{CreatedAt: base, DoneAt: base.Add(24 * time.Hour), CompletedRuns: 2, ReopenCount: 1},   // reopened
		{CreatedAt: base, DoneAt: base.Add(72 * time.Hour), CompletedRuns: 1, HumanAfter: true}, // human stepped in
		{CreatedAt: base, DoneAt: base.Add(24 * time.Hour), CompletedRuns: 0},                   // finished by a person
	}
	in := ScorecardInputs{
		Runs:      ScorecardRunInputs{Completed: 5},
		Done:      rows,
		Cost:      ScorecardCostInputs{PricedTicks: 4 * 1e10, UnpricedTokens: 900},
		Proposals: ScorecardProposalInputs{Approved: 1, Decided: 4},
	}
	res := ComputeScorecard(in)
	if res.Metrics[MetricIssuesDone].Value == nil || *res.Metrics[MetricIssuesDone].Value != 4 {
		t.Fatalf("issues_done wrong: %+v", res.Metrics[MetricIssuesDone])
	}
	// Measured issues are the three with an agent run; one first pass.
	scoreApprox(t, "first_pass_rate", res.Metrics[MetricFirstPassRate].Value, 1.0/3.0)
	// Two runs with no reopen are a first pass under the spec's definition.
	twoRunsNoReopen := ComputeScorecard(ScorecardInputs{Runs: ScorecardRunInputs{Completed: 5}, Done: []ScorecardIssueRow{
		{CreatedAt: base, DoneAt: base.Add(24 * time.Hour), CompletedRuns: 2},
	}})
	scoreApprox(t, "first_pass_rate with two runs and no reopen", twoRunsNoReopen.Metrics[MetricFirstPassRate].Value, 1)
	scoreApprox(t, "human_intervention_rate", res.Metrics[MetricHumanInterventionRate].Value, 1.0/3.0)
	// Cycle time: (2+1+3+1)/4 days.
	scoreApprox(t, "cycle_time_days", res.Metrics[MetricCycleTimeDays].Value, 1.75)
	// Cost: 4 units of 1e10 ticks = $4 over 4 done issues.
	scoreApprox(t, "cost_per_done_usd", res.Metrics[MetricCostPerDoneUSD].Value, 1.0)
	scoreApprox(t, "proposal_acceptance_rate", res.Metrics[MetricProposalAcceptance].Value, 0.25)
	if res.Metrics[MetricUnpricedTokens].Sample != 900 {
		t.Fatalf("unpriced tokens not reported")
	}
}

func TestComputeScorecardNoDoneIssuesHasNoRates(t *testing.T) {
	res := ComputeScorecard(ScorecardInputs{Runs: ScorecardRunInputs{Completed: 5}})
	if res.Metrics[MetricCostPerDoneUSD].Value != nil || res.Metrics[MetricFirstPassRate].Value != nil {
		t.Fatalf("rates with no done issues must have no value")
	}
	if res.Metrics[MetricRoutineCompliance].Value != nil {
		t.Fatalf("routine compliance must be absent for an agent")
	}
}

func TestComputeScorecardRoutinesAndBrief(t *testing.T) {
	res := ComputeScorecard(ScorecardInputs{
		Runs:     ScorecardRunInputs{Completed: 5},
		Routines: &ScorecardRoutineInputs{OnTime: 3, Total: 4},
		Brief:    &ScorecardBriefInputs{Useful: 2, Total: 4},
	})
	scoreApprox(t, "routine_compliance_rate", res.Metrics[MetricRoutineCompliance].Value, 0.75)
	scoreApprox(t, "brief_usefulness", res.Metrics[MetricBriefUsefulness].Value, 0.5)
}

func TestProbationOutcome(t *testing.T) {
	v := func(f float64) ScorecardMetric { return ScorecardMetric{Value: &f, Sample: 10} }
	cases := []struct {
		name          string
		before, after ScorecardMetric
		want          string
	}{
		{"improved", v(0.5), v(0.7), ProbationImproved},
		{"neutral", v(0.7), v(0.72), ProbationNeutral},
		{"worse", v(0.8), v(0.6), ProbationWorse},
		{"defer: no before", ScorecardMetric{}, v(0.6), ""},
		{"defer: no after", v(0.6), ScorecardMetric{}, ""},
	}
	for _, c := range cases {
		got, _ := ProbationOutcome(c.before, c.after)
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestScorecardPeriodBounds(t *testing.T) {
	// 2026-11-03 is a Tuesday; the last completed week is Mon 26 Oct to Mon 2 Nov.
	now := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	start, end, err := ScorecardPeriodBounds(ScorecardPeriodWeek, now)
	if err != nil {
		t.Fatal(err)
	}
	if start.Format("2006-01-02") != "2026-10-26" || end.Format("2006-01-02") != "2026-11-02" {
		t.Fatalf("week bounds %s..%s", start, end)
	}
	start, end, err = ScorecardPeriodBounds(ScorecardPeriodMonth, now)
	if err != nil {
		t.Fatal(err)
	}
	if start.Format("2006-01-02") != "2026-10-01" || end.Format("2006-01-02") != "2026-11-01" {
		t.Fatalf("month bounds %s..%s", start, end)
	}
	if _, _, err := ScorecardPeriodBounds("day", now); err == nil {
		t.Fatal("unknown period must fail")
	}
}

func TestValidateReviewRequest(t *testing.T) {
	ok := ReviewRequest{
		SubjectType: "agent", SubjectID: "11111111-1111-1111-1111-111111111111", Period: "month",
		PeriodStart: "2026-10-01", Rating: "below", Narrative: "Runs failed on the agent's own errors.",
		CitedMetrics: []string{"success_rate"},
	}
	if _, end, err := ValidateReviewRequest(ok); err != nil || end.Format("2006-01-02") != "2026-11-01" {
		t.Fatalf("valid review refused: %v", err)
	}
	bad := map[string]func(r *ReviewRequest){
		"no citation":    func(r *ReviewRequest) { r.CitedMetrics = nil },
		"unknown metric": func(r *ReviewRequest) { r.CitedMetrics = []string{"vibes"} },
		"bad rating":     func(r *ReviewRequest) { r.Rating = "great" },
		"bad recommend": func(r *ReviewRequest) {
			r.Recommendations = []ReviewRecommendationInput{{Type: "fire", Rationale: "x"}}
		},
		"no rationale": func(r *ReviewRequest) { r.Recommendations = []ReviewRecommendationInput{{Type: "observe"}} },
		"change w/o body": func(r *ReviewRequest) {
			r.Recommendations = []ReviewRecommendationInput{{Type: "move", Rationale: "x"}}
		},
		"bad period":      func(r *ReviewRequest) { r.Period = "day" },
		"bad date":        func(r *ReviewRequest) { r.PeriodStart = "October" },
		"empty narrative": func(r *ReviewRequest) { r.Narrative = "  " },
	}
	for name, mutate := range bad {
		r := ok
		mutate(&r)
		if _, _, err := ValidateReviewRequest(r); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestReviewFiguresCopiesStoredValues(t *testing.T) {
	v := 0.8
	doc := ScorecardDocument{Metrics: map[string]ScorecardMetric{
		MetricSuccessRate: {Value: &v, Sample: 10},
	}}
	lines, err := ReviewFigures(doc, []string{MetricSuccessRate})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0] != "- success_rate: 80% (sample 10)" {
		t.Fatalf("figure line %q", lines)
	}
	if _, err := ReviewFigures(doc, []string{MetricFirstPassRate}); err == nil {
		t.Fatal("citing a figure the scorecard lacks must fail")
	}
}

func TestMergeChangeTypeRefusesMismatch(t *testing.T) {
	if _, err := mergeChangeType("move", []byte(`{"type":"hire"}`)); err == nil {
		t.Fatal("a payload naming another type must be refused")
	}
	raw, err := mergeChangeType("move", []byte(`{"agent_id":"a","to_squad_id":"b","rationale":"r"}`))
	if err != nil {
		t.Fatal(err)
	}
	if OrgChangeTypeOf(raw) != "move" {
		t.Fatalf("type not set: %s", raw)
	}
}
