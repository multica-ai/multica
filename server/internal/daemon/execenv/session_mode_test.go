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
		SessionModeEvalIDs:        []string{"7e767171-6a19-41d5-b3e6-edfdc97eedf7"}, // CEREBRO-PATCH(session-mode-evals): FIR-4047 evaluations, not skills.
	})
	if !strings.Contains(out, "PLAN MODE · v7") {
		t.Fatalf("published version missing: %s", out)
	}
	if !strings.Contains(out, "Use the workspace Plan contract.") {
		t.Fatalf("published instruction missing: %s", out)
	}
	for _, want := range []string{"Writes are disabled", "graphify", "Company Brain", "Approval is required", "1 evaluation(s) configured on this Mode"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Mode policy %q missing: %s", want, out)
		}
	}
}

// CEREBRO-PATCH(session-mode-evals): FIR-4047 the brief must not claim a
// prohibition the tool-policy chain is not enforcing. Plan Mode is instructed to
// save a plan, so the write line stays silent about plans and notes.
func TestRenderIssueContextWriteLineDoesNotForbidSavingAPlan(t *testing.T) {
	planOut := renderIssueContext("codex", TaskContextForEnv{IssueID: "issue-1", SessionMode: "plan"})
	if !strings.Contains(planOut, "Writes are disabled. Do not edit code or data and do not make external mutations.") {
		t.Fatalf("code-write prohibition missing: %s", planOut)
	}
	if strings.Contains(planOut, "do not save plans") {
		t.Fatalf("brief forbids the one deliverable Plan Mode exists to produce: %s", planOut)
	}

	buildOut := renderIssueContext("codex", TaskContextForEnv{
		IssueID:                "issue-1",
		SessionMode:            "build",
		SessionModeAllowsWrite: true,
	})
	if !strings.Contains(buildOut, "Writes are enabled") {
		t.Fatalf("build mode lost its write permission: %s", buildOut)
	}
}

// A Mode with no evaluations must not mention evaluations at all.
func TestRenderIssueContextOmitsEvaluationLineWhenNoneConfigured(t *testing.T) {
	out := renderIssueContext("codex", TaskContextForEnv{IssueID: "issue-1", SessionMode: "review"})
	if strings.Contains(out, "evaluation(s) configured") {
		t.Fatalf("evaluation line rendered for a Mode with no evaluations: %s", out)
	}
}
