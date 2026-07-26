package platformcatalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type externalContractTest struct {
	file string
	name string
}

// TestEveryExternallyManagedPermissionHasBehavioralProof keeps the 11
// read-only catalog entries honest. A named owner is not enough: each entry
// must point at a test that exercises the security boundary which actually
// admits or rejects the operation.
func TestEveryExternallyManagedPermissionHasBehavioralProof(t *testing.T) {
	contracts := map[string][]externalContractTest{
		"rerun_issue": {
			{file: "server/internal/handler/cancel_task_by_user_test.go", name: "TestCancelTaskByUser_PrivateAgent_PlainMember_Returns403"},
		},
		"trigger_autopilot": {
			{file: "server/internal/cerebro/access/autopilot_scope_test.go", name: "TestCanTrigger"},
		},
		"autopilot_scope": {
			{file: "server/internal/cerebro/access/autopilot_scope_test.go", name: "TestCanTrigger"},
		},
		"autopilot_webhook": {
			{file: "server/internal/handler/autopilot_webhook_handler_test.go", name: "TestWebhookHandler_404OnUnknownToken"},
		},
		"schedule_agent_wakeup": {
			{file: "server/internal/middleware/scope_test.go", name: "TestRequireUserScope_RejectsTaskScope"},
			{file: "server/internal/cerebro/wakeup/service_db_test.go", name: "TestValidateIssueAndAgentRejectsForeignWorkspaceOrAgent"},
		},
		"use_other_runtime": {
			{file: "server/internal/handler/runtime_visibility_test.go", name: "TestCanUseRuntimeForAgent_Pure"},
		},
		"daemon_runtime_callback": {
			{file: "server/internal/middleware/daemon_auth_test.go", name: "TestDaemonAuth_MissingAuth"},
		},
		"manage_project_access": {
			{file: "server/internal/handler/project_access_test.go", name: "TestUpdateProjectAccess_MemberRejected"},
		},
		"gateway_channel_delivery": {
			{file: "server/internal/cerebro/webhookgateway/deliver_test.go", name: "TestAuthorized"},
			{file: "server/internal/handler/channel_search_cerebro_test.go", name: "TestSearchChannelMessages_NonParticipantForbidden"},
		},
		"read_issues": {
			{file: "server/internal/handler/access_handlers_test.go", name: "TestListIssues_FiltersRestrictedForOutsider"},
		},
		"read_projects": {
			{file: "server/internal/handler/access_test.go", name: "TestCanAccessProject_MemberOnlyOpenAndExplicitRestricted"},
			{file: "server/internal/cerebro/grouppermissions/permissions_test.go", name: "TestProjectGroupAccess_RoundTrip"},
		},
	}

	seen := map[string]bool{}
	for _, capability := range All() {
		if !capability.ManagedExternally {
			continue
		}
		seen[capability.Key] = true
		refs := contracts[capability.Key]
		if len(refs) == 0 {
			t.Errorf("%q has no behavioral security test", capability.Key)
			continue
		}
		for _, ref := range refs {
			sourcePath := filepath.Join("..", "..", "..", "..", ref.file)
			source, err := os.ReadFile(sourcePath)
			if err != nil {
				t.Errorf("%q security test %s: %v", capability.Key, ref.file, err)
				continue
			}
			if !strings.Contains(string(source), "func "+ref.name+"(") {
				t.Errorf("%q security test %s#%s does not exist", capability.Key, ref.file, ref.name)
			}
		}
	}
	for key := range contracts {
		if !seen[key] {
			t.Errorf("behavioral security test registered for non-external or missing permission %q", key)
		}
	}
}
