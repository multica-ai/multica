package apps

import (
	"os"
	"strings"
	"testing"
)

func TestAppsExposeNoWorkflowRoutes(t *testing.T) {
	raw, err := os.ReadFile("../../../cmd/server/router.go")
	if err != nil {
		t.Fatal(err)
	}
	router := string(raw)
	for _, route := range []string{
		`/api/cerebro/app-workflows`,
		`/api/cerebro/app-workflow-webhooks`,
		`/api/cerebro/app-workflow-triggers`,
		`/api/cerebro/apps/workflow-runs`,
	} {
		if strings.Contains(router, route) {
			t.Errorf("Apps still expose workflow route %s", route)
		}
	}
}

func TestWorkflowDefinitionAcceptsExactlyFiveTriggerTypes(t *testing.T) {
	for _, trigger := range []string{"schedule", "webhook", "data_event", "manual", "chat"} {
		if !supportedWorkflowTrigger(trigger) {
			t.Errorf("trigger %q rejected", trigger)
		}
	}
	if supportedWorkflowTrigger("sample") {
		t.Fatal("unknown trigger accepted")
	}
}
