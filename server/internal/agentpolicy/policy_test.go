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
