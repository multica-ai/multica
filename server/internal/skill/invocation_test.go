package skill

import (
	"strings"
	"testing"
)

func TestExtractSkillInvocations(t *testing.T) {
	t.Run("leading native skill commands", func(t *testing.T) {
		refs := ExtractSkillInvocations("\n/skill:deploy\n/skill:review\nplease ship")
		if len(refs) != 2 {
			t.Fatalf("refs = %+v, want 2", refs)
		}
		if refs[0].Name != "deploy" || refs[1].Name != "review" {
			t.Fatalf("refs = %+v, want deploy/review", refs)
		}
	})

	t.Run("ignores non-leading native command", func(t *testing.T) {
		refs := ExtractSkillInvocations("please\n/skill:deploy")
		if len(refs) != 0 {
			t.Fatalf("refs = %+v, want none", refs)
		}
	})

	t.Run("extracts slash skill links", func(t *testing.T) {
		refs := ExtractSkillInvocations("please [/deploy](slash://skill/abc-123)")
		if len(refs) != 1 || refs[0].ID != "abc-123" || refs[0].Label != "deploy" {
			t.Fatalf("refs = %+v, want slash skill link", refs)
		}
	})

	t.Run("deduplicates repeated refs", func(t *testing.T) {
		refs := ExtractSkillInvocations("/skill:deploy\n/skill:deploy\n[/d](slash://skill/id)\n[/d2](slash://skill/id)")
		if len(refs) != 2 {
			t.Fatalf("refs = %+v, want 2", refs)
		}
	})
}

func TestResolveSelectedSkillInvocations(t *testing.T) {
	skills := []AssignedSkill{
		{
			ID:   "deploy-id",
			Name: "Deploy Skill",
			Content: `---
name: deploy
disable-model-invocation: true
---

body`,
		},
		{
			ID:   "private-id",
			Name: "Private Skill",
			Content: `---
name: private
user-invocable: false
---

body`,
		},
	}

	t.Run("accepts assigned disable-model-invocation skill", func(t *testing.T) {
		got, err := ResolveSelectedSkillInvocations([]string{"/skill:deploy\nship it"}, skills, "pi")
		if err != nil {
			t.Fatalf("ResolveSelectedSkillInvocations: %v", err)
		}
		if len(got) != 1 || got[0].Name != "deploy" {
			t.Fatalf("selected = %+v, want deploy", got)
		}
	})

	t.Run("accepts slash link by id", func(t *testing.T) {
		got, err := ResolveSelectedSkillInvocations([]string{"please [/Deploy](slash://skill/deploy-id)"}, skills, "pi")
		if err != nil {
			t.Fatalf("ResolveSelectedSkillInvocations: %v", err)
		}
		if len(got) != 1 || got[0].Name != "deploy" {
			t.Fatalf("selected = %+v, want deploy", got)
		}
	})

	t.Run("rejects typo", func(t *testing.T) {
		_, err := ResolveSelectedSkillInvocations([]string{"/skill:deply\nship it"}, skills, "pi")
		if err == nil || !strings.Contains(err.Error(), "/skill:deply") {
			t.Fatalf("err = %v, want typo message", err)
		}
	})

	t.Run("rejects non user invocable skill", func(t *testing.T) {
		_, err := ResolveSelectedSkillInvocations([]string{"/skill:private\nship it"}, skills, "pi")
		if err == nil || !strings.Contains(err.Error(), "not user-invocable") {
			t.Fatalf("err = %v, want user-invocable message", err)
		}
	})

	t.Run("rejects unsupported provider after valid selection", func(t *testing.T) {
		_, err := ResolveSelectedSkillInvocations([]string{"/skill:deploy\nship it"}, skills, "codex")
		if err == nil || !strings.Contains(err.Error(), `provider "codex"`) {
			t.Fatalf("err = %v, want unsupported provider message", err)
		}
	})
}
