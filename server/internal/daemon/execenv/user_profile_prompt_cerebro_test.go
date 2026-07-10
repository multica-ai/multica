package execenv

import (
	"strings"
	"testing"
)

func TestUserProfilePromptSectionEmpty(t *testing.T) {
	t.Parallel()
	if got := userProfilePromptSection(""); got != "" {
		t.Errorf("empty profile must render nothing, got %q", got)
	}
	if got := userProfilePromptSection("  \n\t"); got != "" {
		t.Errorf("whitespace-only profile must render nothing, got %q", got)
	}
}

func TestUserProfilePromptSectionRendersProfile(t *testing.T) {
	t.Parallel()
	got := userProfilePromptSection("Keep replies short.\nAlways answer in Danish.")
	if !strings.Contains(got, "## User Communication Profile") {
		t.Errorf("section heading missing:\n%s", got)
	}
	if !strings.Contains(got, "Keep replies short.\nAlways answer in Danish.") {
		t.Errorf("profile body missing:\n%s", got)
	}
	if !strings.Contains(got, "```\n") {
		t.Errorf("profile body must be fenced so user text can't inject brief headings:\n%s", got)
	}
}

// TestRuntimeBriefIncludesUserProfilePrompt guards the call site in
// runtime_config.go: the section must actually be reachable from the brief
// builder, not just exist as a helper (FIR-2743 — an upstream sync deleted the
// inline render block once already).
func TestRuntimeBriefIncludesUserProfilePrompt(t *testing.T) {
	t.Parallel()
	ctx := TaskContextForEnv{
		AgentName:         "TestAgent",
		UserProfilePrompt: "PROFILE-SENTINEL-2743",
	}
	brief := buildMetaSkillContent("claude", ctx)
	if !strings.Contains(brief, "## User Communication Profile") {
		t.Fatalf("runtime brief is missing the User Communication Profile section")
	}
	if !strings.Contains(brief, "PROFILE-SENTINEL-2743") {
		t.Fatalf("runtime brief does not embed the compiled profile prompt")
	}
}
