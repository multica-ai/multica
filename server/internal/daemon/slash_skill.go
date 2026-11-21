package daemon

import (
	"regexp"
	"strings"
)

var slashSkillRe = regexp.MustCompile(
	`\[/((?:[^\]\\]|\\.)+)\]\(slash://skill/([^)]+)\)`,
)

type SlashSkillRef struct {
	Label string
	ID    string
}

func ExtractSlashSkills(md string) []SlashSkillRef {
	return uniqueSlashSkillRefs(slashSkillMatches(md))
}

type slashSkillMatch struct {
	start, end int
	ref        SlashSkillRef
}

func slashSkillMatches(md string) []slashSkillMatch {
	indices := slashSkillRe.FindAllStringSubmatchIndex(md, -1)
	matches := make([]slashSkillMatch, 0, len(indices))
	for _, m := range indices {
		label := strings.ReplaceAll(md[m[2]:m[3]], `\[`, "[")
		label = strings.ReplaceAll(label, `\]`, "]")
		matches = append(matches, slashSkillMatch{start: m[0], end: m[1], ref: SlashSkillRef{Label: label, ID: md[m[4]:m[5]]}})
	}
	return matches
}

func uniqueSlashSkillRefs(matches []slashSkillMatch) []SlashSkillRef {
	seen := make(map[string]struct{}, len(matches))
	refs := make([]SlashSkillRef, 0, len(matches))

	for _, m := range matches {
		if _, ok := seen[m.ref.ID]; ok {
			continue
		}
		seen[m.ref.ID] = struct{}{}
		refs = append(refs, m.ref)
	}

	return refs
}
