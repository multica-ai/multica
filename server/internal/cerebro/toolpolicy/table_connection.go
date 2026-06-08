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

// connectionToolSource labels per-tool connection rows so the UI can separate
// them from the capability-wide connection row and from per-repo rows (both also
// carry a non-empty resource_pattern).
const connectionToolSource = "connection-tool"

// connectionTool is one tool discovered on a connection, persisted on the
// workspace_connection.tools JSON array.
type connectionTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// connectionRow pairs a connection's policy key with the human label its tools
// group under (the connection display name) and the tools themselves.
type connectionRow struct {
	name        string
	displayName string
	tools       []connectionTool
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
		for _, t := range conn.tools {
			row := TableRow{
				ToolKey:         toolKey,
				ResourcePattern: t.Name,
				Title:           t.Name,
				Category:        conn.displayName,
				Source:          connectionToolSource,
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

// discoverConnectionTools returns each enabled MCP connection in the workspace
// together with its persisted tool list, ordered by created_at so the UI order is
// stable. API connections are skipped — their per-endpoint control is a separate
// surface.
func (s *Store) discoverConnectionTools(ctx context.Context, workspaceID pgtype.UUID) ([]connectionRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT name, display_name, tools
		FROM workspace_connection
		WHERE workspace_id = $1 AND enabled = true AND type = 'mcp_http'
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
		var name, displayName string
		var toolsRaw []byte
		if err := rows.Scan(&name, &displayName, &toolsRaw); err != nil {
			return nil, fmt.Errorf("toolpolicy: scan connection: %w", err)
		}
		var tools []connectionTool
		_ = json.Unmarshal(toolsRaw, &tools)
		if len(tools) == 0 {
			continue
		}
		out = append(out, connectionRow{name: name, displayName: displayName, tools: tools})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("toolpolicy: iterate connections: %w", err)
	}
	return out, nil
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
