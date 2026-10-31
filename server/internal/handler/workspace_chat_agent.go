package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const defaultChatAgentKey = "default_chat_agent_id"

// Broadcasts and daemon responses have no single human recipient. Never put
// the private preference in those payloads; HTTP projects it for each caller.
// Leave historical non-object settings in their original shape.
func withoutDefaultChatAgent(raw []byte) []byte {
	var settings map[string]json.RawMessage
	if json.Unmarshal(raw, &settings) != nil || settings == nil {
		return raw
	}
	delete(settings, defaultChatAgentKey)
	out, _ := json.Marshal(settings)
	return out
}

func (h *Handler) usableDefaultChatAgent(ctx context.Context, workspaceID, agentID pgtype.UUID, userID string) bool {
	if userID == "" {
		return false
	}
	agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: agentID, WorkspaceID: workspaceID})
	if err != nil || agent.ArchivedAt.Valid {
		return false
	}
	wsID := uuidToString(workspaceID)
	if !h.canAccessPrivateAgent(ctx, agent, "member", userID, wsID) ||
		!h.canInvokeAgent(ctx, agent, "member", userID, userID, wsID) {
		return false
	}
	verdict, err := service.AgentReadiness(ctx, h.runtimeLookup(obsmetrics.RuntimeLookupSourceChat), agent)
	return err == nil && !verdict.Blocked()
}

func (h *Handler) workspaceResponseForUser(r *http.Request, workspace db.Workspace) WorkspaceResponse {
	resp := h.workspaceToResponse(workspace)
	var stored map[string]json.RawMessage
	if json.Unmarshal(workspace.Settings, &stored) != nil {
		return resp
	}
	var id string
	if json.Unmarshal(stored[defaultChatAgentKey], &id) != nil {
		return resp
	}
	agentID, err := util.ParseUUID(id)
	if err != nil || !h.usableDefaultChatAgent(r.Context(), workspace.ID, agentID, requestUserID(r)) {
		return resp
	}
	if settings, ok := resp.Settings.(map[string]any); ok {
		settings[defaultChatAgentKey] = uuidToString(agentID)
	}
	return resp
}
