package handler

import (
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// canReuseTaskExecution keeps local files and provider sessions within the
// runtime and execution-user boundary. Legacy rows have no provable execution
// user, so they can only resume other legacy rows on the same runtime.
func canReuseTaskExecution(agentID, runtimeID pgtype.UUID, routing []byte, priorRuntimeID pgtype.UUID, priorRouting []byte) bool {
	if !runtimeID.Valid || runtimeID != priorRuntimeID {
		return false
	}
	if len(routing) == 0 || len(priorRouting) == 0 {
		return len(routing) == 0 && len(priorRouting) == 0
	}
	current, err := service.ParseRuntimeRouting(routing)
	if err != nil || current == nil {
		return false
	}
	prior, err := service.ParseRuntimeRouting(priorRouting)
	if err != nil || prior == nil || current.ExecutionUserID != prior.ExecutionUserID {
		return false
	}
	route, err := current.Route(uuidToString(agentID))
	if err != nil || route.RuntimeID != uuidToString(runtimeID) {
		return false
	}
	previous, err := prior.Route(uuidToString(agentID))
	if err != nil {
		return false
	}
	modelsMatch := route.Model == nil && previous.Model == nil
	if route.Model != nil && previous.Model != nil {
		modelsMatch = *route.Model == *previous.Model
	}
	if modelsMatch && previous.RuntimeID == route.RuntimeID && previous.RuntimeOwnerID == route.RuntimeOwnerID && previous.Provider == route.Provider {
		return true
	}

	return false
}

// isolateAgentConfiguration also gates workspace MCP bindings: those entries
// may hold the same owner-supplied credentials as the agent's own config.
func isolateAgentConfiguration(agent db.Agent, runtime db.AgentRuntime, routing []byte, defaultProvider string) bool {
	if len(routing) == 0 {
		return false
	}
	personal := false
	if snapshot, err := service.ParseRuntimeRouting(routing); err == nil && snapshot != nil {
		if route, err := snapshot.Route(uuidToString(agent.ID)); err == nil {
			personal = route.Source == "personal"
		}
	}
	return personal || !agent.OwnerID.Valid || agent.OwnerID != runtime.OwnerID || runtime.Provider != defaultProvider
}

// claimAgentConfiguration never transports another owner's secret-bearing
// agent configuration onto a routed runtime. Per-task connected-app overlays
// remain separate and are resolved for the execution user at enqueue time.
func claimAgentConfiguration(agent db.Agent, runtime db.AgentRuntime, routing []byte, defaultProvider string) db.Agent {
	personal := false
	var model *string
	if snapshot, err := service.ParseRuntimeRouting(routing); err == nil && snapshot != nil {
		if route, err := snapshot.Route(uuidToString(agent.ID)); err == nil {
			personal = route.Source == "personal"
			model = route.Model
		}
	}
	crossProvider := len(routing) > 0 && runtime.Provider != defaultProvider
	if isolateAgentConfiguration(agent, runtime, routing, defaultProvider) {
		agent.CustomEnv = nil
		agent.CustomArgs = nil
		agent.McpConfig = nil
		agent.RuntimeConfig = nil
	}
	if crossProvider || personal {
		agent.Model = pgtype.Text{}
		agent.ThinkingLevel = pgtype.Text{}
		agent.ServiceTier = pgtype.Text{}
	}
	if model != nil {
		agent.Model = pgtype.Text{String: *model, Valid: *model != ""}
	}
	return agent
}
