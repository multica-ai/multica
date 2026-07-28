package daemonmcp

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// BindCodexPlatformServer replaces any inherited server using Multica's
// reserved name, attaches the task identity, and merges the result with the
// agent's managed configuration.
func BindCodexPlatformServer(configPath string, selfEntry, agentConfig json.RawMessage, strict bool, taskEnv map[string]string) (json.RawMessage, error) {
	if !strict {
		if err := RemoveCodexMCPServerTable(configPath, "multica"); err != nil {
			return nil, err
		}
	}
	return Merge(WithTaskEnv(selfEntry, taskEnv), agentConfig), nil
}

// RemoveCodexMCPServerTable removes one inherited MCP server from Codex's
// task-scoped config. Multica uses this for its reserved platform server name
// before writing the task-bound replacement, while leaving every unrelated
// user-global MCP server intact.
func RemoveCodexMCPServerTable(configPath, name string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read Codex config: %w", err)
	}

	updated, removed := stripCodexMCPServerTable(string(data), name)
	if !removed {
		return nil
	}
	if err := os.WriteFile(configPath, []byte(updated), 0o600); err != nil {
		return fmt.Errorf("write Codex config: %w", err)
	}
	if err := os.Chmod(configPath, 0o600); err != nil {
		return fmt.Errorf("chmod Codex config: %w", err)
	}
	return nil
}

func stripCodexMCPServerTable(content, name string) (string, bool) {
	targetHeader := regexp.MustCompile(
		`^\s*\[\s*mcp_servers\s*\.\s*(?:"` + regexp.QuoteMeta(name) + `"|` +
			regexp.QuoteMeta(name) + `)(?:\s*\.|\s*\])`)

	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	skipping := false
	removed := false
	for _, line := range lines {
		if targetHeader.MatchString(line) {
			skipping = true
			removed = true
			continue
		}
		if skipping && strings.HasPrefix(strings.TrimSpace(line), "[") {
			skipping = false
		}
		if !skipping {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n"), removed
}
