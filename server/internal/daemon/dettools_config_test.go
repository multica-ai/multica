package daemon

import "testing"

func TestDefaultDetToolsAllowedIncludesAgentImprovementEvaluate(t *testing.T) {
	for _, tool := range DefaultDetToolsAllowed {
		if tool == "agent_improvement_evaluate" {
			return
		}
	}
	t.Fatal("DefaultDetToolsAllowed missing agent_improvement_evaluate")
}
