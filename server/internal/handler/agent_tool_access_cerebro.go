// CEREBRO-PATCH(cerebro-agent-tool-access-diagnostic): FIR-1480 admin-only diagnostic — why a user can/can't use an agent's tools.
package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
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

// explainToolAccess mirrors the default-deny rule in
// ResolveCerebroAgentToolAccess and returns (callable, human reason).
func explainToolAccess(row cerebrodb.ExplainCerebroAgentToolAccessRow) (bool, string) {
	if !row.RuntimeEnabled {
		return false, "tool disabled on the runtime"
	}
	if row.HasOverride && !row.OverrideEnabled {
		return false, "disabled for this agent (override)"
	}
	if row.UserIsAdmin {
		return true, "workspace owner/admin — access check bypassed"
	}
	if row.UserGrant {
		return true, "granted directly to the user"
	}
	if row.GroupGrants != "" {
		return true, "granted via group: " + row.GroupGrants
	}
	return false, "no grant: not admin, no direct grant, no group grant"
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

	if h.CerebroQueries == nil {
		writeError(w, http.StatusInternalServerError, "cerebro queries unavailable")
		return
	}

	rows, err := h.CerebroQueries.ExplainCerebroAgentToolAccess(r.Context(), cerebrodb.ExplainCerebroAgentToolAccessParams{
		ID:     agent.ID,
		UserID: targetUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "resolve tool access: "+err.Error())
		return
	}

	entries := make([]toolAccessEntry, 0, len(rows))
	for _, row := range rows {
		callable, reason := explainToolAccess(row)
		e := toolAccessEntry{
			Tool:            row.ToolName,
			Source:          row.Source,
			Callable:        callable,
			Reason:          reason,
			RuntimeEnabled:  row.RuntimeEnabled,
			HasOverride:     row.HasOverride,
			OverrideEnabled: row.OverrideEnabled,
			UserIsAdmin:     row.UserIsAdmin,
			UserGrant:       row.UserGrant,
			GroupGrants:     row.GroupGrants,
		}
		if row.McpServerName.Valid {
			e.McpServer = row.McpServerName.String
		}
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
