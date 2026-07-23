// CEREBRO-PATCH(cerebro-agent-tool-access-diagnostic): FIR-1480 admin-only diagnostic — why a user can/can't use an agent's tools.
package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/util"
)

// toolAccessEntry is the per-tool verdict returned by the tool-access diagnostic.
type toolAccessEntry struct {
	Tool            string `json:"tool"`
	Source          string `json:"source"`
	McpServer       string `json:"mcp_server,omitempty"`
	Callable        bool   `json:"callable"`
	Reason          string `json:"reason"`
	RuntimeEnabled  bool   `json:"runtime_enabled"`
	HasOverride     bool   `json:"has_override"`
	OverrideEnabled bool   `json:"override_enabled"`
	UserIsAdmin     bool   `json:"user_is_admin"`
	UserGrant       bool   `json:"user_grant"`
	GroupGrants     string `json:"group_grants,omitempty"`
}

// toolAccessResponse is the wire shape of GET /api/agents/{id}/tool-access.
type toolAccessResponse struct {
	AgentID   string            `json:"agent_id"`
	RuntimeID string            `json:"runtime_id"`
	User      string            `json:"user"`
	UserID    string            `json:"user_id"`
	Tools     []toolAccessEntry `json:"tools"`
}

// ExplainAgentToolAccess handles GET /api/agents/{id}/tool-access?user=<id|email>.
// Admin-only. Lists every tool on the agent's runtime with the inputs to the
// access decision so an operator can see why a user can or cannot call it.
func (h *Handler) ExplainAgentToolAccess(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	if _, ok := h.canManageAgent(w, r, agent); !ok {
		return
	}

	userParam := r.URL.Query().Get("user")
	if userParam == "" {
		writeError(w, http.StatusBadRequest, "user query param required (UUID or email)")
		return
	}

	targetUUID, err := util.ParseUUID(userParam)
	if err != nil {
		user, uerr := h.Queries.GetUserByEmail(r.Context(), userParam)
		if uerr != nil {
			writeError(w, http.StatusNotFound, "user not found: "+userParam)
			return
		}
		targetUUID = user.ID
	}

	if h.runtimeToolAccess == nil || !agent.RuntimeID.Valid {
		writeError(w, http.StatusServiceUnavailable, "runtime tool access preview unavailable")
		return
	}
	rt, err := h.Queries.GetAgentRuntime(r.Context(), agent.RuntimeID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}
	rows, err := h.runtimeToolAccess.ListEffectiveTools(r.Context(), RuntimeToolAccessQuery{
		WorkspaceID: agent.WorkspaceID, RuntimeID: rt.ID, RuntimeMode: rt.RuntimeMode,
		RuntimeProvider: rt.Provider, RuntimeCapabilities: marshalRuntimeCapabilities(normalizedRuntimeCapabilities(rt.Provider, rt.Capabilities, rt.ToolsConfig)),
		AgentID: agent.ID, UserID: targetUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "resolve tool access: "+err.Error())
		return
	}

	entries := make([]toolAccessEntry, 0, len(rows))
	for _, row := range rows {
		e := toolAccessEntry{
			Tool: row.Descriptor.ToolKey, Source: row.Descriptor.Source,
			Callable: row.ExposureEffective.Effective, Reason: row.ExposureEffective.Reason,
			RuntimeEnabled:  row.Inventory.Enabled,
			HasOverride:     row.Layers["decided_by"] == "agent",
			OverrideEnabled: row.Layers["decided_by"] == "agent" && row.Policy.Effective != "deny",
			UserGrant:       row.Layers["decided_by"] == "user",
		}
		e.McpServer = row.Inventory.MCPServerName
		entries = append(entries, e)
	}

	writeJSON(w, http.StatusOK, toolAccessResponse{
		AgentID:   uuidToString(agent.ID),
		RuntimeID: uuidToString(agent.RuntimeID),
		User:      userParam,
		UserID:    uuidToString(targetUUID),
		Tools:     entries,
	})
}
