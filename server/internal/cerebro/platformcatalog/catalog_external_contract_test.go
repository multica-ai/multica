package platformcatalog

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type externalContractSuite struct {
	capabilities []string
	packagePath  string
	tests        []string
}

// TestEveryExternallyManagedPermissionHasBehavioralProof keeps the read-only
// catalog entries honest. A named owner is not enough: this test
// runs the focused gate tests that exercise the security boundary which admits
// or rejects each operation. A source reference alone would only prove that a
// test name exists, not that the action still blocks in the running code.
func TestEveryExternallyManagedPermissionHasBehavioralProof(t *testing.T) {
	contracts := []externalContractSuite{
		// Eight capabilities moved to real tool-policy enforcement (FIR-4220
		// slices 1+2) — their gate proof lives in the policy-engine tests,
		// while their old checks survive as tighten-only ceilings. Only the
		// three machine-intake boundaries remain here.
		{[]string{"autopilot_webhook", "gateway_channel_delivery"}, "./internal/handler", []string{
			"TestWebhookHandler_404OnUnknownToken",
			"TestSearchChannelMessages_NonParticipantForbidden",
		}},
		{[]string{"daemon_runtime_callback"}, "./internal/middleware", []string{"TestDaemonAuth_MissingAuth"}},
		{[]string{"gateway_channel_delivery"}, "./internal/cerebro/webhookgateway", []string{"TestAuthorized"}},
	}

	seen := map[string]bool{}
	repoRoot := filepath.Clean(filepath.Join("..", "..", "..", ".."))
	for _, contract := range contracts {
		for _, capability := range contract.capabilities {
			seen[capability] = true
		}
		t.Run(strings.Join(contract.capabilities, ","), func(t *testing.T) {
			pattern := "^(" + strings.Join(contract.tests, "|") + ")$"
			cmd := exec.Command("go", "test", contract.packagePath, "-run", pattern, "-count=1")
			cmd.Dir = filepath.Join(repoRoot, "server")
			cmd.Env = os.Environ()
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("external permission gate tests failed for %v:\n%s", contract.capabilities, output)
			}
			if strings.Contains(string(output), "--- SKIP:") {
				t.Fatalf("external permission gate tests skipped for %v:\n%s", contract.capabilities, output)
			}
		})
	}
	for _, capability := range All() {
		if capability.ManagedExternally && !seen[capability.Key] {
			t.Errorf("%q has no executable behavioral security test", capability.Key)
		}
	}
	for _, contract := range contracts {
		for _, key := range contract.capabilities {
			capability, ok := ByKey(key)
			if !ok || !capability.ManagedExternally {
				t.Errorf("executable behavioral security test registered for non-external or missing permission %q", key)
			}
		}
	}
}
