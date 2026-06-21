package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type codexMcpConfig struct {
	McpServers map[string]codexMcpServer `json:"mcpServers"`
}

type codexMcpServer struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

func hasManagedCodexMcpConfig(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

func ensureCodexMcpConfig(codexHome string, raw json.RawMessage) error {
	if !hasManagedCodexMcpConfig(raw) {
		return nil
	}

	var cfg codexMcpConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("decode mcp_config: %w", err)
	}
	if cfg.McpServers == nil {
		return fmt.Errorf("mcp_config must contain an mcpServers object")
	}

	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		return fmt.Errorf("create CODEX_HOME: %w", err)
	}

	configPath := filepath.Join(codexHome, "config.toml")
	existing, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read codex config: %w", err)
	}

	var out strings.Builder
	out.Write(bytes.TrimSpace(existing))
	if out.Len() > 0 {
		out.WriteString("\n\n")
	}
	out.WriteString("# Managed by Multica for this task.\n")

	names := make([]string, 0, len(cfg.McpServers))
	for name := range cfg.McpServers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		server := cfg.McpServers[name]
		if strings.TrimSpace(server.Command) == "" {
			return fmt.Errorf("mcp_config server %q requires command", name)
		}
		out.WriteString("[mcp_servers.")
		out.WriteString(tomlKey(name))
		out.WriteString("]\n")
		out.WriteString("command = ")
		out.WriteString(tomlString(server.Command))
		out.WriteString("\n")
		if server.Args != nil {
			out.WriteString("args = ")
			out.WriteString(tomlStringArray(server.Args))
			out.WriteString("\n")
		}
		if len(server.Env) > 0 {
			out.WriteString("[mcp_servers.")
			out.WriteString(tomlKey(name))
			out.WriteString(".env]\n")
			keys := make([]string, 0, len(server.Env))
			for key := range server.Env {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				out.WriteString(tomlKey(key))
				out.WriteString(" = ")
				out.WriteString(tomlString(server.Env[key]))
				out.WriteString("\n")
			}
		}
		out.WriteString("\n")
	}

	if err := os.WriteFile(configPath, []byte(out.String()), 0o600); err != nil {
		return fmt.Errorf("write codex config: %w", err)
	}
	return os.Chmod(configPath, 0o600)
}

func tomlString(value string) string {
	return strconv.Quote(value)
}

func tomlStringArray(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = tomlString(value)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func tomlKey(key string) string {
	if key == "" {
		return `""`
	}
	for _, r := range key {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return tomlString(key)
	}
	return key
}
