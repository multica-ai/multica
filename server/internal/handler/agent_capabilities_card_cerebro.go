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
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebroconnections "github.com/multica-ai/multica/server/internal/cerebro/connections"
	cerebrocredentialpolicy "github.com/multica-ai/multica/server/internal/cerebro/credentialpolicy"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	cerebrotoolpolicy "github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// CEREBRO-PATCH(agent-capabilities-secret-status): TECH-3738 Bid A — every
// secret-bearing section reports an explicit status so an empty list never
// silently reads as "fully covered". "we know there is nothing" must look
// different from "we could not determine it".
const (
	capStatusKnown         = "known"          // read successfully; the list is authoritative
	capStatusUnknown       = "unknown"        // could not determine (lookup failed / runtime unmapped)
	capStatusStale         = "stale"          // last scan is old (reserved for Bid B/C observed access)
	capStatusNotConfigured = "not_configured" // determined, and there is genuinely nothing
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
// Source distinguishes the two layers the runtime actually loads at claim time
// (see daemon.go): workspace-bound skills (the same set `multica agent get`
// lists) and the platform built-in skills every agent additionally receives.
// Labelling the source is why the card's skill total can exceed `agent get`'s
// count without the two views disagreeing.
type AgentCapabilitySkill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"` // capSkillSourceWorkspace | capSkillSourceBuiltin
}

const (
	// capSkillSourceWorkspace marks a skill bound to this agent in the
	// workspace (agent_skill join) — what `multica agent get` returns.
	capSkillSourceWorkspace = "workspace"
	// capSkillSourceBuiltin marks a platform built-in skill that every agent
	// receives on top of its workspace skills (TaskService.BuiltinSkills).
	capSkillSourceBuiltin = "builtin"
)

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
//
// Permission is the per-actor credential-grant verdict (FIR-1479) pinned directly
// on this agent for the credential box of the same name (credential_key). It is
// populated only when the cerebro_credentials_per_actor flag is on for the
// workspace, and only for boxes the agent layer has an explicit setting on;
// otherwise it is empty (omitted). It mirrors AgentCapabilityTool.Permission.
type AgentCapabilityCredential struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Permission  string `json:"permission,omitempty"` // allow | deny | inherit | "" (flag off / no row)
}

// AgentCapabilityInfisicalSecret names one Infisical folder the agent's runtime
// may read. Only the environment + path are shown — never the secret values.
type AgentCapabilityInfisicalSecret struct {
	Environment string `json:"environment"`
	Path        string `json:"path"`
}

// CEREBRO-PATCH(agent-capabilities-secret-set): TECH-3738 Bid A — a group of
// secrets the agent can reach (its own custom_env, or the secret bindings it
// inherits from its runtime). NAMES ONLY, never values, and the names are
// withheld (Redacted=true, Names empty) from any caller who is not a workspace
// owner/admin — including agents reading via CLI/MCP — mirroring the owner/admin
// gate on the env-management endpoint. Count is always populated so a
// non-privileged caller still sees how many secrets exist.
type AgentCapabilitySecretSet struct {
	Source    string   `json:"source"`               // agent_custom_env | runtime
	Status    string   `json:"status"`               // known | unknown | stale | not_configured
	Count     int      `json:"count"`                // number of secrets, always populated
	Names     []string `json:"names"`                // populated only for owner/admin callers
	Redacted  bool     `json:"redacted"`             // true when names were withheld from this caller
	RuntimeID string   `json:"runtime_id,omitempty"` // set when Source=runtime
}

// CEREBRO-PATCH(agent-capabilities-observed-tool): TECH-3738 Bid B — one tool the
// agent ACTUALLY invoked in its recent runs, with how often, when last, and how
// that observed use lines up against its declared policy. Status is the
// declared-vs-observed verdict: allowed (used something it may use),
// needs_approval (used an ask-gated tool), blocked (used a denied tool — drift),
// unmapped (used a tool with no policy row on record — drift, we cannot account
// for it). Drift is the security signal: observed access the declared policy does
// not sanction.
type AgentCapabilityObservedTool struct {
	Name       string `json:"name"`
	Uses       int64  `json:"uses"`
	LastUsed   string `json:"last_used,omitempty"`  // RFC3339, empty if unknown
	Permission string `json:"permission,omitempty"` // allow | ask | deny | "" (no row)
	Status     string `json:"status"`               // allowed | needs_approval | blocked | unmapped
	Drift      bool   `json:"drift"`
}

// AgentCapabilityObservedAccess is what the agent was OBSERVED to use recently
// (Bid B), distinct from the declared layers above. It covers tools only — the
// one runtime-usage signal recorded today (task_message.tool); it never claims
// observed secret use, which is not recorded anywhere. Status mirrors the
// secret-set discipline: known (we have run data), not_configured (the agent
// logged no tool use in the window — genuinely nothing), unknown (lookup failed).
type AgentCapabilityObservedAccess struct {
	Status     string                        `json:"status"`
	WindowDays int                           `json:"window_days"`
	TaskCount  int64                         `json:"task_count"`
	Tools      []AgentCapabilityObservedTool `json:"tools"`
	DriftCount int                           `json:"drift_count"`
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
	// CEREBRO-PATCH(agent-capabilities-secret-sets): TECH-3738 Bid A — the two
	// previously-hidden secret layers: the agent's own custom_env and the
	// secret bindings it inherits from its runtime. Names are owner/admin-only.
	AgentSecrets   AgentCapabilitySecretSet `json:"agent_secrets"`
	RuntimeSecrets AgentCapabilitySecretSet `json:"runtime_secrets"`
	// CEREBRO-PATCH(agent-capabilities-observed-access): TECH-3738 Bid B — tools
	// the agent was observed to actually use recently, each compared against its
	// declared policy so undeclared/blocked use (drift) is visible on the card.
	ObservedAccess AgentCapabilityObservedAccess `json:"observed_access"`
	Limits         AgentCapabilityLimits         `json:"limits"`
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

	// CAN — skills the agent loads. Two layers, mirroring exactly what the
	// runtime assembles at claim time (daemon.go: LoadAgentSkills +
	// BuiltinSkills): first the workspace-bound skills (agent_skill join — the
	// same set `multica agent get` lists), then the platform built-in skills
	// every agent additionally receives. Both are tagged with Source so a
	// reviewer can see the full loaded set and understand why the total exceeds
	// the workspace-only count shown by the CLI.
	if skills, err := h.Queries.ListAgentSkillSummaries(r.Context(), agent.ID); err == nil {
		for _, s := range skills {
			out.Skills = append(out.Skills, AgentCapabilitySkill{
				ID:          uuidToString(s.ID),
				Name:        s.Name,
				Description: s.Description,
				Source:      capSkillSourceWorkspace,
			})
		}
	}
	if h.TaskService != nil {
		for _, b := range h.TaskService.BuiltinSkills() {
			out.Skills = append(out.Skills, AgentCapabilitySkill{
				Name:        b.Name,
				Description: builtinSkillCardDescription(b.Description, b.Content),
				Source:      capSkillSourceBuiltin,
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

	// ACCESS — stamp each bound credential with its per-actor grant verdict
	// (FIR-1479). Mirrors how AgentCapabilityTool.Permission is filled from the
	// tool-policy table, but reads the credential-policy agent layer. Gated: when
	// cerebro_credentials_per_actor is off, credentials are not yet a live
	// permission type, so Permission stays empty and the card looks unchanged.
	h.stampCredentialPermissions(r, agent.WorkspaceID, agent.ID, out.Credentials)

	// ACCESS — Infisical folders this agent may read (paths only).
	if folders, err := h.listAgentInfisicalFolders(r, agent.ID); err == nil {
		for _, f := range folders {
			out.InfisicalSecrets = append(out.InfisicalSecrets, AgentCapabilityInfisicalSecret{
				Environment: f.Environment,
				Path:        f.SecretPath,
			})
		}
	}

	// ACCESS — the two previously-hidden secret layers (TECH-3738 Bid A): the
	// agent's own custom_env and the secret bindings inherited from its runtime.
	// Names are revealed only to workspace owner/admin; everyone else (including
	// agents via CLI/MCP) sees a redacted count, mirroring the env endpoint gate.
	reveal := h.mayRevealAgentSecretNames(r, agent.WorkspaceID)
	out.AgentSecrets = buildAgentSecretSet(agent, reveal)
	out.RuntimeSecrets = h.buildRuntimeSecretSet(r, agent, reveal)

	// OBSERVED (TECH-3738 Bid B) — the tools the agent actually invoked in its
	// recent runs, each compared against the declared policy rows above so
	// blocked/unmapped use surfaces as drift. Tools only: it is the one
	// runtime-usage signal recorded; observed secret use is not tracked anywhere.
	out.ObservedAccess = h.buildObservedAccess(r, agent.ID, rows)

	// LIMITS — sandbox policy + MCP server surface.
	out.Limits = buildAgentCapabilityLimits(agent.RuntimeConfig, agent.McpConfig)

	writeJSON(w, http.StatusOK, out)
}

// stampCredentialPermissions fills the Permission field on each bound credential
// with the per-actor grant pinned directly on this agent (FIR-1479 agent layer),
// matching credential box by name (credential_key). It is a no-op unless the
// cerebro_credentials_per_actor flag is on for the workspace — the same gate the
// credential-grant write endpoints enforce — so the card stays unchanged while
// the feature is dark. Any lookup error leaves Permission empty (the card still
// renders); the verdict is advisory display, never an access decision.
func (h *Handler) stampCredentialPermissions(r *http.Request, workspaceID, agentID pgtype.UUID, creds []AgentCapabilityCredential) {
	if len(creds) == 0 || h.CerebroQueries == nil {
		return
	}
	if !h.credentialsPerActorEnabled(r, workspaceID) {
		return
	}
	store := cerebrocredentialpolicy.NewStoreFromQueries(h.CerebroQueries)
	settings, err := store.ListForSubject(r.Context(), workspaceID, cerebrocredentialpolicy.LayerAgent, agentID)
	if err != nil {
		return
	}
	perms := credentialPermissionLookup(settings)
	for i := range creds {
		if p, ok := perms[creds[i].Name]; ok {
			creds[i].Permission = p
		}
	}
}

// credentialPermissionLookup indexes the agent-layer explicit settings by
// credential_key so each bound credential can be stamped in one pass.
func credentialPermissionLookup(settings []cerebrocredentialpolicy.SubjectSetting) map[string]string {
	m := make(map[string]string, len(settings))
	for _, s := range settings {
		m[s.CredentialKey] = string(s.Setting)
	}
	return m
}

// builtinSkillCardDescription resolves a one-line description for a built-in
// skill shown on the card. BuiltinSkills() leaves Description empty, so fall
// back to the `description:` field in the SKILL.md YAML frontmatter (the
// authoritative one-liner). Returns "" when neither is available rather than
// inventing copy.
func builtinSkillCardDescription(desc, content string) string {
	if d := strings.TrimSpace(desc); d != "" {
		return d
	}
	const marker = "---"
	body := content
	if strings.HasPrefix(strings.TrimSpace(body), marker) {
		// Scan only the frontmatter block (between the first two `---` fences).
		trimmed := strings.TrimSpace(body)
		rest := strings.TrimPrefix(trimmed, marker)
		if end := strings.Index(rest, marker); end >= 0 {
			rest = rest[:end]
		}
		for _, line := range strings.Split(rest, "\n") {
			if after, ok := strings.CutPrefix(strings.TrimSpace(line), "description:"); ok {
				return strings.TrimSpace(after)
			}
		}
	}
	return ""
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

// CEREBRO-PATCH(agent-capabilities-secret-reveal): TECH-3738 Bid A — decide
// whether this caller may see secret NAMES on the card. The capabilities card
// is reachable by any workspace member and by agents via CLI/MCP (loadAgentForUser
// does not gate on role), but the dedicated env-management endpoint is owner/admin
// only and rejects agent actors outright. To avoid widening secret-name exposure
// beyond that gate, names are revealed only to workspace owner/admin members;
// agents and lower-privileged members get a redacted count instead.
func (h *Handler) mayRevealAgentSecretNames(r *http.Request, workspaceID pgtype.UUID) bool {
	wsID := uuidToString(workspaceID)
	userID := requestUserID(r)
	if actorType, _ := h.resolveActor(r, userID, wsID); actorType != "member" {
		return false
	}
	member, err := h.getWorkspaceMember(r.Context(), userID, wsID)
	if err != nil {
		return false
	}
	return roleAllowed(member.Role, "owner", "admin")
}

// buildAgentSecretSet reports the agent's own custom_env secrets as names-only
// (never values). custom_env is always readable, so the status is binary:
// known when there is at least one key, not_configured when empty.
func buildAgentSecretSet(agent db.Agent, reveal bool) AgentCapabilitySecretSet {
	names := sortedKeys(unmarshalCustomEnv(agent))
	set := AgentCapabilitySecretSet{Source: "agent_custom_env", Count: len(names), Names: []string{}}
	if len(names) == 0 {
		set.Status = capStatusNotConfigured
		return set
	}
	set.Status = capStatusKnown
	if reveal {
		set.Names = names
	} else {
		set.Redacted = true
	}
	return set
}

// buildRuntimeSecretSet reports the secret bindings the agent inherits from the
// runtime it runs on. It reads the agent's actual runtime row (not just the
// static provider registry) and normalises it, so the card shows the runtime the
// agent really uses; the registry is the fallback baked into normalizedRuntimeCapabilities.
// A missing/unreadable runtime yields status=unknown — the card never claims
// "no secrets" when it simply could not look them up.
func (h *Handler) buildRuntimeSecretSet(r *http.Request, agent db.Agent, reveal bool) AgentCapabilitySecretSet {
	rt, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
		ID:          agent.RuntimeID,
		WorkspaceID: agent.WorkspaceID,
	})
	if err != nil {
		return AgentCapabilitySecretSet{
			Source:    "runtime",
			Status:    capStatusUnknown,
			Names:     []string{},
			RuntimeID: uuidToString(agent.RuntimeID),
		}
	}
	caps := normalizedRuntimeCapabilities(rt.Provider, rt.Capabilities, rt.ToolsConfig)
	return runtimeSecretSetFromCaps(caps, uuidToString(rt.ID), reveal)
}

// runtimeSecretSetFromCaps is the pure core of buildRuntimeSecretSet (unit-tested
// without a DB). An "unmapped" discovery method means the scan could not map the
// runtime, so the absence of bindings is unknown rather than confirmed-empty.
func runtimeSecretSetFromCaps(caps map[string]any, runtimeID string, reveal bool) AgentCapabilitySecretSet {
	bindings := anyStringSlice(caps["secret_bindings"])
	sort.Strings(bindings)
	method, _ := caps["discovery_method"].(string)
	set := AgentCapabilitySecretSet{Source: "runtime", Count: len(bindings), Names: []string{}, RuntimeID: runtimeID}
	switch {
	case method == "unmapped":
		set.Status = capStatusUnknown
	case len(bindings) == 0:
		set.Status = capStatusNotConfigured
	default:
		set.Status = capStatusKnown
	}
	if len(bindings) > 0 {
		if reveal {
			set.Names = bindings
		} else {
			set.Redacted = true
		}
	}
	return set
}

// observedAccessWindowDays is the look-back window for observed tool usage. 30
// days is long enough to cover an agent's recent working rhythm without drowning
// the card in tools from a one-off task months ago.
const observedAccessWindowDays = 30

// Observed-tool status verdicts: how an observed (actually-used) tool lines up
// against the agent's declared policy. blocked/unmapped are drift.
const (
	observedStatusAllowed       = "allowed"        // used, and the policy allows it
	observedStatusNeedsApproval = "needs_approval" // used, and the policy asks first
	observedStatusBlocked       = "blocked"        // used, but the policy denies it — drift
	observedStatusUnmapped      = "unmapped"       // used, but no policy row on record — drift
)

// buildObservedAccess reads the tools the agent actually invoked in the recent
// window (task_message.tool aggregated per tool) and compares each against the
// declared policy rows to flag drift. A missing CerebroQueries handle or a failed
// lookup yields status=unknown — the card never claims "nothing observed" when it
// simply could not look it up.
func (h *Handler) buildObservedAccess(r *http.Request, agentID pgtype.UUID, rows []cerebrotoolpolicy.TableRow) AgentCapabilityObservedAccess {
	out := AgentCapabilityObservedAccess{
		Status:     capStatusUnknown,
		WindowDays: observedAccessWindowDays,
		Tools:      []AgentCapabilityObservedTool{},
	}
	if h.CerebroQueries == nil {
		return out
	}
	usage, err := h.CerebroQueries.ListAgentObservedToolUsage(r.Context(), cerebrodb.ListAgentObservedToolUsageParams{
		AgentID:    agentID,
		WindowDays: observedAccessWindowDays,
	})
	if err != nil {
		return out
	}
	taskCount, err := h.CerebroQueries.CountAgentTasksInWindow(r.Context(), cerebrodb.CountAgentTasksInWindowParams{
		AgentID:    agentID,
		WindowDays: observedAccessWindowDays,
	})
	if err != nil {
		return out
	}
	return observedAccessFromUsage(usage, taskCount, permissionLookupFromRows(rows), observedAccessWindowDays)
}

// permissionLookupFromRows indexes the declared policy rows by tool name so an
// observed tool ("Bash") can be matched to its effective permission regardless of
// whether the row carries the display title ("Bash") or the key ("bash"). Keys
// are lower-cased on both sides so the runtime's tool name matches the catalog.
func permissionLookupFromRows(rows []cerebrotoolpolicy.TableRow) map[string]string {
	perm := make(map[string]string, len(rows)*2)
	for _, row := range rows {
		setting := string(row.Effective.Setting)
		if setting == "" {
			continue
		}
		if row.Title != "" {
			perm[strings.ToLower(row.Title)] = setting
		}
		if row.ToolKey != "" {
			perm[strings.ToLower(row.ToolKey)] = setting
		}
	}
	return perm
}

// observedAccessFromUsage is the pure core of buildObservedAccess (unit-tested
// without a DB). taskCount distinguishes "the agent ran nothing recently"
// (not_configured) from "it ran but every message was tool-less" — both yield an
// empty tool list, but only a genuine zero-activity agent is not_configured.
func observedAccessFromUsage(usage []cerebrodb.ListAgentObservedToolUsageRow, taskCount int64, permByName map[string]string, windowDays int) AgentCapabilityObservedAccess {
	out := AgentCapabilityObservedAccess{
		WindowDays: windowDays,
		TaskCount:  taskCount,
		Tools:      []AgentCapabilityObservedTool{},
	}
	for _, u := range usage {
		perm, hasRow := permByName[strings.ToLower(u.Tool)]
		status, drift := observedToolStatus(perm, hasRow)
		tool := AgentCapabilityObservedTool{
			Name:       u.Tool,
			Uses:       u.Uses,
			Permission: perm,
			Status:     status,
			Drift:      drift,
		}
		if u.LastUsed.Valid {
			tool.LastUsed = u.LastUsed.Time.UTC().Format(time.RFC3339)
		}
		if drift {
			out.DriftCount++
		}
		out.Tools = append(out.Tools, tool)
	}
	switch {
	case len(out.Tools) > 0:
		out.Status = capStatusKnown
	case taskCount == 0:
		out.Status = capStatusNotConfigured
	default:
		// Ran tasks but logged no tool calls — we know there is nothing to show.
		out.Status = capStatusNotConfigured
	}
	return out
}

// observedToolStatus maps an observed tool's declared permission to its
// observed-vs-declared verdict and whether it is drift. A tool used with no
// policy row (hasRow=false) is unmapped drift: we observed access the declared
// model does not account for. A used-but-denied tool is the strongest drift.
func observedToolStatus(permission string, hasRow bool) (status string, drift bool) {
	if !hasRow {
		return observedStatusUnmapped, true
	}
	switch permission {
	case "allow":
		return observedStatusAllowed, false
	case "ask":
		return observedStatusNeedsApproval, false
	case "deny":
		return observedStatusBlocked, true
	default:
		return observedStatusUnmapped, true
	}
}
