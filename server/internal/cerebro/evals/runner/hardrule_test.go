package runner

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/evals"
)

func grade(t *testing.T, config string, task evals.TaskCase, answer string) (bool, string) {
	t.Helper()
	g, err := NewHardRuleGrader(evals.Grader{Type: evals.GraderHardRule, Config: json.RawMessage(config)})
	if err != nil {
		t.Fatalf("NewHardRuleGrader: %v", err)
	}
	passed, reason, cost, err := g.Grade(context.Background(), task, answer)
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if cost != 0 {
		t.Fatalf("hard rule must be free, got cost %d", cost)
	}
	return passed, reason
}

func TestHardRuleDefaultIsCaseInsensitiveExact(t *testing.T) {
	task := evals.TaskCase{Expected: "Escalate"}
	if passed, _ := grade(t, ``, task, " escalate "); !passed {
		t.Fatal("iexact default should pass on trimmed, case-insensitive match")
	}
	if passed, _ := grade(t, ``, task, "escalate now"); passed {
		t.Fatal("iexact must not pass on partial match")
	}
}

func TestHardRuleContains(t *testing.T) {
	task := evals.TaskCase{Expected: "human"}
	if passed, _ := grade(t, `{"match":"contains"}`, task, "please contact a human agent"); !passed {
		t.Fatal("contains should pass on substring")
	}
	if passed, _ := grade(t, `{"match":"contains"}`, task, "Human"); passed {
		t.Fatal("case-sensitive contains must not pass on different case")
	}
}

func TestHardRuleExpectedOverride(t *testing.T) {
	task := evals.TaskCase{Expected: "task-level"}
	if passed, _ := grade(t, `{"match":"exact","expected":"fixed"}`, task, "fixed"); !passed {
		t.Fatal("config expected override should win over task expected")
	}
}

func TestHardRuleUnknownMatchRejected(t *testing.T) {
	_, err := NewHardRuleGrader(evals.Grader{Config: json.RawMessage(`{"match":"regex"}`)})
	if err == nil {
		t.Fatal("expected unknown match to be rejected")
	}
}
