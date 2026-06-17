package handler

// CEREBRO-PATCH(agent-capabilities-card-handler): TECH-3642 unified per-agent
// capabilities card. One canonical read-model that joins what an agent can do
// (skills), may use (tools), has access to (credentials), and is limited by
// (sandbox + MCP). Served at GET /api/agents/{id}/capabilities and consumed
// identically by the CLI, the MCP server, and the dashboard so an agent (via
// CLI/MCP) and a human (via the UI) always see the same fields.

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// AgentCapabilitySkill is one skill the agent can load (what it CAN do).
type AgentCapabilitySkill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// AgentCapabilityTool is one platform tool grant (what it MAY use).
type AgentCapabilityTool struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// AgentCapabilityCredential names a credential bound to the agent (what it has
// ACCESS to). Secret values are never included — only name, type, and hint.
type AgentCapabilityCredential struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// AgentCapabilityLimits captures the boundaries the agent runs inside (what it
// is LIMITED by): the sandbox policy and the MCP server surface.
type AgentCapabilityLimits struct {
	Sandbox      json.RawMessage `json:"sandbox,omitempty"`
	McpServers   []string        `json:"mcp_servers,omitempty"`
	HasMcpConfig bool            `json:"has_mcp_config"`
}

// AgentCapabilities is the canonical per-agent capabilities card. The four
// sections mirror the four questions every surface asks about an agent:
// can (skills) · may (tools) · access (credentials) · limits (sandbox/mcp).
type AgentCapabilities struct {
	AgentID     string                      `json:"agent_id"`
	Name        string                      `json:"name"`
	Model       string                      `json:"model"`
	Description string                      `json:"description"`
	Skills      []AgentCapabilitySkill      `json:"skills"`
	Tools       []AgentCapabilityTool       `json:"tools"`
	Credentials []AgentCapabilityCredential `json:"credentials"`
	Limits      AgentCapabilityLimits       `json:"limits"`
}

// GetAgentCapabilities handles GET /api/agents/{id}/capabilities. Access control
// is delegated to loadAgentForUser (same gate as the other agent read routes).
func (h *Handler) GetAgentCapabilities(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}

	out := AgentCapabilities{
		AgentID:     uuidToString(agent.ID),
		Name:        agent.Name,
		Model:       agent.Model.String,
		Description: agent.Description,
		Skills:      []AgentCapabilitySkill{},
		Tools:       []AgentCapabilityTool{},
		Credentials: []AgentCapabilityCredential{},
	}

	// CAN — skills the agent loads.
	if skills, err := h.Queries.ListAgentSkillSummaries(r.Context(), agent.ID); err == nil {
		for _, s := range skills {
			out.Skills = append(out.Skills, AgentCapabilitySkill{
				ID:          uuidToString(s.ID),
				Name:        s.Name,
				Description: s.Description,
			})
		}
	}

	// MAY — enabled platform tool grants.
	rows, err := h.DB.Query(r.Context(),
		`SELECT tool_name, enabled FROM agent_tool_grant WHERE agent_id = $1 ORDER BY tool_name`,
		agent.ID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query tool grants: "+err.Error())
		return
	}
	for rows.Next() {
		var (
			name    string
			enabled bool
		)
		if err := rows.Scan(&name, &enabled); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "scan error: "+err.Error())
			return
		}
		out.Tools = append(out.Tools, AgentCapabilityTool{Name: name, Enabled: enabled})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "rows error: "+err.Error())
		return
	}

	// ACCESS — credentials bound to this agent (names/types only, never values).
	if h.CerebroQueries != nil {
		if creds, err := h.CerebroQueries.ListCerebroCredentialsForResource(r.Context(),
			cerebrodb.ListCerebroCredentialsForResourceParams{ResourceType: "agent", ResourceID: agent.ID}); err == nil {
			for _, c := range creds {
				out.Credentials = append(out.Credentials, AgentCapabilityCredential{
					Name:        c.Name,
					Type:        c.Type,
					Description: c.Description,
				})
			}
		}
	}

	// LIMITS — sandbox policy + MCP server surface.
	out.Limits = buildAgentCapabilityLimits(agent.RuntimeConfig, agent.McpConfig)

	writeJSON(w, http.StatusOK, out)
}

// buildAgentCapabilityLimits extracts the agent's boundaries from its opaque
// runtime_config and mcp_config blobs. Both are best-effort: a missing or
// unparseable blob simply yields an empty section rather than an error, because
// the card must always render.
func buildAgentCapabilityLimits(runtimeConfig, mcpConfig []byte) AgentCapabilityLimits {
	limits := AgentCapabilityLimits{McpServers: []string{}}

	if len(runtimeConfig) > 0 {
		var rc map[string]json.RawMessage
		if err := json.Unmarshal(runtimeConfig, &rc); err == nil {
			if sb, ok := rc["sandbox"]; ok && len(sb) > 0 {
				limits.Sandbox = sb
			}
		}
	}

	if len(mcpConfig) > 0 {
		limits.HasMcpConfig = true
		var mc struct {
			McpServers map[string]json.RawMessage `json:"mcpServers"`
		}
		if err := json.Unmarshal(mcpConfig, &mc); err == nil {
			for name := range mc.McpServers {
				limits.McpServers = append(limits.McpServers, name)
			}
		}
	}

	return limits
}
