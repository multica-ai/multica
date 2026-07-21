package execenv

import (
	"strings"

	"gopkg.in/yaml.v3"
)

func modelVisibleSkills(skills []SkillContextForEnv) []SkillContextForEnv {
	if len(skills) == 0 {
		return nil
	}
	visible := make([]SkillContextForEnv, 0, len(skills))
	for _, skill := range skills {
		if skillModelInvocationVisible(skill) {
			visible = append(visible, skill)
		}
	}
	return visible
}

func skillModelInvocationVisible(skill SkillContextForEnv) bool {
	return !skillDisablesModelInvocation(skill.Content)
}

func skillDisablesModelInvocation(content string) bool {
	fmBody, _, ok := frontmatterParts(content)
	if !ok || strings.TrimSpace(fmBody) == "" {
		return false
	}
	var data map[string]any
	if err := yaml.Unmarshal([]byte(fmBody), &data); err != nil {
		return false
	}
	value, ok := data["disable-model-invocation"]
	if !ok {
		return false
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	default:
		return false
	}
}
