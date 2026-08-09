package execenv

import (
	"fmt"
	"sort"
	"strings"
)

// ActiveHookRuleForEnv is the plain-language part of a live Workflow hook
// contract that applies to this task.
type ActiveHookRuleForEnv struct {
	Name            string
	ContractRule    string
	ContractSatisfy string
	Events          []string
}

// cerebroHouseRulesBrief renders the live contracts once for every provider.
// The server resolves applicability before the task reaches the daemon.
func cerebroHouseRulesBrief(rules []ActiveHookRuleForEnv) string {
	if len(rules) == 0 {
		return ""
	}

	grouped := make(map[string][]ActiveHookRuleForEnv)
	events := make([]string, 0)
	for _, rule := range rules {
		for _, event := range rule.Events {
			if _, exists := grouped[event]; !exists {
				events = append(events, event)
			}
			grouped[event] = append(grouped[event], rule)
		}
	}
	sort.Strings(events)

	var b strings.Builder
	b.WriteString("## House rules\n\n")
	b.WriteString("These live Workflow hook contracts apply to this run. Satisfy them before performing the named event.\n\n")
	for _, event := range events {
		rulesForEvent := grouped[event]
		sort.SliceStable(rulesForEvent, func(i, j int) bool { return rulesForEvent[i].Name < rulesForEvent[j].Name })
		fmt.Fprintf(&b, "### %s\n\n", event)
		for _, rule := range rulesForEvent {
			fmt.Fprintf(&b, "- **%s:** %s **How to satisfy it:** %s\n", rule.Name, rule.ContractRule, rule.ContractSatisfy)
		}
		b.WriteString("\n")
	}
	return b.String()
}
