package workflows

import "testing"

func TestWorkflowHooksEmergencySwitchDefaultsOn(t *testing.T) {
	t.Setenv("CEREBRO_WORKFLOW_HOOKS_ENABLED", "")
	if !hookFeatureEnabled() {
		t.Fatal("Workflow lifecycle gates must default on")
	}
	for _, value := range []string{"1", "true", "yes", "on"} {
		t.Setenv("CEREBRO_WORKFLOW_HOOKS_ENABLED", value)
		if !hookFeatureEnabled() {
			t.Fatalf("%q should enable workflow hooks", value)
		}
	}
	for _, value := range []string{"0", "false", "no", "off"} {
		t.Setenv("CEREBRO_WORKFLOW_HOOKS_ENABLED", value)
		if hookFeatureEnabled() {
			t.Fatalf("%q should activate the emergency stop", value)
		}
	}
}
