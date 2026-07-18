// Package runner is the eval engine (FIR-3496, Fase 2): it actually runs an
// eval's tasks against a target, grades each answer, and computes a trustworthy
// pass/fail via evals.Score. The engine depends only on the Target and Grader
// interfaces, so the deterministic orchestration and scoring are unit-tested
// with fakes; the live adapters (agent/skill/workflow targets, AI-judge grader
// on the Firtal Gateway) plug in behind these same interfaces.
package runner

import (
	"context"
	"encoding/json"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebro/evals"
)

// Target runs the thing under test against one task situation and returns the
// answer it produced. costCents lets targets that spend model budget report it.
type Target interface {
	Run(ctx context.Context, task evals.TaskCase) (answer string, costCents int64, err error)
}

// Grader judges whether a produced answer satisfies a task, with a
// human-readable reason that surfaces in "See why". costCents covers graders
// (e.g. an AI judge) that themselves spend budget.
type Grader interface {
	Grade(ctx context.Context, task evals.TaskCase, answer string) (passed bool, reason string, costCents int64, err error)
}

// CaseReport is the full per-task record: what was asked, expected, produced,
// and why it passed or failed. It is the payload the See-why screen reads.
type CaseReport struct {
	CaseID    string `json:"case_id"`
	Situation string `json:"situation"`
	Expected  string `json:"expected"`
	Produced  string `json:"produced"`
	Passed    bool   `json:"passed"`
	Critical  bool   `json:"critical"`
	Reason    string `json:"reason"`
	// Error is set when the target or grader failed to run this task. Such a
	// task counts as a failure (fail-closed) rather than aborting the run.
	Error string `json:"error,omitempty"`
}

// Report is the complete result of an eval run: per-task records, the computed
// verdict, and the run's cost and latency.
type Report struct {
	Cases     []CaseReport     `json:"cases"`
	Outcome   evals.RunOutcome `json:"outcome"`
	CostCents int64            `json:"cost_cents"`
	LatencyMS int64            `json:"latency_ms"`
}

// Runner orchestrates a single eval run.
type Runner struct {
	target Target
	grader Grader
	now    func() time.Time
}

// New builds a Runner. now may be nil, in which case time.Now is used; tests
// inject a fake clock to assert deterministic latency.
func New(target Target, grader Grader, now func() time.Time) *Runner {
	if now == nil {
		now = time.Now
	}
	return &Runner{target: target, grader: grader, now: now}
}

// Execute runs every task through the target and grader, then scores the run
// against the policy. A per-task target/grader error does not abort the run: it
// records the task as a failure and continues, so one broken task cannot hide
// the rest of the results.
func (r *Runner) Execute(ctx context.Context, tasks []evals.TaskCase, policy evals.ThresholdPolicy) Report {
	start := r.now()
	report := Report{Cases: make([]CaseReport, 0, len(tasks))}
	outcomes := make([]evals.CaseOutcome, 0, len(tasks))

	for _, task := range tasks {
		record := CaseReport{
			CaseID:    task.ID,
			Situation: task.Situation,
			Expected:  task.Expected,
			Critical:  task.Critical,
		}

		answer, targetCost, err := r.target.Run(ctx, task)
		report.CostCents += targetCost
		if err != nil {
			record.Passed = false
			record.Error = err.Error()
			record.Reason = "target failed to produce an answer"
			report.Cases = append(report.Cases, record)
			outcomes = append(outcomes, evals.CaseOutcome{Passed: false, Critical: task.Critical})
			continue
		}
		record.Produced = answer

		passed, reason, graderCost, err := r.grader.Grade(ctx, task, answer)
		report.CostCents += graderCost
		if err != nil {
			record.Passed = false
			record.Error = err.Error()
			record.Reason = "grader failed to score the answer"
			report.Cases = append(report.Cases, record)
			outcomes = append(outcomes, evals.CaseOutcome{Passed: false, Critical: task.Critical})
			continue
		}
		record.Passed = passed
		record.Reason = reason
		report.Cases = append(report.Cases, record)
		outcomes = append(outcomes, evals.CaseOutcome{Passed: passed, Critical: task.Critical})
	}

	report.Outcome = evals.Score(outcomes, policy)
	report.LatencyMS = r.now().Sub(start).Milliseconds()
	return report
}

// ResultsJSON serializes the report for the cerebro_eval_run.results column.
func (rep Report) ResultsJSON() (json.RawMessage, error) {
	return json.Marshal(rep)
}
