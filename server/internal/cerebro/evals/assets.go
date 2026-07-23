package evals

import (
	"encoding/json"
	"errors"
	"strings"
)

// This file gives the in-app eval builder (FIR-3496) a typed reading of the
// datasets/graders/thresholds JSONB columns that the 9140 catalog stores as
// raw arrays. The columns keep holding raw JSON so externally authored,
// file-based evals (firtal-evals) still round-trip untouched; the types here
// are the shape the in-app runner loads when an eval is authored in the app.

// GraderType enumerates how a task answer is judged.
const (
	GraderAIJudge  = "ai_judge"
	GraderHardRule = "hard_rule"
)

// TaskCase is one exam question authored in the app: a situation the target is
// asked to handle, the expected answer (the facit), and whether failing it must
// sink the whole run regardless of the pass rate.
type TaskCase struct {
	ID        string `json:"id"`
	Situation string `json:"situation"`
	Expected  string `json:"expected"`
	Critical  bool   `json:"critical"`
}

// Grader describes how answers are scored. Config is grader-type specific
// (rubric/model for ai_judge, match rules for hard_rule) and stays raw so each
// grader implementation owns its own schema.
type Grader struct {
	ID     string          `json:"id"`
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config"`
}

// ThresholdPolicy is the pass/fail contract for a whole run: the minimum share
// of tasks that must pass, and whether every critical task must pass on top of
// that. It is derived from the thresholds JSONB so the catalog keeps one
// thresholds schema instead of forking a second one for the in-app builder.
type ThresholdPolicy struct {
	// MinPassRate is a fraction in [0,1]. Defaults to 1.0 (every task must pass)
	// when the catalog has no pass_rate threshold, so an under-specified eval
	// fails closed rather than passing by accident.
	MinPassRate float64
	// RequireAllCritical, when true, fails the run if any critical task fails
	// even if MinPassRate is met.
	RequireAllCritical bool
	// explicitPassRate records whether a pass_rate threshold was actually
	// present, so callers can distinguish "author chose 100%" from the default.
	explicitPassRate bool
}

// ExplicitPassRate reports whether the pass rate came from the catalog rather
// than the fail-closed default.
func (p ThresholdPolicy) ExplicitPassRate() bool { return p.explicitPassRate }

// thresholdEntry mirrors the existing metric/operator/value threshold rows. The
// in-app builder writes two well-known metrics — pass_rate and
// all_critical_pass — instead of inventing a new column shape.
type thresholdEntry struct {
	Metric   string          `json:"metric"`
	Operator string          `json:"operator"`
	Value    json.RawMessage `json:"value"`
}

var errNoTasks = errors.New("eval has no in-app tasks to run")

// ParseTasks reads the datasets column as in-app authored tasks. Entries with
// an empty situation are skipped so a catalog that mixes file-based datasets
// with in-app tasks does not turn blank rows into runnable questions.
func ParseTasks(datasets json.RawMessage) ([]TaskCase, error) {
	if len(datasets) == 0 {
		return nil, errNoTasks
	}
	var raw []TaskCase
	if err := json.Unmarshal(datasets, &raw); err != nil {
		return nil, err
	}
	tasks := make([]TaskCase, 0, len(raw))
	for _, t := range raw {
		t.Situation = strings.TrimSpace(t.Situation)
		t.Expected = strings.TrimSpace(t.Expected)
		if t.Situation == "" {
			continue
		}
		tasks = append(tasks, t)
	}
	if len(tasks) == 0 {
		return nil, errNoTasks
	}
	return tasks, nil
}

// ParseGraders reads the graders column as typed graders. Only ai_judge and
// hard_rule are recognized for the in-app runner.
func ParseGraders(graders json.RawMessage) ([]Grader, error) {
	if len(graders) == 0 {
		return nil, errors.New("eval has no graders")
	}
	var raw []Grader
	if err := json.Unmarshal(graders, &raw); err != nil {
		return nil, err
	}
	out := make([]Grader, 0, len(raw))
	for _, g := range raw {
		g.Type = strings.TrimSpace(g.Type)
		if g.Type == "" {
			continue
		}
		out = append(out, g)
	}
	if len(out) == 0 {
		return nil, errors.New("eval has no usable graders")
	}
	return out, nil
}

// ParseThresholdPolicy derives the run pass/fail contract from the thresholds
// column. Unknown metrics are ignored; a missing pass_rate defaults to 1.0.
func ParseThresholdPolicy(thresholds json.RawMessage) ThresholdPolicy {
	policy := ThresholdPolicy{MinPassRate: 1.0}
	if len(thresholds) == 0 {
		return policy
	}
	var entries []thresholdEntry
	if err := json.Unmarshal(thresholds, &entries); err != nil {
		return policy
	}
	for _, e := range entries {
		switch strings.TrimSpace(e.Metric) {
		case "pass_rate":
			if rate, ok := parseRate(e.Value); ok {
				policy.MinPassRate = rate
				policy.explicitPassRate = true
			}
		case "all_critical_pass":
			var b bool
			if json.Unmarshal(e.Value, &b) == nil {
				policy.RequireAllCritical = b
			}
		}
	}
	return policy
}

// parseRate accepts a pass rate as a fraction (0.8) or a percentage (80) and
// normalizes it to a clamped fraction in [0,1].
func parseRate(raw json.RawMessage) (float64, bool) {
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, false
	}
	if v > 1 {
		v = v / 100.0
	}
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	return v, true
}
