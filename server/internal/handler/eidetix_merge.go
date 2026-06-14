package handler

import "encoding/json"

// eidetixServerName is the reserved key for the managed Eidetix MCP server in
// an agent's mcp_config. A user-defined server with this exact name is left
// untouched (the operator is presumed to know what they are doing).
const eidetixServerName = "eidetix"

// buildEidetixServerEntry returns the Claude-style MCP server entry for the
// remote Eidetix SSE endpoint. transport "streamable-http" + a url makes it a
// remote HTTP/SSE server (as opposed to a stdio `command` server).
func buildEidetixServerEntry(endpointURL, token string) map[string]any {
	return map[string]any{
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
