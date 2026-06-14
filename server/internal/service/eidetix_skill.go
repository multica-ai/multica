package service

import (
	"embed"
	"io/fs"
	"path"
	"strings"
)

//go:embed eidetix_skill
var eidetixSkillFS embed.FS

const (
	eidetixSkillRoot = "eidetix_skill"
	eidetixSkillName = "multica-eidetix"
)

// EidetixLoopSkill returns the conditionally-shipped Eidetix read/write loop
// skill. It is deliberately NOT part of BuiltinSkills() (which ships to every
// agent): the claim handler appends it only when a project is bound to an
// enabled Eidetix graph. Returns nil if the skill fails to load (fail-open).
func (s *TaskService) EidetixLoopSkill() []AgentSkillData {
	skill, ok := loadEidetixSkill()
	if !ok {
		return nil
	}
	return []AgentSkillData{skill}
}

func loadEidetixSkill() (AgentSkillData, bool) {
	dir := path.Join(eidetixSkillRoot, eidetixSkillName)
	content, err := fs.ReadFile(eidetixSkillFS, path.Join(dir, "SKILL.md"))
	if err != nil {
		return AgentSkillData{}, false
	}
	skill := AgentSkillData{Name: eidetixSkillName, Content: string(content)}
	_ = fs.WalkDir(eidetixSkillFS, dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return walkErr
		}
		rel := strings.TrimPrefix(p, dir+"/")
		if rel == "SKILL.md" {
			return nil
		}
		data, readErr := fs.ReadFile(eidetixSkillFS, p)
		if readErr != nil {
			return nil
		}
		skill.Files = append(skill.Files, AgentSkillFileData{Path: rel, Content: string(data)})
		return nil
	})
	return skill, true
}
