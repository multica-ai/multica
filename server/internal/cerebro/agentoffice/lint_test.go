package agentoffice

import (
	"strings"
	"testing"
)

func findByCode(findings []LintFinding, code string) []LintFinding {
	var out []LintFinding
	for _, f := range findings {
		if f.Code == code {
			out = append(out, f)
		}
	}
	return out
}

func TestLintSkillRefs(t *testing.T) {
	workspace := map[string]bool{"deploy": true, "plain-dansk-gate": true}
	bound := map[string]bool{"deploy": true}

	instructions := strings.Join([]string{
		"Follow the `plain-dansk-gate`-skill before posting.",   // exists, unbound
		"Details: the `verification-gate` skill has the steps.", // dead
		"See skill `deploy` for the merge protocol.",            // bound — clean
		"Run `make build` before pushing.",                      // backtick without skill adjacency — ignored
	}, "\n")

	findings := lintSkillRefs(instructions, workspace, bound)

	dead := findByCode(findings, "dead_skill_ref")
	if len(dead) != 1 || !strings.Contains(dead[0].Message, "verification-gate") {
		t.Fatalf("expected 1 dead_skill_ref for verification-gate, got %+v", dead)
	}
	if dead[0].Line != 2 {
		t.Fatalf("dead ref line = %d, want 2", dead[0].Line)
	}
	unbound := findByCode(findings, "unbound_skill_ref")
	if len(unbound) != 1 || !strings.Contains(unbound[0].Message, "plain-dansk-gate") {
		t.Fatalf("expected 1 unbound_skill_ref for plain-dansk-gate, got %+v", unbound)
	}
	if len(findings) != 2 {
		t.Fatalf("expected exactly 2 findings, got %+v", findings)
	}
}

func TestLintDuplicatedRules(t *testing.T) {
	rule := "Never claim work is done without running the verification pipeline first."
	instructions := "# Persona\n- " + rule + "\nShort line.\n"
	skills := []SkillDoc{
		{Name: "verify", Content: "Intro.\n1. " + rule + "\n"},
		{Name: "other", Content: "Unrelated content that is long enough to be substantive here.\n"},
	}

	findings := lintDuplicatedRules(instructions, skills)
	dups := findByCode(findings, "duplicated_rule")
	if len(dups) != 1 {
		t.Fatalf("expected 1 duplicated_rule, got %+v", findings)
	}
	if !strings.Contains(dups[0].Message, "instructions") || !strings.Contains(dups[0].Message, "skill verify") {
		t.Fatalf("message should name both layers, got %q", dups[0].Message)
	}

	// A line under 40 chars is not substantive enough to flag.
	short := lintDuplicatedRules("Tiny rule here.\n", []SkillDoc{{Name: "s", Content: "Tiny rule here.\n"}})
	if len(short) != 0 {
		t.Fatalf("short lines must not be flagged, got %+v", short)
	}
}

func TestLintRepoLinks(t *testing.T) {
	instructions := strings.Join([]string{
		"Work in https://github.com/firtal-group/firtal-cerebro daily.",
		"Old link: https://github.com/firtal-group/retired-repo.git is gone.",
	}, "\n")

	known := []string{"https://github.com/Firtal-Group/firtal-cerebro"}
	findings := lintRepoLinks(instructions, known)
	stale := findByCode(findings, "stale_repo_link")
	if len(stale) != 1 || !strings.Contains(stale[0].Message, "firtal-group/retired-repo") {
		t.Fatalf("expected 1 stale_repo_link for retired-repo, got %+v", findings)
	}

	// No known repos → staleness cannot be determined → check skipped.
	if got := lintRepoLinks(instructions, nil); len(got) != 0 {
		t.Fatalf("expected no findings without known repos, got %+v", got)
	}
}

func TestLintAgentContextGovernance(t *testing.T) {
	findings := LintAgentContext(AgentLintInput{
		Instructions:        "",
		WorkspaceSkillNames: map[string]bool{},
		HasContextOwner:     false,
		ApproverCount:       0,
	})
	if len(findByCode(findings, "missing_context_owner")) != 1 {
		t.Fatalf("expected missing_context_owner, got %+v", findings)
	}
	if len(findByCode(findings, "missing_approvers")) != 1 {
		t.Fatalf("expected missing_approvers, got %+v", findings)
	}

	clean := LintAgentContext(AgentLintInput{
		Instructions:        "",
		WorkspaceSkillNames: map[string]bool{},
		HasContextOwner:     true,
		ApproverCount:       2,
	})
	if len(clean) != 0 {
		t.Fatalf("expected no findings for governed empty agent, got %+v", clean)
	}
}

func TestLintRepoInstructionFile(t *testing.T) {
	rule := "Always attach proof from a green CI run before claiming anything is live."
	harness := []HarnessDoc{
		{Kind: "agent", Name: "Sabine", Text: "- " + rule + "\n"},
	}
	content := strings.Join([]string{
		"# Project",
		"Run `make dev` to start everything.",
		rule, // duplicated from harness
		"Never mention another agent in a closing comment.", // behavior: mention rules
		"```bash",
		"echo do not post this — fenced code is ignored",
		"```",
		"The build output lands in dist/.",
	}, "\n")

	findings := LintRepoInstructionFile("CLAUDE.md", content, harness)

	dup := findByCode(findings, "duplicated_from_harness")
	if len(dup) != 1 || !strings.Contains(dup[0].Message, "agent Sabine") {
		t.Fatalf("expected 1 duplicated_from_harness naming agent Sabine, got %+v", findings)
	}
	if dup[0].Line != 3 {
		t.Fatalf("duplicate line = %d, want 3", dup[0].Line)
	}
	behavior := findByCode(findings, "agent_behavior_in_repo_file")
	if len(behavior) != 1 || behavior[0].Line != 4 {
		t.Fatalf("expected 1 behavior finding on line 4, got %+v", behavior)
	}
	if len(findings) != 2 {
		t.Fatalf("expected exactly 2 findings (fenced code ignored), got %+v", findings)
	}
}
