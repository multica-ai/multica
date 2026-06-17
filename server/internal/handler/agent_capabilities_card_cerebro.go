package handler

// CEREBRO-PATCH(agent-capabilities-card-handler): TECH-3642 unified per-agent
// capabilities card. One canonical read-model that joins what an agent can do
// (skills), may use (tools, each with its effective permission), which
// connections it reaches (and their underlying endpoints + tools), what it has
// access to (credentials + Infisical secret paths, names only), and what it is
// limited by (sandbox + MCP). Served at GET /api/agents/{id}/capabilities and
// consumed identically by the CLI, the MCP server, and the dashboard so an agent
// (via CLI/MCP) and a human (via the UI) always see the same fields.
//
// Tools and connections are read through the SAME stores the dedicated admin
// screens use (toolpolicy.Store.Table, connections.Store.ListEnabled), so the
// card never invents a second source of truth. The earlier version read the
// legacy agent_tool_grant + credential-binding tables, which are empty for most
// agents — hence the card showed only skills.

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebroconnections "github.com/multica-ai/multica/server/internal/cerebro/connections"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	cerebrotoolpolicy "github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
)

// CEREBRO-PATCH(agent-capabilities-card-sources): TECH-3642 read seams letting
// the card reuse the tool-policy table + connections list (no second source).
//
// AgentCapabilityToolTabler is the read seam over the per-tool policy table —
// the same one the admin Tools screen renders. Satisfied by *toolpolicy.Store.
type AgentCapabilityToolTabler interface {
	Table(ctx context.Context, in cerebrotoolpolicy.TableQuery) ([]cerebrotoolpolicy.TableRow, error)
}

// AgentCapabilityConnectionsLister is the read seam over enabled workspace
// connections — the same data the admin Connections screen renders. Satisfied
// by *connections.Store.
type AgentCapabilityConnectionsLister interface {
	ListEnabled(ctx context.Context, workspaceID pgtype.UUID) ([]cerebroconnections.Connection, error)
}

// AgentCapabilitySkill is one skill the agent can load (what it CAN do).
type AgentCapabilitySkill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// AgentCapabilityTool is one tool resolved for this agent, with the effective
// permission verdict and which layer decided it (what it MAY use, and under
// what rule).
type AgentCapabilityTool struct {
	Key               string   `json:"key"`
	Title             string   `json:"title,omitempty"`
	Source            string   `json:"source,omitempty"`
	Category          string   `json:"category,omitempty"`
	Permission        string   `json:"permission"`           // allow | ask | deny
	DecidedBy         string   `json:"decided_by,omitempty"` // workspace | runtime | agent | group | user
	Reason            string   `json:"reason,omitempty"`     // human-readable, e.g. "Capped by user"
	ManagedExternally bool     `json:"managed_externally"`   // gated elsewhere (membership ACL etc.), informational
	CappedByGroups    []string `json:"capped_by_groups,omitempty"`
}

// AgentCapabilityConnEndpoint is one REST path a connection exposes.
type AgentCapabilityConnEndpoint struct {
	Path    string   `json:"path"`
	Methods []string `json:"methods"`
}

// AgentCapabilityConnTool is one MCP tool a connection exposes.
type AgentCapabilityConnTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// AgentCapabilityConnection is one external system the agent reaches, with the
// underlying endpoints (REST) or tools (MCP) it exposes. Auth secrets are never
// included — only name, type, and URL.
type AgentCapabilityConnection struct {
	Name        string                        `json:"name"`
	DisplayName string                        `json:"display_name,omitempty"`
	Type        string                        `json:"type"` // mcp_http | api
	URL         string                        `json:"url,omitempty"`
	Internal    bool                          `json:"internal"`
	Tools       []AgentCapabilityConnTool     `json:"tools,omitempty"`
	Endpoints   []AgentCapabilityConnEndpoint `json:"endpoints,omitempty"`
}

// AgentCapabilityCredential names a credential bound to the agent (what it has
// ACCESS to). Secret values are never included — only name, type, and hint.
type AgentCapabilityCredential struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// AgentCapabilityInfisicalSecret names one Infisical folder the agent's runtime
// may read. Only the environment + path are shown — never the secret values.
type AgentCapabilityInfisicalSecret struct {
	Environment string `json:"environment"`
	Path        string `json:"path"`
}

// AgentCapabilityLimits captures the boundaries the agent runs inside (what it
// is LIMITED by): the sandbox policy and the MCP server surface.
type AgentCapabilityLimits struct {
	Sandbox      json.RawMessage `json:"sandbox,omitempty"`
	McpServers   []string        `json:"mcp_servers,omitempty"`
	HasMcpConfig bool            `json:"has_mcp_config"`
}

// AgentCapabilities is the canonical per-agent capabilities card.
type AgentCapabilities struct {
	AgentID          string                           `json:"agent_id"`
	Name             string                           `json:"name"`
	Model            string                           `json:"model"`
	Description      string                           `json:"description"`
	Skills           []AgentCapabilitySkill           `json:"skills"`
	Tools            []AgentCapabilityTool            `json:"tools"`
	Connections      []AgentCapabilityConnection      `json:"connections"`
	Credentials      []AgentCapabilityCredential      `json:"credentials"`
	InfisicalSecrets []AgentCapabilityInfisicalSecret `json:"infisical_secrets"`
	Limits           AgentCapabilityLimits            `json:"limits"`
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
		AgentID:          uuidToString(agent.ID),
		Name:             agent.Name,
		Model:            agent.Model.String,
		Description:      agent.Description,
		Skills:           []AgentCapabilitySkill{},
		Tools:            []AgentCapabilityTool{},
		Connections:      []AgentCapabilityConnection{},
		Credentials:      []AgentCapabilityCredential{},
		InfisicalSecrets: []AgentCapabilityInfisicalSecret{},
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

	// MAY — every tool resolved for this agent, with its effective permission.
	out.Tools = h.agentCapabilityTools(r, agent.WorkspaceID, agent.ID)

	// CONNECTIONS — workspace connections + their underlying endpoints/tools.
	out.Connections = h.agentCapabilityConnections(r, agent.WorkspaceID)

	// ACCESS — explicit credential bindings (names/types only, never values).
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

	// ACCESS — Infisical folders this agent's runtime may read (paths only).
	if folders, err := h.listAgentInfisicalFolders(r, agent.ID); err == nil {
		for _, f := range folders {
			out.InfisicalSecrets = append(out.InfisicalSecrets, AgentCapabilityInfisicalSecret{
				Environment: f.Environment,
				Path:        f.SecretPath,
			})
		}
	}

	// LIMITS — sandbox policy + MCP server surface.
	out.Limits = buildAgentCapabilityLimits(agent.RuntimeConfig, agent.McpConfig)

	writeJSON(w, http.StatusOK, out)
}

// agentCapabilityTools resolves the per-tool permission table for this agent in
// the requesting user's context. It reuses toolpolicy.Store.Table — the exact
// read model the admin Tools screen renders — so the card and the admin screen
// never diverge. A missing user (agent calling via CLI/MCP) is fine: the table
// simply omits the user-ceiling layer (Valid=false), resolving the rest of the
// chain. Returns an empty slice (never nil) on any error so the card renders.
func (h *Handler) agentCapabilityTools(r *http.Request, workspaceID, agentID pgtype.UUID) []AgentCapabilityTool {
	out := []AgentCapabilityTool{}
	if h.CapabilityToolPolicy == nil {
		return out
	}
	// Zero/invalid UserID when there is no user in context — Table handles it.
	userID, _ := util.ParseUUID(requestUserID(r))

	rows, err := h.CapabilityToolPolicy.Table(r.Context(), cerebrotoolpolicy.TableQuery{
		WorkspaceID:     workspaceID,
		AgentID:         agentID,
		UserID:          userID,
		Base:            cerebrotoolpolicy.SettingAllow,
		IncludePlatform: true,
	})
	if err != nil {
		return out
	}

	for _, row := range rows {
		// Only the capability-wide row belongs on the card; per-resource rows
		// (e.g. a single repo URL pattern) are admin detail, not a capability.
		if row.ResourcePattern != "" {
			continue
		}
		t := AgentCapabilityTool{
			Key:               row.ToolKey,
			Title:             row.Title,
			Source:            row.Source,
			Category:          row.Category,
			Permission:        string(row.Effective.Setting),
			DecidedBy:         string(row.Effective.DecidedBy),
			Reason:            row.Effective.Reason,
			ManagedExternally: row.ManagedExternally,
		}
		for _, g := range row.CappedByGroups {
			label := g.Name
			if g.Owner != "" {
				label += " (" + g.Owner + ")"
			}
			t.CappedByGroups = append(t.CappedByGroups, label)
		}
		out = append(out, t)
	}
	return out
}

// agentCapabilityConnections lists the enabled workspace connections and the
// endpoints/tools each one exposes. Auth secrets are deliberately dropped.
// Returns an empty slice (never nil) on any error so the card renders.
func (h *Handler) agentCapabilityConnections(r *http.Request, workspaceID pgtype.UUID) []AgentCapabilityConnection {
	out := []AgentCapabilityConnection{}
	if h.CapabilityConnections == nil {
		return out
	}
	conns, err := h.CapabilityConnections.ListEnabled(r.Context(), workspaceID)
	if err != nil {
		return out
	}
	for _, c := range conns {
		entry := AgentCapabilityConnection{
			Name:        c.Name,
			DisplayName: c.DisplayName,
			Type:        c.Type,
			URL:         c.URL,
			Internal:    c.Internal,
		}
		for _, t := range c.Tools {
			entry.Tools = append(entry.Tools, AgentCapabilityConnTool{Name: t.Name, Description: t.Description})
		}
		for _, ep := range c.EndpointPermissions {
			entry.Endpoints = append(entry.Endpoints, AgentCapabilityConnEndpoint{Path: ep.Path, Methods: ep.Methods})
		}
		out = append(out, entry)
	}
	return out
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
