package taskskill

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	slashSkillLinkRe = regexp.MustCompile(`\[/((?:[^\]\\]|\\.)+)\]\(slash://skill/([^)]+)\)`)
	nativeSkillRe    = regexp.MustCompile(`^/skill:([^\s]+)`)
)

type AvailableSkill struct {
	ID      string
	Name    string
	Content string
}

type SelectedSkill struct {
	ID                       string `json:"id"`
	Name                     string `json:"name"`
	NativeName               string `json:"native_name"`
	RequiresNativeActivation bool   `json:"requires_native_activation,omitempty"`
}

type slashSkillRef struct {
	Label string
	ID    string
}

func Resolve(input string, skills []AvailableSkill) ([]SelectedSkill, error) {
	refs := extractSlashSkillLinks(input)
	if len(refs) > 0 {
		return resolveLinkRefs(refs, skills)
	}
	if name, ok := extractNativeSkillName(input); ok {
		return resolveNativeName(name, skills)
	}
	return nil, nil
}

func extractSlashSkillLinks(md string) []slashSkillRef {
	matches := slashSkillLinkRe.FindAllStringSubmatch(md, -1)
	seen := make(map[string]struct{}, len(matches))
	refs := make([]slashSkillRef, 0, len(matches))
	for _, m := range matches {
		id := m[2]
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		label := strings.ReplaceAll(m[1], `\[`, "[")
		label = strings.ReplaceAll(label, `\]`, "]")
		refs = append(refs, slashSkillRef{Label: label, ID: id})
	}
	return refs
}

func extractNativeSkillName(input string) (string, bool) {
	trimmed := strings.TrimSpace(input)
	m := nativeSkillRe.FindStringSubmatch(trimmed)
	if len(m) != 2 {
		return "", false
	}
	name := strings.TrimSpace(m[1])
	return name, name != ""
}

func resolveLinkRefs(refs []slashSkillRef, skills []AvailableSkill) ([]SelectedSkill, error) {
	byID := make(map[string]AvailableSkill, len(skills))
	for _, skill := range skills {
		if skill.ID != "" {
			byID[skill.ID] = skill
		}
	}
	selected := make([]SelectedSkill, 0, len(refs))
	for _, ref := range refs {
		skill, ok := byID[ref.ID]
		if !ok {
			label := ref.Label
			if label == "" {
				label = ref.ID
			}
			return nil, fmt.Errorf("selected skill %q is not assigned to this agent", label)
		}
		selected = append(selected, selectedSkill(skill))
	}
	return selected, nil
}

func resolveNativeName(name string, skills []AvailableSkill) ([]SelectedSkill, error) {
	var matches []AvailableSkill
	for _, skill := range skills {
		if skillNameMatches(name, skill) {
			matches = append(matches, skill)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("selected skill %q is not assigned to this agent", name)
	case 1:
		return []SelectedSkill{selectedSkill(matches[0])}, nil
	default:
		return nil, fmt.Errorf("selected skill %q is ambiguous; use the slash skill picker so Multica can send a skill ID", name)
	}
}

func skillNameMatches(input string, skill AvailableSkill) bool {
	input = strings.TrimSpace(input)
	return strings.EqualFold(input, strings.TrimSpace(skill.Name)) ||
		strings.EqualFold(input, nativeName(skill))
}

func selectedSkill(skill AvailableSkill) SelectedSkill {
	native := nativeName(skill)
	return SelectedSkill{
		ID:                       skill.ID,
		Name:                     skill.Name,
		NativeName:               native,
		RequiresNativeActivation: skillDisablesModelInvocation(skill.Content),
	}
}

func nativeName(skill AvailableSkill) string {
	if name := skillFrontmatterName(skill.Content); name != "" {
		return name
	}
	return strings.TrimSpace(skill.Name)
}

func skillFrontmatterName(content string) string {
	fm, _, ok := frontmatterParts(content)
	if !ok {
		return ""
	}
	var data map[string]any
	if err := yaml.Unmarshal([]byte(fm), &data); err != nil {
		return ""
	}
	if name, ok := data["name"].(string); ok {
		return strings.TrimSpace(name)
	}
	return ""
}

func skillDisablesModelInvocation(content string) bool {
	fm, _, ok := frontmatterParts(content)
	if !ok {
		return false
	}
	var data map[string]any
	if err := yaml.Unmarshal([]byte(fm), &data); err != nil {
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

func frontmatterParts(content string) (fmBody, body string, ok bool) {
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return "", content, false
	}
	bodyStart := 4
	if strings.HasPrefix(content, "---\r\n") {
		bodyStart = 5
	}
	rest := content[bodyStart:]
	for _, marker := range []string{"\n---\n", "\n---\r\n", "\r\n---\n", "\r\n---\r\n"} {
		if idx := strings.Index(rest, marker); idx >= 0 {
			endLineLen := len(marker)
			return rest[:idx], rest[idx+endLineLen:], true
		}
	}
	return "", content, false
}
