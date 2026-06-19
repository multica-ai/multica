package service

import (
	"embed"
	"io/fs"
	"path"
	"strings"
)

//go:embed builtin_skills
var builtinSkillsFS embed.FS

const builtinSkillsRoot = "builtin_skills"

// BuiltinSkills returns the platform's built-in skills, embedded at compile
// time. Every agent receives these on top of its workspace-bound skills, so
// they teach platform-wide "how to" workflows (e.g. mentioning) that the
// runtime brief intentionally leaves to skills.
//
// Layout: builtin_skills/<name>/SKILL.md plus optional supporting files. The
// <name> directory carries a "multica-" prefix so its on-disk slug can never
// collide with a workspace skill a user authored (see writeSkillFiles, which
// derives the skill directory from AgentSkillData.Name).
func (s *TaskService) BuiltinSkills() []AgentSkillData {
	return loadBuiltinSkills()
}

func loadBuiltinSkills() []AgentSkillData {
	entries, err := fs.ReadDir(builtinSkillsFS, builtinSkillsRoot)
	if err != nil {
		return nil
	}
	var skills []AgentSkillData
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if skill, ok := loadBuiltinSkill(entry.Name()); ok {
			skills = append(skills, skill)
		}
	}
	return skills
}

func loadBuiltinSkill(name string) (AgentSkillData, bool) {
	dir := path.Join(builtinSkillsRoot, name)
	content, err := fs.ReadFile(builtinSkillsFS, path.Join(dir, "SKILL.md"))
	if err != nil {
		// A skill directory without a SKILL.md is malformed — skip it rather
		// than ship an empty skill.
		return AgentSkillData{}, false
	}
	skill := AgentSkillData{Name: name, Content: string(content)}
	// Any other file in the directory becomes a supporting file, preserving
	// its relative path so subdirectories (e.g. rules/styling.md) survive.
	_ = fs.WalkDir(builtinSkillsFS, dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return walkErr
		}
		rel := strings.TrimPrefix(p, dir+"/")
		if rel == "SKILL.md" {
			return nil
		}
		data, readErr := fs.ReadFile(builtinSkillsFS, p)
		if readErr != nil {
			return nil
		}
		skill.Files = append(skill.Files, AgentSkillFileData{Path: rel, Content: string(data)})
		return nil
	})
	return skill, true
}
