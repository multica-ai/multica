package toolpolicy

// Synthetic permission rows (repositories, credentials, connections, Registry
// data sources and platform actions) all project the same stored policy chain.
// This file is their single read contract. Keeping the actor predicates and
// condition decoding here prevents one resource family from silently omitting a
// mandate actor or WHEN rule that call-time resolution still enforces.

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
)

type resourcePolicyKey struct {
	toolKey         string
	resourcePattern string
}

type resourcePolicyCell struct {
	layers     map[Layer]Setting
	groups     []Setting
	conditions map[Layer]*Condition
}

type resourcePatternScope string

const (
	resourcePatternAny      resourcePatternScope = "any"
	resourcePatternEmpty    resourcePatternScope = "empty"
	resourcePatternNonEmpty resourcePatternScope = "nonempty"
)

type resourcePolicyFilter struct {
	toolKeys   []string
	toolPrefix string
	scope      resourcePatternScope
}

func (s *Store) loadResourcePolicySettings(
	ctx context.Context,
	in TableQuery,
	groupIDs []pgtype.UUID,
	filter resourcePolicyFilter,
) (map[resourcePolicyKey]*resourcePolicyCell, error) {
	scope := filter.scope
	if scope == "" {
		scope = resourcePatternAny
	}
	switch scope {
	case resourcePatternAny, resourcePatternEmpty, resourcePatternNonEmpty:
	default:
		return nil, fmt.Errorf("toolpolicy: unknown resource pattern scope %q", scope)
	}
	if len(filter.toolKeys) == 0 && filter.toolPrefix == "" {
		return nil, fmt.Errorf("toolpolicy: resource policy filter needs a tool key or prefix")
	}

	rows, err := s.pool.Query(ctx, `
		SELECT p.tool_key, p.resource_pattern, p.layer, p.setting, p.conditions
		FROM cerebro_tool_policy p
		WHERE p.workspace_id = $1
		  AND (
		    $8::text = 'any' OR
		    ($8::text = 'empty' AND p.resource_pattern = '') OR
		    ($8::text = 'nonempty' AND p.resource_pattern <> '')
		  )
		  AND (COALESCE(cardinality($9::text[]), 0) = 0 OR p.tool_key = ANY($9::text[]))
		  AND ($10::text = '' OR p.tool_key LIKE $10)
		  AND (
		    (p.layer = 'workspace'    AND p.subject_id = $1) OR
		    (p.layer = 'runtime'      AND p.subject_id = $2) OR
		    (p.layer = 'agent'        AND p.subject_id = $3) OR
		    (p.layer = 'user'         AND p.subject_id = $4) OR
		    (p.layer = 'group'        AND p.subject_id = ANY($5::uuid[])) OR
		    (p.layer = 'on_behalf_of' AND p.subject_id = $6) OR
		    (p.layer = 'system'       AND p.subject_id = $7)
		  )
	`, in.WorkspaceID, in.RuntimeID, in.AgentID, in.UserID, groupIDs,
		in.OnBehalfOfID, in.SystemID, string(scope), filter.toolKeys, filter.toolPrefix)
	if err != nil {
		return nil, fmt.Errorf("toolpolicy: load resource policy settings: %w", err)
	}
	defer rows.Close()

	out := map[resourcePolicyKey]*resourcePolicyCell{}
	for rows.Next() {
		var toolKey, resourcePattern, layer, setting string
		var conditions []byte
		if err := rows.Scan(&toolKey, &resourcePattern, &layer, &setting, &conditions); err != nil {
			return nil, fmt.Errorf("toolpolicy: scan resource policy setting: %w", err)
		}
		key := resourcePolicyKey{toolKey: toolKey, resourcePattern: resourcePattern}
		cell, ok := out[key]
		if !ok {
			cell = &resourcePolicyCell{
				layers:     map[Layer]Setting{},
				conditions: map[Layer]*Condition{},
			}
			out[key] = cell
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
			return nil, fmt.Errorf(
				"toolpolicy: decode resource conditions for %q on %q at %s: %w",
				toolKey, resourcePattern, l, err,
			)
		}
		if cond != nil {
			cell.conditions[l] = cond
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("toolpolicy: iterate resource policy settings: %w", err)
	}
	return out, nil
}
