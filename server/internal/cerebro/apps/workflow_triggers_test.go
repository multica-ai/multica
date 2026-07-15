package apps

import (
	"os"
	"strings"
	"testing"
)

func TestAllFiveWorkflowTriggersHaveProductionRoutes(t *testing.T) {
	raw, err := os.ReadFile("../../../cmd/server/router.go")
	if err != nil {
		t.Fatal(err)
	}
	router := string(raw)
	for _, route := range []string{
		`r.Post("/{workflowId}/test", cerebroAppsHandler.TestWorkflow)`,
		`r.Post("/{workflowId}/chat", cerebroAppsHandler.TriggerChat)`,
		`r.Post("/api/cerebro/app-workflow-webhooks/{workflowId}/{token}", cerebroAppsHandler.TriggerWebhook)`,
		`r.Post("/api/cerebro/app-workflow-triggers/data-event", cerebroAppsHandler.TriggerDataEvent)`,
		`r.Post("/api/cerebro/app-workflow-triggers/schedule", cerebroAppsHandler.TriggerSchedule)`,
	} {
		if !strings.Contains(router, route) {
			t.Errorf("missing trigger route %s", route)
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
