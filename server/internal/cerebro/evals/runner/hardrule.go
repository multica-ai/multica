package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/cerebro/evals"
)

// HardRuleGrader is the deterministic grader: it matches the produced answer
// against the task's expected facit with no model call, so its verdict is
// reproducible and free. It is the server-side "fast regel køres
// deterministisk" path from the plan.
type HardRuleGrader struct {
	config hardRuleConfig
}

type hardRuleConfig struct {
	// Match selects the comparison: exact, contains, iexact, icontains.
	// Defaults to iexact (case-insensitive exact) when empty.
	Match string `json:"match"`
	// Expected overrides the task's expected value when set, letting a single
	// grader assert a fixed answer across tasks.
	Expected string `json:"expected"`
}

// NewHardRuleGrader builds a HardRuleGrader from a grader definition's config.
func NewHardRuleGrader(g evals.Grader) (*HardRuleGrader, error) {
	cfg := hardRuleConfig{}
	if len(g.Config) > 0 {
		if err := json.Unmarshal(g.Config, &cfg); err != nil {
			return nil, fmt.Errorf("hard_rule config: %w", err)
		}
	}
	cfg.Match = strings.TrimSpace(strings.ToLower(cfg.Match))
	if cfg.Match == "" {
		cfg.Match = "iexact"
	}
	switch cfg.Match {
	case "exact", "contains", "iexact", "icontains":
	default:
		return nil, fmt.Errorf("hard_rule: unknown match %q", cfg.Match)
	}
	return &HardRuleGrader{config: cfg}, nil
}

// Grade implements Grader with a deterministic string comparison. It never
// spends budget, so costCents is always 0.
func (h *HardRuleGrader) Grade(_ context.Context, task evals.TaskCase, answer string) (bool, string, int64, error) {
	expected := h.config.Expected
	if expected == "" {
		expected = task.Expected
	}
	produced := strings.TrimSpace(answer)
	want := strings.TrimSpace(expected)

	var passed bool
	switch h.config.Match {
	case "exact":
		passed = produced == want
	case "iexact":
		passed = strings.EqualFold(produced, want)
	case "contains":
		passed = strings.Contains(produced, want)
	case "icontains":
		passed = strings.Contains(strings.ToLower(produced), strings.ToLower(want))
	}

	if passed {
		return true, fmt.Sprintf("answer %s expected value", h.config.Match), 0, nil
	}
	return false, fmt.Sprintf("answer did not %s expected value", h.config.Match), 0, nil
}
