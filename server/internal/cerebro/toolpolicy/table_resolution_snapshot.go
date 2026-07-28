package toolpolicy

// A permission table resolves many capability/resource rows for the same actor
// context. Resolving every row through Store.loadInput is semantically correct
// but operationally disastrous: each row repeats the direct-policy and active
// role queries. Claim-time connection resolution can contain hundreds of rows,
// so that pattern exhausted the production database pool.
//
// tableResolutionSnapshot loads those actor-scoped inputs once and applies the
// same condition and role rules in memory for every row. It is deliberately
// private to one Table call: permissions and role expiry are still re-read on
// every independent request, preserving the call-time freshness contract.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

type snapshotPolicyRule struct {
	layer     Layer
	setting   Setting
	condition *Condition
}

type snapshotRoleRule struct {
	source          RoleSource
	resourcePattern string
	setting         Setting
	condition       *Condition
}

type tableResolutionSnapshot struct {
	direct map[resourcePolicyKey][]snapshotPolicyRule
	roles  map[string][]snapshotRoleRule
}

func (s *Store) loadTableResolutionSnapshot(ctx context.Context, in TableQuery) (*tableResolutionSnapshot, error) {
	snapshot := &tableResolutionSnapshot{
		direct: map[resourcePolicyKey][]snapshotPolicyRule{},
		roles:  map[string][]snapshotRoleRule{},
	}

	rows, err := s.pool.Query(ctx, `
		SELECT p.tool_key, p.resource_pattern, p.layer, p.setting, p.conditions
		FROM cerebro_tool_policy p
		WHERE p.workspace_id = $1
		  AND (
		    (p.layer = 'workspace'    AND p.subject_id = $1) OR
		    (p.layer = 'runtime'      AND p.subject_id = $2) OR
		    (p.layer = 'agent'        AND p.subject_id = $3) OR
		    (p.layer = 'user'         AND p.subject_id = $4) OR
		    (p.layer = 'group'        AND p.subject_id = ANY($5::uuid[])) OR
		    (p.layer = 'system'       AND p.subject_id = $6) OR
		    (p.layer = 'on_behalf_of' AND p.subject_id = $7)
		  )
	`, in.WorkspaceID, in.RuntimeID, in.AgentID, in.UserID, in.GroupIDs, in.SystemID, in.OnBehalfOfID)
	if err != nil {
		return nil, fmt.Errorf("toolpolicy: load table resolution policies: %w", err)
	}
	for rows.Next() {
		var toolKey, resourcePattern, layer, setting string
		var conditions []byte
		if err := rows.Scan(&toolKey, &resourcePattern, &layer, &setting, &conditions); err != nil {
			rows.Close()
			return nil, fmt.Errorf("toolpolicy: scan table resolution policy: %w", err)
		}
		condition, err := decodeCondition(conditions)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("toolpolicy: decode table resolution condition for %q: %w", toolKey, err)
		}
		key := resourcePolicyKey{toolKey: toolKey, resourcePattern: resourcePattern}
		snapshot.direct[key] = append(snapshot.direct[key], snapshotPolicyRule{
			layer:     Layer(layer),
			setting:   Setting(setting),
			condition: condition,
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("toolpolicy: iterate table resolution policies: %w", err)
	}
	rows.Close()

	if !in.AgentID.Valid && !in.UserID.Valid {
		return snapshot, nil
	}
	roleRows, err := s.pool.Query(ctx, `
		SELECT r.id, r.name, r.version, r.permissions
		FROM cerebro_role_assignment b
		JOIN cerebro_role r ON r.id = b.role_id
		WHERE r.workspace_id = $1
		  AND r.archived_at IS NULL
		  AND (b.expires_at IS NULL OR b.expires_at > now())
		  AND ((b.subject_type = 'agent' AND b.subject_id = $2)
		    OR (b.subject_type = 'member' AND b.subject_id = $3))
	`, in.WorkspaceID, in.AgentID, in.UserID)
	if err != nil {
		return nil, fmt.Errorf("toolpolicy: load table resolution roles: %w", err)
	}
	defer roleRows.Close()
	for roleRows.Next() {
		var id pgtype.UUID
		var name string
		var version int32
		var permissions []byte
		if err := roleRows.Scan(&id, &name, &version, &permissions); err != nil {
			return nil, fmt.Errorf("toolpolicy: scan table resolution role: %w", err)
		}
		var byTool map[string]json.RawMessage
		if err := json.Unmarshal(permissions, &byTool); err != nil {
			return nil, fmt.Errorf("toolpolicy: decode permissions for role %q: %w", name, err)
		}
		for toolKey, raw := range byTool {
			rules, err := decodeRolePermission(raw)
			if err != nil {
				return nil, fmt.Errorf("toolpolicy: decode role permission for %q: %w", toolKey, err)
			}
			for _, rule := range rules {
				setting := Setting(rule.Setting)
				if !validSetting(setting) || setting == SettingInherit {
					continue
				}
				snapshot.roles[toolKey] = append(snapshot.roles[toolKey], snapshotRoleRule{
					source: RoleSource{
						ID:      util.UUIDToString(id),
						Name:    name,
						Version: version,
						Setting: setting,
					},
					resourcePattern: rule.ResourcePattern,
					setting:         setting,
					condition:       rule.Conditions,
				})
			}
		}
	}
	if err := roleRows.Err(); err != nil {
		return nil, fmt.Errorf("toolpolicy: iterate table resolution roles: %w", err)
	}
	return snapshot, nil
}

func (snapshot *tableResolutionSnapshot) resolve(in Query, mode Mode) Effective {
	reqCtx := requestContextWithRuntime(in)
	input := Input{
		Settings: map[Layer]Setting{},
		Base:     in.Base,
		IsSystem: in.IsSystem,
	}
	var groupSettings []Setting
	key := resourcePolicyKey{toolKey: in.ToolKey, resourcePattern: in.ResourcePattern}
	for _, rule := range snapshot.direct[key] {
		setting, applies := ConditionedSetting(rule.setting, rule.condition, reqCtx, in.Eval)
		if !applies {
			continue
		}
		if rule.layer == LayerGroup {
			groupSettings = append(groupSettings, setting)
			continue
		}
		input.Settings[rule.layer] = setting
	}
	if len(groupSettings) > 0 {
		input.Settings[LayerGroup] = CombineGroups(groupSettings...)
	}

	var roleSettings []Setting
	var roleSources []RoleSource
	for _, rule := range snapshot.roles[in.ToolKey] {
		if rule.resourcePattern != in.ResourcePattern {
			continue
		}
		setting, applies := ConditionedSetting(rule.setting, rule.condition, reqCtx, in.Eval)
		if !applies {
			continue
		}
		source := rule.source
		source.Setting = setting
		roleSettings = append(roleSettings, setting)
		roleSources = append(roleSources, source)
	}
	applyResolvedRoles(&input, roleSettings, roleSources)
	return ResolveWithMode(mode, input)
}
