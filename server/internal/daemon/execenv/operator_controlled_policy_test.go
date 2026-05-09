package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInjectRuntimeConfigOperatorControlledAssignmentOmitsStatusCommands(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := TaskContextForEnv{
		IssueID: "policy-issue-id",
		AgentRuntimeConfig: []byte(`{
			"multica_policy": {
				"mode": "operator_controlled",
				"deny_agent_mentions": true
			}
		}`),
	}

	if err := InjectRuntimeConfig(dir, "claude", ctx); err != nil {
		t.Fatalf("InjectRuntimeConfig: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	s := string(content)
	if !strings.Contains(s, "operator-controlled assignment") {
		t.Fatalf("expected operator-controlled assignment instructions, got:\n%s", s)
	}
	for _, forbidden := range []string{
		"multica issue status policy-issue-id in_progress",
		"multica issue status policy-issue-id in_review",
		"multica issue status policy-issue-id blocked",
	} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("operator-controlled runtime config should not contain %q", forbidden)
		}
	}
	if !strings.Contains(s, "Do not include any `mention://agent/...` links") {
		t.Fatalf("expected agent mention guardrail in operator-controlled instructions")
	}
}

func TestInjectRuntimeConfigOperatorControlledCommentTriggerOmitsLifecycleInstructions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := TaskContextForEnv{
		IssueID:          "policy-issue-id",
		TriggerCommentID: "trigger-comment-id",
		AgentRuntimeConfig: []byte(`{
			"multica_policy": {
				"mode": "operator_controlled"
			}
		}`),
	}

	if err := InjectRuntimeConfig(dir, "claude", ctx); err != nil {
		t.Fatalf("InjectRuntimeConfig: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	s := string(content)
	if strings.Contains(s, "unless the comment explicitly asks for it") {
		t.Fatalf("operator-controlled comment trigger should not allow lifecycle changes on request")
	}
	if !strings.Contains(s, "Do NOT change issue status, change assignee, create issues, or mention another agent") {
		t.Fatalf("expected operator-controlled lifecycle and handoff guardrail in comment-triggered instructions")
	}
}
