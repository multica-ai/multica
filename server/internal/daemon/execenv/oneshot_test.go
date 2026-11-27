package execenv

import (
	"strings"
	"testing"
)

func TestOneShotBrief(t *testing.T) {
	t.Parallel()
	instructions := "You are a reviewer.\nPreserve the user input."
	out := buildMetaSkillContent("codex", TaskContextForEnv{OneShot: true, AgentInstructions: instructions})
	for _, want := range []string{instructions, "one-shot request", "captured automatically as the run result"} {
		if !strings.Contains(out, want) {
			t.Fatalf("brief missing %q", want)
		}
	}
	for _, unwanted := range []string{"Post your final results as a comment", "## Sub-issue Creation"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("one-shot brief contains issue workflow: %q", unwanted)
		}
	}
}
