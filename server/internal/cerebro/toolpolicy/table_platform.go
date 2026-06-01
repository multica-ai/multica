package toolpolicy

// table_platform.go appends the Multica platform actions (FIR-2594) to the admin
// table. table.go lists the capability-wide rows for every tool a runtime
// reported into cerebro_capability; table_repo.go adds the per-repo rows. Neither
// covers the platform's OWN mutating operations — create an issue, add a comment,
// trigger an autopilot, manage agents/runtimes/grants — because no runtime
// reports them. The platformcatalog package is the code-owned source of truth for
// those; here we turn each catalog entry into one capability-wide row carrying its
// stored per-layer settings and resolved Effective verdict, so an admin sees and
// sets Allow/Ask/Deny on platform actions in the same screen as runtime tools.
//
// Platform rows are emitted on every view, runtime-scoped included: a platform
// action is workspace-global, not tied to the machine an agent runs on, so the
// runtime filter in table.go (which keeps only that runtime's reported tools)
// must not hide them — exactly the reasoning appendRepoRows uses for repo rows.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/platformcatalog"
)

// FlagPlatformCapabilities is the cerebro feature flag (registry.ts) that gates
// whether the platform-capability rows appear in the table. Default OFF: with no
// override row the platform actions stay hidden, so prod behaviour is unchanged
// until an admin deliberately turns the flag on (FIR-2594).
const FlagPlatformCapabilities = "cerebro_platform_capabilities"

// PlatformCapabilitiesEnabled reports whether the platform-capability catalog
// should be appended to the table for this (workspace, user). The flag defaults
// OFF, so a missing override row means false; a DB error also returns false
// (fail closed — show nothing new) and is logged.
func (s *Store) PlatformCapabilitiesEnabled(ctx context.Context, workspaceID, userID pgtype.UUID) bool {
	if s.q == nil || !userID.Valid {
		return false
	}
	on, err := s.q.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
		FlagKey:     FlagPlatformCapabilities,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	if err != nil {
		slog.Error("toolpolicy: platform-capabilities flag lookup failed", "error", err)
		return false
	}
	return on
}

// appendPlatformRows adds one row per platform capability (platformcatalog.All)
// with the explicit per-layer settings authored for it in the query's context
// and the resolved Effective verdict. groupIDs is the already-resolved group set
// for the context (see Table), reused so the Group layer resolves against the
// same groups as the capability-wide rows.
func (s *Store) appendPlatformRows(ctx context.Context, in TableQuery, groupIDs []pgtype.UUID, out []TableRow) ([]TableRow, error) {
	caps := platformcatalog.All()
	if len(caps) == 0 {
		return out, nil
	}

	settings, err := s.loadPlatformPolicySettings(ctx, in, groupIDs, platformcatalog.Keys())
	if err != nil {
		return nil, err
	}

	for _, c := range caps {
		row := TableRow{
			ToolKey:           c.Key,
			Title:             c.Title,
			Category:          c.Category,
			Source:            platformcatalog.Source,
			ManagedExternally: c.ManagedExternally,
			Layers:            map[Layer]Setting{},
		}
		if cell, ok := settings[c.Key]; ok {
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
	return out, nil
}

// platformPolicyLayers holds the explicit per-layer settings authored for one
// platform capability. group settings accumulate separately and combine with
// CombineGroups, mirroring how table.go folds the capability-wide rows.
type platformPolicyLayers struct {
	layers map[Layer]Setting
	groups []Setting
}

// loadPlatformPolicySettings fetches every explicit per-layer setting authored
// for the given platform capability keys (capability-wide rows, resource_pattern
// = '') in the query's context, bucketed by tool_key. It mirrors the subject
// predicates of the capability-wide query in table.go so an absent (Valid=false)
// subject id never matches and that layer stays Inherit.
func (s *Store) loadPlatformPolicySettings(ctx context.Context, in TableQuery, groupIDs []pgtype.UUID, keys []string) (map[string]*platformPolicyLayers, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.tool_key, p.layer, p.setting
		FROM cerebro_tool_policy p
		WHERE p.workspace_id = $1
		  AND p.resource_pattern = ''
		  AND p.tool_key = ANY($6::text[])
		  AND (
		    (p.layer = 'workspace' AND p.subject_id = $1) OR
		    (p.layer = 'runtime'   AND p.subject_id = $2) OR
		    (p.layer = 'agent'     AND p.subject_id = $3) OR
		    (p.layer = 'user'      AND p.subject_id = $4) OR
		    (p.layer = 'group'     AND p.subject_id = ANY($5::uuid[]))
		  )
	`, in.WorkspaceID, in.RuntimeID, in.AgentID, in.UserID, groupIDs, keys)
	if err != nil {
		return nil, fmt.Errorf("toolpolicy: load platform policy settings: %w", err)
	}
	defer rows.Close()

	settings := map[string]*platformPolicyLayers{}
	for rows.Next() {
		var toolKey, layer, setting string
		if err := rows.Scan(&toolKey, &layer, &setting); err != nil {
			return nil, fmt.Errorf("toolpolicy: scan platform policy setting: %w", err)
		}
		cell, ok := settings[toolKey]
		if !ok {
			cell = &platformPolicyLayers{layers: map[Layer]Setting{}}
			settings[toolKey] = cell
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
		return nil, fmt.Errorf("toolpolicy: iterate platform policy settings: %w", err)
	}
	return settings, nil
}
