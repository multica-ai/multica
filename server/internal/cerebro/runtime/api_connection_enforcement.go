package runtime

// api_connection_enforcement.go is FIR-2166 "C" PR3: it makes the API-connection
// tools that PR2 exposes (api_connection_tools.go) actually enforced PER ACTOR,
// through the same Workspace › Runtime › Agent › Group › User chain MCP
// connection tools already use — via toolpolicy.Store.ConnectionEndpointEffective
// (the resolver PR1 added).
//
// Two halves, mirroring the MCP-connection enforcement (TECH-3174 Deny /
// TECH-3498 Ask):
//
//   - LIST TIME: a Deny endpoint is dropped from the agent's tool list, so a
//     model never even sees a tool it isn't allowed to call. Ask/Allow endpoints
//     stay listed — Ask only pauses at call time, like every other Ask. As of
//     FIR-2388 the list decision lives in the SHARED APIConnectionResolver
//     (api_connection_resolver.go), which every surface calls, so this file no
//     longer carries its own list-time filter — only the CALL-time guard below.
//   - CALL TIME (apiEndpointSetting, folded into guardToolCall): the verdict is
//     re-resolved when the tool is actually called, so a Deny blocks and an Ask
//     routes through the approval inbox even if the list-time filter was bypassed
//     (defence in depth — the list and the gate must agree).
//
// All of this is still behind the default-off cerebro_api_connection_tools flag:
// with the flag off PR2 exposes no API tools, so there is nothing for this to
// resolve.
//
// FIR-2166 "C" v2: ConnectionEndpointEffective is now OPT-IN (default Deny) for
// API connections — an endpoint is granted only by an explicit Allow at some
// actor layer, otherwise it resolves to Deny. So the LIST-TIME filter drops every
// endpoint an agent was not explicitly granted (not just authored Denies), and an
// un-granted agent sees nothing. Ask is not a grant on this gate (anything short
// of an explicit Allow is Deny), so the call-time Ask path is unreachable for API
// connections; it remains for MCP connection tools, which keep Allow-baseline.

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

// apiEndpointSetting resolves the per-actor Allow/Ask/Deny verdict for an
// API-connection endpoint tool, or (Allow, "") when the named tool is not an
// API-connection tool (so a normal tool is never affected). reg is the per-task
// registry; the tool is looked up there and type-asserted to *APIConnectionTool
// to recover its connection/method/path without re-parsing the synthetic name.
//
// failClosed selects the verdict for an API-connection tool whose policy could
// not be resolved (agent lookup or ConnectionEndpointEffective error, logged at
// warn):
//   - false at LIST time: return Allow, keeping the tool listed. The always-on
//     call-time guard re-resolves and is the authoritative check, so a transient
//     list-time error cannot leak access past the gate.
//   - true at CALL time: return Deny, failing CLOSED. This gate fronts the
//     secrets box (FIR-2166), so an unresolved verdict must never grant a call.
//     The blast radius is only API-connection tools (behind the default-off
//     cerebro_api_connection_tools flag) — a DB blip cannot affect normal/MCP
//     tools, so this does not take the gateway fleet offline.
//
// The "not an API-connection tool" cases above always return Allow regardless of
// failClosed, so a normal tool is never denied by a secrets-box error path.
func (e *FirtalGatewayExecutor) apiEndpointSetting(
	ctx context.Context,
	agentID, workspaceID pgtype.UUID,
	reg *Registry,
	toolName string,
	failClosed bool,
	meta GatewayRequestMeta,
) (toolpolicy.Setting, string) {
	if e == nil || e.connDeny == nil || reg == nil || !agentID.Valid || toolName == "" {
		return toolpolicy.SettingAllow, ""
	}
	t, ok := reg.Get(toolName)
	if !ok {
		return toolpolicy.SettingAllow, ""
	}
	api, ok := t.(*APIConnectionTool)
	if !ok {
		return toolpolicy.SettingAllow, ""
	}
	// Past this point the tool is a confirmed API-connection tool, so failing
	// closed here only ever denies a secrets-box endpoint, never a normal tool.
	onErr := toolpolicy.SettingAllow
	if failClosed {
		onErr = toolpolicy.SettingDeny
	}
	agent, err := e.queries.GetAgent(ctx, agentID)
	if err != nil {
		e.logger.Warn("api endpoint policy: agent lookup failed",
			"agent_id", meta.AgentID, "tool", toolName, "fail_closed", failClosed, "error", err)
		return onErr, ""
	}
	eff, connName, err := e.connDeny.ConnectionEndpointEffective(
		ctx, workspaceID, agent.RuntimeID, agentID, agent.OwnerID,
		api.ConnectionName(), api.Method(), api.Path())
	if err != nil {
		e.logger.Warn("api endpoint policy: resolve failed",
			"agent_id", meta.AgentID, "tool", toolName, "fail_closed", failClosed, "error", err)
		return onErr, connName
	}
	return eff, connName
}
