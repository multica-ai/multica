package daemon

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/taskskill"
)

func TestApplyNativeSkillActivationPrefixesPiPrompt(t *testing.T) {
	prompt, activated, err := applyNativeSkillActivation("wrapped prompt", "pi", []taskskill.SelectedSkill{{
		ID:                       "skill-1",
		Name:                     "Deploy",
		NativeName:               "deploy",
		RequiresNativeActivation: true,
	}})
	if err != nil {
		t.Fatalf("applyNativeSkillActivation returned error: %v", err)
	}
	if !activated {
		t.Fatal("expected native activation")
	}
	if !strings.HasPrefix(prompt, "/skill:deploy\n\nwrapped prompt") {
		t.Fatalf("prompt was not prefixed with native skill command:\n%s", prompt)
	}
}

func TestApplyNativeSkillActivationRejectsUnsupportedProvider(t *testing.T) {
	_, _, err := applyNativeSkillActivation("wrapped prompt", "codex", []taskskill.SelectedSkill{{
		ID:                       "skill-1",
		Name:                     "Deploy",
		NativeName:               "deploy",
		RequiresNativeActivation: true,
	}})
	if err == nil {
		t.Fatal("expected unsupported provider error")
	}
	if !strings.Contains(err.Error(), "does not support Multica slash-skill activation") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyNativeSkillActivationSkipsVisibleSkills(t *testing.T) {
	prompt, activated, err := applyNativeSkillActivation("wrapped prompt", "codex", []taskskill.SelectedSkill{{
		ID:                       "skill-1",
		Name:                     "Deploy",
		NativeName:               "deploy",
		RequiresNativeActivation: false,
	}})
	if err != nil {
		t.Fatalf("applyNativeSkillActivation returned error: %v", err)
	}
	if activated {
		t.Fatal("did not expect native activation")
	}
	if prompt != "wrapped prompt" {
		t.Fatalf("prompt changed: %q", prompt)
	}
}
