package agentpolicy

import "testing"

func TestOperatorControlledPolicyDeniesBaselineCommands(t *testing.T) {
	policy := FromRuntimeConfig([]byte(`{
		"multica_policy": {
			"mode": "operator_controlled"
		}
	}`))

	for _, command := range []string{
		CommandIssueCreate,
		CommandIssueUpdateStatus,
		CommandIssueStatus,
		CommandIssueUpdateAssignee,
		CommandIssueAssign,
	} {
		if !policy.DeniesCommand(command) {
			t.Fatalf("expected operator-controlled policy to deny %s", command)
		}
	}

	if !policy.DeniesAgentMentionsByDefault() {
		t.Fatalf("expected operator-controlled policy to deny agent mentions by default")
	}
}

func TestDenyCommandsWorkWithoutOperatorControlledMode(t *testing.T) {
	policy := FromRuntimeConfig([]byte(`{
		"multica_policy": {
			"deny_commands": ["issue.create"],
			"deny_agent_mentions": true
		}
	}`))

	if !policy.DeniesCommand(CommandIssueCreate) {
		t.Fatalf("expected explicit deny_commands to deny issue.create")
	}
	if policy.DeniesCommand(CommandIssueStatus) {
		t.Fatalf("did not expect status to be denied without operator_controlled mode or explicit deny")
	}
	if !policy.DeniesAgentMentionsByDefault() {
		t.Fatalf("expected explicit deny_agent_mentions to be honored")
	}
}

func TestEmptyOrInvalidRuntimeConfigHasNoPolicy(t *testing.T) {
	for _, raw := range [][]byte{nil, []byte(`{}`), []byte(`not json`)} {
		policy := FromRuntimeConfig(raw)
		if policy.DeniesCommand(CommandIssueCreate) {
			t.Fatalf("expected no command denial for raw=%q", string(raw))
		}
		if policy.DeniesAgentMentionsByDefault() {
			t.Fatalf("expected no agent mention denial for raw=%q", string(raw))
		}
	}
}

func TestOperatorControlledAlwaysDeniesAgentMentions(t *testing.T) {
	policy := FromRuntimeConfig([]byte(`{
		"multica_policy": {
			"mode": "operator_controlled",
			"deny_agent_mentions": false
		}
	}`))

	if !policy.DeniesAgentMentionsByDefault() {
		t.Fatalf("expected operator-controlled policy to deny agent mentions even when deny_agent_mentions is false")
	}
}

func TestSupervisedCollaborationParsesCollaborationConfig(t *testing.T) {
	policy := FromRuntimeConfig([]byte(`{
		"multica_policy": {
			"schema_version": "mhs19.v1",
			"mode": "supervised_collaboration",
			"deny_agent_mentions": false,
			"collaboration": {
				"enabled": true,
				"scope": "same_issue",
				"allowed_agent_targets": ["planner", "builder", "reviewer"],
				"raw_agent_mentions": "deny",
				"collaboration_requests": "allow_audited",
				"max_turns": 8,
				"max_depth": 2,
				"ttl_minutes": 120,
				"prevent_self_handoff": true,
				"prevent_cycles": true
			}
		}
	}`))

	if !policy.IsSupervisedCollaboration() {
		t.Fatalf("expected supervised collaboration mode")
	}
	if policy.SchemaVersion != "mhs19.v1" {
		t.Fatalf("unexpected schema version: %q", policy.SchemaVersion)
	}
	if !policy.AllowsAuditedCollaborationRequests() {
		t.Fatalf("expected supervised collaboration to allow audited collaboration requests")
	}
	if len(policy.Collaboration.AllowedAgentTargets) != 3 {
		t.Fatalf("expected three allowed agent targets, got %d", len(policy.Collaboration.AllowedAgentTargets))
	}
	if policy.Collaboration.Scope != "same_issue" || policy.Collaboration.MaxDepth != 2 || policy.Collaboration.TTLMinutes != 120 {
		t.Fatalf("unexpected collaboration config: %+v", policy.Collaboration)
	}
}

func TestSupervisedCollaborationKeepsLifecycleMutationsControlled(t *testing.T) {
	policy := FromRuntimeConfig([]byte(`{
		"multica_policy": {
			"mode": "supervised_collaboration"
		}
	}`))

	for _, command := range []string{
		CommandIssueCreate,
		CommandIssueUpdateStatus,
		CommandIssueStatus,
		CommandIssueUpdateAssignee,
		CommandIssueAssign,
	} {
		if !policy.DeniesCommand(command) {
			t.Fatalf("expected supervised collaboration policy to keep %s controlled", command)
		}
	}
}

func TestSupervisedCollaborationDeniesRawAgentMentionsEvenWhenConfigTriesToWeaken(t *testing.T) {
	policy := FromRuntimeConfig([]byte(`{
		"multica_policy": {
			"mode": "supervised_collaboration",
			"deny_agent_mentions": false,
			"collaboration": {
				"raw_agent_mentions": "allow",
				"collaboration_requests": "allow_audited"
			}
		}
	}`))

	if !policy.DeniesAgentMentionsByDefault() {
		t.Fatalf("expected supervised collaboration to deny raw agent mentions by default")
	}
	if !policy.DeniesRawAgentMentions() {
		t.Fatalf("expected supervised collaboration baseline to deny raw agent mentions")
	}
	if !policy.AllowsAuditedCollaborationRequests() {
		t.Fatalf("expected collaboration_request primitive to remain available")
	}
}

func TestDisabledCollaborationConfigDoesNotAllowAuditedRequests(t *testing.T) {
	policy := FromRuntimeConfig([]byte(`{
		"multica_policy": {
			"mode": "supervised_collaboration",
			"collaboration": {
				"enabled": false,
				"collaboration_requests": "allow_audited"
			}
		}
	}`))

	if policy.AllowsAuditedCollaborationRequests() {
		t.Fatalf("expected disabled collaboration config to block audited requests")
	}
}
