package evals

import (
	"encoding/json"
	"testing"
)

func TestParseTasksSkipsEmptySituations(t *testing.T) {
	datasets := json.RawMessage(`[
		{"id":"t1","situation":"Kunden vil annullere","expected":"Henvis til menneske","critical":true},
		{"id":"filebacked","split":"held_out","path":"cases/x.jsonl"},
		{"id":"t2","situation":" Ordre forsinket ","expected":" Beklag "}
	]`)
	tasks, err := ParseTasks(datasets)
	if err != nil {
		t.Fatalf("ParseTasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 in-app tasks, got %d", len(tasks))
	}
	if tasks[0].Critical != true || tasks[1].Situation != "Ordre forsinket" || tasks[1].Expected != "Beklag" {
		t.Fatalf("unexpected parse: %+v", tasks)
	}
}

func TestParseTasksErrorsWhenNoInAppTasks(t *testing.T) {
	if _, err := ParseTasks(json.RawMessage(`[{"path":"cases/x.jsonl"}]`)); err == nil {
		t.Fatal("expected error when no in-app tasks present")
	}
	if _, err := ParseTasks(nil); err == nil {
		t.Fatal("expected error on empty datasets")
	}
}

func TestParseGradersKeepsTypedGraders(t *testing.T) {
	graders := json.RawMessage(`[
		{"id":"g1","type":"ai_judge","config":{"rubric":"be helpful"}},
		{"id":"g2","type":""},
		{"id":"g3","type":"hard_rule","config":{"match":"contains"}}
	]`)
	out, err := ParseGraders(graders)
	if err != nil {
		t.Fatalf("ParseGraders: %v", err)
	}
	if len(out) != 2 || out[0].Type != GraderAIJudge || out[1].Type != GraderHardRule {
		t.Fatalf("unexpected graders: %+v", out)
	}
}

func TestParseThresholdPolicyDefaultsFailClosed(t *testing.T) {
	policy := ParseThresholdPolicy(nil)
	if policy.MinPassRate != 1.0 {
		t.Fatalf("missing threshold must default to 1.0, got %.2f", policy.MinPassRate)
	}
	if policy.ExplicitPassRate() {
		t.Fatal("default pass rate must not be reported as explicit")
	}
}

func TestParseThresholdPolicyReadsPassRateAndCritical(t *testing.T) {
	// Fraction form.
	p1 := ParseThresholdPolicy(json.RawMessage(`[
		{"metric":"pass_rate","operator":"gte","value":0.8},
		{"metric":"all_critical_pass","operator":"eq","value":true}
	]`))
	if p1.MinPassRate != 0.8 || !p1.RequireAllCritical || !p1.ExplicitPassRate() {
		t.Fatalf("fraction policy wrong: %+v", p1)
	}
	// Percentage form normalizes to a fraction.
	p2 := ParseThresholdPolicy(json.RawMessage(`[{"metric":"pass_rate","operator":"gte","value":90}]`))
	if p2.MinPassRate != 0.9 {
		t.Fatalf("percentage form must normalize to 0.9, got %.2f", p2.MinPassRate)
	}
}
