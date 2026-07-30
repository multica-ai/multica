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
		SessionModeExtraSkillIDs:  []string{"skill-1"}, // CEREBRO-PATCH(session-mode-write-scopes): FIR-4047 extra skills replace the eval-skill naming.
	})
	if !strings.Contains(out, "PLAN MODE · v7") {
		t.Fatalf("published version missing: %s", out)
	}
	if !strings.Contains(out, "Use the workspace Plan contract.") {
		t.Fatalf("published instruction missing: %s", out)
	}
	for _, want := range []string{"Writes are disabled", "graphify", "Company Brain", "Approval is required", "adds 1 extra skill"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Mode policy %q missing: %s", want, out)
		}
	}
}

// CEREBRO-PATCH(session-mode-write-scopes): FIR-4047 the brief used to tell Plan
// Mode "Writes are disabled" right under an instruction to save a plan, so the
// agent skipped its only deliverable.
func TestRenderIssueContextSeparatesPlanWritesFromCodeWrites(t *testing.T) {
	planOut := renderIssueContext("codex", TaskContextForEnv{
		IssueID:              "issue-1",
		SessionMode:          "plan",
		SessionModePlanWrite: true,
	})
	if !strings.Contains(planOut, "You may save plans, notes and artifacts") {
		t.Fatalf("plan-write permission missing: %s", planOut)
	}
	if strings.Contains(planOut, "Writes are disabled") {
		t.Fatalf("plan mode still forbids saving a plan: %s", planOut)
	}
	if !strings.Contains(planOut, "Do not edit code or data") {
		t.Fatalf("plan mode lost the code-write prohibition: %s", planOut)
	}

	reviewOut := renderIssueContext("codex", TaskContextForEnv{IssueID: "issue-1", SessionMode: "review"})
	if !strings.Contains(reviewOut, "Writes are disabled") {
		t.Fatalf("review mode should still disable every write: %s", reviewOut)
	}

	buildOut := renderIssueContext("codex", TaskContextForEnv{
		IssueID:                "issue-1",
		SessionMode:            "build",
		SessionModeAllowsWrite: true,
		SessionModePlanWrite:   true,
	})
	if !strings.Contains(buildOut, "Writes are enabled") {
		t.Fatalf("build mode lost its write permission: %s", buildOut)
	}
}
