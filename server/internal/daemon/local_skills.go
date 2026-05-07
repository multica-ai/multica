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
	// Cap how deep skill discovery descends below a runtime root. opencode
	// stores skills two levels deep (e.g. `release/reporter/SKILL.md`); a
	// few extra levels covers any realistic future layout while bounding
	// work in case an installer accidentally points us at $HOME.
	maxLocalSkillDirDepth = 4
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

// localSkillRootForProvider tracks the user-level skill locations exposed by
// each runtime/provider. Keep these in sync with upstream docs / conventions:
//   - GitHub Copilot: https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/add-skills
//   - OpenCode: https://opencode.ai/docs/skills
//   - OpenClaw: https://github.com/openclaw/openclaw/blob/main/docs/tools/skills.md
//   - Pi: https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/docs/skills.md
//   - Cursor: official forum guidance referencing the built-in /create-skill flow
//     (https://forum.cursor.com/t/cursor-doesnt-know-new-skills-arens-saved/158507)
//   - Kiro: project and user-level .kiro/skills directories discovered by Kiro CLI
//
// Longer-term this mapping would be better colocated with the provider
// definitions under server/pkg/agent so adding a new runtime can't silently
// miss the local-skills surface.
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
	case "kiro":
		return filepath.Join(home, ".kiro", "skills"), true, nil
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

	// filepath.WalkDir does not follow a symlinked root, so when the runtime
	// root contains symlinks into a shared skill installer (e.g. lark-cli's
	// ~/.agents/skills/<name>) walking from the symlink path enumerates zero
	// children and every such skill ends up reporting 0 files. Resolve the
	// real path first so the walk descends into the actual directory.
	walkRoot := skillDir
	if resolved, err := filepath.EvalSymlinks(skillDir); err == nil {
		walkRoot = resolved
	}

	err := filepath.WalkDir(walkRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if path == walkRoot {
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

		rel, err := filepath.Rel(walkRoot, path)
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

	// Walk the runtime root with two extensions over filepath.WalkDir:
	//   - Follow symlinks at every level. Installers like lark-cli ship
	//     each skill as a symlink into a shared ~/.agents/skills/<name>;
	//     the previous WalkDir path silently dropped them via the
	//     os.ModeSymlink early return.
	//   - Allow nested layouts. opencode stores skills as
	//     `release/reporter/SKILL.md`, and `loadRuntimeLocalSkillBundle`
	//     already accepts slash-delimited keys, so the list endpoint
	//     must surface those nested skills too.
	skills := make([]runtimeLocalSkillSummary, 0)
	visited := make(map[string]bool)
	enumerateLocalSkills(provider, root, root, 0, visited, &skills)

	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Key < skills[j].Key
	})
	return skills, true, nil
}

// enumerateLocalSkills walks `currentDir` looking for skill directories
// (directories that contain a SKILL.md). When one is found it is registered
// at a key relative to `walkRoot` and the recursion stops at that branch —
// we never descend into a directory that already qualifies as a skill, even
// if it happens to contain nested SKILL.md files of its own.
//
// `visited` keys on the resolved (symlink-followed) absolute path so a
// cyclic symlink can't loop forever; this is the only reason we eagerly
// EvalSymlinks up front. Errors from EvalSymlinks just stop the descent on
// that branch — most often it's a dangling link, which we want to ignore.
func enumerateLocalSkills(
	provider, walkRoot, currentDir string,
	depth int,
	visited map[string]bool,
	skills *[]runtimeLocalSkillSummary,
) {
	if depth > maxLocalSkillDirDepth {
		return
	}
	resolved, err := filepath.EvalSymlinks(currentDir)
	if err != nil {
		return
	}
	if visited[resolved] {
		return
	}
	visited[resolved] = true

	entries, err := os.ReadDir(currentDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		name := entry.Name()
		if isIgnoredLocalSkillEntry(name) {
			continue
		}
		path := filepath.Join(currentDir, name)
		info, statErr := os.Stat(path) // follows symlinks
		if statErr != nil || !info.IsDir() {
			continue
		}

		mainPath := filepath.Join(path, "SKILL.md")
		if _, err := os.Stat(mainPath); err == nil {
			rel, err := filepath.Rel(walkRoot, path)
			if err != nil {
				continue
			}
			key, err := normalizeLocalSkillKey(rel)
			if err != nil {
				continue
			}

			content, err := readLocalSkillMainFile(path)
			if err != nil {
				continue
			}
			skillName, description := parseLocalSkillFrontmatter(content)
			if skillName == "" {
				skillName = filepath.Base(path)
			}

			files, err := collectLocalSkillFiles(path, false)
			if err != nil {
				continue
			}

			*skills = append(*skills, runtimeLocalSkillSummary{
				Key:         key,
				Name:        skillName,
				Description: description,
				SourcePath:  relativizeHomePath(path),
				Provider:    provider,
				// `files` is the supporting bundle (collectLocalSkillFiles
				// intentionally excludes SKILL.md so the bundle's `Content`
				// field can carry it without duplication on import). For the
				// list summary the user expects the total file count, so add
				// one back for SKILL.md itself.
				FileCount: len(files) + 1,
			})
			continue
		}

		// No SKILL.md here — descend looking for nested skills.
		enumerateLocalSkills(provider, walkRoot, path, depth+1, visited, skills)
	}
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
