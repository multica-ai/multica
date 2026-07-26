package execenv

import "testing"

// FIR-3805 — the whole point of the flag is that the skill's TEXT reaches the
// agent without the agent choosing to open it. These tests pin that: an
// always-on skill's content is present, a normal skill's content is not.

func TestAlwaysOnSkillsBriefInlinesFullText(t *testing.T) {
	out := cerebroAlwaysOnSkillsBrief([]SkillContextForEnv{
		{Name: "caveman", Description: "compressed style", Content: "RULE: cut filler words.", AlwaysOn: true},
		{Name: "deploy", Description: "how to ship", Content: "STEP 1: merge the PR."},
	})

	if !contains(out, "RULE: cut filler words.") {
		t.Fatalf("always-on skill content missing from brief:\n%s", out)
	}
	if contains(out, "STEP 1: merge the PR.") {
		t.Fatalf("load-on-demand skill content must NOT be inlined:\n%s", out)
	}
	if !contains(out, "caveman") {
		t.Errorf("always-on skill name missing from brief:\n%s", out)
	}
	if contains(out, "### Always-on skill: deploy") {
		t.Errorf("deploy must not get an always-on section:\n%s", out)
	}
}

func TestAlwaysOnSkillsBriefEmptyWhenNothingFlagged(t *testing.T) {
	if out := cerebroAlwaysOnSkillsBrief([]SkillContextForEnv{
		{Name: "deploy", Content: "STEP 1: merge the PR."},
	}); out != "" {
		t.Fatalf("expected empty section when no skill is always-on, got:\n%s", out)
	}
	if out := cerebroAlwaysOnSkillsBrief(nil); out != "" {
		t.Fatalf("expected empty section for no skills, got:\n%s", out)
	}
}

// A flagged skill with no text would otherwise render an empty heading that
// reads as "this rule exists and says nothing".
func TestAlwaysOnSkillsBriefSkipsEmptyContent(t *testing.T) {
	if out := cerebroAlwaysOnSkillsBrief([]SkillContextForEnv{
		{Name: "blank", Content: "   \n", AlwaysOn: true},
	}); out != "" {
		t.Fatalf("expected empty section for a flagged skill with no text, got:\n%s", out)
	}
}

func TestAlwaysOnSkillSuffixMarksOnlyFlaggedSkills(t *testing.T) {
	flagged := SkillContextForEnv{Name: "caveman", Content: "RULE", AlwaysOn: true}
	if got := cerebroAlwaysOnSkillSuffix(flagged); got == "" {
		t.Error("expected an always-on marker in the skills list for a flagged skill")
	}
	normal := SkillContextForEnv{Name: "deploy", Content: "STEP 1"}
	if got := cerebroAlwaysOnSkillSuffix(normal); got != "" {
		t.Errorf("expected no marker for a load-on-demand skill, got %q", got)
	}
	// Flagged but empty is skipped by the section, so it must not be advertised
	// in the list either — otherwise the list promises text that is not there.
	empty := SkillContextForEnv{Name: "blank", Content: "", AlwaysOn: true}
	if got := cerebroAlwaysOnSkillSuffix(empty); got != "" {
		t.Errorf("expected no marker for a flagged skill with no text, got %q", got)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
