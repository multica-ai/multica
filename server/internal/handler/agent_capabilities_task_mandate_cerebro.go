package handler

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/connmeta"
	"github.com/multica-ai/multica/server/internal/cerebro/taskmandate"
)

// applyCerebroTaskMandateEndpointLimits projects the immutable task ceiling
// onto API endpoints using the same callable identity as gateway enforcement.
func applyCerebroTaskMandateEndpointLimits(
	ctx context.Context,
	mandates AgentCapabilityTaskMandate,
	taskID, workspaceID, agentID pgtype.UUID,
	card *AgentCapabilities,
	claimGeneration []int64,
) {
	for i := range card.Connections {
		for j := range card.Connections[i].Endpoints {
			endpoint := &card.Connections[i].Endpoints[j]
			for _, method := range endpoint.Methods {
				toolName := connmeta.APIEndpointToolName(card.Connections[i].Name, method, endpoint.Path)
				if err := authorizeCapabilityTaskMandate(ctx, mandates, taskID, workspaceID, agentID, toolName, claimGeneration); err != nil {
					verdict := taskmandate.VerdictForError(err)
					endpoint.Permission, endpoint.Allowed, endpoint.Callable = "deny", false, false
					endpoint.BlockedReason = verdict.Message
					endpoint.HowToFix = taskMandateRecoveryCopy(verdict.RecoveryAction)
					endpoint.Verdict = &verdict
					break
				}
			}
		}
	}
}
