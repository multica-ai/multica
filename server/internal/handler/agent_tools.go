package handler

// CEREBRO-PATCH(agent-tools-handler): cerebro agent tool grant admin API.

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// AgentToolGrantResponse is the wire shape for an agent tool grant.
type AgentToolGrantResponse struct {
	ID        string          `json:"id"`
	AgentID   string          `json:"agent_id"`
	ToolName  string          `json:"tool_name"`
	ConfigJSON json.RawMessage `json:"config,omitempty"`
	Enabled   bool            `json:"enabled"`
	CreatedAt string          `json:"created_at"`
}

// ListAgentTools handles GET /api/agents/{id}/tools
// Returns all tool grants for this agent with their enabled status. Includes
// every grant row regardless of enabled=true/false so the admin UI can toggle
// them.
func (h *Handler) ListAgentTools(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}

	rows, err := h.DB.Query(r.Context(),
		`SELECT id, agent_id, tool_name, config_json, enabled, created_at
         FROM agent_tool_grant
         WHERE agent_id = $1
         ORDER BY tool_name`,
		agent.ID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query tool grants: "+err.Error())
		return
	}
	defer rows.Close()

	out := make([]AgentToolGrantResponse, 0)
	for rows.Next() {
		var (
			id        pgtype.UUID
			agentID   pgtype.UUID
			toolName  string
			configRaw []byte
			enabled   bool
			createdAt pgtype.Timestamptz
		)
		if err := rows.Scan(&id, &agentID, &toolName, &configRaw, &enabled, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan error: "+err.Error())
			return
		}
		grant := AgentToolGrantResponse{
			ID:       uuidToString(id),
			AgentID:  uuidToString(agentID),
			ToolName: toolName,
			Enabled:  enabled,
		}
		if len(configRaw) > 0 {
			grant.ConfigJSON = json.RawMessage(configRaw)
		}
		if createdAt.Valid {
			grant.CreatedAt = timestampToString(createdAt)
		}
		out = append(out, grant)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "rows error: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, out)
}

// UpsertAgentTool handles PUT /api/agents/{id}/tools/{name}
// Toggles the enabled flag and updates config_json for a tool grant. Creates
// the grant if it doesn't exist yet.
func (h *Handler) UpsertAgentTool(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	toolName := chi.URLParam(r, "name")
	if toolName == "" {
		writeError(w, http.StatusBadRequest, "tool name is required")
		return
	}

	var body struct {
		Enabled bool            `json:"enabled"`
		Config  json.RawMessage `json:"config"`
	}
	body.Enabled = true // default to enabled when not specified
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	var configJSON []byte
	if len(body.Config) > 0 && string(body.Config) != "null" {
		configJSON = body.Config
	}

	tag, err := h.DB.Exec(r.Context(),
		`INSERT INTO agent_tool_grant (agent_id, tool_name, config_json, enabled)
         VALUES ($1, $2, $3, $4)
         ON CONFLICT (agent_id, tool_name)
         DO UPDATE SET config_json = EXCLUDED.config_json, enabled = EXCLUDED.enabled`,
		agent.ID, toolName, configJSON, body.Enabled,
	)
	if err != nil {
		// Check for FK violation (agent doesn't exist) — should not happen since
		// loadAgentForUser already resolved the agent, but be defensive.
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23503" {
			writeError(w, http.StatusNotFound, "agent not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "upsert tool grant: "+err.Error())
		return
	}
	_ = tag

	// Return the updated grant row.
	row := h.DB.QueryRow(r.Context(),
		`SELECT id, agent_id, tool_name, config_json, enabled, created_at
         FROM agent_tool_grant
         WHERE agent_id = $1 AND tool_name = $2`,
		agent.ID, toolName,
	)
	var (
		grantID   pgtype.UUID
		grantAgent pgtype.UUID
		name      string
		cfgRaw    []byte
		enabled   bool
		createdAt pgtype.Timestamptz
	)
	if err := row.Scan(&grantID, &grantAgent, &name, &cfgRaw, &enabled, &createdAt); err != nil {
		writeError(w, http.StatusInternalServerError, "fetch updated grant: "+err.Error())
		return
	}
	grant := AgentToolGrantResponse{
		ID:       uuidToString(grantID),
		AgentID:  uuidToString(grantAgent),
		ToolName: name,
		Enabled:  enabled,
	}
	if len(cfgRaw) > 0 {
		grant.ConfigJSON = json.RawMessage(cfgRaw)
	}
	if createdAt.Valid {
		grant.CreatedAt = timestampToString(createdAt)
	}
	writeJSON(w, http.StatusOK, grant)
}
