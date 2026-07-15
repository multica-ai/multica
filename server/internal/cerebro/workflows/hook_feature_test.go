package workflows

import "testing"

func TestWorkflowHooksMasterFlagDefaultsOffAndAcceptsTruthyValues(t *testing.T) {
	t.Setenv("CEREBRO_WORKFLOW_HOOKS_ENABLED", "")
	if hookFeatureEnabled() {
		t.Fatal("workflow hooks must default off")
	}
	for _, value := range []string{"1", "true", "yes", "on"} {
		t.Setenv("CEREBRO_WORKFLOW_HOOKS_ENABLED", value)
		if !hookFeatureEnabled() {
			t.Fatalf("%q should enable workflow hooks", value)
		}
	}
}
