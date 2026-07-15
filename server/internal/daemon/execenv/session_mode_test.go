package execenv // CEREBRO-PATCH(session-plan-mode): fork behavior regression coverage.

import (
	"strings"
	"testing"
)

// CEREBRO-PATCH(session-modes): FIR-3111 covers the five-mode runtime brief contract.
func TestRenderIssueContextIncludesSelectedModeRule(t *testing.T) {
	out := renderIssueContext("codex", TaskContextForEnv{IssueID: "issue-1", SessionMode: "plan"})
	if !strings.Contains(out, "PLAN MODE") || !strings.Contains(out, "must NOT write or edit code") {
		t.Fatalf("plan mode rule missing: %s", out)
	}
	defaultOut := renderIssueContext("codex", TaskContextForEnv{IssueID: "issue-1"})
	if strings.Contains(defaultOut, "PLAN MODE") {
		t.Fatalf("default session unexpectedly has plan rule: %s", defaultOut)
	}
	researchOut := renderIssueContext("codex", TaskContextForEnv{ChatSessionID: "chat-1", SessionMode: "research"})
	if !strings.Contains(researchOut, "RESEARCH MODE") || !strings.Contains(researchOut, "read-only investigation") {
		t.Fatalf("research mode rule missing: %s", researchOut)
	}
}

func TestRenderIssueContextUsesPublishedModeInstructionAndVersion(t *testing.T) {
	// CEREBRO-PATCH(session-mode-config-brief-test): FIR-3111 pins every interface-managed field in the brief.
	out := renderIssueContext("codex", TaskContextForEnv{
		IssueID:                   "issue-1",
		SessionMode:               "plan",
		SessionModeVersion:        "7",
		SessionModeInstruction:    "Use the workspace Plan contract.",
		SessionModeAllowsWrite:    false,
		SessionModeAllowedTools:   []string{"graphify", "multica issue get"},
		SessionModeDataSources:    []string{"Company Brain"},
		SessionModeApprovalPolicy: "require",
		SessionModeWorkflowID:     "workflow-1",
		SessionModeEvalSkillIDs:   []string{"skill-1"},
	})
	if !strings.Contains(out, "PLAN MODE · v7") {
		t.Fatalf("published version missing: %s", out)
	}
	if !strings.Contains(out, "Use the workspace Plan contract.") {
		t.Fatalf("published instruction missing: %s", out)
	}
	for _, want := range []string{"Writes are disabled", "graphify", "Company Brain", "Approval is required", "workflow-1", "skill-1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Mode policy %q missing: %s", want, out)
		}
	}
}
