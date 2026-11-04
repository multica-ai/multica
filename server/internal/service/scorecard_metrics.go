package service

import (
	"math"
	"time"
)

// Scorecard metric arithmetic (spec 4.6). The inputs are counts and rows read
// by the scorecard queries; this file turns them into metric values and a
// status with no database, so each rule is tested on its own.

// Scorecard periods, subject types and statuses.
const (
	ScorecardPeriodWeek  = "week"
	ScorecardPeriodMonth = "month"

	ScorecardSubjectAgent   = "agent"
	ScorecardSubjectSquad   = "squad"
	ScorecardSubjectCompany = "company"

	ScorecardStatusOK           = "ok"
	ScorecardStatusInsufficient = "insufficient"
)

// ScorecardMinSample is the fewest scored runs a scorecard needs to be reviewed.
// A scorecard below it is marked insufficient, and no review may cite it.
const ScorecardMinSample = 5

// ScorecardMetric is one metric as stored: value is nil when the metric has no
// data in the period, and sample is the count it was computed from.
type ScorecardMetric struct {
	Value  *float64 `json:"value"`
	Sample int64    `json:"sample"`
}

// Metric keys. The stored metrics object is keyed by these.
const (
	MetricRuns                  = "runs"
	MetricRunsUncharged         = "runs_uncharged"
	MetricSuccessRate           = "success_rate"
	MetricFailuresByCause       = "failures_by_cause"
	MetricIssuesDone            = "issues_done"
	MetricFirstPassRate         = "first_pass_rate"
	MetricCycleTimeDays         = "cycle_time_days"
	MetricCostPerDoneUSD        = "cost_per_done_usd"
	MetricUnpricedTokens        = "unpriced_tokens"
	MetricHumanInterventionRate = "human_intervention_rate"
	MetricProposalAcceptance    = "proposal_acceptance_rate"
	MetricRoutineCompliance     = "routine_compliance_rate"
	MetricKnowledgePublished    = "knowledge_published"
	MetricKnowledgeReviews      = "knowledge_reviews"
	MetricBriefUsefulness       = "brief_usefulness"
)

// ScorecardRunInputs counts the terminal runs of a scope in the period.
type ScorecardRunInputs struct {
	Completed     int64
	ChargedFailed int64
	// UnchargedFailed counts failures the runtime or a person caused. They are
	// reported, but they are not in the success rate.
	UnchargedFailed int64
	// FailuresByCause maps each failure cause to its run count, charged and not.
	FailuresByCause map[string]int64
}

// ScorecardIssueRow is one done issue of the scope, last updated in the period.
type ScorecardIssueRow struct {
	CreatedAt time.Time
	// DoneAt is when the issue was done (its last done transition).
	DoneAt time.Time
	// ReopenCount is how many times the issue was reopened out of done.
	ReopenCount   int64
	CompletedRuns int64
	// HumanAfter is true when a person commented after the issue was done.
	HumanAfter bool
}

// ScorecardCostInputs is the provider-reported cost of the scope's runs, in
// 1e-10 USD, and the tokens of runs with no reported price.
type ScorecardCostInputs struct {
	PricedTicks    int64
	UnpricedTokens int64
}

// ScorecardProposalInputs counts the proposals the scope filed in the period.
type ScorecardProposalInputs struct {
	Approved int64
	Decided  int64
}

// ScorecardKnowledgeInputs counts the knowledge the scope published and reviewed.
type ScorecardKnowledgeInputs struct {
	Published int64
	Reviewed  int64
}

// ScorecardRoutineInputs counts the required routines of a squad or the company
// and how many were on time when the scorecard was computed.
type ScorecardRoutineInputs struct {
	OnTime int64
	Total  int64
}

// ScorecardBriefInputs counts the boss's ratings of a Secretary's briefs.
type ScorecardBriefInputs struct {
	Useful int64
	Total  int64
}

// ScorecardInputs is everything one scorecard is computed from. Routines is nil
// for agents, and Brief is nil for every subject but the Secretary.
type ScorecardInputs struct {
	Runs      ScorecardRunInputs
	Done      []ScorecardIssueRow
	Cost      ScorecardCostInputs
	Proposals ScorecardProposalInputs
	Knowledge ScorecardKnowledgeInputs
	Routines  *ScorecardRoutineInputs
	Brief     *ScorecardBriefInputs
}

// ScorecardResult is a computed scorecard, ready to store.
type ScorecardResult struct {
	Status     string
	SampleSize int64
	Metrics    map[string]ScorecardMetric
	// FailuresByCause is the failed runs of the period by cause, charged and
	// uncharged alike. It is not a rate, so it is kept apart from Metrics.
	FailuresByCause map[string]int64
}

// ComputeScorecard derives every metric and the status. A scorecard whose
// scored runs fall below ScorecardMinSample is insufficient, and its metrics
// are still stored so the reader can see them, but nothing reviews it.
func ComputeScorecard(in ScorecardInputs) ScorecardResult {
	scored := in.Runs.Completed + in.Runs.ChargedFailed
	res := ScorecardResult{SampleSize: scored, Status: ScorecardStatusOK, Metrics: map[string]ScorecardMetric{}}
	if scored < ScorecardMinSample {
		res.Status = ScorecardStatusInsufficient
	}

	res.Metrics[MetricRuns] = ScorecardMetric{Value: f(float64(scored)), Sample: scored}
	res.Metrics[MetricRunsUncharged] = ScorecardMetric{Value: f(float64(in.Runs.UnchargedFailed)), Sample: in.Runs.UnchargedFailed}
	res.Metrics[MetricSuccessRate] = ratio(in.Runs.Completed, scored)
	res.FailuresByCause = in.Runs.FailuresByCause

	done := int64(len(in.Done))
	res.Metrics[MetricIssuesDone] = ScorecardMetric{Value: f(float64(done)), Sample: done}

	// The issue metrics count only issues an agent run finished: an issue that
	// was done with no completed run was finished by a person, not measured.
	var worked, firstPass, humanAfter int64
	var cycleSum float64
	for _, row := range in.Done {
		cycleSum += row.DoneAt.Sub(row.CreatedAt).Hours() / 24
		if row.CompletedRuns < 1 {
			continue
		}
		worked++
		// First pass: done without being reopened, and no person came back to it.
		if row.ReopenCount == 0 && !row.HumanAfter {
			firstPass++
		}
		if row.HumanAfter {
			humanAfter++
		}
	}
	res.Metrics[MetricFirstPassRate] = ratio(firstPass, worked)
	res.Metrics[MetricHumanInterventionRate] = ratio(humanAfter, worked)
	if done > 0 {
		res.Metrics[MetricCycleTimeDays] = ScorecardMetric{Value: f(round4(cycleSum / float64(done))), Sample: done}
	} else {
		res.Metrics[MetricCycleTimeDays] = ScorecardMetric{Sample: 0}
	}

	cost := ScorecardMetric{Sample: done}
	if done > 0 {
		cost.Value = f(round4(float64(in.Cost.PricedTicks) / 1e10 / float64(done)))
	}
	res.Metrics[MetricCostPerDoneUSD] = cost
	res.Metrics[MetricUnpricedTokens] = ScorecardMetric{Value: f(float64(in.Cost.UnpricedTokens)), Sample: in.Cost.UnpricedTokens}

	res.Metrics[MetricProposalAcceptance] = ratio(in.Proposals.Approved, in.Proposals.Decided)
	res.Metrics[MetricKnowledgePublished] = ScorecardMetric{Value: f(float64(in.Knowledge.Published)), Sample: in.Knowledge.Published}
	res.Metrics[MetricKnowledgeReviews] = ScorecardMetric{Value: f(float64(in.Knowledge.Reviewed)), Sample: in.Knowledge.Reviewed}

	if in.Routines != nil {
		res.Metrics[MetricRoutineCompliance] = ratio(in.Routines.OnTime, in.Routines.Total)
	}
	if in.Brief != nil {
		res.Metrics[MetricBriefUsefulness] = ratio(in.Brief.Useful, in.Brief.Total)
	}
	return res
}

// ratio is numerator over denominator, or no value when there is nothing to
// divide by. Sample is the denominator.
func ratio(num, den int64) ScorecardMetric {
	m := ScorecardMetric{Sample: den}
	if den > 0 {
		m.Value = f(round4(float64(num) / float64(den)))
	}
	return m
}

func f(v float64) *float64 { return &v }

func round4(v float64) float64 { return math.Round(v*1e4) / 1e4 }

// Probation thresholds (spec 4.6). The compared metric is the success rate; a
// change of at least the threshold either way is improved or worse, and a
// smaller one is neutral.
const ProbationMaterialDelta = 0.05

// ProbationOutcome compares the success rate before and after a change. It
// returns "" when either figure is missing, which defers the verdict.
func ProbationOutcome(before, after ScorecardMetric) (outcome string, delta float64) {
	if before.Value == nil || after.Value == nil {
		return "", 0
	}
	delta = round4(*after.Value - *before.Value)
	switch {
	case delta >= ProbationMaterialDelta:
		return ProbationImproved, delta
	case delta <= -ProbationMaterialDelta:
		return ProbationWorse, delta
	}
	return ProbationNeutral, delta
}

// Probation outcomes, as stored.
const (
	ProbationImproved = "improved"
	ProbationNeutral  = "neutral"
	ProbationWorse    = "worse"
)
