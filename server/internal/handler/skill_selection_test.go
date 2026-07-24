package handler

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
)

func TestResolveSelectedSkillsForTaskValidatesMaterializedSkills(t *testing.T) {
	skills := []service.AgentSkillData{{
		ID:   "skill-1",
		Name: "Deploy",
		Content: `---
name: deploy
disable-model-invocation: true
---

body`,
	}}

	selected, errText := resolveSelectedSkillsForTask("[/deploy](slash://skill/skill-1)", skills)
	if errText != "" {
		t.Fatalf("unexpected selection error: %s", errText)
	}
	if len(selected) != 1 {
		t.Fatalf("selected len = %d, want 1", len(selected))
	}
	if !selected[0].RequiresNativeActivation || selected[0].NativeName != "deploy" {
		t.Fatalf("unexpected selected skill: %+v", selected[0])
	}
}

func TestResolveSelectedSkillsForTaskUnavailableSkillReturnsClearError(t *testing.T) {
	_, errText := resolveSelectedSkillsForTask("[/missing](slash://skill/missing)", []service.AgentSkillData{{
		ID:   "skill-1",
		Name: "Deploy",
	}})
	if errText == "" {
		t.Fatal("expected selection error")
	}
	if !strings.Contains(errText, "not assigned") {
		t.Fatalf("unexpected error: %s", errText)
	}
}
