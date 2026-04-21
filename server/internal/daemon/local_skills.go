package daemon

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	maxLocalSkillFileSize   int64 = 1 << 20
	maxLocalSkillBundleSize int64 = 8 << 20
	maxLocalSkillFileCount        = 128
)

type runtimeLocalSkillSummary struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	SourcePath  string `json:"source_path"`
	Provider    string `json:"provider"`
	FileCount   int    `json:"file_count"`
}

type runtimeLocalSkillBundle struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Content     string          `json:"content"`
	SourcePath  string          `json:"source_path"`
	Provider    string          `json:"provider"`
	Files       []SkillFileData `json:"files,omitempty"`
}

func localSkillRootForProvider(provider string) (string, bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, fmt.Errorf("resolve user home: %w", err)
	}

	switch provider {
	case "claude":
		return filepath.Join(home, ".claude", "skills"), true, nil
	case "codex":
		codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
		if codexHome == "" {
			codexHome = filepath.Join(home, ".codex")
		}
		return filepath.Join(codexHome, "skills"), true, nil
	case "copilot":
		return filepath.Join(home, ".copilot", "skills"), true, nil
	case "opencode":
		return filepath.Join(home, ".config", "opencode", "skills"), true, nil
	case "openclaw":
		return filepath.Join(home, ".openclaw", "skills"), true, nil
	case "pi":
		return filepath.Join(home, ".pi", "agent", "skills"), true, nil
	case "cursor":
		return filepath.Join(home, ".cursor", "skills"), true, nil
	default:
		return "", false, nil
	}
}

func isIgnoredLocalSkillEntry(name string) bool {
	if name == "" {
		return true
	}
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch strings.ToLower(name) {
	case "license", "license.md", "license.txt":
		return true
	default:
		return false
	}
}

func normalizeLocalSkillKey(key string) (string, error) {
	if strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("skill key is required")
	}
	cleaned := filepath.Clean(filepath.FromSlash(strings.TrimSpace(key)))
	if cleaned == "." || filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("invalid skill key")
	}
	return filepath.ToSlash(cleaned), nil
}

func relativizeHomePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.ToSlash(path)
	}
	if path == home {
		return "~"
	}
	prefix := home + string(filepath.Separator)
	if strings.HasPrefix(path, prefix) {
		return filepath.ToSlash("~" + string(filepath.Separator) + strings.TrimPrefix(path, prefix))
	}
	return filepath.ToSlash(path)
}

func parseLocalSkillFrontmatter(content string) (name, description string) {
	if !strings.HasPrefix(content, "---") {
		return "", ""
	}
	end := strings.Index(content[3:], "---")
	if end < 0 {
		return "", ""
	}
	frontmatter := content[3 : 3+end]
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			name = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "name:")), "\"'")
		} else if strings.HasPrefix(line, "description:") {
			description = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "description:")), "\"'")
		}
	}
	return name, description
}

func readLocalSkillMainFile(skillDir string) (string, error) {
	mainPath := filepath.Join(skillDir, "SKILL.md")
	info, err := os.Stat(mainPath)
	if err != nil {
		return "", err
	}
	if info.Size() > maxLocalSkillFileSize {
		return "", fmt.Errorf("SKILL.md exceeds %d bytes", maxLocalSkillFileSize)
	}
	content, err := os.ReadFile(mainPath)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func collectLocalSkillFiles(skillDir string, includeContent bool) ([]SkillFileData, error) {
	files := make([]SkillFileData, 0)
	var totalSize int64

	err := filepath.WalkDir(skillDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if path == skillDir {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if isIgnoredLocalSkillEntry(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if isIgnoredLocalSkillEntry(entry.Name()) || strings.EqualFold(entry.Name(), "SKILL.md") {
			return nil
		}

		rel, err := filepath.Rel(skillDir, path)
		if err != nil {
			return nil
		}
		rel = filepath.Clean(rel)
		if rel == "." || filepath.IsAbs(rel) || strings.HasPrefix(rel, "..") {
			return nil
		}

		info, err := entry.Info()
		if err != nil || info.Size() > maxLocalSkillFileSize {
			return nil
		}
		if len(files) >= maxLocalSkillFileCount {
			return fmt.Errorf("local skill exceeds %d files", maxLocalSkillFileCount)
		}
		totalSize += info.Size()
		if totalSize > maxLocalSkillBundleSize {
			return fmt.Errorf("local skill exceeds %d bytes in total", maxLocalSkillBundleSize)
		}

		file := SkillFileData{Path: filepath.ToSlash(rel)}
		if includeContent {
			content, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			file.Content = string(content)
		}
		files = append(files, file)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	return files, nil
}

func listRuntimeLocalSkills(provider string) ([]runtimeLocalSkillSummary, bool, error) {
	root, supported, err := localSkillRootForProvider(provider)
	if err != nil || !supported {
		return nil, supported, err
	}

	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return []runtimeLocalSkillSummary{}, true, nil
		}
		return nil, true, err
	}

	skills := make([]runtimeLocalSkillSummary, 0)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if path == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		if isIgnoredLocalSkillEntry(entry.Name()) {
			return filepath.SkipDir
		}

		mainPath := filepath.Join(path, "SKILL.md")
		if _, err := os.Stat(mainPath); err != nil {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return filepath.SkipDir
		}
		key, err := normalizeLocalSkillKey(rel)
		if err != nil {
			return filepath.SkipDir
		}

		content, err := readLocalSkillMainFile(path)
		if err != nil {
			return filepath.SkipDir
		}
		name, description := parseLocalSkillFrontmatter(content)
		if name == "" {
			name = filepath.Base(path)
		}

		files, err := collectLocalSkillFiles(path, false)
		if err != nil {
			return filepath.SkipDir
		}

		skills = append(skills, runtimeLocalSkillSummary{
			Key:         key,
			Name:        name,
			Description: description,
			SourcePath:  relativizeHomePath(path),
			Provider:    provider,
			FileCount:   len(files),
		})
		return filepath.SkipDir
	})
	if err != nil {
		return nil, true, err
	}

	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Key < skills[j].Key
	})
	return skills, true, nil
}

func loadRuntimeLocalSkillBundle(provider, skillKey string) (*runtimeLocalSkillBundle, bool, error) {
	root, supported, err := localSkillRootForProvider(provider)
	if err != nil || !supported {
		return nil, supported, err
	}

	key, err := normalizeLocalSkillKey(skillKey)
	if err != nil {
		return nil, true, err
	}

	skillDir := filepath.Join(root, filepath.FromSlash(key))
	info, err := os.Stat(skillDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, true, fmt.Errorf("local skill not found")
		}
		return nil, true, err
	}
	if !info.IsDir() {
		return nil, true, fmt.Errorf("local skill is not a directory")
	}

	content, err := readLocalSkillMainFile(skillDir)
	if err != nil {
		return nil, true, err
	}
	name, description := parseLocalSkillFrontmatter(content)
	if name == "" {
		name = filepath.Base(skillDir)
	}

	files, err := collectLocalSkillFiles(skillDir, true)
	if err != nil {
		return nil, true, err
	}

	return &runtimeLocalSkillBundle{
		Name:        name,
		Description: description,
		Content:     content,
		SourcePath:  relativizeHomePath(skillDir),
		Provider:    provider,
		Files:       files,
	}, true, nil
}
