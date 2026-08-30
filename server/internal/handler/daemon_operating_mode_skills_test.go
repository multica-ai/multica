package handler

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
)

func TestOperationalWorkflowSkillDataIsModeGated(t *testing.T) {
	skills := []service.AgentSkillData{
		{ID: "workspace-1", Name: "workspace-skill"},
		{Name: operationalWorkflowSkillName},
	}

	for _, mode := range []string{"", "unknown", "coding"} {
		got := filterOperationalWorkflowSkillData(skills, mode)
		if hasSkillDataNamed(got, operationalWorkflowSkillName) {
			t.Fatalf("mode %q received the operational workflow skill", mode)
		}
		if !hasSkillDataNamed(got, "workspace-skill") {
			t.Fatalf("mode %q lost an unrelated workspace skill", mode)
		}
	}

	for _, mode := range []string{"operational", "hybrid"} {
		got := filterOperationalWorkflowSkillData(skills, mode)
		if !hasSkillDataNamed(got, operationalWorkflowSkillName) {
			t.Fatalf("mode %q did not receive the operational workflow skill", mode)
		}
	}
}

func TestOperationalWorkflowSkillRefsAreModeGated(t *testing.T) {
	refs := []service.AgentSkillRefData{
		{ID: "workspace-1", Source: "workspace", Name: "workspace-skill"},
		{ID: "builtin:" + operationalWorkflowSkillName, Source: "builtin", Name: operationalWorkflowSkillName},
	}

	for _, mode := range []string{"", "unknown", "coding"} {
		got := filterOperationalWorkflowSkillRefs(refs, mode)
		if hasSkillRefNamed(got, operationalWorkflowSkillName) {
			t.Fatalf("mode %q received the operational workflow skill ref", mode)
		}
		if !hasSkillRefNamed(got, "workspace-skill") {
			t.Fatalf("mode %q lost an unrelated workspace skill ref", mode)
		}
	}

	for _, mode := range []string{"operational", "hybrid"} {
		got := filterOperationalWorkflowSkillRefs(refs, mode)
		if !hasSkillRefNamed(got, operationalWorkflowSkillName) {
			t.Fatalf("mode %q did not receive the operational workflow skill ref", mode)
		}
	}
}

func hasSkillDataNamed(skills []service.AgentSkillData, name string) bool {
	for _, skill := range skills {
		if skill.Name == name {
			return true
		}
	}
	return false
}

func hasSkillRefNamed(skills []service.AgentSkillRefData, name string) bool {
	for _, skill := range skills {
		if skill.Name == name {
			return true
		}
	}
	return false
}
