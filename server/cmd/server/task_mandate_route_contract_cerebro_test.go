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
