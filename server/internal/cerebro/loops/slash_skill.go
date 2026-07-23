package loops

import (
	"regexp"
	"strings"
)

var slashSkillRe = regexp.MustCompile(`\[/((?:[^\]\\]|\\.)+)\]\(slash://skill/([^)]+)\)`)

type SlashSkillRef struct {
	Label string
	ID    string
}

// ExtractSlashSkills mirrors the markdown contract used by ContentEditor while
// keeping the Chain v2 runtime inside the Cerebro fork zone.
func ExtractSlashSkills(markdown string) []SlashSkillRef {
	matches := slashSkillRe.FindAllStringSubmatch(markdown, -1)
	seen := make(map[string]struct{}, len(matches))
	refs := make([]SlashSkillRef, 0, len(matches))
	for _, match := range matches {
		id := match[2]
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		label := strings.ReplaceAll(match[1], `\[`, "[")
		label = strings.ReplaceAll(label, `\]`, "]")
		refs = append(refs, SlashSkillRef{Label: label, ID: id})
	}
	return refs
}
