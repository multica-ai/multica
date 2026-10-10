package execenv

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// ClaudePluginCopiesParams crosses the private preparation-helper boundary.
// Plugins comes from the runtime's native inventory, never from an API client.
type ClaudePluginCopiesParams struct {
	RootDir   string
	WorkDir   string
	ConfigDir string
	Disabled  []RuntimeSkillRefForEnv
	Plugins   []agent.ClaudePluginState
}

// PrepareClaudePluginCopies only snapshots active, unambiguous user installs.
// The caller holds the environment's lifetime claim while this helper runs.
func PrepareClaudePluginCopies(params ClaudePluginCopiesParams) ([]string, error) {
	if params.Plugins == nil {
		return nil, fmt.Errorf("Claude plugin filtering requires a complete native inventory")
	}
	installed := make(map[string][]agent.ClaudePluginState)
	for _, plugin := range params.Plugins {
		installed[plugin.ID] = append(installed[plugin.ID], plugin)
	}
	missing := make(map[string]bool)
	active := make([]RuntimeSkillRefForEnv, 0, len(params.Disabled))
	for _, ref := range params.Disabled {
		if ref.Root != "plugin" {
			continue
		}
		if ref.Plugin == "" {
			return nil, fmt.Errorf("disabled Claude plugin skill %q has no plugin identity", ref.Key)
		}
		matches := installed[ref.Plugin]
		if len(matches) == 0 {
			missing[ref.Plugin] = true
			continue
		}
		var enabled []agent.ClaudePluginState
		for _, plugin := range matches {
			if plugin.Enabled {
				enabled = append(enabled, plugin)
			}
		}
		if len(enabled) == 0 {
			// Do not load an @inline copy of a project/local-disabled plugin.
			continue
		}
		plugin := enabled[0]
		hasUserInstall := false
		for _, match := range enabled {
			// Claude may register the same user install again at local scope
			// after a project override. Identical paths are one copy, not two
			// competing plugin versions. Distinct paths remain ambiguous.
			hasUserInstall = hasUserInstall || match.Scope == "user"
			if match.InstallPath != plugin.InstallPath || (match.Scope != "user" && match.Scope != "project" && match.Scope != "local") {
				return nil, fmt.Errorf("Claude plugin %q has ambiguous native installations", ref.Plugin)
			}
			if len(match.Errors) != 0 {
				return nil, fmt.Errorf("Claude plugin %q has native loading errors; repair the plugin before filtering its skills", ref.Plugin)
			}
		}
		if !hasUserInstall {
			return nil, fmt.Errorf("Claude plugin %q does not have a user installation", ref.Plugin)
		}
		if !filepath.IsAbs(plugin.InstallPath) {
			return nil, fmt.Errorf("Claude plugin %q has no absolute native install path", ref.Plugin)
		}
		namespace, _, _ := strings.Cut(ref.Key, ":")
		for _, other := range params.Plugins {
			name, _, _ := strings.Cut(other.ID, "@")
			if other.Enabled && other.ID != ref.Plugin && name == namespace {
				return nil, fmt.Errorf("ambiguous Claude plugin namespace %q", namespace)
			}
		}
		// Ignore a path carried by the old conversion shape; only this native
		// inventory is authoritative for the current launch context.
		ref.PluginPath = plugin.InstallPath
		active = append(active, ref)
	}
	if len(missing) != 0 {
		if err := checkUnregisteredClaudePlugins(params.ConfigDir, params.WorkDir, missing); err != nil {
			return nil, err
		}
	}
	if len(active) != 0 {
		if err := validateCachedClaudeMarketplaces(params.ConfigDir, params.WorkDir, active); err != nil {
			return nil, err
		}
	}
	return prepareClaudePluginSkillCopies(params.RootDir, active)
}

// Local-development marketplaces load directly from their source, even when
// `plugin list` reports an older install-cache path. Copying that cache would
// silently roll back enabled skills/hooks, so limit snapshots to cached sources.
func validateCachedClaudeMarketplaces(configDir, workDir string, refs []RuntimeSkillRefForEnv) error {
	known, err := readClaudePluginPolicyObject(filepath.Join(configDir, "plugins", "known_marketplaces.json"))
	if err != nil {
		return err
	}
	marketplaces := make(map[string]bool)
	for _, ref := range refs {
		_, marketplace, ok := strings.Cut(ref.Plugin, "@")
		if !ok || marketplace == "" {
			return fmt.Errorf("Claude plugin %q has no marketplace identity", ref.Plugin)
		}
		marketplaces[marketplace] = true
	}
	validate := func(name string, raw json.RawMessage) error {
		var entry struct {
			Source struct {
				Source string `json:"source"`
			} `json:"source"`
		}
		if err := json.Unmarshal(raw, &entry); err != nil {
			return fmt.Errorf("read Claude marketplace %q: %w", name, err)
		}
		switch entry.Source.Source {
		case "github", "git", "url":
			return nil
		default:
			return fmt.Errorf("Claude marketplace %q uses source %q; task-local skill filtering requires a cached GitHub, Git or URL marketplace, not a local-development or unknown source", name, entry.Source.Source)
		}
	}
	for name := range marketplaces {
		if err := validate(name, known[name]); err != nil {
			return err
		}
	}
	paths, err := claudePluginPolicySettingsPaths(configDir, workDir)
	if err != nil {
		return err
	}
	for path := range paths {
		settings, err := readClaudePluginPolicyObject(path)
		if err != nil {
			return err
		}
		if raw, ok := settings["extraKnownMarketplaces"]; ok {
			var extra map[string]json.RawMessage
			if err := json.Unmarshal(raw, &extra); err != nil {
				return fmt.Errorf("read Claude marketplace overrides %s: %w", path, err)
			}
			for name := range marketplaces {
				if entry, ok := extra[name]; ok {
					if err := validate(name, entry); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// Claude can still load a marketplace plugin named in enabledPlugins even
// after its install record disappears, and `plugin list` silently treats a
// malformed registry as empty. Absence is therefore only an uninstall when
// there is no orphaned enablement. This is a conservative integrity check,
// not a second implementation of native settings precedence.
func checkUnregisteredClaudePlugins(configDir, workDir string, missing map[string]bool) error {
	if _, err := readClaudePluginPolicyObject(filepath.Join(configDir, "plugins", "installed_plugins.json")); err != nil {
		return err
	}
	paths, err := claudePluginPolicySettingsPaths(configDir, workDir)
	if err != nil {
		return err
	}
	for path := range paths {
		settings, err := readClaudePluginPolicyObject(path)
		if err != nil {
			return err
		}
		raw, ok := settings["enabledPlugins"]
		if !ok {
			continue
		}
		var enabled map[string]bool
		if err := json.Unmarshal(raw, &enabled); err != nil {
			return fmt.Errorf("read Claude plugin policy %s: %w", path, err)
		}
		for id := range missing {
			if enabled[id] {
				return fmt.Errorf("Claude plugin %q is unregistered but still enabled in %s; repair or remove its stale plugin configuration", id, path)
			}
		}
	}
	return nil
}

func claudePluginPolicySettingsPaths(configDir, workDir string) (map[string]bool, error) {
	if !filepath.IsAbs(configDir) || !filepath.IsAbs(workDir) {
		return nil, fmt.Errorf("cannot establish Claude plugin policy without absolute configuration and working directories")
	}
	// Claude observes the physical cwd, including when a configured local
	// directory reaches the project through a symlink.
	var err error
	workDir, err = filepath.EvalSymlinks(workDir)
	if err != nil {
		return nil, fmt.Errorf("resolve Claude plugin policy workdir: %w", err)
	}
	paths := map[string]bool{filepath.Join(configDir, "settings.json"): true}
	for dir := filepath.Clean(workDir); ; dir = filepath.Dir(dir) {
		paths[filepath.Join(dir, ".claude", "settings.json")] = true
		paths[filepath.Join(dir, ".claude", "settings.local.json")] = true
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			common, err := gitCommonDirFor(dir)
			if err != nil {
				return nil, err
			}
			// Current Claude versions can read local settings from the main
			// checkout, even when the task is in a linked worktree/subdirectory.
			paths[filepath.Join(filepath.Dir(common), ".claude", "settings.local.json")] = true
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	return paths, nil
}

func readClaudePluginPolicyObject(path string) (map[string]json.RawMessage, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Claude plugin policy %s: %w", path, err)
	}
	defer file.Close()
	const maxBytes = 4 << 20
	raw, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxBytes {
		return nil, fmt.Errorf("Claude plugin policy %s exceeds 4 MiB", path)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("read Claude plugin policy %s: %w", path, err)
	}
	if object == nil {
		return nil, fmt.Errorf("Claude plugin policy %s must be an object", path)
	}
	return object, nil
}
