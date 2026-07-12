package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// mergeMCPOverlay layers a per-task overlay on top of an agent's saved
// mcp_config and returns the merged JSON for the daemon claim wire shape.
//
// The merge contract (kept deliberately shallow):
//
//   - Both inputs are expected to be the Claude-style
//     `{"mcpServers": {<name>: <object>}}` shape every supported runtime
//     consumes via execenv's cursor / openclaw / opencode / codex / hermes
//     sidecar generators. Anything not under `mcpServers` is preserved from
//     the AGENT side only — overlays today only carry server entries and
//     should not silently introduce other top-level keys.
//
//   - Merge is by SERVER NAME (the inner-map key under `mcpServers`).
//     On a name collision the OVERLAY wins. This is on purpose: the overlay
//     carries the live, user-scoped session URL (e.g. the user's own
//     Composio MCP bearer), whereas the agent's saved entry under the same
//     name — if any — would be a stale or admin-shared placeholder.
//
//   - Either side may be empty / nil / the literal `null`. Empty-everywhere
//     returns `nil` so the daemon's `hasManagedCursorMcpConfig` short-circuit
//     keeps treating the task as "no managed MCP at all".
//
// Failure mode: on malformed input the agent config is returned unchanged
// (with the error). Callers must never silently drop the agent's saved
// servers because the overlay JSON was bad — that would surprise-disable
// existing MCP tools.
func mergeMCPOverlay(agentMcpConfig, overlay json.RawMessage) (json.RawMessage, error) {
	if !hasManagedJSON(overlay) {
		return passthroughAgentMcpConfig(agentMcpConfig), nil
	}
	if !hasManagedJSON(agentMcpConfig) {
		// Re-marshal the overlay alone so the daemon receives the exact
		// canonical shape (the input may have been stored with arbitrary
		// whitespace by Postgres' JSONB representation).
		var oCfg map[string]json.RawMessage
		if err := json.Unmarshal(overlay, &oCfg); err != nil {
			return nil, fmt.Errorf("merge mcp overlay: parse overlay: %w", err)
		}
		out, err := json.Marshal(oCfg)
		if err != nil {
			return nil, fmt.Errorf("merge mcp overlay: marshal overlay: %w", err)
		}
		return out, nil
	}

	var aCfg map[string]json.RawMessage
	if err := json.Unmarshal(agentMcpConfig, &aCfg); err != nil {
		// Agent config malformed: surface the error but return the original
		// bytes unchanged so the agent's setup behavior matches the no-overlay
		// path (which would also have shipped the bad bytes downstream).
		return passthroughAgentMcpConfig(agentMcpConfig), fmt.Errorf("merge mcp overlay: parse agent mcp_config: %w", err)
	}
	var oCfg map[string]json.RawMessage
	if err := json.Unmarshal(overlay, &oCfg); err != nil {
		return passthroughAgentMcpConfig(agentMcpConfig), fmt.Errorf("merge mcp overlay: parse overlay: %w", err)
	}

	// Pull each side's `mcpServers` sub-map, default to empty so a
	// well-formed top level with no servers is treated like absent.
	aServers, err := unmarshalServerMap(aCfg["mcpServers"])
	if err != nil {
		return passthroughAgentMcpConfig(agentMcpConfig), fmt.Errorf("merge mcp overlay: agent mcpServers: %w", err)
	}
	oServers, err := unmarshalServerMap(oCfg["mcpServers"])
	if err != nil {
		return passthroughAgentMcpConfig(agentMcpConfig), fmt.Errorf("merge mcp overlay: overlay mcpServers: %w", err)
	}

	merged := make(map[string]json.RawMessage, len(aServers)+len(oServers))
	for k, v := range aServers {
		merged[k] = v
	}
	// Overlay wins on collisions.
	for k, v := range oServers {
		merged[k] = v
	}

	// Rebuild: keep any non-mcpServers top-level keys from the agent config,
	// then write the merged mcpServers map back. This is the only place we
	// touch top-level keys; everything else is left alone.
	out := make(map[string]json.RawMessage, len(aCfg)+1)
	for k, v := range aCfg {
		if k == "mcpServers" {
			continue
		}
		out[k] = v
	}
	if len(merged) > 0 {
		serversBytes, err := json.Marshal(merged)
		if err != nil {
			return nil, fmt.Errorf("merge mcp overlay: marshal merged servers: %w", err)
		}
		out["mcpServers"] = serversBytes
	}
	if len(out) == 0 {
		return nil, nil
	}
	final, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("merge mcp overlay: marshal merged: %w", err)
	}
	return final, nil
}

// hasManagedJSON reports whether a raw JSON column carries an actual managed
// payload (non-empty and not the literal `null`). Matches the convention
// hasManagedCursorMcpConfig uses on the daemon side so the merge respects
// the same "absent" semantics every consumer already agrees on.
func hasManagedJSON(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

// passthroughAgentMcpConfig returns the agent config unchanged, or nil when
// it is absent. Used on the early-return paths so a caller assigning the
// return value into a wire field gets the same nil/non-nil split as today.
func passthroughAgentMcpConfig(agentMcpConfig json.RawMessage) json.RawMessage {
	if !hasManagedJSON(agentMcpConfig) {
		return nil
	}
	return agentMcpConfig
}

// unmarshalServerMap decodes the `mcpServers` sub-object into a map keyed by
// server name. A nil/absent value returns an empty map (not an error) so the
// caller can compose without an extra nil-check.
func unmarshalServerMap(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if !hasManagedJSON(raw) {
		return map[string]json.RawMessage{}, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if m == nil {
		return map[string]json.RawMessage{}, nil
	}
	// Reject non-object server entries early — every runtime expects the
	// inner value to be an object and would 500 in the sidecar generator
	// otherwise. Mirrors parseCursorManagedMcpServers' guard.
	for name, server := range m {
		if name == "" {
			return nil, errors.New("mcp server name must not be empty")
		}
		trimmed := bytes.TrimSpace(server)
		if len(trimmed) == 0 || trimmed[0] != '{' {
			return nil, fmt.Errorf("mcpServers.%s must be a JSON object", name)
		}
	}
	return m, nil
}
