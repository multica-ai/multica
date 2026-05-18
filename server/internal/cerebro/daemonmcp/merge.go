// Package daemonmcp merges runtime-level and agent-level MCP configurations
// before the daemon spawns a Claude Code subprocess.
//
// JEH-1700 (9031). The runtime row's tools_config column stores a Claude Code
// --mcp-config document (shape: {"mcpServers": {...}}). The agent row's
// mcp_config column stores the same shape. The merge result is what the
// daemon writes to disk and passes via --mcp-config:
//
//   - agent.mcp_config wins per server name (so an agent owner can override
//     a runtime-level server, or shadow it with a noop).
//   - runtime.tools_config supplies servers the agent didn't specify.
//   - Any non-mcpServers fields on either document are preserved (agent
//     wins on collisions). Claude Code only reads mcpServers today but the
//     shape is open for future fields like "mcpAllowList".
//
// Both inputs are nil-safe: a nil or empty input is treated as an empty
// document. If both are empty the function returns nil so the daemon skips
// writing the --mcp-config file entirely (matches pre-patch behaviour).
package daemonmcp

import "encoding/json"

// Merge returns the merged MCP configuration as a JSON document, or nil if
// neither input contributes anything. Both inputs are expected to be the
// Claude Code --mcp-config shape; non-object JSON is ignored.
func Merge(runtimeRaw, agentRaw json.RawMessage) json.RawMessage {
	runtime := decode(runtimeRaw)
	agent := decode(agentRaw)
	if len(runtime) == 0 && len(agent) == 0 {
		return nil
	}

	out := make(map[string]any, len(runtime)+len(agent))
	for k, v := range runtime {
		out[k] = v
	}
	for k, v := range agent {
		if k == "mcpServers" {
			out[k] = mergeServers(runtime[k], v)
			continue
		}
		out[k] = v
	}
	// If agent omitted mcpServers but runtime had it, the loop above already
	// copied it via the runtime iteration — no extra work needed.

	encoded, err := json.Marshal(out)
	if err != nil {
		// json.Marshal on map[string]any only fails on unsupported types,
		// which can't appear here because decode produced standard JSON
		// values. Fall back to whichever input was non-empty so the daemon
		// still gets something.
		if len(agentRaw) > 0 {
			return agentRaw
		}
		return runtimeRaw
	}
	return encoded
}

func decode(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		// Malformed input — treat as empty so the daemon falls back to the
		// other side. Surfacing the error would block task execution on a
		// bad runtime config; better to log + recover at the daemon edge.
		return nil
	}
	return m
}

// mergeServers combines runtime + agent mcpServers maps. Agent server entries
// win per name. If either side is missing or not a map the function returns
// whichever side is a map (or an empty map if neither is).
func mergeServers(runtimeVal, agentVal any) map[string]any {
	runtimeMap, _ := runtimeVal.(map[string]any)
	agentMap, _ := agentVal.(map[string]any)

	out := make(map[string]any, len(runtimeMap)+len(agentMap))
	for k, v := range runtimeMap {
		out[k] = v
	}
	for k, v := range agentMap {
		out[k] = v
	}
	return out
}
