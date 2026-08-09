package execenv

import (
	"strings"
	"testing"
)

func TestCerebroHouseRulesBriefGroupsTheLiveHookContracts(t *testing.T) {
	rules := []ActiveHookRuleForEnv{{
		Name:            "Require a next step",
		ContractRule:    "Runs must leave a visible next step.",
		ContractSatisfy: "Register a continuation before stopping.",
		Events:          []string{"before.task.complete"},
	}}
	out := cerebroHouseRulesBrief(rules)
	for _, want := range []string{
		"## House rules",
		"### before.task.complete",
		"Require a next step",
		"Runs must leave a visible next step.",
		"Register a continuation before stopping.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("House rules brief missing %q:\n%s", want, out)
		}
	}
	if out != cerebroHouseRulesBrief(rules) {
		t.Fatal("House rules brief is not stable")
	}
}

func TestCerebroHouseRulesBriefOmitsAnEmptySection(t *testing.T) {
	if got := cerebroHouseRulesBrief(nil); got != "" {
		t.Fatalf("empty House rules brief = %q", got)
	}
}
