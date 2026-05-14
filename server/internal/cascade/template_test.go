package cascade

import (
	"strings"
	"testing"
)

func TestRenderBlock_EmptyOnMissingRequiredFields(t *testing.T) {
	// Caller-bug protection: a half-populated BlockParams must render
	// nothing, so the agent falls back to the legacy per-PR approval
	// workflow rather than acting on partial cascade context.
	cases := []struct {
		name string
		in   BlockParams
	}{
		{"empty", BlockParams{}},
		{"missing issue id", BlockParams{PlanRepoCloneURL: "u", PlanFilePath: "p"}},
		{"missing plan repo", BlockParams{IssueID: "i", PlanFilePath: "p"}},
		{"missing plan path", BlockParams{IssueID: "i", PlanRepoCloneURL: "u"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RenderBlock(tc.in); got != "" {
				t.Fatalf("expected empty render, got %d bytes:\n%s", len(got), got)
			}
		})
	}
}

func TestRenderBlock_FullPopulated(t *testing.T) {
	p := BlockParams{
		IssueID:          "47033de5-0992-4ae5-bec1-0a3224cd3f87",
		PUL:              "PUL-102",
		PlanRepoCloneURL: "https://github.com/rabbeet/plans.git",
		PlanFilePath:     "Multica/2026-05-13-pul-102-event-driven-multi-pr-autonomy.md",
		CurrentStep:      3,
		TotalPRs:         8,
		BranchName:       "agent-2/pul-102-foo",
		MainBranch:       "main",
	}
	out := RenderBlock(p)
	if out == "" {
		t.Fatal("expected non-empty render")
	}
	// Pin a few load-bearing strings so an accidental rephrasing
	// during a future refactor is loud.
	must := []string{
		"## Cascade Execution (PUL-102)",
		"PUL-102",
		"47033de5-0992-4ae5-bec1-0a3224cd3f87",
		"https://github.com/rabbeet/plans.git",
		"Multica/2026-05-13-pul-102-event-driven-multi-pr-autonomy.md",
		"step 3 of 8",
		"agent-2/pul-102-foo",
		"git rebase origin/main",
		"On every wake-up",
		"atomic init",
		"After each successful PR",
		"Hard stops",
	}
	for _, s := range must {
		if !strings.Contains(out, s) {
			t.Errorf("rendered block missing %q\n--- full ---\n%s", s, out)
		}
	}
}

func TestRenderBlock_DefaultsMainBranch(t *testing.T) {
	p := BlockParams{
		IssueID:          "x",
		PlanRepoCloneURL: "u",
		PlanFilePath:     "p",
		// MainBranch left empty
	}
	out := RenderBlock(p)
	if !strings.Contains(out, "git rebase origin/main") {
		t.Fatalf("expected default main branch 'main', got:\n%s", out)
	}
}

func TestRenderBlock_RespectsCustomMainBranch(t *testing.T) {
	p := BlockParams{
		IssueID:          "x",
		PlanRepoCloneURL: "u",
		PlanFilePath:     "p",
		MainBranch:       "develop",
	}
	out := RenderBlock(p)
	if !strings.Contains(out, "git rebase origin/develop") {
		t.Fatalf("expected custom main branch 'develop', got:\n%s", out)
	}
}

func TestRenderBlock_UninitializedProgress(t *testing.T) {
	// total_prs unknown → renderer prints "not yet initialized" plus
	// the atomic-init instruction with a placeholder for total_prs.
	p := BlockParams{
		IssueID:          "x",
		PlanRepoCloneURL: "u",
		PlanFilePath:     "p",
	}
	out := RenderBlock(p)
	if !strings.Contains(out, "not yet initialized") {
		t.Errorf("expected 'not yet initialized' marker:\n%s", out)
	}
	if !strings.Contains(out, "<from plan>") {
		t.Errorf("expected '<from plan>' placeholder in atomic init:\n%s", out)
	}
}

func TestRenderBlock_KnownProgressInline(t *testing.T) {
	p := BlockParams{
		IssueID:          "x",
		PlanRepoCloneURL: "u",
		PlanFilePath:     "p",
		TotalPRs:         5,
		CurrentStep:      1,
	}
	out := RenderBlock(p)
	if !strings.Contains(out, `"total_prs": 5`) {
		t.Errorf("expected hardcoded total_prs=5 in atomic init:\n%s", out)
	}
}

func TestRenderBlock_CurrentStepClampedAtLeastOne(t *testing.T) {
	// When current_step is 0 (atomic init not yet run but caller
	// passed concrete params anyway), display as step 1 instead of
	// "step 0 of N" which would read as nonsense.
	p := BlockParams{
		IssueID:          "x",
		PlanRepoCloneURL: "u",
		PlanFilePath:     "p",
		TotalPRs:         5,
		CurrentStep:      0,
	}
	out := RenderBlock(p)
	if !strings.Contains(out, "step 1 of 5") {
		t.Errorf("expected step clamped to 1:\n%s", out)
	}
}
