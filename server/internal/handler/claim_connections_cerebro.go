package handler

// CEREBRO-PATCH(claim-connections-cerebro): FIR-2341 / FIR-2441 — claim-time
// mcp_http connection injection, extracted from daemon.go so the unified-resolver
// Flip and the legacy fallback live in one cerebro-owned place instead of an
// ever-growing inline block in an upstream file.
//
// Two paths, selected per claim:
//
//   - Flip (authoritative): when the default-off cerebro_api_connection_tools
//     flag is on for the workspace, the unified runtime.ConnectionToolResolver
//     decides BOTH the injected mcp_http --mcp-config AND the --disallowedTools
//     deny tokens from ONE Resolve() — the same resolver the claim brief, cloud
//     executor and local connectiontools handler already read. It applies the
//     Allow-only rule (a connection with any Ask/Deny tool is withheld from the
//     raw relay, Lone #1) and derives deny from the same verdict table, so
//     "listed == callable" holds at claim too.
//
//   - Legacy (reversible fallback): when the flag is OFF (the prod default),
//     handled==false and this reproduces the original FIR-2341 / TECH-3156
//     behavior verbatim — BuildMCPConfig injects every enabled mcp_http
//     connection and DisallowedMCPTools supplies the Deny tokens. Because the
//     mcp_http claim injection is a LIVE feature (NOT behind the flag), keeping
//     the legacy branch is what makes the Flip reversible: flag off ⇒ identical
//     to today. Deleting the legacy branch (grep BuildMCPConfig ⇒ nul) tightens
//     default behavior and is a separate, reviewed step once the flag is
//     permanently on in prod.

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/daemonmcp"
)

// CerebroConnectionClaimIdentity is the actor a claim-time connection resolve is
// gated for. OwnerID feeds the tool-policy user layer (the agent owner), matching
// the legacy DisallowedMCPTools owner resolution exactly.
type CerebroConnectionClaimIdentity struct {
	WorkspaceID pgtype.UUID
	RuntimeID   pgtype.UUID
	AgentID     pgtype.UUID
	OwnerID     pgtype.UUID
	// InitiatorID is the human this run is performed for (the task initiator /
	// on_behalf_of member), distinct from OwnerID (the agent owner). It gates the
	// tighten-only on_behalf_of policy layer (FIR-2441 — member as a full actor
	// level): a member denied a connection tool has it withheld across every agent
	// they drive. Zero when there is no member initiator (agent/autopilot-triggered).
	InitiatorID pgtype.UUID
}

// CerebroConnectionClaimResolver is the seam the unified runtime resolver
// satisfies. handled reports whether the resolver was authoritative for this
// claim (flag on): handled==false means the caller must fall back to the legacy
// BuildMCPConfig / DisallowedMCPTools path, so the wiring swap is reversible.
//
// CEREBRO-PATCH(handler-connection-claim-resolver-iface): FIR-2441 seam.
type CerebroConnectionClaimResolver interface {
	ClaimConnectionMCP(ctx context.Context, ident CerebroConnectionClaimIdentity) (mcpConfig json.RawMessage, deny []string, handled bool)
}

// injectClaimConnectionMCP folds the workspace's mcp_http connections into the
// claim's mcpConfig and returns the --disallowedTools deny tokens. It picks the
// unified resolver when it is authoritative (flag on) and otherwise runs the
// legacy path unchanged. mcpConfig is returned merged; the caller assigns the
// deny slice onto the task response.
func (h *Handler) injectClaimConnectionMCP(
	ctx context.Context,
	workspaceID, runtimeID, agentID, ownerID, initiatorID pgtype.UUID,
	mcpConfig json.RawMessage,
) (json.RawMessage, []string) {
	// --- Flip: unified resolver decides when the flag is on ---
	if h.ConnectionClaimResolver != nil {
		if connMCP, deny, handled := h.ConnectionClaimResolver.ClaimConnectionMCP(ctx, CerebroConnectionClaimIdentity{
			WorkspaceID: workspaceID,
			RuntimeID:   runtimeID,
			AgentID:     agentID,
			OwnerID:     ownerID,
			InitiatorID: initiatorID,
		}); handled {
			if len(connMCP) > 0 {
				mcpConfig = daemonmcp.Merge(connMCP, mcpConfig)
			}
			return mcpConfig, deny
		}
	}

	// --- Legacy path (flag off): identical to the original daemon.go block ---
	if h.ConnectionsInjector == nil {
		return mcpConfig, nil
	}
	// FIR-2441 fix-list #2 follow-up: when the injector is the mcp relay it can mint
	// actor-scoped relay tokens, so the relay honors an agent- or member-level Deny
	// server-side even for a CLI that ignores the --disallowedTools list below.
	// Optional-interface assertion keeps the handler seam unchanged: a non-relay
	// injector (relay unconfigured) just uses the workspace-scoped BuildMCPConfig.
	var connMCP json.RawMessage
	if actorInj, ok := h.ConnectionsInjector.(interface {
		BuildMCPConfigForActor(ctx context.Context, workspaceID, runtimeID, agentID, ownerID, initiatorID pgtype.UUID) json.RawMessage
	}); ok {
		connMCP = actorInj.BuildMCPConfigForActor(ctx, workspaceID, runtimeID, agentID, ownerID, initiatorID)
	} else {
		connMCP = h.ConnectionsInjector.BuildMCPConfig(ctx, workspaceID)
	}
	if len(connMCP) == 0 {
		return mcpConfig, nil
	}
	var deny []string
	if h.ConnectionToolDeny != nil {
		tokens, err := h.ConnectionToolDeny.DisallowedMCPTools(ctx, workspaceID, runtimeID, agentID)
		if err != nil {
			// Fail closed: withhold the connections this claim rather than
			// injecting them without their deny list.
			slog.Warn("withholding workspace MCP connections after deny resolution failed",
				"workspace_id", uuidToString(workspaceID),
				"runtime_id", uuidToString(runtimeID),
				"agent_id", uuidToString(agentID),
				"error", err,
			)
			return mcpConfig, nil
		}
		deny = tokens
	}
	mcpConfig = daemonmcp.Merge(connMCP, mcpConfig)
	return mcpConfig, deny
}
