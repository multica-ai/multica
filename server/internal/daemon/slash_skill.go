package daemon

import (
	skillpkg "github.com/multica-ai/multica/server/internal/skill"
)

type SlashSkillRef struct {
	Label string
	ID    string
}

func ExtractSlashSkills(md string) []SlashSkillRef {
	invocations := skillpkg.ExtractSkillInvocations(md)
	refs := make([]SlashSkillRef, 0, len(invocations))
	for _, ref := range invocations {
		if ref.ID == "" {
			continue
		}
		refs = append(refs, SlashSkillRef{Label: ref.Label, ID: ref.ID})
	}
	return refs
}
