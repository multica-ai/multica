package daemon

import "github.com/multica-ai/multica/server/pkg/slashskill"

type SlashSkillRef = slashskill.Ref

func ExtractSlashSkills(markdown string) []SlashSkillRef {
	return slashskill.Extract(markdown)
}
