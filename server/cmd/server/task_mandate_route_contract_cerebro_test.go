package main

// CEREBRO-PATCH(task-mandate-exact-platform-routes): FIR-4292 route-wiring tripwire for subscribe_issue.

import (
	"os"
	"regexp"
	"testing"
)

func TestIssueSubscriptionMutationRoutesAreTaskMandateGated(t *testing.T) {
	source, err := os.ReadFile("router.go")
	if err != nil {
		t.Fatalf("read router source: %v", err)
	}

	for name, pattern := range map[string]string{
		"subscribe":       `RequirePlatformCapability\("subscribe_issue"\)\)\.Post\("/subscribe"`,
		"unsubscribe":     `RequirePlatformCapability\("subscribe_issue"\)\)\.Post\("/unsubscribe"`,
		"add reaction":    `RequirePlatformCapability\("subscribe_issue"\)\)\.Post\("/reactions"`,
		"remove reaction": `RequirePlatformCapability\("subscribe_issue"\)\)\.Delete\("/reactions"`,
	} {
		if !regexp.MustCompile(pattern).Match(source) {
			t.Errorf("%s route is missing RequirePlatformCapability(\"subscribe_issue\")", name)
		}
	}
}

func TestAutopilotManagementRoutesUseExistingPermissionGate(t *testing.T) {
	source, err := os.ReadFile("router.go")
	if err != nil {
		t.Fatalf("read router source: %v", err)
	}
	// access.CanEdit admits every member for a workspace-scoped autopilot
	// (FIR-4359), so the route gate below is the whole boundary — every mutating
	// autopilot route must carry one, not just the five obvious ones.
	// Each pattern pins the handler too: the autopilot-level and trigger-level
	// Patch("/") / Delete("/") routes are textually identical without it, so a
	// handler-free pattern would let a missing trigger gate pass on the strength
	// of the autopilot gate.
	for name, pattern := range map[string]string{
		"create":          `RequirePlatformCapability\("create_autopilot"\)\)\.Post\("/", h\.CreateAutopilot\)`,
		"update":          `RequirePlatformCapability\("create_autopilot"\)\)\.Patch\("/", h\.UpdateAutopilot\)`,
		"delete":          `RequirePlatformCapability\("create_autopilot"\)\)\.Delete\("/", h\.DeleteAutopilot\)`,
		"trigger create":  `RequirePlatformCapability\("create_autopilot"\)\)\.Post\("/triggers", h\.CreateAutopilotTrigger\)`,
		"trigger update":  `RequirePlatformCapability\("create_autopilot"\)\)\.Patch\("/", h\.UpdateAutopilotTrigger\)`,
		"trigger delete":  `RequirePlatformCapability\("create_autopilot"\)\)\.Delete\("/", h\.DeleteAutopilotTrigger\)`,
		"rotate token":    `RequirePlatformCapability\("create_autopilot"\)\)\.Post\("/rotate-webhook-token", h\.RotateAutopilotTriggerWebhookToken\)`,
		"signing secret":  `RequirePlatformCapability\("create_autopilot"\)\)\.Put\("/signing-secret", h\.SetAutopilotTriggerSigningSecret\)`,
		"manual trigger":  `RequirePlatformCapability\("trigger_autopilot"\)\)\.Post\("/trigger", h\.TriggerAutopilot\)`,
		"delivery replay": `RequirePlatformCapability\("trigger_autopilot"\)\)\.Post\("/deliveries/\{deliveryId\}/replay", h\.ReplayAutopilotDelivery\)`,
	} {
		if !regexp.MustCompile(pattern).Match(source) {
			t.Errorf("%s route is missing the existing Permissions gate", name)
		}
	}
}
