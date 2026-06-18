package daemonmcp

import (
	"encoding/json"
	"sort"
	"strings"
)

// AddExcludedTools adds Gemini-compatible per-server excludeTools entries for
// Claude-style deny tokens ("mcp__<server>__<tool>"). It keeps the MCP server
// available while withholding only the denied tools.
func AddExcludedTools(raw json.RawMessage, tokens []string) json.RawMessage {
	denied := deniedToolsByServer(tokens)
	if len(raw) == 0 || len(denied) == 0 {
		return raw
	}
	doc := decode(raw)
	if doc == nil {
		return raw
	}
	servers, ok := doc["mcpServers"].(map[string]any)
	if !ok || len(servers) == 0 {
		return raw
	}

	changed := false
	for serverName, tools := range denied {
		entry, ok := servers[serverName].(map[string]any)
		if !ok || len(tools) == 0 {
			continue
		}
		excludeSet := map[string]struct{}{}
		if existing, ok := entry["excludeTools"].([]any); ok {
			for _, item := range existing {
				if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
					excludeSet[s] = struct{}{}
				}
			}
		}
		before := len(excludeSet)
		for _, tool := range tools {
			excludeSet[tool] = struct{}{}
		}
		if len(excludeSet) == before {
			continue
		}
		excludeTools := make([]string, 0, len(excludeSet))
		for tool := range excludeSet {
			excludeTools = append(excludeTools, tool)
		}
		sort.Strings(excludeTools)
		entry["excludeTools"] = excludeTools
		changed = true
	}

	if !changed {
		return raw
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		return raw
	}
	return encoded
}

func deniedToolsByServer(tokens []string) map[string][]string {
	sets := map[string]map[string]struct{}{}
	for _, token := range tokens {
		rest := strings.TrimPrefix(strings.TrimSpace(token), "mcp__")
		server, tool, ok := strings.Cut(rest, "__")
		if !ok || server == "" || tool == "" {
			continue
		}
		if sets[server] == nil {
			sets[server] = map[string]struct{}{}
		}
		sets[server][tool] = struct{}{}
	}

	out := make(map[string][]string, len(sets))
	for server, tools := range sets {
		names := make([]string, 0, len(tools))
		for tool := range tools {
			names = append(names, tool)
		}
		sort.Strings(names)
		out[server] = names
	}
	return out
}
