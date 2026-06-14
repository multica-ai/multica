package service

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestEidetixLoopSkillConformsToTemplate(t *testing.T) {
	var svc TaskService
	skills := svc.EidetixLoopSkill()
	if len(skills) != 1 {
		t.Fatalf("EidetixLoopSkill() returned %d skills, want 1", len(skills))
	}
	skill := skills[0]

	if skill.Name != "multica-eidetix" {
		t.Errorf("name = %q, want multica-eidetix", skill.Name)
	}
	if !strings.HasPrefix(skill.Name, "multica-") {
		t.Errorf("name %q must carry the multica- prefix", skill.Name)
	}

	fm, body, ok := splitFrontmatter(skill.Content)
	if !ok {
		t.Fatalf("SKILL.md must lead with a --- frontmatter block")
	}
	if strings.TrimSpace(fm["name"]) == "" {
		t.Errorf("frontmatter missing name")
	}
	desc := strings.TrimSpace(fm["description"])
	if desc == "" {
		t.Errorf("frontmatter missing description")
	}
	if len(desc) > maxDescriptionChars {
		t.Errorf("description is %d chars, over the %d cap", len(desc), maxDescriptionChars)
	}
	if n := strings.Count(body, "\n") + 1; n > maxSkillBodyLines {
		t.Errorf("SKILL.md body is %d lines, over the %d-line budget", n, maxSkillBodyLines)
	}
	if got := strings.TrimSpace(fm["user-invocable"]); got != "false" {
		t.Errorf("user-invocable = %q, want false (context-triggered platform skill)", got)
	}

	if !strings.HasPrefix(skill.Content, "---\n") {
		t.Fatalf("missing leading frontmatter delimiter")
	}
	rest := skill.Content[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		t.Fatalf("frontmatter has no closing delimiter")
	}
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(rest[:end]), &parsed); err != nil {
		t.Fatalf("frontmatter is not valid YAML: %v", err)
	}

	for _, f := range skill.Files {
		lower := strings.ToLower(f.Path)
		if strings.Contains(lower, "eval") || strings.HasSuffix(lower, "_test.go") || strings.HasSuffix(lower, "_test.md") {
			t.Errorf("supporting file %q looks like an eval/test", f.Path)
		}
	}

	if !skillHasFile(skill, "references/eidetix-tools-source-map.md") {
		t.Errorf("missing supporting file references/eidetix-tools-source-map.md")
	}

	mustContain := []string{"recall", "ingest_traces", "get_schema", "resolve_entities"}
	for _, want := range mustContain {
		if !strings.Contains(skill.Content, want) {
			t.Errorf("skill body missing %q", want)
		}
	}
}

func TestEidetixSkillNotInBuiltins(t *testing.T) {
	for _, s := range loadBuiltinSkills() {
		if s.Name == "multica-eidetix" {
			t.Fatalf("multica-eidetix must NOT be a built-in skill; it ships only to Eidetix-enabled projects")
		}
	}
}
