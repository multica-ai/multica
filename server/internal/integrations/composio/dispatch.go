package composio

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
)

// mcpOverlayServerName is the deterministic key under `mcpServers` used to
// place the Composio session into the merged MCP config. Daemon-side merge
// is by server name, so this constant is the integration's namespace: a
// future provider adding its own overlay must pick a distinct name (e.g.
// "pipedream") to avoid collisions, and an agent's own `mcp_config` entry
// named "composio" is overridden by this overlay on purpose — the overlay
// carries the live user-scoped session URL, the agent config carries a
// generic service-wide entry at most.
const mcpOverlayServerName = "composio"

// composioMCPServer is the wire shape of one MCP server entry in the
// Claude-style `{"mcpServers": {...}}` config that every supported runtime
// (Cursor, Codex, Claude, OpenCode, OpenClaw, Hermes/Kiro) consumes.
//
// `type: http` is what marks the entry as a streamable HTTP MCP endpoint —
// the form Composio's session helper returns. Headers carry the per-session
// bearer token (`Authorization: Bearer mcp_…`). Bearer secret material in
// the value, so callers must NEVER log this struct without redacting
// Headers; the daemon's redact pipeline already pattern-matches the
// `Bearer mcp_…` shape, but the safe rule remains "log the URL only".
type composioMCPServer struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

// mcpOverlayPayload is the per-task overlay JSON written to
// agent_task_queue.runtime_mcp_overlay and read by the daemon claim handler
// at task dispatch.
//
// Shape is deliberately a subset of agent.mcp_config (Claude-style
// `mcpServers` map) so the daemon's merge is a flat dictionary union keyed
// by server name. Anything more elaborate (capability filtering, env
// injection, …) would force every sidecar generator to learn about overlays
// individually; keeping the shape identical lets the merge stay pure
// substitution.
type mcpOverlayPayload struct {
	MCPServers map[string]composioMCPServer `json:"mcpServers"`
}

// BuildTaskOverlay returns the JSON overlay to write into
// agent_task_queue.runtime_mcp_overlay for a task whose initiator is the
// given Multica user, or (nil, nil) when the user has no active Composio
// connections — in which case no Composio session is created and no token
// is provisioned.
//
// The overlay shape is exactly the one the daemon-side merge expects:
//
//	{"mcpServers": {"composio": {"type": "http", "url": "...", "headers": {...}}}}
//
// The (nil, nil) early return is load-bearing for cost / privacy reasons:
//
//   - cost: each Composio MCP session is a separate session id at Composio,
//     so we do not provision one for users who would have nothing to call
//     anyway. This makes the integration scale with the active-connect
//     population, not the total task population.
//
//   - privacy: a user without any connection has not consented to any
//     third-party reach — emitting an overlay would still attach a bearer
//     scoped to their composio user namespace, which is wasted attack
//     surface.
//
// Idempotency: this is called per-enqueue, not per-claim, so a single task
// has at most one overlay generated and stored. A task that gets re-enqueued
// (only via the retry path, which inserts a fresh row, not the same row)
// will compute a new overlay for the new row — the parent row's overlay is
// already cleared by the terminal-state trigger by the time retry runs.
//
// Errors are returned so the caller can decide whether to fail the enqueue
// (probably no — best-effort enqueue keeps the agent runnable without
// Composio tools) or just log and continue. CreateMCPSession failures are
// expected to be transient (Composio API outage / network blip).
func (s *Service) BuildTaskOverlay(ctx context.Context, userID pgtype.UUID) (json.RawMessage, error) {
	session, err := s.CreateMCPSession(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("composio: build task overlay: %w", err)
	}
	if session == nil {
		// No active connections — see method comment for why this is the
		// load-bearing zero-cost path.
		return nil, nil
	}
	if session.URL == "" {
		// Defensive: Composio returned a session row but no URL. Treat as
		// "no overlay" rather than writing a half-baked entry; the daemon
		// would otherwise wire up an MCP server with an empty URL which
		// every runtime fails on noisily.
		return nil, nil
	}

	payload := mcpOverlayPayload{
		MCPServers: map[string]composioMCPServer{
			mcpOverlayServerName: {
				Type:    "http",
				URL:     session.URL,
				Headers: session.Headers,
			},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("composio: marshal task overlay: %w", err)
	}
	return raw, nil
}
