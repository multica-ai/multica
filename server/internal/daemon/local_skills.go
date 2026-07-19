package daemon

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/multica-ai/multica/server/internal/skill"
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
	// Root classifies which discovery root surfaced this skill:
	// localSkillRootProvider ("provider") for the runtime's own skill
	// directory (e.g. ~/.claude/skills) or localSkillRootUniversal
	// ("universal") for the cross-tool ~/.agents/skills fallback. The UI
	// uses it to label a skill's origin and to hint, in the import dialog,
	// whether a skill came from a provider-specific or a shared location.
	// Older daemons that predate multi-root discovery omit the field; the
	// server treats an empty value as "unknown" rather than a provider/
	// universal assertion.
	Root      string `json:"root,omitempty"`
	Plugin    string `json:"plugin,omitempty"`
	FileCount int    `json:"file_count"`
}

type runtimeLocalSkillBundle struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Content     string          `json:"content"`
	SourcePath  string          `json:"source_path"`
	Provider    string          `json:"provider"`
	Files       []SkillFileData `json:"files,omitempty"`
}

// localSkillRoot is a single discovery location plus a classifier for where
// it came from. Roots are returned in priority order by
// localSkillRootsForProvider; the kind is surfaced to the UI on each
// discovered skill (see runtimeLocalSkillSummary.Root).
type localSkillRoot struct {
	path      string
	kind      string
	keyPrefix string
	plugin    string
}

const (
	// localSkillRootProvider marks a runtime's own skill directory (e.g.
	// ~/.claude/skills). These take priority over the universal root.
	localSkillRootProvider = "provider"
	// localSkillRootUniversal marks the cross-tool ~/.agents/skills root,
	// a convention shared by Codex, Gemini CLI, Augment and others as a
	// universal home-level skill store. It is always searched last so a
	// same-key skill in the provider directory keeps winning.
	localSkillRootUniversal = "universal"
	// localSkillRootPlugin marks skills contributed by an enabled runtime
	// plugin. Plugin roots use a namespace prefix so their invocation keys
	// match Claude Code (for example paper-desktop:design-to-code) and never
	// collide with standalone user skills.
	localSkillRootPlugin = "plugin"
)

// localSkillRootsForProvider returns the ordered user-level skill roots
// scanned for each runtime/provider. The slice is in priority order:
//
//  1. the runtime's provider-specific root (backward-compatible with the
//     single-root behavior that predates ~/.agents/skills support), then
//  2. the cross-tool universal root ~/.agents/skills.
//
// Listing and import both walk the roots in this order and the first match of
// a given skill key wins, so adding the universal root never changes what an
// existing provider-root skill resolves to — it only surfaces additional,
// non-conflicting skills.
//
// Keep the provider roots in sync with upstream docs / conventions:
//   - GitHub Copilot: https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/add-skills
//   - OpenCode: https://opencode.ai/docs/skills
//   - OpenClaw: https://github.com/openclaw/openclaw/blob/main/docs/tools/skills.md
//   - Pi: https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/docs/skills.md
//   - Cursor: official forum guidance referencing the built-in /create-skill flow
//     (https://forum.cursor.com/t/cursor-doesnt-know-new-skills-arens-saved/158507)
//   - Hermes: ~/.hermes/skills is Hermes Agent's primary skill directory
//     (https://hermes-agent.nousresearch.com/docs/user-guide/features/skills)
//   - Kimi: ~/.kimi/skills mirrors Kimi CLI's project-level .kimi/skills layout
//   - Kiro: project and user-level .kiro/skills directories discovered by Kiro CLI
//   - Qoder: ~/.qoder/skills mirrors Qoder CLI's project-level .qoder/skills layout
//   - Antigravity: ~/.gemini/antigravity-cli/skills user-level skill root
//     (https://antigravity.google/docs/gcli-migration "Global skills")
//   - Grok: $GROK_HOME/skills, defaulting to ~/.grok/skills
//
// The universal ~/.agents/skills root is documented as a cross-tool skill
// location by Codex (https://developers.openai.com/codex/skills) and Gemini
// CLI (https://geminicli.com/docs/cli/skills/), among others.
//
// Longer-term this mapping would be better colocated with the provider
// definitions under server/pkg/agent so adding a new runtime can't silently
// miss the local-skills surface.
func localSkillRootsForProvider(provider string) ([]localSkillRoot, bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, false, fmt.Errorf("resolve user home: %w", err)
	}

	var providerRoot string
	switch provider {
	case "claude":
		providerRoot = filepath.Join(home, ".claude", "skills")
	case "codebuddy":
		// CodeBuddy Code is a Claude Code fork but ships its own native
		// config directory; it does NOT read ~/.claude/skills unless the
		// user manually symlinks it in (the vendor's documented Claude
		// Code migration path). See
		// https://www.codebuddy.ai/docs/cli/codebuddy-dir ("Global
		// directory ~/.codebuddy/") and
		// https://www.codebuddy.ai/docs/cli/skills ("User-level Skills:
		// ~/.codebuddy/skills/").
		providerRoot = filepath.Join(home, ".codebuddy", "skills")
	case "codex":
		codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
		if codexHome == "" {
			codexHome = filepath.Join(home, ".codex")
		}
		providerRoot = filepath.Join(codexHome, "skills")
	case "copilot":
		providerRoot = filepath.Join(home, ".copilot", "skills")
	case "opencode":
		providerRoot = filepath.Join(home, ".config", "opencode", "skills")
	case "deveco":
		providerRoot = filepath.Join(home, ".config", "deveco", "skills")
	case "openclaw":
		providerRoot = filepath.Join(home, ".openclaw", "skills")
	case "pi":
		providerRoot = filepath.Join(home, ".pi", "agent", "skills")
	case "cursor":
		providerRoot = filepath.Join(home, ".cursor", "skills")
	case "hermes":
		providerRoot = filepath.Join(home, ".hermes", "skills")
	case "kimi":
		providerRoot = filepath.Join(home, ".kimi", "skills")
	case "kiro":
		providerRoot = filepath.Join(home, ".kiro", "skills")
	case "qoder":
		providerRoot = filepath.Join(home, ".qoder", "skills")
	case "traecli":
		// Official TRAE CLI global skills live in ~/.traecli/skills.
		// See https://docs.trae.cn/cli_skills
		providerRoot = filepath.Join(home, ".traecli", "skills")
	case "antigravity":
		// agy inherits Gemini CLI's global skill root; see
		// https://antigravity.google/docs/gcli-migration ("Global skills").
		providerRoot = filepath.Join(home, ".gemini", "antigravity-cli", "skills")
	case "grok":
		// GROK_HOME replaces the default ~/.grok home for settings, sessions,
		// and user-level skills.
		grokHome := strings.TrimSpace(os.Getenv("GROK_HOME"))
		if grokHome == "" {
			grokHome = filepath.Join(home, ".grok")
		}
		providerRoot = filepath.Join(grokHome, "skills")
	default:
		return nil, false, nil
	}

	roots := []localSkillRoot{
		{path: providerRoot, kind: localSkillRootProvider},
		{path: filepath.Join(home, ".agents", "skills"), kind: localSkillRootUniversal},
	}
	if provider == "claude" {
		for _, plugin := range listEnabledClaudePlugins(home) {
			manifest, _ := readClaudePluginManifest(plugin.InstallPath)
			for _, path := range claudePluginComponentPaths(
				plugin.InstallPath,
				manifest.Skills,
				filepath.Join(plugin.InstallPath, "skills"),
			) {
				roots = append(roots, localSkillRoot{
					path:      path,
					kind:      localSkillRootPlugin,
					keyPrefix: plugin.Name + ":",
					plugin:    plugin.ID,
				})
			}
		}
	}
	return roots, true, nil
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
	roots, supported, err := localSkillRootsForProvider(provider)
	if err != nil || !supported {
		return nil, supported, err
	}

	// Walk each runtime root with two extensions over filepath.WalkDir:
	//   - Follow symlinks at every level. Installers like lark-cli ship
	//     each skill as a symlink into a shared ~/.agents/skills/<name>;
	//     the previous WalkDir path silently dropped them via the
	//     os.ModeSymlink early return.
	//   - Allow nested layouts. opencode stores skills as
	//     `release/reporter/SKILL.md`, and `loadRuntimeLocalSkillBundle`
	//     already accepts slash-delimited keys, so the list endpoint
	//     must surface those nested skills too.
	skills := make([]runtimeLocalSkillSummary, 0)
	// Dedupe strictly by Key. Roots are visited in priority order
	// (provider-specific first, ~/.agents/skills last); the first
	// occurrence of a Key wins. This keeps backward compatibility provable:
	// every skill visible under the single-root behavior keeps its Key,
	// SourcePath and FileCount, and we only ever *add* non-conflicting Keys
	// discovered under the universal root.
	seenKeys := make(map[string]bool)
	for _, root := range roots {
		if _, err := os.Stat(root.path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, true, err
		}

		// Each root gets its OWN `visited` set. It must NOT be shared across
		// roots: a user can deliberately expose the same on-disk skill under
		// two names by symlinking, e.g. ~/.claude/skills/bar ->
		// ~/.agents/skills/foo. With per-root visited sets both `bar` (claude
		// root) and `foo` (agents root) are listed; a shared set would mark
		// the resolved real path visited from the first root and silently
		// drop the legitimate second entry.
		rootSkills := make([]runtimeLocalSkillSummary, 0)
		visited := make(map[string]bool)
		enumerateLocalSkills(provider, root, root.path, root.path, 0, visited, &rootSkills)

		for _, s := range rootSkills {
			if seenKeys[s.Key] {
				continue
			}
			seenKeys[s.Key] = true
			skills = append(skills, s)
		}
	}

	// Sort once, after every root has been merged, so the result is stable
	// regardless of how many roots contributed.
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
	provider string,
	root localSkillRoot,
	walkRoot, currentDir string,
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
			key = root.keyPrefix + key

			content, err := readLocalSkillMainFile(path)
			if err != nil {
				continue
			}
			skillName, description := skill.ParseSkillFrontmatter(content)
			if root.plugin != "" {
				skillName = key
			} else if skillName == "" {
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
				Root:        root.kind,
				Plugin:      root.plugin,
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
		enumerateLocalSkills(provider, root, walkRoot, path, depth+1, visited, skills)
	}
}

func loadRuntimeLocalSkillBundle(provider, skillKey string) (*runtimeLocalSkillBundle, bool, error) {
	roots, supported, err := localSkillRootsForProvider(provider)
	if err != nil || !supported {
		return nil, supported, err
	}

	key, err := normalizeLocalSkillKey(skillKey)
	if err != nil {
		return nil, true, err
	}

	// Walk the roots in the same priority order as listRuntimeLocalSkills so
	// import resolves to exactly the skill the list endpoint surfaced. The
	// guiding invariant is list/load agreement: a root "has" the skill at this
	// key only when it is a directory carrying a SKILL.md — the exact
	// condition listRuntimeLocalSkills registers on. Anything that is not a
	// valid skill at this key (no entry, not a directory, or a directory
	// without a SKILL.md) means "this root doesn't have it" and we fall
	// through to the next root. Only a genuine IO/permission fault is
	// returned, so we never silently substitute a different-content same-key
	// skill from a lower-priority root.
	for _, root := range roots {
		rootKey := key
		if root.keyPrefix != "" {
			if !strings.HasPrefix(key, root.keyPrefix) {
				continue
			}
			rootKey = strings.TrimPrefix(key, root.keyPrefix)
		}
		skillDir := filepath.Join(root.path, filepath.FromSlash(rootKey))
		info, err := os.Stat(skillDir)
		if err != nil {
			// IsNotExist => this root simply lacks the skill, try the next.
			// Any other stat error (permission, IO) is returned as-is rather
			// than silently skipped, since skipping could load a DIFFERENT
			// same-key skill from a lower-priority root that does not match
			// what the user picked in the list (Eve review #1).
			if os.IsNotExist(err) {
				continue
			}
			return nil, true, err
		}
		if !info.IsDir() {
			// Not a directory: listRuntimeLocalSkills never surfaces a non-dir
			// as a skill, so this root has no skill at this key. Fall through
			// to the next root instead of erroring.
			continue
		}

		// A directory only counts as a skill when it actually contains a
		// SKILL.md. A same-key directory WITHOUT one (e.g. ~/.claude/skills/foo
		// that is just an empty/unrelated folder) is NOT this skill — list
		// would have descended past it — so it must not shadow a valid
		// ~/.agents/skills/foo. Treat a missing SKILL.md as "this root doesn't
		// have it" and continue; only a non-IsNotExist stat error on the
		// SKILL.md (permission, IO) is returned.
		mainPath := filepath.Join(skillDir, "SKILL.md")
		if _, err := os.Stat(mainPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, true, err
		}

		content, err := readLocalSkillMainFile(skillDir)
		if err != nil {
			return nil, true, err
		}
		name, description := skill.ParseSkillFrontmatter(content)
		if root.plugin != "" {
			name = key
		} else if name == "" {
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

	return nil, true, fmt.Errorf("local skill not found")
}
