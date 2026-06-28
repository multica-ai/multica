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
//   - LIST TIME (filterDeniedAPIEndpoints): a Deny endpoint is dropped from the
//     agent's tool list, so a model never even sees a tool it isn't allowed to
//     call. Ask/Allow endpoints stay listed — Ask only pauses at call time, like
//     every other Ask.
//   - CALL TIME (apiEndpointSetting, folded into guardToolCall): the verdict is
//     re-resolved when the tool is actually called, so a Deny blocks and an Ask
//     routes through the approval inbox even if the list-time filter was bypassed
//     (defence in depth — the list and the gate must agree).
//
// All of this is still behind the default-off cerebro_api_connection_tools flag:
// with the flag off PR2 exposes no API tools, so there is nothing for this to
// resolve.

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
// Fail-open to Allow on a lookup/resolve error (logged at warn), exactly like
// connectionToolSetting: an always-on per-call check must not take the gateway
// fleet offline on a transient DB blip. A genuine Deny/Ask holds in every
// non-error case, which is the requirement.
func (e *FirtalGatewayExecutor) apiEndpointSetting(
	ctx context.Context,
	agentID, workspaceID pgtype.UUID,
	reg *Registry,
	toolName string,
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
	agent, err := e.queries.GetAgent(ctx, agentID)
	if err != nil {
		e.logger.Warn("api endpoint policy: agent lookup failed — allowing",
			"agent_id", meta.AgentID, "tool", toolName, "error", err)
		return toolpolicy.SettingAllow, ""
	}
	eff, connName, err := e.connDeny.ConnectionEndpointEffective(
		ctx, workspaceID, agent.RuntimeID, agentID, agent.OwnerID,
		api.ConnectionName(), api.Method(), api.Path())
	if err != nil {
		e.logger.Warn("api endpoint policy: resolve failed — allowing",
			"agent_id", meta.AgentID, "tool", toolName, "error", err)
		return toolpolicy.SettingAllow, ""
	}
	return eff, connName
}

// filterDeniedAPIEndpoints drops every API-connection tool whose effective
// endpoint verdict is Deny for this agent, so a denied endpoint is never listed.
// Ask and Allow endpoints are kept (Ask pauses at call time). Non-API tools pass
// through untouched. A resolve error keeps the tool listed (fail-open at list
// time) — the always-on call-time guard re-checks and is the real enforcement
// point, so a transient list-time error cannot leak access past the gate.
func (e *FirtalGatewayExecutor) filterDeniedAPIEndpoints(
	ctx context.Context,
	agentID, workspaceID pgtype.UUID,
	reg *Registry,
	tools []Tool,
	meta GatewayRequestMeta,
) []Tool {
	if len(tools) == 0 {
		return tools
	}
	out := tools[:0:0]
	for _, t := range tools {
		api, ok := t.(*APIConnectionTool)
		if !ok {
			out = append(out, t)
			continue
		}
		setting, _ := e.apiEndpointSetting(ctx, agentID, workspaceID, reg, api.Name(), meta)
		if setting == toolpolicy.SettingDeny {
			e.logger.Info("api endpoint hidden from tool list by Deny (FIR-2166 C PR3)",
				"agent_id", meta.AgentID, "tool", api.Name(), "connection", api.ConnectionName())
			continue
		}
		out = append(out, t)
	}
	return out
}
