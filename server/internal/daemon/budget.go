package daemon

// GAP-28 (fork issue #23): opt-in per-task budget caps, enforced daemon-side.
//
// Caps come from the daemon's environment (MULTICA_BUDGET_MAX_COST_USD,
// MULTICA_BUDGET_MAX_TOKENS, MULTICA_BUDGET_MAX_WALL_CLOCK) and are overridden
// per-agent via the agent's CustomEnv map (the existing agent-config channel):
// same key names. Zero = cap off, so behavior is byte-identical until an
// operator opts in.
//
// Granularity ceiling: usage reaches the daemon only when a run attempt
// returns (TaskResult.Usage, GAP-4 attribution), so cost/token checks run per
// attempt and the wall-clock cap is what bounds burn inside a single attempt.
// ponytail: per-attempt check; live per-step enforcement needs a runner usage callback — add when caps prove too coarse.
//
// Cost uses provider-reported CostUSDTicks (1e-10 USD). Backends that report
// no cost leave the cost cap inert for those runs; token and wall-clock caps
// still bound them.

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"
)

// errBudgetExceeded marks a task stopped by a MULTICA_BUDGET_* cap.
// taskRunFailureReason maps it to agent_error.budget_exceeded.
var errBudgetExceeded = errors.New("budget cap exceeded")

type taskBudget struct {
	maxCostUSD float64       // budgetCostEnv
	maxTokens  int64         // budgetTokensEnv (input+output)
	wallClock  time.Duration // budgetWallClockEnv, e.g. "90m"
}

const (
	budgetCostEnv      = "MULTICA_BUDGET_MAX_COST_USD"
	budgetTokensEnv    = "MULTICA_BUDGET_MAX_TOKENS"
	budgetWallClockEnv = "MULTICA_BUDGET_MAX_WALL_CLOCK"
)

// budgetForTask resolves the caps for one task: daemon env defaults first,
// then any override carried on the agent's CustomEnv. Invalid values disable
// that cap with a warning — a typoed knob must not fail every task.
func budgetForTask(t *Task) taskBudget {
	b := taskBudget{
		maxCostUSD: parseBudgetFloat(os.Getenv(budgetCostEnv)),
		maxTokens:  parseBudgetInt(os.Getenv(budgetTokensEnv)),
		wallClock:  parseBudgetDuration(os.Getenv(budgetWallClockEnv)),
	}
	if t == nil || t.Agent == nil {
		return b
	}
	if v, ok := t.Agent.CustomEnv[budgetCostEnv]; ok {
		b.maxCostUSD = parseBudgetFloat(v)
	}
	if v, ok := t.Agent.CustomEnv[budgetTokensEnv]; ok {
		b.maxTokens = parseBudgetInt(v)
	}
	if v, ok := t.Agent.CustomEnv[budgetWallClockEnv]; ok {
		b.wallClock = parseBudgetDuration(v)
	}
	return b
}

// overBudget returns a human-readable overrun description or "" while every
// enabled cap still has headroom.
func overBudget(b taskBudget, usage []TaskUsageEntry) string {
	var tokens, costTicks int64
	for _, u := range usage {
		tokens += u.InputTokens + u.OutputTokens // billable generation; cache reads are near-free, excluded like the server's rollup
		costTicks += u.CostUSDTicks
	}
	costUSD := float64(costTicks) / 1e10
	if b.maxTokens > 0 && tokens >= b.maxTokens {
		return fmt.Sprintf("tokens %d >= cap %d", tokens, b.maxTokens)
	}
	if b.maxCostUSD > 0 && costUSD >= b.maxCostUSD {
		return fmt.Sprintf("cost $%.4f >= cap $%.2f", costUSD, b.maxCostUSD)
	}
	return ""
}

func parseBudgetFloat(v string) float64 {
	if v == "" {
		return 0
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f <= 0 {
		slog.Warn("ignoring bad budget value", "value", v, "error", err)
		return 0
	}
	return f
}

func parseBudgetInt(v string) int64 {
	if v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		slog.Warn("ignoring bad budget value", "value", v, "error", err)
		return 0
	}
	return n
}

func parseBudgetDuration(v string) time.Duration {
	if v == "" {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		slog.Warn("ignoring bad budget value", "value", v, "error", err)
		return 0
	}
	return d
}
