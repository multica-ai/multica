package skill

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	slashSkillLinkRe           = regexp.MustCompile(`\[/((?:[^\]\\]|\\.)+)\]\(slash://skill/([^)]+)\)`)
	nativeSkillRe              = regexp.MustCompile(`^/skill:([A-Za-z0-9][A-Za-z0-9_.-]*)\b`)
	skillInvocationNonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)
)

// InvocationRef is a user-authored skill selection. ID is present for
// structured slash://skill links; Name is present for native /skill:<name>
// commands typed at the start of a task/comment/chat prompt.
type InvocationRef struct {
	Label string
	ID    string
	Name  string
}

type AssignedSkill struct {
	ID      string
	Name    string
	Content string
}

type SelectedInvocation struct {
	Name string
}

// ExtractSkillInvocations returns explicit skill selections from Multica rich
// text links and from leading native /skill:<name> commands.
func ExtractSkillInvocations(text string) []InvocationRef {
	refs := extractNativeSkillCommands(text)
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if ref.Name != "" {
			seen["name:"+ref.Name] = struct{}{}
		}
	}

	for _, m := range slashSkillLinkRe.FindAllStringSubmatch(text, -1) {
		id := strings.TrimSpace(m[2])
		if id == "" {
			continue
		}
		key := "id:" + id
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		label := strings.ReplaceAll(m[1], `\[`, "[")
		label = strings.ReplaceAll(label, `\]`, "]")
		refs = append(refs, InvocationRef{Label: label, ID: id})
	}
	return refs
}

func ResolveSelectedSkillInvocations(sourceTexts []string, skills []AssignedSkill, provider string) ([]SelectedInvocation, error) {
	refs := collectSkillInvocationRefs(sourceTexts)
	if len(refs) == 0 {
		return nil, nil
	}

	byID := make(map[string]AssignedSkill, len(skills))
	byName := make(map[string]AssignedSkill, len(skills))
	for _, skill := range skills {
		if !skillUserInvocable(skill.Content) {
			continue
		}
		if strings.TrimSpace(skill.ID) != "" {
			byID[skill.ID] = skill
		}
		for _, name := range nativeSkillInvocationAliases(skill) {
			if _, exists := byName[name]; !exists {
				byName[name] = skill
			}
		}
	}

	selected := make([]SelectedInvocation, 0, len(refs))
	seen := map[string]struct{}{}
	for _, ref := range refs {
		var skill AssignedSkill
		var ok bool
		switch {
		case ref.ID != "":
			skill, ok = byID[ref.ID]
			if !ok {
				return nil, fmt.Errorf("selected skill %q is not assigned to this agent or is not user-invocable", displaySkillRef(ref))
			}
		case ref.Name != "":
			skill, ok = byName[ref.Name]
			if !ok {
				return nil, fmt.Errorf("selected skill /skill:%s is not assigned to this agent or is not user-invocable", ref.Name)
			}
		default:
			continue
		}

		name := nativeSkillInvocationName(skill)
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		selected = append(selected, SelectedInvocation{Name: name})
	}

	if len(selected) > 0 && !ProviderSupportsNativeSkillInvocation(provider) {
		return nil, fmt.Errorf("runtime provider %q does not support native /skill invocation", provider)
	}
	return selected, nil
}

func collectSkillInvocationRefs(sourceTexts []string) []InvocationRef {
	var refs []InvocationRef
	seen := map[string]struct{}{}
	for _, text := range sourceTexts {
		for _, ref := range ExtractSkillInvocations(text) {
			key := ref.ID
			if key == "" {
				key = "name:" + ref.Name
			} else {
				key = "id:" + key
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			refs = append(refs, ref)
		}
	}
	return refs
}

func ProviderSupportsNativeSkillInvocation(provider string) bool {
	return provider == "pi"
}

func nativeSkillInvocationAliases(skill AssignedSkill) []string {
	names := []string{nativeSkillInvocationName(skill)}
	if slug := sanitizeSkillInvocationName(skill.Name); slug != "" && slug != names[0] {
		names = append(names, slug)
	}
	if display := strings.TrimSpace(skill.Name); display != "" && display != names[0] && display == sanitizeSkillInvocationName(display) {
		names = append(names, display)
	}
	return names
}

func nativeSkillInvocationName(skill AssignedSkill) string {
	if name, _ := ParseSkillFrontmatter(skill.Content); strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	return sanitizeSkillInvocationName(skill.Name)
}

func sanitizeSkillInvocationName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = skillInvocationNonAlphaNum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "skill"
	}
	return s
}

func skillUserInvocable(content string) bool {
	fm, ok := skillFrontmatterMap(content)
	if !ok {
		return true
	}
	value, ok := fm["user-invocable"]
	if !ok {
		return true
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return !strings.EqualFold(strings.TrimSpace(v), "false")
	default:
		return true
	}
}

func skillFrontmatterMap(content string) (map[string]any, bool) {
	if !strings.HasPrefix(content, "---") {
		return nil, false
	}
	start := 0
	switch {
	case strings.HasPrefix(content, "---\n"):
		start = len("---\n")
	case strings.HasPrefix(content, "---\r\n"):
		start = len("---\r\n")
	default:
		return nil, false
	}
	rest := content[start:]
	closeIdx := strings.Index(rest, "\n---")
	if closeIdx < 0 {
		return nil, false
	}
	var fm map[string]any
	if err := yaml.Unmarshal([]byte(rest[:closeIdx]), &fm); err != nil {
		return nil, false
	}
	return fm, true
}

func displaySkillRef(ref InvocationRef) string {
	if ref.Label != "" {
		return ref.Label
	}
	if ref.ID != "" {
		return ref.ID
	}
	return ref.Name
}

func extractNativeSkillCommands(text string) []InvocationRef {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	var refs []InvocationRef
	seen := map[string]struct{}{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" && len(refs) == 0 {
			continue
		}
		m := nativeSkillRe.FindStringSubmatch(line)
		if m == nil {
			break
		}
		name := m[1]
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		refs = append(refs, InvocationRef{Label: "/skill:" + name, Name: name})
	}
	return refs
}
