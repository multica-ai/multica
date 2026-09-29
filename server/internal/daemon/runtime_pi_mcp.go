package daemon

import (
	"bytes"
	"errors"
	"os"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// piRuntimeMcpInventory reads user-level adapter sources only. Pi is deliberately
// absent from loadRuntimeMcpServerConfigs: listing local servers must not make
// them available to agents with an explicit managed configuration.
func piRuntimeMcpInventory() (map[string]any, bool, error) {
	paths, supported, err := agent.PiMCPUserConfigPaths()
	if err != nil || !supported {
		return nil, supported, err
	}
	servers := map[string]any{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, true, errors.New("read Pi MCP user configuration failed")
		}
		cfg, err := unmarshalRuntimeMcpConfig(bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf}), "jsonc")
		if err != nil {
			return nil, true, errors.New("parse Pi MCP user configuration failed")
		}
		key := "mcpServers"
		if cfg[key] == nil {
			key = "mcp-servers"
		}
		entries, ok := nestedRuntimeMcpMap(cfg, key)
		if !ok {
			continue
		}
		for name, value := range entries {
			entry, ok := value.(map[string]any)
			if !ok {
				continue
			}
			current, _ := servers[name].(map[string]any)
			if current == nil {
				current = map[string]any{}
			}
			// The adapter merges partial overrides per field. A new transport replaces
			// the old transport, while a disabled-only override retains its kind.
			for _, transport := range []string{"command", "url", "socket"} {
				if _, ok := entry[transport].(string); ok {
					if transport != "url" || current["url"] == nil {
						delete(current, "type")
					}
					delete(current, "command")
					delete(current, "url")
					delete(current, "socket")
					current[transport] = true
					break
				}
			}
			if _, ok := entry["httpTransport"]; ok && current["url"] != nil {
				switch entry["httpTransport"] {
				case "sse":
					current["type"] = "sse"
				case "streamable-http":
					current["type"] = "http"
				}
			}
			// Only fields used by the shared summary helper survive parsing. URLs,
			// command values, arguments, env, headers, and plugin settings stay local.
			// Pi disables only on literal disabled:true; it does not interpret
			// enabled:false. Normalize this before using the common reporter.
			if value, exists := entry["disabled"]; exists {
				disabled, _ := value.(bool)
				current["disabled"] = disabled
			}
			servers[name] = current
		}
	}
	return servers, true, nil
}
