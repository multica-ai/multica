package composio

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	sdk "github.com/multica-ai/multica/server/pkg/composio"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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
// agent_task_queue.runtime_mcp_overlay for a task whose top-of-chain human
// originator is the given Multica user and whose dispatching agent is
// `agent`, or (nil, nil) when ANY of the gates below trip — meaning no
// Composio session is created and no token is provisioned.
//
// Five short-circuit gates, in order:
//
//  1. originator is not a valid UUID — autopilot / system run with no human
//     in the chain. There is no "current user's connected apps" view, and
//     gate 2 would also fail, so skip the work.
//
//  2. originator != agent.OwnerID — Stage 3.1 of MUL-3721 / MUL-3869. An
//     agent's Composio overlay is the agent OWNER's connected-apps view
//     projected into the run. When the run was triggered by anyone else
//     (another workspace member, an agent fan-out whose originator chain
//     terminates at someone else), we MUST NOT project the owner's
//     integration footprint into that run: it would let any workspace
//     member who can @-mention the agent read into the owner's accounts.
//     Returns (nil, nil) deterministically — not an error — so the agent
//     still runs with whatever it had in agent.mcp_config.
//
//  3. agent.composio_toolkit_allowlist is empty/NULL — the agent owner
//     never explicitly opted into ANY toolkit. Default behavior is OFF: no
//     overlay. This is the "all-on → opt-in" inversion the Stage 3.1
//     redesign brought in.
//
//  4. After intersecting the allowlist with the originator's currently-
//     active Composio connections, NO toolkit has an active connection.
//     Either the owner allowlisted toolkits they haven't connected, or the
//     connection was revoked. Either way we have nothing to mount.
//
//  5. Composio returns a session with no URL — defensive: a half-baked
//     overlay would make every runtime fail noisily on an empty MCP URL.
//
// On the happy path the overlay is:
//
//	{"mcpServers": {"composio": {"type": "http", "url": "...", "headers": {...}}}}
//
// CreateSession is called with BOTH the `toolkits.slugs` allowlist filter
// and `connected_accounts` pinning so the session is narrowed twice over:
// the tool-router only sees the intersection of what the agent owner
// allowlisted AND what the originator has live credentials for. This is
// belt-and-suspenders — neither field alone would be enough to prevent the
// session from drifting toward toolkits the owner did not authorize.
func (s *Service) BuildTaskOverlay(ctx context.Context, originatorUserID pgtype.UUID, agent db.Agent) (json.RawMessage, error) {
	// Gate 1: no human in the chain.
	if !originatorUserID.Valid {
		return nil, nil
	}
	// Gate 2: originator must be the agent owner. Without this gate, any
	// workspace member who can @-mention the agent (public agent) gets the
	// owner's connected apps projected into their run, which is the
	// privacy hole the Stage 3.1 redesign closes.
	if !agent.OwnerID.Valid || agent.OwnerID.Bytes != originatorUserID.Bytes {
		return nil, nil
	}
	// Gate 3: agent owner has not allowlisted any toolkit. NULL and empty
	// `{}` are treated identically — both mean "no overlay".
	allowSet := normaliseAllowlistToSet(agent.ComposioToolkitAllowlist)
	if len(allowSet) == 0 {
		return nil, nil
	}

	// Resolve the originator's active connections and intersect with the
	// allowlist. The intersection is the canonical input both for filtering
	// the Composio CreateSession call AND for the early bail-out below.
	rows, err := s.store.ListActiveUserComposioConnections(ctx, originatorUserID)
	if err != nil {
		return nil, fmt.Errorf("composio: build task overlay: list connections: %w", err)
	}
	pinned := pinConnectedAccounts(rows, allowSet)
	if len(pinned) == 0 {
		// Gate 4: owner allowlisted toolkits the originator (who is the
		// owner per gate 2) has not connected — or has revoked since.
		return nil, nil
	}
	// `toolkits.slugs` narrows what the tool-router exposes; pair it with
	// the connected-account pin so the session can never surface an
	// account outside the (allowlist ∩ active connections) set.
	slugs := make([]string, 0, len(pinned))
	for slug := range pinned {
		slugs = append(slugs, slug)
	}

	resp, err := s.sdk.CreateSession(ctx, sdk.CreateSessionRequest{
		UserID:            util.UUIDToString(originatorUserID),
		Toolkits:          map[string]any{"slugs": slugs},
		ConnectedAccounts: pinned,
	})
	if err != nil {
		return nil, fmt.Errorf("composio: build task overlay: create session: %w", err)
	}
	// Gate 5: Composio answered 200 with no MCP URL. Treat as "no overlay"
	// rather than wire up a server with an empty URL — every runtime fails
	// noisily on that.
	if resp == nil || resp.MCP.URL == "" {
		return nil, nil
	}

	payload := mcpOverlayPayload{
		MCPServers: map[string]composioMCPServer{
			mcpOverlayServerName: {
				Type:    "http",
				URL:     resp.MCP.URL,
				Headers: s.sdk.MCPAuthHeaders(),
			},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("composio: marshal task overlay: %w", err)
	}
	return raw, nil
}

// normaliseAllowlistToSet maps the agent.composio_toolkit_allowlist
// TEXT[] column into a slug→{} set. Each entry is lowercased + trimmed
// defensively (the API layer already normalises on write, but DB-level
// migrations / out-of-band writes might bypass that, and the cost of a
// re-normalise is one map walk). An empty result triggers gate 3 in
// BuildTaskOverlay, identically for NULL columns and `{}` arrays.
func normaliseAllowlistToSet(allow []string) map[string]struct{} {
	if len(allow) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(allow))
	for _, s := range allow {
		slug := lowerTrim(s)
		if slug == "" {
			continue
		}
		out[slug] = struct{}{}
	}
	return out
}

// pinConnectedAccounts intersects the originator's active connection rows
// with the allowlist set and returns the `connected_accounts` map shape
// the Composio /tool_router/session endpoint expects: one entry per
// (allowlisted) toolkit slug, value = the originator's connected account
// id for that toolkit.
//
// Newest-wins on duplicates: rows arrive ordered by connected_at DESC
// (see ListActiveUserComposioConnections), so the first row seen for a
// given slug is the most recently connected account, matching the
// single-account-per-toolkit invariant CreateMCPSession already documents.
func pinConnectedAccounts(rows []db.UserComposioConnection, allowSet map[string]struct{}) map[string]any {
	pinned := make(map[string]any, len(rows))
	for _, row := range rows {
		slug := lowerTrim(row.ToolkitSlug)
		if slug == "" {
			continue
		}
		if _, allowed := allowSet[slug]; !allowed {
			continue
		}
		if _, dup := pinned[slug]; dup {
			continue
		}
		pinned[slug] = row.ConnectedAccountID
	}
	return pinned
}

// lowerTrim is the tiny inlined helper that keeps allowlist and connection
// slug comparison consistent without dragging the unicode lib for what is
// always an ASCII slug.
func lowerTrim(s string) string {
	// strings.ToLower + TrimSpace would do, but we avoid importing strings
	// just for two ASCII transforms in a hot path. Manual loop is
	// allocation-free for the common all-ASCII case.
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	if start == end {
		return ""
	}
	// Detect upper-case before allocating.
	upper := false
	for i := start; i < end; i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			upper = true
			break
		}
	}
	if !upper {
		return s[start:end]
	}
	b := make([]byte, end-start)
	for i := start; i < end; i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i-start] = c
	}
	return string(b)
}
