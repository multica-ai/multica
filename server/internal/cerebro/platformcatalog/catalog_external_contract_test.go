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

// TestEveryExternallyManagedPermissionHasBehavioralProof keeps the 11
// read-only catalog entries honest. A named owner is not enough: this test
// runs the focused gate tests that exercise the security boundary which admits
// or rejects each operation. A source reference alone would only prove that a
// test name exists, not that the action still blocks in the running code.
func TestEveryExternallyManagedPermissionHasBehavioralProof(t *testing.T) {
	contracts := []externalContractSuite{
		{[]string{"rerun_issue", "autopilot_webhook", "use_other_runtime", "manage_project_access", "gateway_channel_delivery", "read_issues", "read_projects"}, "./internal/handler", []string{
			"TestCancelTaskByUser_PrivateAgent_PlainMember_Returns403",
			"TestWebhookHandler_404OnUnknownToken",
			"TestCanUseRuntimeForAgent_Pure",
			"TestUpdateProjectAccess_MemberRejected",
			"TestSearchChannelMessages_NonParticipantForbidden",
			"TestListIssues_FiltersRestrictedForOutsider",
			"TestCanAccessProject_MemberOnlyOpenAndExplicitRestricted",
		}},
		{[]string{"trigger_autopilot", "autopilot_scope"}, "./internal/cerebro/access", []string{"TestCanTrigger"}},
		{[]string{"daemon_runtime_callback"}, "./internal/middleware", []string{"TestDaemonAuth_MissingAuth"}},
		{[]string{"schedule_agent_wakeup"}, "./internal/cerebro/wakeup", []string{"TestValidateIssueAndAgentRejectsForeignWorkspaceOrAgent"}},
		{[]string{"gateway_channel_delivery"}, "./internal/cerebro/webhookgateway", []string{"TestAuthorized"}},
		{[]string{"read_projects"}, "./internal/cerebro/grouppermissions", []string{"TestProjectGroupAccess_RoundTrip"}},
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
