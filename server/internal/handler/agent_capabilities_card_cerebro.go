package handler

// CEREBRO-PATCH(agent-capabilities-card-handler): TECH-3642 unified per-agent
// capabilities card. One canonical read-model that joins what an agent can do
// (skills), may use (tools, each with its effective permission), which repos and
// connections it reaches (and their underlying endpoints + tools, each with a
// permission), what it has access to (credentials + Infisical secret paths,
// names only), and what it is limited by (sandbox + MCP). Served at
// GET /api/agents/{id}/capabilities and consumed identically by the CLI, the MCP
// server, and the dashboard so an agent (via CLI/MCP) and a human (via the UI)
// always see the same fields.
//
// Tools, repos, and connection permissions all come from the SAME tool-policy
// table the admin Tools screen renders (toolpolicy.Store.Table), and connections
// from connections.Store.List — the card never invents a second source of truth.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebroconnections "github.com/multica-ai/multica/server/internal/cerebro/connections"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	cerebrotoolpolicy "github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
)

// CEREBRO-PATCH(agent-capabilities-card-sections): TECH-3642 route tool-policy
// rows into tools / repos / connection-tool sections instead of one flat list.
//
// Source labels the tool-policy table stamps on rows, so the card can route each
// row to the right section instead of flattening everything into one list.
const (
	capSourceRepo              = "repo"
	capSourceConnection        = "connection"
	capSourceConnectionTool    = "connection-tool"
	capSourceConnectionEndpnt  = "connection-endpoint"
	capConnectionToolKeyPrefix = "connection:"
)

// CEREBRO-PATCH(agent-capabilities-card-sources): TECH-3642 read seams letting
// the card reuse the tool-policy table + connections list (no second source).
//
// AgentCapabilityToolTabler is the read seam over the per-tool policy table —
// the same one the admin Tools screen renders. Satisfied by *toolpolicy.Store.
type AgentCapabilityToolTabler interface {
	Table(ctx context.Context, in cerebrotoolpolicy.TableQuery) ([]cerebrotoolpolicy.TableRow, error)
}

// AgentCapabilityConnectionsLister is the read seam over workspace connections —
// the same data the admin Connections screen renders. List returns ALL
// connections (enabled + disabled). Satisfied by *connections.Store.
type AgentCapabilityConnectionsLister interface {
	List(ctx context.Context, workspaceID pgtype.UUID) ([]cerebroconnections.Connection, error)
}

// AgentCapabilitySkill is one skill the agent can load (what it CAN do).
type AgentCapabilitySkill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// AgentCapabilityTool is one tool/permission resolved for this agent, with the
// effective verdict and which layer decided it.
type AgentCapabilityTool struct {
	Key               string   `json:"key"`
	Title             string   `json:"title,omitempty"`
	Source            string   `json:"source,omitempty"`
	Category          string   `json:"category,omitempty"`
	Permission        string   `json:"permission"`           // allow | ask | deny
	DecidedBy         string   `json:"decided_by,omitempty"` // workspace | runtime | agent | group | user
	Reason            string   `json:"reason,omitempty"`
	ManagedExternally bool     `json:"managed_externally"`
	CappedByGroups    []string `json:"capped_by_groups,omitempty"`
}

// AgentCapabilityRepo groups one repository's permissions (read / check out /
// push), each with its effective verdict for this agent.
type AgentCapabilityRepo struct {
	URL         string                `json:"url"`
	Permissions []AgentCapabilityTool `json:"permissions"`
}

// AgentCapabilityConnEndpoint is one REST path a connection exposes.
type AgentCapabilityConnEndpoint struct {
	Path    string   `json:"path"`
	Methods []string `json:"methods"`
}

// AgentCapabilityConnTool is one MCP tool a connection exposes, with this agent's
// effective permission on it (empty when the tool-policy table has no row).
type AgentCapabilityConnTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Permission  string `json:"permission,omitempty"`
}

// AgentCapabilityConnection is one external system the agent reaches, with the
// underlying endpoints (REST) or tools (MCP) it exposes. Auth secrets are never
// included — only name, type, and URL. Disabled connections are included with
// enabled=false so the full picture is visible.
type AgentCapabilityConnection struct {
	Name        string                        `json:"name"`
	DisplayName string                        `json:"display_name,omitempty"`
	Type        string                        `json:"type"` // mcp_http | api
	URL         string                        `json:"url,omitempty"`
	Internal    bool                          `json:"internal"`
	Enabled     bool                          `json:"enabled"`
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
	Repos            []AgentCapabilityRepo            `json:"repos"`
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
		Repos:            []AgentCapabilityRepo{},
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

	// MAY — every permission row resolved for this agent, split into tools,
	// repos, and per-connection-tool verdicts (one tool-policy table read).
	rows := h.agentCapabilityRows(r, agent.WorkspaceID, agent.ID)
	tools, repos, connPerms := classifyCapabilityRows(rows)
	out.Tools = tools
	out.Repos = repos

	// CONNECTIONS — all workspace connections + endpoints/tools, each tool
	// stamped with its effective permission from the rows above.
	out.Connections = h.agentCapabilityConnections(r, agent.WorkspaceID, connPerms)

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

	// ACCESS — Infisical folders this agent may read (paths only).
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

// agentCapabilityRows resolves the full per-tool policy table for this agent in
// the requesting user's context. It reuses toolpolicy.Store.Table — the exact
// read model the admin Tools screen renders — so the card and the admin screen
// never diverge. A missing user (agent calling via CLI/MCP) is fine: the table
// omits the user-ceiling layer (Valid=false) and resolves the rest. Returns nil
// on any error so the card still renders.
func (h *Handler) agentCapabilityRows(r *http.Request, workspaceID, agentID pgtype.UUID) []cerebrotoolpolicy.TableRow {
	if h.CapabilityToolPolicy == nil {
		return nil
	}
	userID, _ := util.ParseUUID(requestUserID(r))
	rows, err := h.CapabilityToolPolicy.Table(r.Context(), cerebrotoolpolicy.TableQuery{
		WorkspaceID:     workspaceID,
		AgentID:         agentID,
		UserID:          userID,
		Base:            cerebrotoolpolicy.SettingAllow,
		IncludePlatform: true,
	})
	if err != nil {
		return nil
	}
	return rows
}

// classifyCapabilityRows splits the flat tool-policy table into the card's
// sections: general tools, per-repo permission groups, and a lookup of
// connection-tool verdicts keyed by connection name then tool name. Connection
// capability-wide and endpoint rows are dropped here because connections render
// structurally from the connections store. Pure function — unit-tested.
func classifyCapabilityRows(rows []cerebrotoolpolicy.TableRow) (
	tools []AgentCapabilityTool,
	repos []AgentCapabilityRepo,
	connPerms map[string]map[string]string,
) {
	tools = []AgentCapabilityTool{}
	repos = []AgentCapabilityRepo{}
	connPerms = map[string]map[string]string{}

	repoIndex := map[string]int{} // repo URL -> index into repos (preserves order)

	for _, row := range rows {
		switch row.Source {
		case capSourceRepo:
			url := row.ResourcePattern
			idx, ok := repoIndex[url]
			if !ok {
				idx = len(repos)
				repoIndex[url] = idx
				repos = append(repos, AgentCapabilityRepo{URL: url})
			}
			repos[idx].Permissions = append(repos[idx].Permissions, capabilityToolFromRow(row))
		case capSourceConnectionTool:
			conn := strings.TrimPrefix(row.ToolKey, capConnectionToolKeyPrefix)
			if connPerms[conn] == nil {
				connPerms[conn] = map[string]string{}
			}
			connPerms[conn][row.ResourcePattern] = string(row.Effective.Setting)
		case capSourceConnection, capSourceConnectionEndpnt:
			// Rendered structurally from the connections store; skip here.
		default:
			tools = append(tools, capabilityToolFromRow(row))
		}
	}
	return tools, repos, connPerms
}

func capabilityToolFromRow(row cerebrotoolpolicy.TableRow) AgentCapabilityTool {
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
	return t
}

// agentCapabilityConnections lists ALL workspace connections (enabled and
// disabled) with the endpoints/tools each exposes, stamping each MCP tool with
// the agent's effective permission from connPerms. Auth secrets are dropped.
// Returns an empty slice (never nil) on any error so the card renders.
func (h *Handler) agentCapabilityConnections(r *http.Request, workspaceID pgtype.UUID, connPerms map[string]map[string]string) []AgentCapabilityConnection {
	out := []AgentCapabilityConnection{}
	if h.CapabilityConnections == nil {
		return out
	}
	conns, err := h.CapabilityConnections.List(r.Context(), workspaceID)
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
			Enabled:     c.Enabled,
		}
		for _, t := range c.Tools {
			entry.Tools = append(entry.Tools, AgentCapabilityConnTool{
				Name:        t.Name,
				Description: t.Description,
				Permission:  connPerms[c.Name][t.Name],
			})
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
