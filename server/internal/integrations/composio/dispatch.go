package composio

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/runtimeapps"
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
// the form Composio's session helper returns. Headers carry the Composio API
// key, so callers must NEVER log this struct without redacting Headers.
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

// BuildTaskOverlay returns the overlay payload to write for a task dispatching
// `agent`, or a zero result when ANY of the gates below trip — meaning no
// Composio session is created and no token is provisioned.
//
// MUL-3963: Composio MCP now FOLLOWS the agent invocation permission instead
// of requiring originator == owner. The security boundary is upstream —
// canInvokeAgent decides who may enqueue a run for this agent at all — so any
// task that reaches dispatch has already been authorised to run the agent, and
// therefore to use the Composio apps the OWNER attached to it. The overlay is
// always built from the AGENT OWNER's connected-apps view: the owner shares
// those apps as part of the agent's capability with everyone allowed to invoke
// it (private -> only the owner; public_to -> the allow-list). The front-end
// warns the owner about this when a shared agent has Composio apps enabled.
//
// originatorUserID is retained only for audit logging (who triggered) — it no
// longer gates the overlay.
//
// Gates, in order:
//
//  1. agent has no owner — cannot resolve a connected-apps view. No overlay.
//
//  2. agent.composio_toolkit_allowlist is empty/NULL — the agent owner never
//     opted into any toolkit. Default OFF: no overlay.
//
//  3. After intersecting the allowlist with the OWNER's currently-active
//     Composio connections, no toolkit has an active connection. Nothing to
//     mount.
//
//  4. Composio returns a session with no URL — defensive.
//
// CreateSession is called with the owner as the session user, and with BOTH
// the `toolkits.enable` allowlist filter and `connected_accounts` pinning so
// the session is narrowed to (owner allowlist ∩ owner active connections).
func (s *Service) BuildTaskOverlay(ctx context.Context, originatorUserID pgtype.UUID, agent db.Agent) (runtimeapps.MCPOverlayResult, error) {
	// Gate 1: the overlay is the agent OWNER's connected-apps view. Without an
	// owner there is nothing to project.
	if !agent.OwnerID.Valid {
		return runtimeapps.MCPOverlayResult{}, nil
	}
	ownerUserID := agent.OwnerID
	// Gate 2: agent owner has not allowlisted any toolkit. NULL and empty
	// `{}` are treated identically — both mean "no overlay".
	allowSet := normaliseAllowlistToSet(agent.ComposioToolkitAllowlist)
	if len(allowSet) == 0 {
		return runtimeapps.MCPOverlayResult{}, nil
	}

	// Resolve the OWNER's active connections and intersect with the allowlist.
	// The intersection is the canonical input both for filtering the Composio
	// CreateSession call AND for the early bail-out below.
	rows, err := s.store.ListActiveUserComposioConnections(ctx, ownerUserID)
	if err != nil {
		return runtimeapps.MCPOverlayResult{}, fmt.Errorf("composio: build task overlay: list connections: %w", err)
	}
	pinned := pinConnectedAccounts(rows, allowSet)
	if len(pinned) == 0 {
		// Gate 3: owner allowlisted toolkits they have not connected — or have
		// revoked since.
		return runtimeapps.MCPOverlayResult{}, nil
	}
	// `toolkits.enable` narrows what the tool-router exposes; pair it with
	// the connected-account pin so the session can never surface an
	// account outside the (allowlist ∩ active connections) set.
	slugs := make([]string, 0, len(pinned))
	for slug := range pinned {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)

	resp, err := s.sdk.CreateSession(ctx, sdk.CreateSessionRequest{
		UserID:            util.UUIDToString(ownerUserID),
		Toolkits:          map[string]any{"enable": slugs},
		ConnectedAccounts: pinned,
	})
	if err != nil {
		return runtimeapps.MCPOverlayResult{}, fmt.Errorf("composio: build task overlay: create session: %w", err)
	}
	// Gate 4: Composio answered 200 with no MCP URL. Treat as "no overlay"
	// rather than wire up a server with an empty URL — every runtime fails
	// noisily on that.
	if resp == nil || resp.MCP.URL == "" {
		return runtimeapps.MCPOverlayResult{}, nil
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
		return runtimeapps.MCPOverlayResult{}, fmt.Errorf("composio: marshal task overlay: %w", err)
	}
	apps := make([]runtimeapps.ConnectedApp, 0, len(slugs))
	for _, slug := range slugs {
		apps = append(apps, runtimeapps.ConnectedApp{
			Provider:    "composio",
			ServerName:  mcpOverlayServerName,
			ToolkitSlug: slug,
			ToolkitName: runtimeapps.DisplayNameForToolkitSlug(slug),
		})
	}
	return runtimeapps.MCPOverlayResult{MCPOverlay: raw, ConnectedApps: apps}, nil
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
// with the allowlist set and returns the `connected_accounts` map shape the
// Composio /tool_router/session endpoint expects: one entry per allowlisted
// toolkit slug, value = array of the originator's connected account ids for
// that toolkit. The product currently permits one active account per toolkit,
// so each array has one element.
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
		pinned[slug] = []string{row.ConnectedAccountID}
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
