package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type claudePluginInstall struct {
	ID          string
	Name        string
	InstallPath string
}

type claudeInstalledPluginsFile struct {
	Plugins map[string][]struct {
		Scope       string `json:"scope"`
		InstallPath string `json:"installPath"`
	} `json:"plugins"`
}

type claudeSettingsFile struct {
	EnabledPlugins map[string]bool `json:"enabledPlugins"`
}

type claudePluginManifest struct {
	Name       string          `json:"name"`
	Skills     json.RawMessage `json:"skills"`
	MCPServers json.RawMessage `json:"mcpServers"`
}

// listEnabledClaudePlugins resolves the current user-scope plugin installs
// that Claude Code itself has enabled. Reading the install registry is
// deliberate: recursively scanning ~/.claude/plugins would surface both the
// marketplace checkout and every cached version of the same plugin.
func listEnabledClaudePlugins(home string) []claudePluginInstall {
	settingsRaw, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		return nil
	}
	var settings claudeSettingsFile
	if json.Unmarshal(settingsRaw, &settings) != nil || len(settings.EnabledPlugins) == 0 {
		return nil
	}

	installedRaw, err := os.ReadFile(filepath.Join(home, ".claude", "plugins", "installed_plugins.json"))
	if err != nil {
		return nil
	}
	var installed claudeInstalledPluginsFile
	if json.Unmarshal(installedRaw, &installed) != nil {
		return nil
	}

	pluginIDs := make([]string, 0, len(settings.EnabledPlugins))
	for id, enabled := range settings.EnabledPlugins {
		if enabled {
			pluginIDs = append(pluginIDs, id)
		}
	}
	sort.Strings(pluginIDs)

	plugins := make([]claudePluginInstall, 0, len(pluginIDs))
	for _, id := range pluginIDs {
		installs := installed.Plugins[id]
		if len(installs) == 0 {
			continue
		}
		selected := installs[len(installs)-1]
		for _, install := range installs {
			if install.Scope == "user" {
				selected = install
			}
		}
		installPath := strings.TrimSpace(selected.InstallPath)
		if installPath == "" {
			continue
		}

		name := strings.TrimSpace(strings.SplitN(id, "@", 2)[0])
		if manifest, ok := readClaudePluginManifest(installPath); ok && strings.TrimSpace(manifest.Name) != "" {
			name = strings.TrimSpace(manifest.Name)
		}
		if name == "" {
			continue
		}
		plugins = append(plugins, claudePluginInstall{ID: id, Name: name, InstallPath: installPath})
	}
	return plugins
}

func readClaudePluginManifest(installPath string) (claudePluginManifest, bool) {
	raw, err := os.ReadFile(filepath.Join(installPath, ".claude-plugin", "plugin.json"))
	if err != nil {
		return claudePluginManifest{}, false
	}
	var manifest claudePluginManifest
	if json.Unmarshal(raw, &manifest) != nil {
		return claudePluginManifest{}, false
	}
	return manifest, true
}

func claudePluginComponentPaths(installPath string, raw json.RawMessage, defaults ...string) []string {
	paths := append([]string(nil), defaults...)
	var one string
	if json.Unmarshal(raw, &one) == nil && strings.TrimSpace(one) != "" {
		paths = append(paths, one)
	} else {
		var many []string
		if json.Unmarshal(raw, &many) == nil {
			paths = append(paths, many...)
		}
	}

	seen := make(map[string]bool)
	out := make([]string, 0, len(paths))
	for _, candidate := range paths {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(installPath, filepath.FromSlash(candidate))
		}
		candidate = filepath.Clean(candidate)
		rel, err := filepath.Rel(installPath, candidate)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if !seen[candidate] {
			seen[candidate] = true
			out = append(out, candidate)
		}
	}
	return out
}
