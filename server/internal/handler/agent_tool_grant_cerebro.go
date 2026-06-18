// CEREBRO-PATCH(cerebro-agent-tool-grant-write): FIR-1496 admin-only write path —
// grant or revoke an agent's runtime tools for a group or user from one
// agent-centric endpoint. This closes the Saga/FIR-426 hole where the runtime
// tool grant (the layer that silently gave Jakob zero tools) had no UI and no
// CLI write path when the unified tool-policy table is enabled. The underlying
// per-(runtime,tool,target) grant store already exists; this resolves the
// agent's runtime once so callers can think in agents (Saga) instead of runtimes.
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

// agentToolGrantRequest is the wire body for POST/DELETE /api/agents/{id}/tool-grants.
//
// Exactly one of group_id / user must be set. Exactly one of tool / all_tools
// must select the tool scope.
type agentToolGrantRequest struct {
	Tool     string `json:"tool"`
	AllTools bool   `json:"all_tools"`
	GroupID  string `json:"group_id"`
	User     string `json:"user"` // UUID or email
}

// agentToolGrantResult summarises what changed so the CLI can print a confirmation.
type agentToolGrantResult struct {
	AgentID    string   `json:"agent_id"`
	RuntimeID  string   `json:"runtime_id"`
	Action     string   `json:"action"` // "grant" | "revoke"
	TargetType string   `json:"target_type"`
	TargetID   string   `json:"target_id"`
	Tools      []string `json:"tools"`
	Count      int      `json:"count"`
}

// AddAgentToolGrant handles POST /api/agents/{id}/tool-grants — grant access.
func (h *Handler) AddAgentToolGrant(w http.ResponseWriter, r *http.Request) {
	h.writeAgentToolGrant(w, r, "grant")
}

// RemoveAgentToolGrant handles DELETE /api/agents/{id}/tool-grants — revoke access.
func (h *Handler) RemoveAgentToolGrant(w http.ResponseWriter, r *http.Request) {
	h.writeAgentToolGrant(w, r, "revoke")
}

func (h *Handler) writeAgentToolGrant(w http.ResponseWriter, r *http.Request, action string) {
	if !h.requireToolsAdmin(w) {
		return
	}

	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	// Runtime tool grants govern every agent on the runtime, so require the
	// workspace owner/admin role — the same threat model as the runtime-scoped
	// grant endpoints — not merely "can manage this agent".
	wsID := uuidToString(agent.WorkspaceID)
	member, ok := h.requireWorkspaceMember(w, r, wsID, "agent not found")
	if !ok {
		return
	}
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "only workspace owners and admins can manage tool grants")
		return
	}
	if !agent.RuntimeID.Valid {
		writeError(w, http.StatusBadRequest, "agent has no runtime assigned")
		return
	}

	var body agentToolGrantRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Resolve the target: exactly one of group / user.
	if (body.GroupID == "") == (body.User == "") {
		writeError(w, http.StatusBadRequest, "specify exactly one of group_id or user")
		return
	}
	if body.AllTools == (body.Tool != "") {
		writeError(w, http.StatusBadRequest, "specify exactly one of tool or all_tools")
		return
	}

	var (
		targetType string
		targetID   pgtype.UUID
	)
	if body.GroupID != "" {
		gid, err := util.ParseUUID(body.GroupID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid group_id")
			return
		}
		targetType, targetID = "group", gid
	} else {
		uid, err := util.ParseUUID(body.User)
		if err != nil {
			u, uerr := h.Queries.GetUserByEmail(r.Context(), body.User)
			if uerr != nil {
				writeError(w, http.StatusNotFound, "user not found: "+body.User)
				return
			}
			uid = u.ID
		}
		targetType, targetID = "user", uid
	}

	// Resolve the tool scope against the runtime's live inventory so we never
	// write a grant for a tool the runtime does not have.
	inventory, err := h.runtimeToolsAdmin.ListTools(r.Context(), agent.RuntimeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list runtime tools: "+err.Error())
		return
	}
	tools := make([]string, 0, len(inventory))
	if body.AllTools {
		for _, t := range inventory {
			tools = append(tools, t.Name)
		}
	} else {
		found := false
		for _, t := range inventory {
			if t.Name == body.Tool {
				found = true
				break
			}
		}
		if !found {
			writeError(w, http.StatusBadRequest, "tool not found on runtime: "+body.Tool)
			return
		}
		tools = append(tools, body.Tool)
	}
	if len(tools) == 0 {
		writeError(w, http.StatusBadRequest, "runtime has no tools to grant")
		return
	}

	for _, tool := range tools {
		var gerr error
		switch {
		case action == "grant" && targetType == "group":
			gerr = h.runtimeToolsAdmin.AddGroupGrant(r.Context(), agent.RuntimeID, tool, targetID, member.UserID)
		case action == "revoke" && targetType == "group":
			gerr = h.runtimeToolsAdmin.RemoveGroupGrant(r.Context(), agent.RuntimeID, tool, targetID)
		case action == "grant" && targetType == "user":
			gerr = h.runtimeToolsAdmin.AddUserGrant(r.Context(), agent.RuntimeID, tool, targetID, member.UserID)
		case action == "revoke" && targetType == "user":
			gerr = h.runtimeToolsAdmin.RemoveUserGrant(r.Context(), agent.RuntimeID, tool, targetID)
		}
		if gerr != nil {
			writeError(w, http.StatusInternalServerError, action+" "+targetType+" grant for "+tool+": "+gerr.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, agentToolGrantResult{
		AgentID:    uuidToString(agent.ID),
		RuntimeID:  uuidToString(agent.RuntimeID),
		Action:     action,
		TargetType: targetType,
		TargetID:   uuidToString(targetID),
		Tools:      tools,
		Count:      len(tools),
	})
}
