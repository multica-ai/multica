package handler

// CEREBRO-PATCH(agent-tools-handler): cerebro agent tool grant admin API.

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// CerebroToolItem holds display metadata for one registered tool.
// Injected into the Handler via SetCerebroToolMeta.
type CerebroToolItem struct {
	Name        string
	Description string
}

// AgentToolResponse is the wire shape returned by GET /api/agents/{id}/tools.
// Includes display metadata (name, description) plus the grant's enabled flag
// and optional config. Tools without a grant row are included with enabled=false.
type AgentToolResponse struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Enabled     bool            `json:"enabled"`
	ConfigJSON  json.RawMessage `json:"config,omitempty"`
}

// SetCerebroToolMeta wires tool display metadata into the handler. Called by
// the router after construction so the upstream handler.New signature stays
// unchanged.
//
// CEREBRO-PATCH(handler-tool-meta-setter): wires tool metadata without
// changing the upstream handler.New signature.
func (h *Handler) SetCerebroToolMeta(items []CerebroToolItem) {
	h.cerebroToolItems = items
	m := make(map[string]string, len(items))
	for _, it := range items {
		m[it.Name] = it.Description
	}
	h.cerebroToolDesc = m
}

// ListAgentTools handles GET /api/agents/{id}/tools
// Returns all registered tools, annotated with description and enabled status.
// Tools with no grant row are included with enabled=false so the admin UI can
// toggle them without a pre-existing row.
func (h *Handler) ListAgentTools(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}

	// Fetch all grant rows for this agent.
	rows, err := h.DB.Query(r.Context(),
		`SELECT tool_name, config_json, enabled
         FROM agent_tool_grant
         WHERE agent_id = $1`,
		agent.ID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query tool grants: "+err.Error())
		return
	}
	defer rows.Close()

	type grantRow struct {
		config  []byte
		enabled bool
	}
	grants := make(map[string]grantRow)
	for rows.Next() {
		var (
			name      string
			configRaw []byte
			enabled   bool
		)
		if err := rows.Scan(&name, &configRaw, &enabled); err != nil {
			writeError(w, http.StatusInternalServerError, "scan error: "+err.Error())
			return
		}
		grants[name] = grantRow{config: configRaw, enabled: enabled}
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "rows error: "+err.Error())
		return
	}

	// Build response from the ordered tool list merged with grant data.
	out := make([]AgentToolResponse, 0, len(h.cerebroToolItems))
	for _, item := range h.cerebroToolItems {
		resp := AgentToolResponse{
			Name:        item.Name,
			Description: item.Description,
		}
		if g, ok := grants[item.Name]; ok {
			resp.Enabled = g.enabled
			if len(g.config) > 0 {
				resp.ConfigJSON = json.RawMessage(g.config)
			}
		}
		out = append(out, resp)
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
	body.Enabled = true
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	var configJSON []byte
	if len(body.Config) > 0 && string(body.Config) != "null" {
		configJSON = body.Config
	}

	_, err := h.DB.Exec(r.Context(),
		`INSERT INTO agent_tool_grant (agent_id, tool_name, config_json, enabled)
         VALUES ($1, $2, $3, $4)
         ON CONFLICT (agent_id, tool_name)
         DO UPDATE SET config_json = EXCLUDED.config_json, enabled = EXCLUDED.enabled`,
		agent.ID, toolName, configJSON, body.Enabled,
	)
	if err != nil {
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23503" {
			writeError(w, http.StatusNotFound, "agent not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "upsert tool grant: "+err.Error())
		return
	}

	row := h.DB.QueryRow(r.Context(),
		`SELECT tool_name, config_json, enabled
         FROM agent_tool_grant
         WHERE agent_id = $1 AND tool_name = $2`,
		agent.ID, toolName,
	)
	var (
		name      string
		cfgRaw    []byte
		enabled   bool
	)
	if err := row.Scan(&name, &cfgRaw, &enabled); err != nil {
		writeError(w, http.StatusInternalServerError, "fetch updated grant: "+err.Error())
		return
	}

	resp := AgentToolResponse{
		Name:    name,
		Enabled: enabled,
	}
	if desc, ok := h.cerebroToolDesc[name]; ok {
		resp.Description = desc
	}
	if len(cfgRaw) > 0 {
		resp.ConfigJSON = json.RawMessage(cfgRaw)
	}
	writeJSON(w, http.StatusOK, resp)
}
