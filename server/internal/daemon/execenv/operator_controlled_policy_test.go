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
	if !strings.Contains(s, "do not include any `mention://agent/...` links") {
		t.Fatalf("expected agent mention guardrail in operator-controlled instructions")
	}
	if strings.Contains(s, "--content \"...\"") {
		t.Fatalf("operator-controlled instructions should not use inline --content example")
	}
	if !strings.Contains(s, "--content-stdin") {
		t.Fatalf("expected stdin-based comment guidance")
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

func TestInjectRuntimeConfigSupervisedCollaborationAssignmentUsesDiscussionOnlyInstructions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := TaskContextForEnv{
		IssueID: "policy-issue-id",
		AgentRuntimeConfig: []byte(`{
			"multica_policy": {
				"mode": "supervised_collaboration",
				"collaboration": {
					"collaboration_requests": "allow_audited"
				}
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
	if !strings.Contains(s, "supervised collaboration assignment") {
		t.Fatalf("expected supervised collaboration assignment instructions, got:\n%s", s)
	}
	for _, forbidden := range []string{
		"multica issue status policy-issue-id in_progress",
		"multica issue status policy-issue-id in_review",
		"multica issue status policy-issue-id blocked",
	} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("supervised collaboration instructions should not contain %q", forbidden)
		}
	}
	if !strings.Contains(s, "multica issue collaboration-request create policy-issue-id") {
		t.Fatalf("expected supervised collaboration audited collaboration request guidance")
	}
	if !strings.Contains(s, "Do NOT change issue status, change assignee, create issues, or include raw `mention://agent/...` links") {
		t.Fatalf("expected lifecycle/raw mention guardrail in supervised collaboration instructions")
	}
	if !strings.Contains(s, "For this task's policy, do NOT use `mention://agent/...` links") {
		t.Fatalf("expected global mention guidance to honor supervised collaboration raw mention denial")
	}
	if strings.Contains(s, "--content \"...\"") {
		t.Fatalf("supervised collaboration instructions should not use inline --content example")
	}
}

func TestInjectRuntimeConfigSupervisedCollaborationWithoutIssueOmitsCollaborationRequestCommand(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := TaskContextForEnv{
		ChatSessionID: "chat-session-id",
		AgentRuntimeConfig: []byte(`{
			"multica_policy": {
				"mode": "supervised_collaboration",
				"collaboration": {
					"collaboration_requests": "allow_audited"
				}
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
	if strings.Contains(s, "multica issue collaboration-request create") {
		t.Fatalf("non-issue supervised collaboration task should not include collaboration-request command")
	}
	if !strings.Contains(s, "HANDOFF_RECOMMENDATION") {
		t.Fatalf("expected non-issue supervised collaboration to fall back to handoff recommendation guidance")
	}
}

func TestInjectRuntimeConfigSupervisedCollaborationCommentTriggerOmitsRawMentionInstructions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := TaskContextForEnv{
		IssueID:          "policy-issue-id",
		TriggerCommentID: "trigger-comment-id",
		AgentRuntimeConfig: []byte(`{
			"multica_policy": {
				"mode": "supervised_collaboration"
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
		t.Fatalf("supervised collaboration comment trigger should not allow lifecycle changes on request")
	}
	if !strings.Contains(s, "You may discuss, critique, and recommend a handoff") {
		t.Fatalf("expected supervised collaboration discussion guidance")
	}
	if !strings.Contains(s, "write a plain handoff recommendation") {
		t.Fatalf("expected plain handoff recommendation guidance")
	}
}
