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
	capSourceScan              = "scan"
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
	// repos, per-connection-tool verdicts, and the scanned MCP tools grouped
	// under the connection that exposes them (one tool-policy table read).
	rows := h.agentCapabilityRows(r, agent.WorkspaceID, agent.ID)
	conns := h.listCapabilityConnections(r, agent.WorkspaceID)
	tools, repos, connPerms, connTools := classifyCapabilityRows(rows, connectionNameSet(conns))
	out.Tools = tools
	out.Repos = repos

	// CONNECTIONS — all workspace connections + endpoints/tools, each tool
	// stamped with its effective permission from the rows above.
	out.Connections = buildAgentCapabilityConnections(conns, connPerms, connTools)

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
// sections: general tools, per-repo permission groups, a lookup of
// connection-tool verdicts (keyed by connection name then tool name), and the
// scanned MCP tools grouped under the connection that exposes them (keyed by
// connection name). Connection capability-wide and endpoint rows are dropped
// here because connections render structurally from the connections store.
//
// connectionNames is the set of workspace connection names. A scanned tool row
// (source 'scan') whose Category matches a connection name is a tool that
// connection exposes — the scan→capability bridge stamps Category = MCP server
// name = connection name, capability_key = "<conn>.<tool>", and Title = the bare
// tool name. Those rows are routed under the connection instead of the flat
// Tools list so each connection shows its own tools. Scanned tools whose
// Category is not a connection (an mcp_config server the workspace never
// registered as a connection) stay in the flat list. Pure function — unit-tested.
func classifyCapabilityRows(rows []cerebrotoolpolicy.TableRow, connectionNames map[string]bool) (
	tools []AgentCapabilityTool,
	repos []AgentCapabilityRepo,
	connPerms map[string]map[string]string,
	connTools map[string][]AgentCapabilityTool,
) {
	tools = []AgentCapabilityTool{}
	repos = []AgentCapabilityRepo{}
	connPerms = map[string]map[string]string{}
	connTools = map[string][]AgentCapabilityTool{}

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
			// CEREBRO-PATCH(agent-capabilities-card-connection-nesting): TECH-3642
			// nest a connection's scanned MCP tools under it instead of the flat list.
			if row.Source == capSourceScan && connectionNames[row.Category] {
				connTools[row.Category] = append(connTools[row.Category], capabilityToolFromRow(row))
				continue
			}
			tools = append(tools, capabilityToolFromRow(row))
		}
	}
	return tools, repos, connPerms, connTools
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

// listCapabilityConnections returns ALL workspace connections (enabled and
// disabled) via the connections read seam. Returns nil on any error (or an unset
// seam) so the card still renders.
func (h *Handler) listCapabilityConnections(r *http.Request, workspaceID pgtype.UUID) []cerebroconnections.Connection {
	if h.CapabilityConnections == nil {
		return nil
	}
	conns, err := h.CapabilityConnections.List(r.Context(), workspaceID)
	if err != nil {
		return nil
	}
	return conns
}

// connectionNameSet collapses the connection list to the set of connection
// names, used to recognise which scanned tools belong to a connection.
func connectionNameSet(conns []cerebroconnections.Connection) map[string]bool {
	out := make(map[string]bool, len(conns))
	for _, c := range conns {
		if c.Name != "" {
			out[c.Name] = true
		}
	}
	return out
}

// buildAgentCapabilityConnections turns the workspace connection list into the
// card's connection section: each connection with the endpoints/tools it exposes,
// each MCP tool stamped with the agent's effective permission. The tool list for
// an MCP connection comes from the live scan inventory grouped in connTools (the
// scan→capability bridge keys those tools under the connection name); it falls
// back to the connection's persisted tools/list — which only a manual "Test
// connection" populates — when no scanned rows exist. Auth secrets are never
// read here. Returns an empty slice (never nil) so the card renders.
func buildAgentCapabilityConnections(conns []cerebroconnections.Connection, connPerms map[string]map[string]string, connTools map[string][]AgentCapabilityTool) []AgentCapabilityConnection {
	out := []AgentCapabilityConnection{}
	for _, c := range conns {
		entry := AgentCapabilityConnection{
			Name:        c.Name,
			DisplayName: c.DisplayName,
			Type:        c.Type,
			URL:         c.URL,
			Internal:    c.Internal,
			Enabled:     c.Enabled,
		}
		if scanned := connTools[c.Name]; len(scanned) > 0 {
			for _, t := range scanned {
				name := t.Title
				if name == "" {
					name = t.Key
				}
				perm := t.Permission
				if p, ok := connPerms[c.Name][name]; ok && p != "" {
					perm = p
				}
				entry.Tools = append(entry.Tools, AgentCapabilityConnTool{
					Name:       name,
					Permission: perm,
				})
			}
		} else {
			for _, t := range c.Tools {
				entry.Tools = append(entry.Tools, AgentCapabilityConnTool{
					Name:        t.Name,
					Description: t.Description,
					Permission:  connPerms[c.Name][t.Name],
				})
			}
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
