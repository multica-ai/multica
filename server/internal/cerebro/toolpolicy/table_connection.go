package toolpolicy

// table_connection.go is the per-tool half of the admin table for workspace
// connections (TECH-3156).
//
// table.go emits one capability-wide row per connection ("connection:<name>",
// resource_pattern = ''). That row controls the whole connection. But a single
// MCP connection exposes many underlying tools (customer-service alone exposes
// ~23: draft_reply, lookup_order, search_knowledge, …) and admins need to allow
// or deny each one individually.
//
// So — exactly like repos (table_repo.go) — we emit, per connection, one extra
// row per discovered tool carrying a non-empty resource_pattern (the tool name)
// and that (connection, tool) cell's explicit per-layer settings + resolved
// Effective verdict. The connection's display name is the row Category so the UI
// can group a connection's tools under it. The five-level inheritance chain is
// reused unchanged: a per-tool row can only tighten the connection-wide row.
//
// The tool universe is read from workspace_connection.tools, which the "Test
// connection" call persists from the MCP server's tools/list result.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// pgUndefinedTable is the Postgres SQLSTATE for "relation does not exist". The
// workspace_connection table ships with a cerebro migration; if a deployment is
// mid-migration (or connections are simply not provisioned yet) we treat it as
// "no connections" rather than failing the whole permissions table.
const pgUndefinedTable = "42P01"

func isUndefinedTable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUndefinedTable
}

// connectionToolKeyPrefix mirrors connections.CapabilityKey ("connection:<name>")
// without importing the connections package — toolpolicy stays dependency-free of
// the feature packages whose policy it resolves.
const connectionToolKeyPrefix = "connection:"

// connectionToolSource labels per-tool rows for MCP (mcp_http) connections, and
// connectionEndpointSource labels per-endpoint+method rows for REST (api)
// connections. Both carry a non-empty resource_pattern (the tool name, or
// "<METHOD> <path>"), so the UI separates them from the capability-wide
// connection row and from per-repo rows. Only MCP tools are runtime-enforced via
// --disallowedTools today; API rows are config-only (CRUD control) until an
// API-call enforcement path is designed — see DeniedConnectionTools.
const (
	connectionToolSource     = "connection-tool"
	connectionEndpointSource = "connection-endpoint"
)

// connectionTool is one tool discovered on a connection, persisted on the
// workspace_connection.tools JSON array.
type connectionTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// endpointPermission mirrors connections.EndpointPermission: one REST path and
// the HTTP methods configured on it. Used to synthesize per-endpoint+method rows
// for API connections.
type endpointPermission struct {
	Path    string   `json:"path"`
	Methods []string `json:"methods"`
}

// connectionRow pairs a connection's policy key with the human label its tools
// group under (the connection display name), the connection kind ("mcp_http" or
// "api"), and the per-tool rows (MCP tools, or synthesized "<METHOD> <path>"
// entries for API endpoints).
type connectionRow struct {
	name        string
	displayName string
	kind        string
	tools       []connectionTool
}

// sourceForKind maps a connection type to the row Source the UI groups on.
func sourceForKind(kind string) string {
	if kind == "api" {
		return connectionEndpointSource
	}
	return connectionToolSource
}

// appendConnectionToolRows discovers the workspace's enabled MCP connections and
// appends, per connection, one row per discovered tool carrying that (connection,
// tool) cell's explicit per-layer settings and resolved Effective verdict.
// groupIDs is the already-resolved group set for the query's context, reused so
// this path resolves the Group layer against the same groups as the
// capability-wide rows.
//
// Like repo rows, connection-tool rows are emitted on every view (including a
// runtime-scoped one): connection capabilities are not runtime-reported, so the
// runtime filter in table.go would otherwise hide them — but connection access is
// authored at all five layers, so the admin must see them everywhere.
func (s *Store) appendConnectionToolRows(ctx context.Context, in TableQuery, groupIDs []pgtype.UUID, out []TableRow) ([]TableRow, error) {
	conns, err := s.discoverConnectionTools(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if len(conns) == 0 {
		return out, nil
	}

	settings, err := s.loadConnectionPolicySettings(ctx, in, groupIDs)
	if err != nil {
		return nil, err
	}

	for _, conn := range conns {
		toolKey := connectionToolKeyPrefix + conn.name
		source := sourceForKind(conn.kind)
		for _, t := range conn.tools {
			row := TableRow{
				ToolKey:         toolKey,
				ResourcePattern: t.Name,
				Title:           t.Name,
				Category:        conn.displayName,
				Source:          source,
				Layers:          map[Layer]Setting{},
			}
			if cell, ok := settings[repoPolicyKey{toolKey, t.Name}]; ok {
				for l, set := range cell.layers {
					row.Layers[l] = set
				}
				if len(cell.groups) > 0 {
					row.Layers[LayerGroup] = CombineGroups(cell.groups...)
				}
			}
			row.Effective = Resolve(Input{Settings: row.Layers, Base: in.Base})
			out = append(out, row)
		}
	}
	return out, nil
}

// discoverConnectionTools returns each enabled connection in the workspace with
// its per-tool rows, ordered by created_at so the UI order is stable. For MCP
// (mcp_http) connections the rows are the persisted tools/list. For REST (api)
// connections the rows are synthesized per endpoint+method ("<METHOD> <path>")
// from endpoint_permissions, so the same Configure sheet drives CRUD control.
func (s *Store) discoverConnectionTools(ctx context.Context, workspaceID pgtype.UUID) ([]connectionRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT name, display_name, type, tools, endpoint_permissions
		FROM workspace_connection
		WHERE workspace_id = $1 AND enabled = true AND type IN ('mcp_http', 'api')
		ORDER BY created_at ASC
	`, workspaceID)
	if err != nil {
		if isUndefinedTable(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("toolpolicy: load connection tools: %w", err)
	}
	defer rows.Close()

	var out []connectionRow
	for rows.Next() {
		var name, displayName, kind string
		var toolsRaw, endpointsRaw []byte
		if err := rows.Scan(&name, &displayName, &kind, &toolsRaw, &endpointsRaw); err != nil {
			return nil, fmt.Errorf("toolpolicy: scan connection: %w", err)
		}
		var tools []connectionTool
		if kind == "api" {
			tools = endpointMethodTools(endpointsRaw)
		} else {
			_ = json.Unmarshal(toolsRaw, &tools)
		}
		if len(tools) == 0 {
			continue
		}
		out = append(out, connectionRow{name: name, displayName: displayName, kind: kind, tools: tools})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("toolpolicy: iterate connections: %w", err)
	}
	return out, nil
}

// endpointMethodTools expands an api connection's endpoint_permissions JSON into
// one synthetic tool per (method, path) — the unit the CRUD-control sheet gates.
// The resource_pattern is "<METHOD> <path>" (e.g. "POST /orders").
func endpointMethodTools(raw []byte) []connectionTool {
	var eps []endpointPermission
	if json.Unmarshal(raw, &eps) != nil {
		return nil
	}
	var tools []connectionTool
	for _, ep := range eps {
		if ep.Path == "" {
			continue
		}
		for _, m := range ep.Methods {
			if m == "" {
				continue
			}
			tools = append(tools, connectionTool{Name: m + " " + ep.Path})
		}
	}
	return tools
}

// DeniedConnectionTools resolves, for the given chain context, every MCP
// connection tool whose effective verdict is Deny, and returns them as Claude
// Code --disallowedTools tokens ("mcp__<connection>__<tool>"). The daemon passes
// these at spawn so a denied tool is never callable — the runtime half of the
// per-tool permission feature (TECH-3156).
//
// It reuses the same Table resolution the admin UI reads from, so the runtime
// enforcement and the screen can never drift: a tool the screen shows as Deny is
// exactly a tool that ends up on this list.
func (s *Store) DeniedConnectionTools(ctx context.Context, in TableQuery) ([]string, error) {
	rows, err := s.Table(ctx, in)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range rows {
		if r.Source != connectionToolSource {
			continue
		}
		if r.Effective.Setting != SettingDeny {
			continue
		}
		connName := strings.TrimPrefix(r.ToolKey, connectionToolKeyPrefix)
		if connName == "" || r.ResourcePattern == "" {
			continue
		}
		out = append(out, mcpToolToken(connName, r.ResourcePattern))
	}
	return out, nil
}

// DisallowedMCPTools is the claim-time adapter that satisfies the handler's
// ConnectionToolDenyResolver seam. It resolves the agent owner (the user
// ceiling) so user/group-layer denies are honoured, then returns the denied
// connection tools as --disallowedTools tokens.
//
// It FAILS CLOSED: any resolve error is returned, not swallowed, so the caller
// withholds the connections this claim rather than silently letting a denied
// tool through. A genuinely absent owner (agent with no owner_id) is not an
// error — the user layer is simply dropped.
func (s *Store) DisallowedMCPTools(ctx context.Context, workspaceID, runtimeID, agentID pgtype.UUID) ([]string, error) {
	var ownerID pgtype.UUID
	if agentID.Valid {
		err := s.pool.QueryRow(ctx,
			`SELECT owner_id FROM agent WHERE id = $1`, agentID).Scan(&ownerID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("toolpolicy: load agent owner: %w", err)
		}
	}
	return s.DeniedConnectionTools(ctx, TableQuery{
		WorkspaceID: workspaceID,
		RuntimeID:   runtimeID,
		AgentID:     agentID,
		UserID:      ownerID,
	})
}

// mcpToolToken builds Claude Code's namespaced MCP tool identifier. Claude Code
// exposes an MCP server's tools as "mcp__<server>__<tool>", and the connection
// name is the server key the daemon injects into --mcp-config (connections.Store
// BuildMCPConfig), so the two match by construction.
func mcpToolToken(connectionName, tool string) string {
	return "mcp__" + connectionName + "__" + tool
}

// loadConnectionPolicySettings fetches every explicit per-layer setting authored
// for a connection tool (tool_key 'connection:%', resource_pattern non-empty) in
// the query's context, bucketed by (tool_key, tool). It mirrors the subject
// predicates of table_repo.go's loader so an absent subject id never matches and
// that layer stays Inherit.
func (s *Store) loadConnectionPolicySettings(ctx context.Context, in TableQuery, groupIDs []pgtype.UUID) (map[repoPolicyKey]*repoPolicyLayers, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.tool_key, p.resource_pattern, p.layer, p.setting
		FROM cerebro_tool_policy p
		WHERE p.workspace_id = $1
		  AND p.resource_pattern <> ''
		  AND p.tool_key LIKE 'connection:%'
		  AND (
		    (p.layer = 'workspace' AND p.subject_id = $1) OR
		    (p.layer = 'runtime'   AND p.subject_id = $2) OR
		    (p.layer = 'agent'     AND p.subject_id = $3) OR
		    (p.layer = 'user'      AND p.subject_id = $4) OR
		    (p.layer = 'group'     AND p.subject_id = ANY($5::uuid[]))
		  )
	`, in.WorkspaceID, in.RuntimeID, in.AgentID, in.UserID, groupIDs)
	if err != nil {
		return nil, fmt.Errorf("toolpolicy: load connection policy settings: %w", err)
	}
	defer rows.Close()

	out := map[repoPolicyKey]*repoPolicyLayers{}
	for rows.Next() {
		var toolKey, resourcePattern, layer, setting string
		if err := rows.Scan(&toolKey, &resourcePattern, &layer, &setting); err != nil {
			return nil, fmt.Errorf("toolpolicy: scan connection policy setting: %w", err)
		}
		key := repoPolicyKey{toolKey, resourcePattern}
		cell, ok := out[key]
		if !ok {
			cell = &repoPolicyLayers{layers: map[Layer]Setting{}}
			out[key] = cell
		}
		l := Layer(layer)
		set := Setting(setting)
		if l == LayerGroup {
			cell.groups = append(cell.groups, set)
		} else {
			cell.layers[l] = set
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("toolpolicy: iterate connection policy settings: %w", err)
	}
	return out, nil
}
