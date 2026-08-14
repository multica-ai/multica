package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// mcpServerContainers are the two top-level keys an mcp_config document may
// keep its servers under. `mcpServers` is the Claude-style container every
// runtime consumes; `mcp` is a legacy spelling a few agents still carry and
// which the agent settings UI keeps editing in place (mcp-config-model.ts).
// Both are read when deciding what an agent already declares; only
// `mcpServers` is ever written.
var mcpServerContainers = [...]string{"mcpServers", "mcp"}

// ResolveWorkspaceMcpConfig layers the workspace's shared MCP document UNDER
// an agent's own mcp_config and returns the agent's effective configuration.
// It runs before the per-task overlay (mergeMCPOverlay), so the full
// precedence chain on a claim is:
//
//	workspace  <  agent  <  per-task overlay
//
// The resolution contract (GH #6062, MUL-5421):
//
//   - Agent config absent (SQL NULL / JSON `null`) means "nothing configured
//     at the agent layer", so the workspace document is inherited whole. This
//     is the case that removes the copy-paste: an agent nobody has touched
//     picks up every shared server.
//
//   - An agent config that declares NO servers is an explicit "this agent runs
//     with zero MCP" (that is exactly what `{}` and `{"mcpServers":{}}` mean
//     today under the three-state contract from #3545) and therefore opts the
//     agent out of the workspace layer entirely. Preserving this is what keeps
//     shared credentials from silently propagating into an agent that was
//     deliberately given none.
//
//   - Otherwise the two are merged by SERVER NAME and the AGENT wins on a
//     collision. Union rather than replacement is deliberate: if declaring one
//     private server dropped every shared one, agents would be right back to
//     re-entering the workspace's servers by hand.
//
//   - A workspace document that declares no servers means "nothing shared",
//     not "strict empty". It leaves the agent config untouched, so setting an
//     empty workspace document can never flip inheriting agents into the
//     managed-empty mode that suppresses their runtime's own MCP servers.
//
// Shape: the AGENT document owns the result's top-level keys, and the merged
// result is NORMALIZED onto the canonical `mcpServers` container. Normalizing
// is mandatory, not cosmetic: the daemon's own runtime merge only falls back
// to the legacy top-level `mcp` container when `mcpServers` is ABSENT
// (`internal/daemon/runtime_mcp.go`), so writing shared servers into
// `mcpServers` while leaving an agent's legacy entries beside them would make
// the daemon read the shared set and silently drop that agent's own servers.
// Folding both containers into one is what keeps a legacy OpenCode agent
// running with everything it had. A workspace server is skipped when the agent
// declares that name in EITHER container, so the merge can never produce the
// same server twice.
//
// Failure mode matches mergeMCPOverlay: on malformed input the agent config is
// returned unchanged along with the error. A bad shared document must never
// take away servers an agent runs with today.
func ResolveWorkspaceMcpConfig(workspaceMcpConfig, agentMcpConfig json.RawMessage) (json.RawMessage, error) {
	if !hasManagedJSON(workspaceMcpConfig) {
		return passthroughAgentMcpConfig(agentMcpConfig), nil
	}

	wsDoc, wsServers, err := parseMcpDocument(workspaceMcpConfig)
	if err != nil {
		return passthroughAgentMcpConfig(agentMcpConfig), fmt.Errorf("resolve workspace mcp_config: parse workspace document: %w", err)
	}
	if len(wsServers) == 0 {
		// Nothing shared — leave the agent layer exactly as it was.
		return passthroughAgentMcpConfig(agentMcpConfig), nil
	}

	if !hasManagedJSON(agentMcpConfig) {
		// Inherit whole. Re-marshal so the daemon receives the canonical
		// shape regardless of how Postgres stored the JSONB bytes.
		out, err := json.Marshal(wsDoc)
		if err != nil {
			return nil, fmt.Errorf("resolve workspace mcp_config: marshal workspace document: %w", err)
		}
		return out, nil
	}

	agentDoc, agentServers, err := parseMcpDocument(agentMcpConfig)
	if err != nil {
		return passthroughAgentMcpConfig(agentMcpConfig), fmt.Errorf("resolve workspace mcp_config: parse agent mcp_config: %w", err)
	}
	if len(agentServers) == 0 {
		// Explicit "zero MCP for this agent" — opt out of the workspace layer.
		return passthroughAgentMcpConfig(agentMcpConfig), nil
	}

	// Only the workspace's `mcpServers` entries are shared; a workspace
	// document is validated on write to keep its servers in that container.
	shared, err := unmarshalServerMap(wsDoc["mcpServers"])
	if err != nil {
		return passthroughAgentMcpConfig(agentMcpConfig), fmt.Errorf("resolve workspace mcp_config: workspace mcpServers: %w", err)
	}

	merged := make(map[string]json.RawMessage, len(shared)+len(agentServers))
	for name, server := range shared {
		// The agent may declare this name in the legacy container instead;
		// agentServers spans both, so this check covers each spelling.
		if _, taken := agentServers[name]; taken {
			continue
		}
		merged[name] = server
	}
	// Fold the agent's own entries in, legacy container first so a name
	// present in both resolves to the canonical one — the precedence the
	// agent settings UI already shows (mcp-config-model.ts reads `mcpServers`
	// before `mcp`).
	for _, container := range [...]string{"mcp", "mcpServers"} {
		own, err := unmarshalServerMap(agentDoc[container])
		if err != nil {
			return passthroughAgentMcpConfig(agentMcpConfig), fmt.Errorf("resolve workspace mcp_config: agent %s: %w", container, err)
		}
		for name, server := range own {
			merged[name] = server
		}
	}

	out := make(map[string]json.RawMessage, len(agentDoc)+1)
	for k, v := range agentDoc {
		// Both containers are consumed into the canonical map above; leaving
		// the legacy key behind would hand the daemon a second, stale copy.
		if k == "mcpServers" || k == "mcp" {
			continue
		}
		out[k] = v
	}
	serversBytes, err := json.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace mcp_config: marshal merged servers: %w", err)
	}
	out["mcpServers"] = serversBytes

	final, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace mcp_config: marshal merged document: %w", err)
	}
	return final, nil
}

// parseMcpDocument decodes an mcp_config document into its top-level keys plus
// the set of server names it declares across BOTH containers. The name set is
// what the resolution contract keys "declares no servers" and "agent already
// owns this name" off, so it must span the legacy spelling too.
func parseMcpDocument(raw json.RawMessage) (map[string]json.RawMessage, map[string]struct{}, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil, err
	}
	if doc == nil {
		doc = map[string]json.RawMessage{}
	}
	names := map[string]struct{}{}
	for _, container := range mcpServerContainers {
		servers, err := unmarshalServerMap(doc[container])
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", container, err)
		}
		for name := range servers {
			names[name] = struct{}{}
		}
	}
	return doc, names, nil
}

// validateWorkspaceMcpConfig checks the shape of a shared document before it
// is stored. The agent column is effectively unvalidated server-side (the
// agent settings UI is what enforces shape there), but one bad workspace
// document reaches EVERY agent in the workspace, so it is worth rejecting at
// the edge rather than fail-softing on every claim afterwards.
//
// The check stays deliberately shallow — shape only, never the contents of a
// server entry, which are runtime-specific and carry secrets we do not want to
// inspect or echo back in an error.
func validateWorkspaceMcpConfig(raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return errors.New("mcp_config must be a JSON object, or null to clear")
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &doc); err != nil {
		// Never wrap: the underlying error can echo fragments of a document
		// whose server entries routinely embed API tokens.
		return errors.New("mcp_config must be a JSON object, or null to clear")
	}
	if _, legacy := doc["mcp"]; legacy {
		return errors.New(`mcp_config: shared servers must be declared under "mcpServers"; the legacy "mcp" container is not supported at the workspace level`)
	}
	servers, err := unmarshalServerMap(doc["mcpServers"])
	if err != nil {
		return fmt.Errorf("mcp_config: %w", err)
	}
	if len(servers) == 0 && len(doc) > 0 {
		// An object with unrelated top-level keys and no servers would be
		// stored and then ignored by the resolver — reject rather than
		// silently no-op.
		if _, ok := doc["mcpServers"]; !ok {
			return errors.New(`mcp_config: expected an "mcpServers" object; pass null to clear the workspace configuration`)
		}
	}
	return nil
}
