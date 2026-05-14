package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/util"

	"github.com/jackc/pgx/v5/pgtype"
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
			InputSchema: t.InputSchema(),
		}
	}
	if len(out) > 0 {
		out[len(out)-1].CacheControl = &AnthropicCacheControl{Type: "ephemeral"}
	}
	return out
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

// mvpToolNames lists the tool names granted to Kristian's agent as MVP tools.
var mvpToolNames = []string{
	"get_issue",
	"list_comments",
	"add_comment",
	"list_issues",
	"create_issue",
	"update_issue",
	"assign_issue",
	"web_fetch",
	"firtal_bq_query",
	"gogcli_sheets_write",
}

// SeedKristianTools grants all MVP tools to Kristian's agent. It is idempotent
// — safe to call on every server start.
func SeedKristianTools(ctx context.Context, pool *pgxpool.Pool, kristianAgentID pgtype.UUID) error {
	if !kristianAgentID.Valid {
		return fmt.Errorf("SeedKristianTools: invalid agent UUID")
	}
	for _, name := range mvpToolNames {
		if err := GrantAgentTool(ctx, pool, kristianAgentID, name, nil); err != nil {
			return fmt.Errorf("SeedKristianTools: grant %q: %w", name, err)
		}
	}
	return nil
}
