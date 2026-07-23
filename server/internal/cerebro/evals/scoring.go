package evals

// scoring.go is the heart of "authenticity" (FIR-3496, decision A): given the
// per-task outcomes an eval run actually produced, it decides pass/fail against
// the threshold policy. It is pure and deterministic — no DB, no network — so a
// green result can be trusted to mean the tasks were really scored.

// CaseOutcome is the minimal per-task signal the scorer needs.
type CaseOutcome struct {
	Passed   bool
	Critical bool
}

// RunOutcome is the computed verdict for a whole run.
type RunOutcome struct {
	Total           int     `json:"total"`
	Passed          int     `json:"passed"`
	PassRate        float64 `json:"pass_rate"`
	CriticalTotal   int     `json:"critical_total"`
	CriticalFailed  int     `json:"critical_failed"`
	MinPassRate     float64 `json:"min_pass_rate"`
	ThresholdMet    bool    `json:"threshold_met"`
	CriticalRuleMet bool    `json:"critical_rule_met"`
	// Status is "passed" or "failed". A run with zero tasks is always "failed":
	// nothing was tested, so it cannot be a trustworthy pass.
	Status string `json:"status"`
}

// Run status constants shared with cerebro_eval_run.status.
const (
	RunStatusPassed = "passed"
	RunStatusFailed = "failed"
	RunStatusError  = "error"
)

// Score computes the run verdict from per-task outcomes and the threshold
// policy. Fail-closed: an empty run, an unmet pass rate, or any critical
// failure (when required) all yield "failed".
func Score(cases []CaseOutcome, policy ThresholdPolicy) RunOutcome {
	out := RunOutcome{
		Total:       len(cases),
		MinPassRate: policy.MinPassRate,
	}
	for _, c := range cases {
		if c.Passed {
			out.Passed++
		}
		if c.Critical {
			out.CriticalTotal++
			if !c.Passed {
				out.CriticalFailed++
			}
		}
	}
	if out.Total > 0 {
		out.PassRate = float64(out.Passed) / float64(out.Total)
	}
	out.ThresholdMet = out.Total > 0 && out.PassRate >= policy.MinPassRate
	out.CriticalRuleMet = !policy.RequireAllCritical || out.CriticalFailed == 0
	if out.ThresholdMet && out.CriticalRuleMet {
		out.Status = RunStatusPassed
	} else {
		out.Status = RunStatusFailed
	}
	return out
}
