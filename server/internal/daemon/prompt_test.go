package daemon

import (
	"strings"
	"testing"
)

// TestBuildQuickCreatePromptRules locks in the rules that govern how the
// quick-create agent is allowed to translate raw user input into the issue
// description body. Each substring corresponds to a concrete failure mode
// observed in production output:
//   - meta-instructions ("create an issue", "cc @X") leaking into the body
//   - the Context section being misused as an apology log when no external
//     references were actually fetched
//   - hard-line rules being silently dropped on prompt rewrites
func TestBuildQuickCreatePromptRules(t *testing.T) {
	out := buildQuickCreatePrompt(Task{QuickCreatePrompt: "fix the login button color"})

	mustContain := []string{
		// rule 1a: strip verbal routing meta-instructions
		"Routing meta-instructions to the issue-creating agent itself",
		// rule 1b: strip pure conversational fillers
		"Pure conversational fillers with no information",
		// rule 1c: cc mentions must keep the mention link in the description body,
		// since `multica issue create` has no --subscriber flag and the platform
		// auto-subscribes via mention parsing — see PR review feedback that
		// originally exposed this gap
		"CC mentions are special",
		"the platform auto-subscribes members",
		// rule 2: emit Context only when references were actually fetched
		"include this section ONLY when the input cited external references",
		"Do NOT emit a Context section to:",
		"Apologize for resources you could not fetch",
		// rule 3: pre-existing high-fidelity invariants must remain
		"Faithfully restate what the user wants done",
		"NEVER invent requirements",
		"Preserve specific names, identifiers, file paths",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("buildQuickCreatePrompt output missing required rule: %q", s)
		}
	}
}
