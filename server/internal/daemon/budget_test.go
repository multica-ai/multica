package daemon

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestOverBudget(t *testing.T) {
	usage := []TaskUsageEntry{
		{InputTokens: 600, OutputTokens: 400, CostUSDTicks: 3e9}, // $0.30
		{InputTokens: 100, OutputTokens: 900, CostUSDTicks: 7e9}, // $0.70
	}
	tests := []struct {
		name   string
		budget taskBudget
		usage  []TaskUsageEntry
		want   string
	}{
		{"no caps set", taskBudget{}, usage, ""},
		{"under all caps", taskBudget{maxCostUSD: 2, maxTokens: 5000}, usage, ""},
		{"token cap hit", taskBudget{maxTokens: 2000}, usage, "tokens 2000 >= cap 2000"},
		{"token cap exact boundary trips", taskBudget{maxTokens: 1999}, usage, "tokens 2000 >= cap 1999"},
		{"cost cap hit", taskBudget{maxCostUSD: 1}, usage, "cost $1.0000 >= cap $1.00"},
		{"cache tokens excluded from token cap", taskBudget{maxTokens: 2000}, []TaskUsageEntry{{InputTokens: 10, OutputTokens: 10, CacheReadTokens: 1e6}}, ""},
		{"no cost reported leaves cost cap inert", taskBudget{maxCostUSD: 1}, []TaskUsageEntry{{InputTokens: 5, OutputTokens: 5}}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := overBudget(tc.budget, tc.usage); got != tc.want {
				t.Fatalf("overBudget() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBudgetForTaskAgentCustomEnvOverride(t *testing.T) {
	t.Setenv(budgetWallClockEnv, "90m")
	t.Setenv(budgetCostEnv, "5")
	task := &Task{Agent: &AgentData{CustomEnv: map[string]string{
		budgetCostEnv:      "1.50",
		budgetWallClockEnv: "bogus", // invalid agent override disables that cap
	}}}
	got := budgetForTask(task)
	if got.maxCostUSD != 1.50 {
		t.Fatalf("agent CustomEnv cost override = %v, want 1.50", got.maxCostUSD)
	}
	if got.wallClock != 0 {
		t.Fatalf("invalid agent wall-clock override should disable cap, got %v", got.wallClock)
	}
	if envOnly := budgetForTask(&Task{}).wallClock; envOnly != 90*time.Minute {
		t.Fatalf("daemon env wall clock = %v, want 90m", envOnly)
	}
	// nil task / no agent payload = daemon env caps only.
	if envOnly := budgetForTask(nil); envOnly.maxCostUSD != 5 || envOnly.wallClock != 90*time.Minute {
		t.Fatalf("nil task budget = %+v, want daemon env caps only", envOnly)
	}
}

func TestTaskRunFailureReasonBudgetExceeded(t *testing.T) {
	err := fmt.Errorf("%w: cost $1.25 >= cap $1.00", errBudgetExceeded)
	if got := taskRunFailureReason(err); got != taskfailure.ReasonAgentBudgetExceeded.String() {
		t.Fatalf("taskRunFailureReason() = %q, want %q", got, taskfailure.ReasonAgentBudgetExceeded.String())
	}
}

// The deadline branch must fire only on the budget timer, never on the
// server-side cancel poll — a cancelled task is discarded upstream and must
// not be relabeled budget_exceeded.
func TestDeadlineExceededIsDistinguishableFromCancel(t *testing.T) {
	deadlineCtx, cancel := context.WithTimeout(context.Background(), -time.Second)
	defer cancel()
	if !errors.Is(deadlineCtx.Err(), context.DeadlineExceeded) || errors.Is(context.Canceled, context.DeadlineExceeded) {
		t.Fatal("context error sentinels no longer distinguish deadline from cancel; GAP-28 loop check is wrong")
	}
}
