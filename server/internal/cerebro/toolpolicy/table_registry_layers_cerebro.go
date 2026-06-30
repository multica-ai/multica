package toolpolicy

// CEREBRO-FEATURE(FIR-2269): author firtal_registry per-data-source rules at
// every actor layer, not only the per-agent layer.
//
// table_registry.go projects the legacy per-agent grant (agent_tool_grant) onto
// the agent layer — that path is unchanged and still serves the agent's Tools
// tab. This file adds the missing surface: at the workspace, runtime, group,
// user, and system scopes there is no per-agent grant, so the picker must read
// the AUTHORED chain rows (cerebro_tool_policy, resource_pattern = data source
// id) — exactly the rows the gate enforces in chainGateDataSource — and render
// one authorable row per data source. It mirrors appendRepoRows/loadRepoPolicy
// Settings: a catalog (injected, because the FDR proxy lives in package handler)
// crossed with the authored per-layer settings, resolved against Base.

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
)

// loadRegistryResourceSettings reads the authored per-data-source firtal_registry
// chain rows for the query's in-scope subjects, keyed by data source id
// (resource_pattern). It mirrors loadRepoPolicySettings but for the single
// firtal_registry tool key, so each layer (workspace → runtime → group → user)
// surfaces the same Allow/Ask/Deny the gate reads. A user in scope expands to
// the group layer via groupIDs, supplied already-resolved by the caller.
func (s *Store) loadRegistryResourceSettings(ctx context.Context, in TableQuery, groupIDs []pgtype.UUID) (map[string]*repoPolicyLayers, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.resource_pattern, p.layer, p.setting, p.conditions
		FROM cerebro_tool_policy p
		WHERE p.workspace_id = $1
		  AND p.resource_pattern <> ''
		  AND p.tool_key = $7
		  AND (
		    (p.layer = 'workspace' AND p.subject_id = $1) OR
		    (p.layer = 'runtime'   AND p.subject_id = $2) OR
		    (p.layer = 'agent'     AND p.subject_id = $3) OR
		    (p.layer = 'user'      AND p.subject_id = $4) OR
		    (p.layer = 'group'     AND p.subject_id = ANY($5::uuid[])) OR
		    (p.layer = 'system'    AND p.subject_id = $6)
		  )
	`, in.WorkspaceID, in.RuntimeID, in.AgentID, in.UserID, groupIDs, in.SystemID, RegistryToolKey)
	if err != nil {
		return nil, fmt.Errorf("toolpolicy: load registry resource settings: %w", err)
	}
	defer rows.Close()

	out := map[string]*repoPolicyLayers{}
	for rows.Next() {
		var resourcePattern, layer, setting string
		var conditions []byte
		if err := rows.Scan(&resourcePattern, &layer, &setting, &conditions); err != nil {
			return nil, fmt.Errorf("toolpolicy: scan registry resource setting: %w", err)
		}
		cell, ok := out[resourcePattern]
		if !ok {
			cell = &repoPolicyLayers{layers: map[Layer]Setting{}, conditions: map[Layer]*Condition{}}
			out[resourcePattern] = cell
		}
		l := Layer(layer)
		set := Setting(setting)
		if l == LayerGroup {
			cell.groups = append(cell.groups, set)
			continue
		}
		cell.layers[l] = set
		cond, err := decodeCondition(conditions)
		if err != nil {
			return nil, fmt.Errorf("toolpolicy: decode registry conditions for %q at %s: %w", resourcePattern, l, err)
		}
		if cond != nil {
			cell.conditions[l] = cond
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("toolpolicy: iterate registry resource settings: %w", err)
	}
	return out, nil
}

// AppendRegistryDataSourceRows folds the workspace's data-source catalog into the
// table as one authorable firtal_registry per-source row each, populated with the
// authored chain settings for the query's scope and resolved against Base. It is
// the non-agent-scope counterpart to AppendRegistryProjection: used at the
// workspace, runtime, group, user, and system views, where there is no per-agent
// grant to project. dataSources is the catalog supplied by the handler (the FDR
// proxy is owned there, so toolpolicy.Table stays free of the registry API). A
// data source whose row was already authored on the table is left as-is.
func (s *Store) AppendRegistryDataSourceRows(ctx context.Context, in TableQuery, dataSources []RegistryDataSource, out []TableRow) ([]TableRow, error) {
	if len(dataSources) == 0 {
		return out, nil
	}
	groupIDs, err := s.resolveGroupIDs(ctx, in.WorkspaceID, in.UserID, in.GroupIDs)
	if err != nil {
		return nil, err
	}
	settings, err := s.loadRegistryResourceSettings(ctx, in, groupIDs)
	if err != nil {
		return nil, err
	}

	authored := map[string]bool{}
	for _, row := range out {
		if row.ToolKey == RegistryToolKey && row.ResourcePattern != "" {
			authored[row.ResourcePattern] = true
		}
	}

	for _, ds := range dataSources {
		id := ds.ID
		if id == "" || authored[id] {
			continue
		}
		row := TableRow{
			ToolKey:         RegistryToolKey,
			ResourcePattern: id,
			Title:           ds.Name,
			Category:        registryDataSourceCategory,
			Source:          registryDataSourceSource,
			Layers:          map[Layer]Setting{},
			Conditions:      map[Layer]*Condition{},
		}
		if cell, ok := settings[id]; ok {
			for l, set := range cell.layers {
				row.Layers[l] = set
			}
			for l, cond := range cell.conditions {
				row.Conditions[l] = cond
			}
			if len(cell.groups) > 0 {
				row.Layers[LayerGroup] = CombineGroups(cell.groups...)
			}
		}
		row.Effective = Resolve(Input{Settings: row.Layers, Base: in.Base})
		out = append(out, row)
	}
	return out, nil
}
