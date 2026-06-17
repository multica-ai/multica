package handler

// CEREBRO-PATCH(agent-surface-visibility): TECH-3670 — per-surface discovery
// visibility for agents. This is a NEW axis, orthogonal to agent.visibility
// (workspace|private, which governs who may ASSIGN/TRIGGER). surface_visibility
// governs where a non-owner MEMBER can DISCOVER the agent: the agent directory
// and assignee picker ("lists"), @-mention autocomplete ("mention"), the
// chat/DM picker ("chat"), and channel member pickers ("channels").
//
// Enforcement is client-side, per surface (see
// packages/cerebro-access/views/surface-visibility.ts). It is deliberately NOT
// enforced by dropping rows from ListAgents: that endpoint is the single
// source the frontend uses to resolve an agent's name/avatar everywhere it
// appears, including as an assignee or commenter on a PUBLIC issue. Hiding it
// from the list would render a hidden agent as "Unknown Agent" on the very
// issues TECH-3670 wants it to stay visible on. So the server only STORES and
// RETURNS the field; each discovery picker filters on it. Owner + workspace
// owner/admin always see the agent (handled in the frontend resolver).
//
// The column is per-agent JSONB: {"lists":false,"mention":true,...} where the
// bool means "visible to a non-owner member on this surface". A missing key
// defaults to visible; a NULL/empty column means visible everywhere (the
// legacy behaviour), so existing agents are unchanged.

import (
	"encoding/json"
)

// agentSurface enumerates the discovery surfaces a non-owner member can be
// hidden from. Keep in sync with AGENT_SURFACES in
// packages/cerebro-access/views/surface-visibility.ts.
const (
	agentSurfaceLists    = "lists"
	agentSurfaceMention  = "mention"
	agentSurfaceChat     = "chat"
	agentSurfaceChannels = "channels"
)

// allAgentSurfaces is the closed set of valid surface keys.
var allAgentSurfaces = []string{
	agentSurfaceLists,
	agentSurfaceMention,
	agentSurfaceChat,
	agentSurfaceChannels,
}

// parseSurfaceVisibility decodes the JSONB column. A nil/empty/invalid blob
// yields a nil map, which the response treats as "visible everywhere".
func parseSurfaceVisibility(raw []byte) map[string]bool {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]bool
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

// surfaceVisibilityResponse converts the stored JSONB into the response map.
// Returns nil when unset so the JSON field is omitted for legacy agents.
func surfaceVisibilityResponse(raw []byte) map[string]bool {
	return parseSurfaceVisibility(raw)
}

// normalizeSurfaceVisibilityInput validates and re-encodes a surface_visibility
// payload from a create/update request. Unknown keys are dropped; non-bool
// values are rejected. Returns (nil, true) for empty input (leave unchanged),
// ([]byte("{}"), true) for an all-default/empty object (visible everywhere),
// and (nil, false) on malformed input.
func normalizeSurfaceVisibilityInput(raw json.RawMessage) ([]byte, bool) {
	if len(raw) == 0 {
		return nil, true
	}
	var m map[string]bool
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, false
	}
	out := map[string]bool{}
	for _, s := range allAgentSurfaces {
		if v, ok := m[s]; ok {
			out[s] = v
		}
	}
	if len(out) == 0 {
		return []byte("{}"), true
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, false
	}
	return encoded, true
}
