package execenv

import (
	"os"
	"path/filepath"
	"strings"
)

// GlobalSkillFile is a supporting file within a global skill directory.
type GlobalSkillFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// GlobalSkill represents a skill discovered from ~/.agents/skills/.
type GlobalSkill struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Content     string            `json:"content"` // body of SKILL.md (after frontmatter)
	Files       []GlobalSkillFile `json:"files"`   // all other files in the skill dir
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

// readGlobalSkill reads a skill directory: extracts name/description from
// SKILL.md frontmatter, stores the SKILL.md body as Content, and reads all
// other files in the directory into Files.
func readGlobalSkill(parentDir, dirName string) GlobalSkill {
	skillDir := filepath.Join(parentDir, dirName)

	// Read SKILL.md
	skillMdPath := filepath.Join(skillDir, "SKILL.md")
	raw, err := os.ReadFile(skillMdPath)
	if err != nil {
		return GlobalSkill{Name: dirName}
	}

	name, description, body := parseGlobalSkillFrontmatter(string(raw))
	if name == "" {
		name = dirName
	}

	skill := GlobalSkill{
		Name:        name,
		Description: description,
		Content:     body,
	}

	// Recursively collect all files except SKILL.md, preserving relative paths.
	collectFiles(skillDir, skillDir, &skill.Files)

	return skill
}

// collectFiles walks dir recursively and appends all files (excluding the root
// SKILL.md) to out, using paths relative to rootDir.
func collectFiles(rootDir, dir string, out *[]GlobalSkillFile) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		fullPath := filepath.Join(dir, entry.Name())
		relPath, _ := filepath.Rel(rootDir, fullPath)
		// Normalise to forward slashes so paths are consistent cross-platform.
		relPath = filepath.ToSlash(relPath)

		if entry.IsDir() {
			collectFiles(rootDir, fullPath, out)
			continue
		}
		if relPath == "SKILL.md" {
			continue
		}
		data, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}
		*out = append(*out, GlobalSkillFile{Path: relPath, Content: string(data)})
	}
}

// parseGlobalSkillFrontmatter extracts name and description from YAML
// frontmatter at the top of a SKILL.md file, and returns the body after it.
func parseGlobalSkillFrontmatter(content string) (name, description, body string) {
	body = content
	if !strings.HasPrefix(content, "---") {
		return "", "", body
	}
	end := strings.Index(content[3:], "---")
	if end < 0 {
		return "", "", body
	}
	frontmatterBlock := content[3 : 3+end]
	body = strings.TrimPrefix(content[3+end+3:], "\n")

	for _, line := range strings.Split(frontmatterBlock, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "name:"):
			name = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "name:")), "\"'")
		case strings.HasPrefix(line, "description:"):
			description = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "description:")), "\"'")
		}
	}
	return name, description, body
}
