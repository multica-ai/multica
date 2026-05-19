// Package runtimetools implements the cerebro runtime-level tool inventory
// and access-control plane (JEH-1710).
//
// Workspace admins use this to:
//
//   - see every tool a runtime exposes — both cloud-side built-ins and tools
//     discovered through an MCP server's tools/list — in one unified list;
//   - toggle each tool on/off at the runtime level;
//   - decide which groups and which individual users may invoke each tool;
//   - override the runtime default for an individual agent (rarely needed).
//
// Cascading: an agent inherits the runtime's enabled tools by default. The
// per-agent override table can disable or force-enable a single tool for one
// agent.
//
// Access rule (default deny):
//
//	tool callable iff
//	  runtime_tool.enabled = true
//	  AND (user has a direct user_grant
//	       OR user is in a group with group_grant
//	       OR user is workspace owner/admin)
//	  AND (no agent override OR override.enabled = true)
//
// The handler layer talks to this service through interfaces (see
// runtime_tools_cerebro.go), matching the runtime-account / persona-mask
// seam pattern.
package runtimetools

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// Source distinguishes the two kinds of tools the registry tracks.
const (
	SourceCloud = "cloud"
	SourceMCP   = "mcp"
)

// Errors returned to handlers so they can map to HTTP status codes without
// caring about the underlying SQL details.
var (
	ErrNotFound       = errors.New("runtime tool not found")
	ErrInvalidSource  = errors.New("tool source must be 'cloud' or 'mcp'")
	ErrMCPNeedsServer = errors.New("mcp tool requires mcp_server_name")
)

// Service exposes the runtime-tool admin operations.
type Service struct {
	pool *pgxpool.Pool
	q    *cerebrodb.Queries
}

// New constructs a Service backed by the given pool.
func New(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, q: cerebrodb.New(pool)}
}

// Tool is the unified tool registry row returned to admin callers.
type Tool struct {
	ID            pgtype.UUID
	RuntimeID     pgtype.UUID
	Name          string
	Source        string
	MCPServerName string
	Description   string
	SchemaJSON    []byte
	Enabled       bool
	LastScannedAt pgtype.Timestamptz
	CreatedAt     pgtype.Timestamptz
	UpdatedAt     pgtype.Timestamptz
}

// GroupGrant is a row in the runtime-tool group grant whitelist.
type GroupGrant struct {
	RuntimeID pgtype.UUID
	ToolName  string
	GroupID   pgtype.UUID
	GroupName string
	GrantedBy pgtype.UUID
	GrantedAt pgtype.Timestamptz
}

// UserGrant is a row in the runtime-tool user grant whitelist.
type UserGrant struct {
	RuntimeID     pgtype.UUID
	ToolName      string
	UserID        pgtype.UUID
	UserName      string
	UserEmail     string
	UserAvatarURL string
	GrantedBy     pgtype.UUID
	GrantedAt     pgtype.Timestamptz
}

// AgentOverride is a per-agent override of the runtime default.
type AgentOverride struct {
	AgentID   pgtype.UUID
	ToolName  string
	Enabled   bool
	UpdatedBy pgtype.UUID
	UpdatedAt pgtype.Timestamptz
}

// ResolvedTool is one entry in the resolved tool list for a given
// (agent, user) pair.
type ResolvedTool struct {
	Name          string
	Source        string
	MCPServerName string
}

// UpsertToolInput is the argument bundle for UpsertTool. Keeping it as a
// struct keeps callers explicit instead of relying on positional args.
type UpsertToolInput struct {
	RuntimeID     pgtype.UUID
	ToolName      string
	Source        string
	MCPServerName string
	Description   string
	SchemaJSON    []byte
	// Enabled, when non-nil, forces the enabled flag. Pass nil to keep the
	// current value on conflict (scan path).
	Enabled       *bool
	LastScannedAt pgtype.Timestamptz
}

// ListTools returns every registered tool for the runtime, ordered by source
// then name.
func (s *Service) ListTools(ctx context.Context, runtimeID pgtype.UUID) ([]Tool, error) {
	rows, err := s.q.ListCerebroRuntimeTools(ctx, runtimeID)
	if err != nil {
		return nil, fmt.Errorf("list runtime tools: %w", err)
	}
	out := make([]Tool, 0, len(rows))
	for _, r := range rows {
		out = append(out, toolFromRow(r))
	}
	return out, nil
}

// UpsertTool inserts a tool row or updates the metadata on conflict.
//
// source must be SourceCloud or SourceMCP. For SourceMCP, mcpServerName must
// be non-empty.
func (s *Service) UpsertTool(ctx context.Context, in UpsertToolInput) (Tool, error) {
	if in.Source != SourceCloud && in.Source != SourceMCP {
		return Tool{}, ErrInvalidSource
	}
	if in.Source == SourceMCP && in.MCPServerName == "" {
		return Tool{}, ErrMCPNeedsServer
	}

	params := cerebrodb.UpsertCerebroRuntimeToolParams{
		RuntimeID:     in.RuntimeID,
		ToolName:      in.ToolName,
		Source:        in.Source,
		LastScannedAt: in.LastScannedAt,
		SchemaJson:    in.SchemaJSON,
	}
	if in.MCPServerName != "" {
		params.McpServerName = pgtype.Text{String: in.MCPServerName, Valid: true}
	}
	if in.Description != "" {
		params.Description = pgtype.Text{String: in.Description, Valid: true}
	}
	if in.Enabled != nil {
		params.Enabled = pgtype.Bool{Bool: *in.Enabled, Valid: true}
	}

	row, err := s.q.UpsertCerebroRuntimeTool(ctx, params)
	if err != nil {
		return Tool{}, fmt.Errorf("upsert runtime tool: %w", err)
	}
	return toolFromRow(row), nil
}

// SetEnabled flips the enabled flag for a (runtime, tool) row.
func (s *Service) SetEnabled(ctx context.Context, runtimeID pgtype.UUID, toolName string, enabled bool) (Tool, error) {
	row, err := s.q.SetCerebroRuntimeToolEnabled(ctx, cerebrodb.SetCerebroRuntimeToolEnabledParams{
		RuntimeID: runtimeID,
		ToolName:  toolName,
		Enabled:   enabled,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Tool{}, ErrNotFound
		}
		return Tool{}, fmt.Errorf("set runtime tool enabled: %w", err)
	}
	return toolFromRow(row), nil
}

// ListGroupGrants returns every group grant for a runtime, ordered by tool
// name then group name.
func (s *Service) ListGroupGrants(ctx context.Context, runtimeID pgtype.UUID) ([]GroupGrant, error) {
	rows, err := s.q.ListCerebroRuntimeToolGroupGrants(ctx, runtimeID)
	if err != nil {
		return nil, fmt.Errorf("list group grants: %w", err)
	}
	out := make([]GroupGrant, 0, len(rows))
	for _, r := range rows {
		out = append(out, GroupGrant{
			RuntimeID: r.RuntimeID,
			ToolName:  r.ToolName,
			GroupID:   r.GroupID,
			GroupName: r.GroupName,
			GrantedBy: r.GrantedBy,
			GrantedAt: r.GrantedAt,
		})
	}
	return out, nil
}

// AddGroupGrant whitelists a group for (runtime, tool). Idempotent.
func (s *Service) AddGroupGrant(ctx context.Context, runtimeID pgtype.UUID, toolName string, groupID, grantedBy pgtype.UUID) error {
	if err := s.q.AddCerebroRuntimeToolGroupGrant(ctx, cerebrodb.AddCerebroRuntimeToolGroupGrantParams{
		RuntimeID: runtimeID,
		ToolName:  toolName,
		GroupID:   groupID,
		GrantedBy: grantedBy,
	}); err != nil {
		return fmt.Errorf("add group grant: %w", err)
	}
	return nil
}

// RemoveGroupGrant detaches a group from (runtime, tool). Idempotent.
func (s *Service) RemoveGroupGrant(ctx context.Context, runtimeID pgtype.UUID, toolName string, groupID pgtype.UUID) error {
	if err := s.q.RemoveCerebroRuntimeToolGroupGrant(ctx, cerebrodb.RemoveCerebroRuntimeToolGroupGrantParams{
		RuntimeID: runtimeID,
		ToolName:  toolName,
		GroupID:   groupID,
	}); err != nil {
		return fmt.Errorf("remove group grant: %w", err)
	}
	return nil
}

// ListUserGrants returns every user grant for a runtime.
func (s *Service) ListUserGrants(ctx context.Context, runtimeID pgtype.UUID) ([]UserGrant, error) {
	rows, err := s.q.ListCerebroRuntimeToolUserGrants(ctx, runtimeID)
	if err != nil {
		return nil, fmt.Errorf("list user grants: %w", err)
	}
	out := make([]UserGrant, 0, len(rows))
	for _, r := range rows {
		out = append(out, UserGrant{
			RuntimeID:     r.RuntimeID,
			ToolName:      r.ToolName,
			UserID:        r.UserID,
			UserName:      r.UserName,
			UserEmail:     r.UserEmail,
			UserAvatarURL: textValue(r.UserAvatarUrl),
			GrantedBy:     r.GrantedBy,
			GrantedAt:     r.GrantedAt,
		})
	}
	return out, nil
}

// AddUserGrant whitelists a user for (runtime, tool). Idempotent.
func (s *Service) AddUserGrant(ctx context.Context, runtimeID pgtype.UUID, toolName string, userID, grantedBy pgtype.UUID) error {
	if err := s.q.AddCerebroRuntimeToolUserGrant(ctx, cerebrodb.AddCerebroRuntimeToolUserGrantParams{
		RuntimeID: runtimeID,
		ToolName:  toolName,
		UserID:    userID,
		GrantedBy: grantedBy,
	}); err != nil {
		return fmt.Errorf("add user grant: %w", err)
	}
	return nil
}

// RemoveUserGrant detaches a user from (runtime, tool). Idempotent.
func (s *Service) RemoveUserGrant(ctx context.Context, runtimeID pgtype.UUID, toolName string, userID pgtype.UUID) error {
	if err := s.q.RemoveCerebroRuntimeToolUserGrant(ctx, cerebrodb.RemoveCerebroRuntimeToolUserGrantParams{
		RuntimeID: runtimeID,
		ToolName:  toolName,
		UserID:    userID,
	}); err != nil {
		return fmt.Errorf("remove user grant: %w", err)
	}
	return nil
}

// ListAgentOverrides returns every override for one agent.
func (s *Service) ListAgentOverrides(ctx context.Context, agentID pgtype.UUID) ([]AgentOverride, error) {
	rows, err := s.q.ListCerebroAgentRuntimeToolOverrides(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("list agent overrides: %w", err)
	}
	out := make([]AgentOverride, 0, len(rows))
	for _, r := range rows {
		out = append(out, AgentOverride{
			AgentID:   r.AgentID,
			ToolName:  r.ToolName,
			Enabled:   r.Enabled,
			UpdatedBy: r.UpdatedBy,
			UpdatedAt: r.UpdatedAt,
		})
	}
	return out, nil
}

// UpsertAgentOverride sets a per-agent override for a tool.
func (s *Service) UpsertAgentOverride(ctx context.Context, agentID pgtype.UUID, toolName string, enabled bool, updatedBy pgtype.UUID) (AgentOverride, error) {
	row, err := s.q.UpsertCerebroAgentRuntimeToolOverride(ctx, cerebrodb.UpsertCerebroAgentRuntimeToolOverrideParams{
		AgentID:   agentID,
		ToolName:  toolName,
		Enabled:   enabled,
		UpdatedBy: updatedBy,
	})
	if err != nil {
		return AgentOverride{}, fmt.Errorf("upsert agent override: %w", err)
	}
	return AgentOverride{
		AgentID:   row.AgentID,
		ToolName:  row.ToolName,
		Enabled:   row.Enabled,
		UpdatedBy: row.UpdatedBy,
		UpdatedAt: row.UpdatedAt,
	}, nil
}

// DeleteAgentOverride drops the override row so the agent falls back to the
// runtime default.
func (s *Service) DeleteAgentOverride(ctx context.Context, agentID pgtype.UUID, toolName string) error {
	if err := s.q.DeleteCerebroAgentRuntimeToolOverride(ctx, cerebrodb.DeleteCerebroAgentRuntimeToolOverrideParams{
		AgentID:  agentID,
		ToolName: toolName,
	}); err != nil {
		return fmt.Errorf("delete agent override: %w", err)
	}
	return nil
}

// ResolveAccess returns the set of tools the (agent, user) pair may invoke,
// applying the default-deny rule encoded in ResolveCerebroAgentToolAccess.
//
// Callers wire this into the cloud-side tool dispatcher so an agent only
// sees the tools its invoking user is granted access to.
func (s *Service) ResolveAccess(ctx context.Context, agentID, userID pgtype.UUID) ([]ResolvedTool, error) {
	rows, err := s.q.ResolveCerebroAgentToolAccess(ctx, cerebrodb.ResolveCerebroAgentToolAccessParams{
		ID:     agentID,
		UserID: userID,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve agent tool access: %w", err)
	}
	out := make([]ResolvedTool, 0, len(rows))
	for _, r := range rows {
		out = append(out, ResolvedTool{
			Name:          r.ToolName,
			Source:        r.Source,
			MCPServerName: textValue(r.McpServerName),
		})
	}
	return out, nil
}

func toolFromRow(r cerebrodb.CerebroRuntimeTool) Tool {
	return Tool{
		ID:            r.ID,
		RuntimeID:     r.RuntimeID,
		Name:          r.ToolName,
		Source:        r.Source,
		MCPServerName: textValue(r.McpServerName),
		Description:   textValue(r.Description),
		SchemaJSON:    r.SchemaJson,
		Enabled:       r.Enabled,
		LastScannedAt: r.LastScannedAt,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	}
}

func textValue(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return t.String
}
