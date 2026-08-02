package execenv

import (
	"strings"
	"testing"
)

// FIR-4282 — cerebro_always_on_skills_brief_test.go proves the two always-on
// helpers render correctly, but nothing proved they are still CALLED. Deleting
// either call site in buildMetaSkillContent left every existing test green
// while silently dropping every always-on rule out of the brief, which is the
// one failure mode the flag exists to prevent. These tests pin the wiring.

// alwaysOnPropagationSkills is one flagged skill with text and one ordinary
// skill, so each assertion below can distinguish "inlined" from "listed".
func alwaysOnPropagationSkills() []SkillContextForEnv {
	return []SkillContextForEnv{
		{
			Name:        "effective-comments",
			Description: "communication gate",
			Content:     "GATE: lead with the conclusion.",
			AlwaysOn:    true,
		},
		{
			Name:        "deploy",
			Description: "how to ship",
			Content:     "STEP 1: merge the PR.",
		},
	}
}

func TestAlwaysOnSkillTextReachesTheBrief(t *testing.T) {
	t.Parallel()

	// The skills section branches per provider; the always-on block must survive
	// every branch, including the non-Claude ones that read skills from disk.
	for _, provider := range []string{"claude", "codex", "gemini", "hermes"} {
		t.Run(provider, func(t *testing.T) {
			t.Parallel()

			out := buildMetaSkillContent(provider, TaskContextForEnv{
				IssueID:     "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
				AgentSkills: alwaysOnPropagationSkills(),
			})

			if !strings.Contains(out, "## Always-on skills") {
				t.Fatalf("brief is missing the always-on section:\n%s", out)
			}
			if !strings.Contains(out, "GATE: lead with the conclusion.") {
				t.Fatalf("always-on skill text never reached the brief:\n%s", out)
			}
			if strings.Contains(out, "STEP 1: merge the PR.") {
				t.Fatalf("load-on-demand skill text must not be inlined:\n%s", out)
			}
		})
	}
}

func TestAlwaysOnSkillIsMarkedInTheSkillsList(t *testing.T) {
	t.Parallel()

	out := buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID:     "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		AgentSkills: alwaysOnPropagationSkills(),
	})

	// Without the marker the one-line list and the reproduced text below it read
	// as two unrelated sets of skills.
	marker := "**(always on — full text below)**"
	idx := strings.Index(out, marker)
	if idx < 0 {
		t.Fatalf("skills list is missing the always-on marker:\n%s", out)
	}

	line := out[strings.LastIndex(out[:idx], "\n")+1 : idx]
	if !strings.Contains(line, "effective-comments") {
		t.Fatalf("always-on marker landed on the wrong skill: %q", line)
	}
	if strings.Count(out, marker) != 1 {
		t.Fatalf("expected exactly one marked skill, got %d:\n%s", strings.Count(out, marker), out)
	}
}

func TestAlwaysOnBriefTellsTheAgentToReinjectAfterCompaction(t *testing.T) {
	t.Parallel()

	// A long run compacts its earliest context first, and the always-on text sits
	// near the top of the brief. Without this sentence a faded rule reads as an
	// absent rule, which is indistinguishable from the flag never having been set.
	out := buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID:     "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		AgentSkills: alwaysOnPropagationSkills(),
	})

	notice := "compacted away"
	if !strings.Contains(out, notice) {
		t.Fatalf("always-on section is missing the compaction notice:\n%s", out)
	}
	if idx := strings.Index(out, notice); idx < strings.Index(out, "GATE: lead with the conclusion.") {
		t.Fatalf("compaction notice must follow the reproduced skill text it refers to")
	}
}

func TestBriefHasNoAlwaysOnSectionWhenNothingIsFlagged(t *testing.T) {
	t.Parallel()

	out := buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		AgentSkills: []SkillContextForEnv{
			{Name: "deploy", Description: "how to ship", Content: "STEP 1: merge the PR."},
		},
	})

	if strings.Contains(out, "## Always-on skills") {
		t.Fatalf("brief grew an always-on section with nothing flagged:\n%s", out)
	}
	if strings.Contains(out, "always on — full text below") {
		t.Fatalf("brief marked a load-on-demand skill as always-on:\n%s", out)
	}
}
