package execenv

import (
	"os"
	"path/filepath"
	"strings"
)

// GlobalSkill holds the name and description of a skill discovered from
// ~/.agents/skills/ on the local machine.
type GlobalSkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ScanGlobalSkills returns all skills found in ~/.agents/skills/.
// Returns nil (not an error) when the directory does not exist.
func ScanGlobalSkills() []GlobalSkill {
	dir, err := agentsSkillsDir()
	if err != nil {
			return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		// Directory missing or unreadable — not an error condition.
		return nil
	}

	var skills []GlobalSkill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skills = append(skills, readGlobalSkill(dir, entry.Name()))
	}
	return skills
}

// agentsSkillsDir returns ~/.agents/skills.
func agentsSkillsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".agents", "skills"), nil
}

// readGlobalSkill reads a skill directory and extracts name/description.
func readGlobalSkill(parentDir, dirName string) GlobalSkill {
	content, err := os.ReadFile(filepath.Join(parentDir, dirName, "SKILL.md"))
	if err != nil {
		return GlobalSkill{Name: dirName}
	}
	name, description := parseGlobalSkillFrontmatter(string(content))
	if name == "" {
		name = dirName
	}
	return GlobalSkill{Name: name, Description: description}
}

// parseGlobalSkillFrontmatter extracts name and description from YAML
// frontmatter at the top of a SKILL.md file.
func parseGlobalSkillFrontmatter(content string) (name, description string) {
	if !strings.HasPrefix(content, "---") {
		return "", ""
	}
	end := strings.Index(content[3:], "---")
	if end < 0 {
		return "", ""
	}
	for _, line := range strings.Split(content[3:3+end], "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "name:"):
			name = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "name:")), "\"'")
		case strings.HasPrefix(line, "description:"):
			description = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "description:")), "\"'")
		}
	}
	return name, description
}
