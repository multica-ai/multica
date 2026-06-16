package handler

import "encoding/json"

// eidetixServerName is the reserved key for the managed Eidetix MCP server in
// an agent's mcp_config. A user-defined server with this exact name is left
// untouched (the operator is presumed to know what they are doing).
const eidetixServerName = "eidetix"

// buildEidetixServerEntry returns the MCP server entry for the remote Eidetix
// endpoint. It is a cross-provider remote (HTTP/SSE) entry: backends key off
// different fields, so we emit both and let each ignore the other.
//
//   - Claude Code's `--mcp-config` parser requires `type: "http"` to load a
//     remote server. It does NOT understand `transport`; without `type` it
//     silently skips the server and the agent runs with no eidetix tools
//     (verified against claude 2.1.x). It tolerates the extra `transport` key.
//   - OpenClaw keys off `transport: "streamable-http"` and tolerates `type`.
//
// One entry therefore serves both. `url` (vs a stdio `command`) marks it remote.
func buildEidetixServerEntry(endpointURL, token string) map[string]any {
	return map[string]any{
		"type":      "http",
		"url":       endpointURL,
		"transport": "streamable-http",
		"headers": map[string]any{
			"Authorization": "Bearer " + token,
		},
	}
}

// mergeEidetixServer merges the managed eidetix server into an existing
// Claude-style mcp_config (`{"mcpServers": {...}}`). It returns the merged
// config and whether the server was added.
//
//   - existing == nil/empty  → a fresh {"mcpServers":{"eidetix":...}}
//   - existing has no eidetix → eidetix added, all other servers preserved
//   - existing has an eidetix → NOT clobbered; returns existing unchanged, added=false
//   - existing is malformed   → error (caller fails open and proceeds unchanged)
func mergeEidetixServer(existing json.RawMessage, endpointURL, token string) (json.RawMessage, bool, error) {
	root := map[string]json.RawMessage{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &root); err != nil {
			return nil, false, err
		}
	}

	servers := map[string]json.RawMessage{}
	if raw, ok := root["mcpServers"]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return nil, false, err
		}
	}

	if _, exists := servers[eidetixServerName]; exists {
		// User-defined server of the same name — do not clobber.
		return existing, false, nil
	}

	entryBytes, err := json.Marshal(buildEidetixServerEntry(endpointURL, token))
	if err != nil {
		return nil, false, err
	}
	servers[eidetixServerName] = entryBytes

	serversBytes, err := json.Marshal(servers)
	if err != nil {
		return nil, false, err
	}
	root["mcpServers"] = serversBytes

	out, err := json.Marshal(root)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}
