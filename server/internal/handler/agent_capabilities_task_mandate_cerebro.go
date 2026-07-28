package handler

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/connmeta"
)

// applyCerebroTaskMandateEndpointLimits projects the immutable task ceiling
// onto API endpoints using the same callable identity as gateway enforcement.
func applyCerebroTaskMandateEndpointLimits(
	ctx context.Context,
	mandates AgentCapabilityTaskMandate,
	taskID, workspaceID, agentID pgtype.UUID,
	card *AgentCapabilities,
) {
	for i := range card.Connections {
		for j := range card.Connections[i].Endpoints {
			endpoint := &card.Connections[i].Endpoints[j]
			for _, method := range endpoint.Methods {
				toolName := connmeta.APIEndpointToolName(card.Connections[i].Name, method, endpoint.Path)
				if err := mandates.Authorize(ctx, taskID, workspaceID, agentID, toolName); err != nil {
					endpoint.Permission, endpoint.Allowed, endpoint.Callable = "deny", false, false
					endpoint.BlockedReason = fmt.Sprintf("task mandate denied the capability: %v", err)
					endpoint.HowToFix = "Start a new task whose issued mandate includes this capability."
					break
				}
			}
		}
	}
}
