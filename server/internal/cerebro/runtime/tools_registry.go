package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

// Tool is the interface every in-process tool must implement.
type Tool interface {
	Name() string
	Description() string
	InputSchema() map[string]any
	Call(ctx context.Context, args map[string]any) (string, error)
}

// Registry holds all registered Tool implementations and provides a
// DB-backed lookup of which tools are enabled for a given agent via the
// agent_tool_grant table.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
	db    *pgxpool.Pool
}

// NewRegistry creates a Registry backed by the given connection pool.
func NewRegistry(db *pgxpool.Pool) *Registry {
	return &Registry{
		tools: make(map[string]Tool),
		db:    db,
	}
}

// Register adds a tool implementation to the registry. Existing tools with the
// same name are silently overwritten.
func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
}

func (r *Registry) hasTool(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.tools[name]
	return ok
}

// GetEnabledToolsForAgent queries agent_tool_grant for the given agent and
// returns the subset of registered tools that are both enabled in the DB and
// present in the in-memory registry. On DB error it falls back to an empty
// list (fail-safe: don't give unexpected tools on error).
func (r *Registry) GetEnabledToolsForAgent(ctx context.Context, agentID pgtype.UUID) []Tool {
	if !agentID.Valid || r.db == nil {
		return nil
	}

	rows, err := r.db.Query(ctx,
		`SELECT tool_name FROM agent_tool_grant WHERE agent_id = $1 AND enabled = true`,
		agentID,
	)
	if err != nil {
		slog.Warn("tool registry: agent_tool_grant query failed",
			"agent_id", util.UUIDToString(agentID),
			"error", err,
		)
		return nil
	}
	defer rows.Close()

	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []Tool
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		if t, ok := r.tools[name]; ok {
			out = append(out, t)
		}
	}
	if err := rows.Err(); err != nil {
		slog.Warn("tool registry: scan error",
			"agent_id", util.UUIDToString(agentID),
			"error", err,
		)
	}
	return out
}

// GetCascadeEnabledToolsForAgent resolves the tool list for an agent using
// the cerebro_runtime_tool cascade (JEH-1710 bid 5):
//
//	tool is callable iff
//	  runtime_tool.enabled = true
//	  AND (user has a direct user_grant
//	       OR user is in a group with a group_grant
//	       OR the user is workspace owner/admin)
//	  AND (no override OR override.enabled = true)
//
// If the agent's runtime has no cerebro_runtime_tool rows (the new grant
// system has not been configured for this runtime yet), this falls back to
// the legacy agent_tool_grant path so existing agents keep working until the
// bid-6 migration backfills the new tables.
//
// userID may be invalid (e.g. autopilot runs without an originating user).
// In that case the cascade cannot match the workspace-admin/user-grant arms
// of the rule, so we fall back to the legacy path rather than fail closed —
// otherwise scheduled autopilots would lose every tool overnight when bid 6
// rolls out. Once issue-driven autopilots all carry an original_user_id this
// can be tightened to fail closed.
func (r *Registry) GetCascadeEnabledToolsForAgent(ctx context.Context, cerebro *cerebrodb.Queries, agentID, userID pgtype.UUID) []Tool {
	if cerebro == nil || !agentID.Valid {
		return r.GetEnabledToolsForAgent(ctx, agentID)
	}

	configured, err := cerebro.HasCerebroRuntimeToolsForAgent(ctx, agentID)
	if err != nil {
		slog.Warn("tool registry: cascade configured-check failed",
			"agent_id", util.UUIDToString(agentID),
			"error", err,
		)
		return nil
	}
	if !configured {
		return r.GetEnabledToolsForAgent(ctx, agentID)
	}

	if !userID.Valid {
		slog.Info("tool registry: cascade skipped — no originating user, falling back to legacy grants",
			"agent_id", util.UUIDToString(agentID),
		)
		return r.GetEnabledToolsForAgent(ctx, agentID)
	}

	rows, err := cerebro.ResolveCerebroAgentToolAccess(ctx, cerebrodb.ResolveCerebroAgentToolAccessParams{
		ID:     agentID,
		UserID: userID,
	})
	if err != nil {
		slog.Warn("tool registry: cascade resolution failed",
			"agent_id", util.UUIDToString(agentID),
			"user_id", util.UUIDToString(userID),
			"error", err,
		)
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if _, dup := seen[row.ToolName]; dup {
			// Same tool surfaced via multiple grant arms — only register once.
			continue
		}
		seen[row.ToolName] = struct{}{}
		if t, ok := r.tools[row.ToolName]; ok {
			out = append(out, t)
		}
	}
	return out
}

// Call dispatches a named tool call. Returns an error if the tool is not
// registered.
func (r *Registry) Call(ctx context.Context, toolName string, args map[string]any) (string, error) {
	r.mu.RLock()
	t, ok := r.tools[toolName]
	r.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("tool %q not registered", toolName)
	}
	return t.Call(ctx, args)
}

// ToAnthropicTools converts a slice of Tool implementations to the Anthropic
// wire format. Cache control is added to the last tool so the tool list is
// eligible for prompt caching alongside the system prompt.
func (r *Registry) ToAnthropicTools(tools []Tool) []AnthropicTool {
	out := make([]AnthropicTool, len(tools))
	for i, t := range tools {
		out[i] = AnthropicTool{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: normalizeAnthropicToolSchema(t.InputSchema()), // CEREBRO-PATCH(firtal-gateway-anthropic-tool-schema): strip provider-rejected empty schema fields before Anthropic requests.
		}
	}
	if len(out) > 0 {
		out[len(out)-1].CacheControl = &AnthropicCacheControl{Type: "ephemeral"}
	}
	return out
}

func normalizeAnthropicToolSchema(schema map[string]any) map[string]any {
	return normalizeAnthropicSchemaNode(schema).(map[string]any)
}

func normalizeAnthropicSchemaNode(node any) any {
	switch v := node.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			normalized := normalizeAnthropicSchemaNode(child)
			if key == "required" {
				if values, ok := normalized.([]any); ok && len(values) == 0 {
					continue
				}
				if values, ok := normalized.([]string); ok && len(values) == 0 {
					continue
				}
			}
			out[key] = normalized
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = normalizeAnthropicSchemaNode(child)
		}
		return out
	case []string:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = child
		}
		return out
	default:
		return v
	}
}

// GrantAgentTool upserts an agent_tool_grant row. Pass nil configJSON for no
// configuration. If the grant already exists the enabled flag and config are
// updated.
func GrantAgentTool(ctx context.Context, pool *pgxpool.Pool, agentID pgtype.UUID, toolName string, configJSON []byte) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO agent_tool_grant (agent_id, tool_name, config_json, enabled)
         VALUES ($1, $2, $3, true)
         ON CONFLICT (agent_id, tool_name)
         DO UPDATE SET config_json = EXCLUDED.config_json, enabled = true`,
		agentID, toolName, configJSON,
	)
	return err
}

// ToolMeta holds display metadata for a registered tool. Used by the admin API
// to enrich tool grant responses with names and descriptions without requiring
// a per-task ToolContext.
type ToolMeta struct {
	Name        string
	Description string
	Status      string
}

const (
	ToolStatusImplemented      = "implemented"
	ToolStatusNewlyImplemented = "newly_implemented"
	ToolStatusExcluded         = "explicitly_excluded"
)

var legacyGatewayToolMeta = []ToolMeta{
	{Name: "web_fetch", Description: "Fetch the text content of an allowlisted URL.", Status: ToolStatusImplemented},
	{Name: "firtal_bq_query", Description: "Run a BigQuery SQL query via Firtal Data Registry.", Status: ToolStatusImplemented},
	{Name: "gogcli_sheets_write", Description: "Write data to a Google Sheets spreadsheet range.", Status: ToolStatusImplemented},
}

var multicaMCPToolMatrix = []ToolMeta{
	{Name: "add_attachment", Description: "Upload and attach a file to a Multica issue or comment.", Status: ToolStatusExcluded},
	{Name: "add_comment", Description: "Post a comment on a Multica issue as the calling agent.", Status: ToolStatusImplemented},
	{Name: "add_group_agent", Description: "Allow an agent for a workspace group.", Status: ToolStatusExcluded},
	{Name: "add_group_member", Description: "Add a workspace member to a group.", Status: ToolStatusExcluded},
	{Name: "add_group_runtime", Description: "Allow a runtime for a workspace group.", Status: ToolStatusExcluded},
	{Name: "add_project_group", Description: "Grant a group access to a project.", Status: ToolStatusExcluded},
	{Name: "attach_session", Description: "Attach the current coding session to a Multica issue.", Status: ToolStatusExcluded},
	{Name: "bind_repo", Description: "Bind the current repository to a Multica workspace and project.", Status: ToolStatusExcluded},
	{Name: "complete_work", Description: "Mark attached session work complete with a summary.", Status: ToolStatusExcluded},
	{Name: "create_artifact", Description: "Create a workspace artifact.", Status: ToolStatusExcluded},
	{Name: "create_artifact_folder", Description: "Create an artifact folder.", Status: ToolStatusExcluded},
	{Name: "create_grant", Description: "Create a Persona capability grant.", Status: ToolStatusExcluded},
	{Name: "create_group", Description: "Create a workspace group.", Status: ToolStatusExcluded},
	{Name: "create_issue", Description: "Create a new issue in the current workspace.", Status: ToolStatusImplemented},
	{Name: "create_project", Description: "Create a project in the current workspace.", Status: ToolStatusNewlyImplemented},
	{Name: "credential_audit_log", Description: "List credential governance audit events.", Status: ToolStatusExcluded},
	{Name: "credential_list", Description: "List credential registry entries visible in the workspace.", Status: ToolStatusNewlyImplemented},
	{Name: "credential_policy_get", Description: "Read the credential governance policy.", Status: ToolStatusExcluded},
	{Name: "credential_policy_set", Description: "Update the credential governance policy.", Status: ToolStatusExcluded},
	{Name: "delete_artifact", Description: "Delete an artifact.", Status: ToolStatusExcluded},
	{Name: "delete_artifact_folder", Description: "Delete an artifact folder.", Status: ToolStatusExcluded},
	{Name: "delete_grant", Description: "Revoke a Persona grant.", Status: ToolStatusExcluded},
	{Name: "delete_group", Description: "Delete a workspace group.", Status: ToolStatusExcluded},
	{Name: "evaluate_policy", Description: "Dry-run a Persona permission evaluation.", Status: ToolStatusExcluded},
	{Name: "fork_session", Description: "Fork an attached work session for a subtask.", Status: ToolStatusExcluded},
	{Name: "get_artifact", Description: "Fetch one artifact with metadata and content.", Status: ToolStatusExcluded},
	{Name: "get_grant", Description: "Fetch one Persona grant.", Status: ToolStatusNewlyImplemented},
	{Name: "get_group", Description: "Fetch one workspace group.", Status: ToolStatusExcluded},
	{Name: "get_issue", Description: "Get full details of a Multica issue: title, description, status, priority, and comments.", Status: ToolStatusImplemented},
	{Name: "get_me", Description: "Return the calling agent's ID, name, and workspace context.", Status: ToolStatusImplemented},
	{Name: "list_artifact_folders", Description: "List artifact folders in the workspace.", Status: ToolStatusExcluded},
	{Name: "list_artifacts", Description: "List workspace artifacts.", Status: ToolStatusNewlyImplemented},
	{Name: "list_comments", Description: "List all comments on a Multica issue in chronological order.", Status: ToolStatusImplemented},
	{Name: "list_grant_audit", Description: "List Persona grant audit entries.", Status: ToolStatusExcluded},
	{Name: "list_grants", Description: "List Persona grants in the workspace.", Status: ToolStatusExcluded},
	{Name: "list_group_agents", Description: "List agents allowed for a group.", Status: ToolStatusExcluded},
	{Name: "list_group_capabilities", Description: "List capabilities granted to a group.", Status: ToolStatusExcluded},
	{Name: "list_group_members", Description: "List members of a group.", Status: ToolStatusExcluded},
	{Name: "list_group_runtimes", Description: "List runtimes allowed for a group.", Status: ToolStatusExcluded},
	{Name: "list_groups", Description: "List workspace groups.", Status: ToolStatusNewlyImplemented},
	{Name: "list_issues", Description: "List issues in the current workspace.", Status: ToolStatusImplemented},
	{Name: "list_project_groups", Description: "List groups with access to a project.", Status: ToolStatusExcluded},
	{Name: "list_projects", Description: "List all projects in the current workspace.", Status: ToolStatusImplemented},
	{Name: "list_runtimes", Description: "List agent runtimes in the current workspace.", Status: ToolStatusNewlyImplemented},
	{Name: "move_artifact", Description: "Move an artifact into another folder.", Status: ToolStatusExcluded},
	{Name: "remove_group_agent", Description: "Remove an agent from a group allowlist.", Status: ToolStatusExcluded},
	{Name: "remove_group_capability", Description: "Revoke a capability from a group.", Status: ToolStatusExcluded},
	{Name: "remove_group_member", Description: "Remove a member from a group.", Status: ToolStatusExcluded},
	{Name: "remove_group_runtime", Description: "Remove a runtime from a group allowlist.", Status: ToolStatusExcluded},
	{Name: "remove_project_group", Description: "Remove a group's access to a project.", Status: ToolStatusExcluded},
	{Name: "report_activity", Description: "Report progress activity for an attached work session.", Status: ToolStatusExcluded},
	{Name: "resume_session", Description: "Resume an attached work session.", Status: ToolStatusExcluded},
	{Name: "search_artifacts", Description: "Search artifacts by query text.", Status: ToolStatusNewlyImplemented},
	{Name: "search_issues", Description: "Search issues by text query.", Status: ToolStatusExcluded},
	{Name: "set_artifact_folder", Description: "Set an artifact's folder.", Status: ToolStatusExcluded},
	{Name: "set_group_capability", Description: "Grant a capability to a group.", Status: ToolStatusExcluded},
	{Name: "update_artifact", Description: "Update artifact metadata or content.", Status: ToolStatusExcluded},
	{Name: "update_artifact_folder", Description: "Update an artifact folder.", Status: ToolStatusExcluded},
	{Name: "update_grant", Description: "Update an existing Persona grant.", Status: ToolStatusExcluded},
	{Name: "update_group", Description: "Rename or update a group's description.", Status: ToolStatusExcluded},
	{Name: "update_issue", Description: "Update a Multica issue's status, title, or priority.", Status: ToolStatusImplemented},
	{Name: "update_project", Description: "Update a project's title, description, or status.", Status: ToolStatusExcluded},
}

func MulticaMCPToolMatrix() []ToolMeta {
	out := make([]ToolMeta, len(multicaMCPToolMatrix))
	copy(out, multicaMCPToolMatrix)
	return out
}

// AllBuiltinToolMeta returns display metadata for every tool the admin UI
// should show, including explicit exclusions and non-Multica legacy gateway
// tools that predate the MCP inventory.
func AllBuiltinToolMeta() []ToolMeta {
	out := make([]ToolMeta, 0, len(legacyGatewayToolMeta)+len(multicaMCPToolMatrix))
	out = append(out, legacyGatewayToolMeta...)
	out = append(out, multicaMCPToolMatrix...)
	return out
}

func callableBuiltinToolNames() []string {
	meta := AllBuiltinToolMeta()
	names := make([]string, 0, len(meta))
	for _, item := range meta {
		if item.Status == ToolStatusImplemented || item.Status == ToolStatusNewlyImplemented {
			names = append(names, item.Name)
		}
	}
	return names
}

// SeedKristianTools grants only concrete callable tools to Kristian's agent. It
// is idempotent — safe to call on every server start.
func SeedKristianTools(ctx context.Context, pool *pgxpool.Pool, kristianAgentID pgtype.UUID) error {
	if !kristianAgentID.Valid {
		return fmt.Errorf("SeedKristianTools: invalid agent UUID")
	}
	for _, name := range callableBuiltinToolNames() {
		if err := GrantAgentTool(ctx, pool, kristianAgentID, name, nil); err != nil {
			return fmt.Errorf("SeedKristianTools: grant %q: %w", name, err)
		}
	}
	return nil
}
