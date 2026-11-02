package execenv

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func prepareAntigravityMcpConfig(workDir, provider string, raw json.RawMessage, manifest *sidecarManifest) (resultErr error) {
	trimmed := bytes.TrimSpace(raw)
	if provider != "antigravity" || len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	if workDir == "" {
		return errors.New("managed antigravity mcp_config requires a working directory")
	}
	config, err := antigravityMcpConfig(raw)
	if err != nil {
		return err
	}
	pluginDir := filepath.Join(workDir, ".agents", "plugins", "multica")
	for _, path := range []string{filepath.Join(workDir, ".agents"), filepath.Dir(pluginDir), pluginDir} {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect antigravity plugin path: %w", err)
		}
		if path == pluginDir {
			return fmt.Errorf("managed antigravity mcp_config would overwrite existing %s: %w", path, errPathPreExists)
		}
		if !info.IsDir() {
			return fmt.Errorf("managed antigravity mcp_config requires a non-symlink directory: %s", path)
		}
	}
	created := &sidecarManifest{}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, rollBackPreparedSidecars(*created))
		}
	}()
	if err := recordMkdirAll(filepath.Dir(pluginDir), 0o700, created); err != nil {
		return fmt.Errorf("create antigravity plugins directory: %w", err)
	}
	if err := os.Mkdir(pluginDir, 0o700); err != nil {
		return fmt.Errorf("create antigravity managed plugin: %w", err)
	}
	created.Dirs = append(created.Dirs, pluginDir)
	plugin := []byte("{\"name\":\"multica\",\"description\":\"Multica Managed Workspace MCP Bundle\"}\n")
	if err := recordWriteFile(filepath.Join(pluginDir, "plugin.json"), plugin, 0o600, created); err != nil {
		return fmt.Errorf("write antigravity plugin manifest: %w", err)
	}
	if err := recordWriteFile(filepath.Join(pluginDir, "mcp_config.json"), config, 0o600, created); err != nil {
		return fmt.Errorf("write antigravity mcp config: %w", err)
	}
	if manifest != nil {
		manifest.Files = append(manifest.Files, created.Files...)
		manifest.Dirs = append(manifest.Dirs, created.Dirs...)
	}
	return nil
}

func antigravityMcpConfig(raw json.RawMessage) ([]byte, error) {
	var config struct {
		Servers map[string]map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("parse antigravity mcp_config: %w", err)
	}
	if config.Servers == nil {
		config.Servers = make(map[string]map[string]json.RawMessage)
	}
	for name, server := range config.Servers {
		if strings.TrimSpace(name) == "" || server == nil {
			return nil, errors.New("antigravity MCP servers require non-empty names and object entries")
		}
		values := make(map[string]string)
		for _, key := range []string{"command", "url", "serverUrl"} {
			if value, exists := server[key]; exists {
				var text string
				if err := json.Unmarshal(value, &text); err != nil || strings.TrimSpace(text) == "" {
					return nil, fmt.Errorf("antigravity MCP server %q requires a non-empty string for %s", name, key)
				}
				values[key] = text
			}
		}
		remoteURL := values["url"]
		if remoteURL == "" {
			remoteURL = values["serverUrl"]
		}
		if values["url"] != "" && values["serverUrl"] != "" && values["url"] != values["serverUrl"] {
			return nil, fmt.Errorf("antigravity MCP server %q has conflicting url and serverUrl", name)
		}
		if (values["command"] == "") == (remoteURL == "") {
			return nil, fmt.Errorf("antigravity MCP server %q requires either command or a remote URL, not both", name)
		}
		if remoteURL != "" {
			encoded, err := json.Marshal(remoteURL)
			if err != nil {
				return nil, err
			}
			server["url"], server["serverUrl"] = encoded, encoded
		}
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal antigravity mcp_config: %w", err)
	}
	return append(data, '\n'), nil
}
