package daemon

// CEREBRO-PATCH(workflow-hook-house-rules): keep the daemon wire shape separate
// from the shared runtime-brief shape.

import "github.com/multica-ai/multica/server/internal/daemon/execenv"

func activeHookRulesForEnv(rules []ActiveHookRuleData) []execenv.ActiveHookRuleForEnv {
	out := make([]execenv.ActiveHookRuleForEnv, 0, len(rules))
	for _, rule := range rules {
		out = append(out, execenv.ActiveHookRuleForEnv{
			Name: rule.Name, ContractRule: rule.ContractRule, ContractSatisfy: rule.ContractSatisfy, Events: rule.Events,
		})
	}
	return out
}
