package execenv

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// antigravityPluginManifest is the plugin.json that marks the directory as a
// plugin for agy. Its servers are exposed as multica_<server>, so the name is
// what keeps tool names stable across `--conversation` resumes.
const antigravityPluginManifest = `{"name":"multica"}` + "\n"

// antigravityPluginGitignore makes git ignore the plugin directory for as long
// as it exists. A local resource puts the plugin inside the user's repository,
// where a bulk `git add` would otherwise commit the credentials in
// mcp_config.json. A .gitignore of its own works in every checkout layout and
// leaves the user's ignore configuration untouched.
const antigravityPluginGitignore = "*\n"

// prepareAntigravityMcpConfig writes an agent's managed MCP servers as a
// workspace plugin under the workdir. agy discovers workspace MCP servers
// through a plugin's mcp_config.json. A nil or null config writes nothing,
// leaving agy's own MCP settings in place.
func prepareAntigravityMcpConfig(workDir, provider string, raw json.RawMessage, manifest *sidecarManifest) error {
	if provider != "antigravity" {
		return nil
	}
	config := bytes.TrimSpace(raw)
	if len(config) == 0 || bytes.Equal(config, []byte("null")) {
		return nil
	}
	if workDir == "" {
		return errors.New("managed mcp_config requires a working directory")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(config, &object); err != nil {
		return fmt.Errorf("managed mcp_config must be a JSON object: %w", err)
	}

	pluginDir := filepath.Join(workDir, ".agents", "plugins", "multica")
	// The config carries credentials, so it must land in the workdir itself:
	// never write through a symlinked ancestor, nor into a plugin directory
	// that is already there.
	for _, dir := range []string{filepath.Join(workDir, ".agents"), filepath.Dir(pluginDir), pluginDir} {
		info, err := os.Lstat(dir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("stat %s: %w", dir, err)
		}
		if dir == pluginDir {
			return fmt.Errorf("managed mcp_config would overwrite existing %s", dir)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("managed mcp_config will not write through the symlink or non-directory %s", dir)
		}
	}
	if err := recordMkdirAll(pluginDir, 0o700, manifest); err != nil {
		return err
	}
	// The ignore file goes first, so the credentials never exist unignored.
	for _, file := range [...]struct{ name, content string }{
		{".gitignore", antigravityPluginGitignore},
		{"plugin.json", antigravityPluginManifest},
		{"mcp_config.json", string(raw)},
	} {
		if err := recordWriteFile(filepath.Join(pluginDir, file.name), []byte(file.content), 0o600, manifest); err != nil {
			return err
		}
	}
	return nil
}
