package daemon

import (
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/taskskill"
)

func applyNativeSkillActivation(prompt, provider string, selected []taskskill.SelectedSkill) (string, bool, error) {
	if len(selected) == 0 {
		return prompt, false, nil
	}
	needsNative := false
	for _, skill := range selected {
		if skill.RequiresNativeActivation {
			needsNative = true
			break
		}
	}
	if !needsNative {
		return prompt, false, nil
	}
	if !providerSupportsNativeSkillActivation(provider) {
		names := make([]string, 0, len(selected))
		for _, skill := range selected {
			if skill.RequiresNativeActivation {
				names = append(names, skill.Name)
			}
		}
		return "", false, fmt.Errorf("selected skill %s requires native runtime activation, but provider %q does not support Multica slash-skill activation yet", strings.Join(names, ", "), provider)
	}

	var b strings.Builder
	for _, skill := range selected {
		name := strings.TrimSpace(skill.NativeName)
		if name == "" {
			name = strings.TrimSpace(skill.Name)
		}
		if name == "" {
			return "", false, fmt.Errorf("selected skill %q cannot be activated because it has no native command name", skill.ID)
		}
		fmt.Fprintf(&b, "/skill:%s\n", name)
	}
	b.WriteString("\n")
	b.WriteString(prompt)
	return b.String(), true, nil
}

func providerSupportsNativeSkillActivation(provider string) bool {
	return provider == "pi"
}
