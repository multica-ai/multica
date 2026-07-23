package runner

import (
	"fmt"

	"github.com/multica-ai/multica/server/internal/cerebro/evals"
)

// factory.go maps an eval's typed target/grader specs to concrete adapters
// behind the Target and Grader interfaces. Unknown or not-yet-wired kinds
// return a clear error rather than a silent no-op, so an eval that selects a
// target the runner cannot execute fails loudly instead of passing by accident.

// NewTarget builds the Target for a parsed target spec. The prompt target runs
// fully in-process via the completer; agent/skill/workflow targets require the
// execution engine and are not yet wired, so selecting them errors instead of
// producing an untested "answer".
func NewTarget(completer Completer, spec evals.TargetSpec) (Target, error) {
	switch spec.Kind {
	case evals.TargetPrompt:
		return NewPromptTarget(completer, spec)
	case evals.TargetAgent, evals.TargetSkill, evals.TargetWorkflow:
		return nil, fmt.Errorf("target kind %q is not yet wired to the execution engine", spec.Kind)
	default:
		return nil, fmt.Errorf("unknown target kind %q", spec.Kind)
	}
}

// NewGrader builds the Grader for a grader definition. hard_rule is
// deterministic and needs no completer; ai_judge calls the completer.
func NewGrader(completer Completer, g evals.Grader) (Grader, error) {
	switch g.Type {
	case evals.GraderHardRule:
		return NewHardRuleGrader(g)
	case evals.GraderAIJudge:
		return NewAIJudgeGrader(completer, g)
	default:
		return nil, fmt.Errorf("unknown grader type %q", g.Type)
	}
}
