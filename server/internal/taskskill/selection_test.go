package taskskill

import (
	"strings"
	"testing"
)

func TestResolveSlashSkillLinkByID(t *testing.T) {
	skills := []AvailableSkill{{
		ID:   "skill-1",
		Name: "Deploy",
		Content: `---
name: deploy
disable-model-invocation: true
---

body`,
	}}

	selected, err := Resolve("please [/anything](slash://skill/skill-1)", skills)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if len(selected) != 1 {
		t.Fatalf("selected len = %d, want 1", len(selected))
	}
	got := selected[0]
	if got.Name != "Deploy" || got.NativeName != "deploy" || !got.RequiresNativeActivation {
		t.Fatalf("unexpected selected skill: %+v", got)
	}
}

func TestResolveRejectsUnavailableSlashSkill(t *testing.T) {
	_, err := Resolve("[/missing](slash://skill/missing)", []AvailableSkill{{ID: "skill-1", Name: "Deploy"}})
	if err == nil {
		t.Fatal("expected unavailable skill error")
	}
	if !strings.Contains(err.Error(), "not assigned") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveNativeSkillCommand(t *testing.T) {
	skills := []AvailableSkill{{
		ID:   "skill-1",
		Name: "Deploy Production",
		Content: `---
name: deploy-prod
---

body`,
	}}

	selected, err := Resolve("  /skill:deploy-prod\nship it", skills)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if len(selected) != 1 || selected[0].ID != "skill-1" || selected[0].NativeName != "deploy-prod" {
		t.Fatalf("unexpected selected skills: %+v", selected)
	}
}

func TestResolveNativeSkillCommandAmbiguous(t *testing.T) {
	skills := []AvailableSkill{
		{ID: "a", Name: "Deploy"},
		{ID: "b", Name: "deploy"},
	}
	_, err := Resolve("/skill:deploy", skills)
	if err == nil {
		t.Fatal("expected ambiguous skill error")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("unexpected error: %v", err)
	}
}
