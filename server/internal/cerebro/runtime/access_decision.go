package runtime

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/accessdecision"
	"github.com/multica-ai/multica/server/internal/cerebro/availabilityevidence"
	"github.com/multica-ai/multica/server/internal/cerebro/capabilitycatalog"
	"github.com/multica-ai/multica/server/internal/cerebro/taskmandate"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
)

// canonicalGatewayCapabilityID resolves the concrete callable implementation
// to the stable identity established by the capability catalog. Dynamic
// connection tools use their endpoint/tool identity; ordinary Gateway tools
// must also be present in the declared callable inventory. Anything else stays
// unknown so the service fails closed instead of minting an identity from input.
func canonicalGatewayCapabilityID(toolName string, registry *Registry) string {
	if registry != nil {
		tool, ok := registry.Get(toolName)
		if !ok {
			return ""
		}
		switch concrete := tool.(type) {
		case *APIConnectionTool:
			return capabilitycatalog.APIConnectionEndpoint(
				concrete.ConnectionName(),
				concrete.Method(),
				concrete.Path(),
				concrete.Name(),
			).ID
		case *gatewayMCPTool:
			if concrete.connectionName == "" || concrete.toolName == "" {
				return ""
			}
			return capabilitycatalog.MCPConnectionTool(concrete.connectionName, concrete.toolName).ID
		}
	}

	// Use the same combined inventory that feeds registration. It contains
	// bridge tools, Gateway-native tools, and built-in connection-family tools,
	// so a registered implementation cannot lose its canonical identity through
	// a second, narrower list.
	for _, callable := range callableBuiltinToolNames() {
		if callable == toolName {
			return capabilitycatalog.PlatformTool(toolName).ID
		}
	}
	if registry != nil {
		switch toolName {
		case "memory_recall", "memory_remember", "memory_delete":
			return capabilitycatalog.PlatformTool(toolName).ID
		}
	}
	return ""
}

// decideAccess resolves one Gateway call exclusively through the Policy
// Decision Service. A missing service or identity lookup fails closed.
func (e *FirtalGatewayExecutor) decideAccess(
	ctx context.Context,
	agentID, workspaceID pgtype.UUID,
	toolName string,
	registry *Registry,
	meta GatewayRequestMeta,
	requestContext toolpolicy.RequestContext,
) accessdecision.Entry {
	if e == nil || e.accessDecisions == nil || e.queries == nil {
		return accessdecision.Entry{Decision: accessdecision.DecisionDeny, Reason: "policy decision service unavailable"}
	}
	call, ok := e.accessDecisionCall(ctx, agentID, workspaceID, toolName, registry, meta, requestContext)
	if !ok {
		return accessdecision.Entry{Decision: accessdecision.DecisionDeny, Reason: "agent identity lookup failed"}
	}
	return e.accessDecisions.Decide(ctx, call)
}

func (e *FirtalGatewayExecutor) accessDecisionCall(
	ctx context.Context,
	agentID, workspaceID pgtype.UUID,
	toolName string,
	registry *Registry,
	meta GatewayRequestMeta,
	requestContext toolpolicy.RequestContext,
) (accessdecision.Call, bool) {
	if e == nil || e.queries == nil {
		return accessdecision.Call{}, false
	}
	agent, err := e.queries.GetAgent(ctx, agentID)
	if err != nil {
		if e.logger != nil {
			e.logger.Warn("policy decision service: agent identity lookup failed", "agent_id", meta.AgentID, "tool_name", toolName, "error", err)
		}
		return accessdecision.Call{}, false
	}

	onBehalfOfID := optionalGatewayUUID(meta.TriggerUserID)
	systemID := pgtype.UUID{}
	isSystem := meta.TriggerUserID == ""
	if isSystem {
		systemID = optionalGatewayUUID(meta.AutopilotID)
	}
	capabilityID := canonicalGatewayCapabilityID(toolName, registry)
	evidenceLevel := availabilityevidence.Level("")
	if registry != nil && capabilityID != "" {
		if _, ok := registry.Get(toolName); ok {
			evidenceLevel = availabilityevidence.LevelDiscovered
		}
	}
	return accessdecision.Call{
		WorkspaceID:           workspaceID,
		AgentID:               agentID,
		RuntimeID:             agent.RuntimeID,
		OwnerID:               agent.OwnerID,
		OnBehalfOfID:          onBehalfOfID,
		SystemID:              systemID,
		TaskID:                optionalGatewayUUID(meta.TaskID),
		IssueID:               optionalGatewayUUID(meta.IssueID),
		CanonicalCapabilityID: capabilityID,
		ObservedToolName:      toolName,
		IsSystem:              isSystem,
		RequestContext:        requestContext,
		EvidenceLevel:         evidenceLevel,
	}, true
}

func (e *FirtalGatewayExecutor) recordTaskMandateDenial(
	ctx context.Context,
	agentID, workspaceID pgtype.UUID,
	toolName string,
	registry *Registry,
	meta GatewayRequestMeta,
	verdict taskmandate.Verdict,
) {
	if e == nil || e.accessDecisions == nil {
		return
	}
	call, ok := e.accessDecisionCall(ctx, agentID, workspaceID, toolName, registry, meta, gatewayPolicyRequestContext(toolName, nil))
	if !ok {
		return
	}
	e.accessDecisions.RecordTaskMandateDenial(ctx, call, string(verdict.Code), verdict.Message)
}

func gatewayPolicyRequestContext(toolName string, args map[string]any) toolpolicy.RequestContext {
	requestContext := toolpolicy.RequestContext{Action: toolpolicy.ActionOf(toolName)}
	if rawURL, ok := args["url"].(string); ok {
		requestContext.Host = toolpolicy.HostOf(rawURL)
	}
	return requestContext
}

func optionalGatewayUUID(value string) pgtype.UUID {
	if value == "" {
		return pgtype.UUID{}
	}
	id, err := util.ParseUUID(value)
	if err != nil {
		return pgtype.UUID{}
	}
	return id
}
