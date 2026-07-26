// CEREBRO-PATCH(workspace-copy): TECH-3742 — role / group / permission graph.
//
// Workspace roles and groups are workspace-scoped, so a merge has to rebuild
// them in the target. Members and users are shared across workspaces (a person
// has one user row and one member row per workspace they belong to), so
// member-scoped subjects are remapped by resolving the source member's user_id
// to the target workspace's member row.
//
// Copied here (foundation pass, resolvable immediately):
//   - cerebro_role (role definitions, dedup by workspace+name)
//   - cerebro_role_assignment for member subjects (by user→member)
//   - cerebro_group + cerebro_group_member (by user) + cerebro_group_capability
//
// The cerebro_workspace_grant control plane is NOT copied: repo + workspace-copy
// no longer read it (FIR-1777 §5.3 step 3) — repo access resolves through the
// tool-policy chain and the only remaining reader is the credentials resolver,
// which is owner-scoped and migrates onto the chain via the engine-flip.
//
// Agent-scoped subjects (role assignments, group→agent access) and group→project
// access reference entities copied later (agents, projects), so they are healed
// by RelinkGroupAccess after those passes run. Group→runtime access is
// intentionally parked — copied agents land unpaired and the owner re-pairs
// runtimes in the target. Idempotent and non-destructive throughout.
package workspacecopy

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// copyRolesGroupsPermissionsTx rebuilds the role/group graph in the target.
// Returns (roles, groups, roleAssignments) copied.
func copyRolesGroupsPermissionsTx(ctx context.Context, tx pgx.Tx, runID, sourceWorkspace, targetWorkspace pgtype.UUID) (int64, int64, int64, error) {
	// --- roles (cerebro_role; a same-name role already in the target is reused
	// rather than duplicated, since (workspace_id, name) is unique). ---
	roleRows, err := tx.Query(ctx, `SELECT id, name FROM cerebro_role WHERE workspace_id = $1`, sourceWorkspace)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("list roles: %w", err)
	}
	type named struct {
		id   pgtype.UUID
		name string
	}
	var roles []named
	for roleRows.Next() {
		var rl named
		if err := roleRows.Scan(&rl.id, &rl.name); err != nil {
			roleRows.Close()
			return 0, 0, 0, fmt.Errorf("scan role: %w", err)
		}
		roles = append(roles, rl)
	}
	roleRows.Close()
	if err := roleRows.Err(); err != nil {
		return 0, 0, 0, fmt.Errorf("iterate roles: %w", err)
	}

	var rolesCopied int64
	for _, rl := range roles {
		if _, done, err := lookupMapping(ctx, tx, targetWorkspace, "role", rl.id); err != nil {
			return rolesCopied, 0, 0, err
		} else if done {
			continue
		}
		var targetID pgtype.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM cerebro_role WHERE workspace_id = $1 AND name = $2`, targetWorkspace, rl.name).Scan(&targetID)
		if err == pgx.ErrNoRows {
			if err := tx.QueryRow(ctx, `
				INSERT INTO cerebro_role (id, workspace_id, name, description, created_by, created_at, updated_at)
				SELECT gen_random_uuid(), $1, r.name, r.description, r.created_by, r.created_at, now()
				FROM cerebro_role r WHERE r.id = $2
				RETURNING id`,
				targetWorkspace, rl.id).Scan(&targetID); err != nil {
				return rolesCopied, 0, 0, fmt.Errorf("copy role: %w", err)
			}
			rolesCopied++
		} else if err != nil {
			return rolesCopied, 0, 0, fmt.Errorf("find role: %w", err)
		}
		if err := recordMapping(ctx, tx, runID, sourceWorkspace, targetWorkspace, "role", rl.id, targetID, nil, nil); err != nil {
			return rolesCopied, 0, 0, err
		}
	}

	// --- role assignments, member subjects (resolve source member → target
	// member by user_id). Agent subjects are healed by RelinkGroupAccess once
	// the agents have been copied. ---
	raTag, err := tx.Exec(ctx, `
		INSERT INTO cerebro_role_assignment (role_id, subject_type, subject_id, added_by, added_at)
		SELECT rm.target_id, 'member', tm.id, ra.added_by, ra.added_at
		FROM cerebro_role_assignment ra
		JOIN cerebro_workspace_copy_map rm
		  ON rm.target_workspace_id = $2 AND rm.entity_type = 'role' AND rm.source_id = ra.role_id
		JOIN member sm ON sm.id = ra.subject_id AND sm.workspace_id = $1
		JOIN member tm ON tm.user_id = sm.user_id AND tm.workspace_id = $2
		WHERE ra.subject_type = 'member'
		ON CONFLICT (role_id, subject_type, subject_id) DO NOTHING`,
		sourceWorkspace, targetWorkspace)
	if err != nil {
		return rolesCopied, 0, 0, fmt.Errorf("copy role assignments: %w", err)
	}

	// --- groups + their members and capabilities ---
	groupsCopied, err := copyGroupsTx(ctx, tx, runID, sourceWorkspace, targetWorkspace)
	if err != nil {
		return rolesCopied, 0, raTag.RowsAffected(), err
	}

	return rolesCopied, groupsCopied, raTag.RowsAffected(), nil
}

// copyGroupsTx copies cerebro_group rows (reuse same-name target group) plus
// their members (by user) and capabilities. Group→agent access is healed later
// by RelinkGroupAccess; group→runtime access is intentionally parked.
func copyGroupsTx(ctx context.Context, tx pgx.Tx, runID, sourceWorkspace, targetWorkspace pgtype.UUID) (int64, error) {
	rows, err := tx.Query(ctx, `SELECT id, name FROM cerebro_group WHERE workspace_id = $1`, sourceWorkspace)
	if err != nil {
		return 0, fmt.Errorf("list groups: %w", err)
	}
	type named struct {
		id   pgtype.UUID
		name string
	}
	var groups []named
	for rows.Next() {
		var g named
		if err := rows.Scan(&g.id, &g.name); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan group: %w", err)
		}
		groups = append(groups, g)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate groups: %w", err)
	}

	var copied int64
	for _, g := range groups {
		if _, done, err := lookupMapping(ctx, tx, targetWorkspace, "group", g.id); err != nil {
			return copied, err
		} else if done {
			continue
		}
		var targetID pgtype.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM cerebro_group WHERE workspace_id = $1 AND name = $2`, targetWorkspace, g.name).Scan(&targetID)
		if err == pgx.ErrNoRows {
			if err := tx.QueryRow(ctx, `
				INSERT INTO cerebro_group (id, workspace_id, name, description, created_by, created_at, updated_at)
				SELECT gen_random_uuid(), $1, gr.name, gr.description, gr.created_by, gr.created_at, now()
				FROM cerebro_group gr WHERE gr.id = $2
				RETURNING id`,
				targetWorkspace, g.id).Scan(&targetID); err != nil {
				return copied, fmt.Errorf("copy group: %w", err)
			}
			copied++
		} else if err != nil {
			return copied, fmt.Errorf("find group: %w", err)
		}
		if err := recordMapping(ctx, tx, runID, sourceWorkspace, targetWorkspace, "group", g.id, targetID, nil, nil); err != nil {
			return copied, err
		}

		// members by user_id (shared across workspaces)
		if _, err := tx.Exec(ctx, `
			INSERT INTO cerebro_group_member (group_id, user_id, added_by, added_at)
			SELECT $1, gm.user_id, gm.added_by, gm.added_at
			FROM cerebro_group_member gm WHERE gm.group_id = $2
			ON CONFLICT (group_id, user_id) DO NOTHING`,
			targetID, g.id); err != nil {
			return copied, fmt.Errorf("copy group members: %w", err)
		}
		// capabilities
		if _, err := tx.Exec(ctx, `
			INSERT INTO cerebro_group_capability (group_id, capability, granted_by, granted_at)
			SELECT $1, gc.capability, gc.granted_by, gc.granted_at
			FROM cerebro_group_capability gc WHERE gc.group_id = $2
			ON CONFLICT (group_id, capability) DO NOTHING`,
			targetID, g.id); err != nil {
			return copied, fmt.Errorf("copy group capabilities: %w", err)
		}
	}
	return copied, nil
}

// RelinkGroupAccess heals the access grants that reference entities copied AFTER
// the foundation pass — agents and projects. Run it once both the foundation and
// the per-item agent/project copies have completed. It heals, in the target:
//   - cerebro_group_agent_access (group + agent both mapped)
//   - cerebro_role_assignment for agent subjects (role + agent both mapped)
//   - cerebro_project_group_member (project + group both mapped)
//
// Group→runtime access is intentionally NOT carried — copied agents are parked
// (runtime_id null) and the owner re-pairs runtimes in the target. Idempotent
// and never touches the source workspace. Returns the total rows healed.
func (s *Store) RelinkGroupAccess(ctx context.Context, targetWorkspace pgtype.UUID) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var total int64

	gaTag, err := tx.Exec(ctx, `
		INSERT INTO cerebro_group_agent_access (group_id, agent_id, granted_by, granted_at)
		SELECT gmap.target_id, amap.target_id, ga.granted_by, ga.granted_at
		FROM cerebro_group_agent_access ga
		JOIN cerebro_workspace_copy_map gmap
		  ON gmap.target_workspace_id = $1 AND gmap.entity_type = 'group' AND gmap.source_id = ga.group_id
		JOIN cerebro_workspace_copy_map amap
		  ON amap.target_workspace_id = $1 AND amap.entity_type = 'agent' AND amap.source_id = ga.agent_id
		ON CONFLICT (group_id, agent_id) DO NOTHING`,
		targetWorkspace)
	if err != nil {
		return 0, fmt.Errorf("relink group agent access: %w", err)
	}
	total += gaTag.RowsAffected()

	raTag, err := tx.Exec(ctx, `
		INSERT INTO cerebro_role_assignment (role_id, subject_type, subject_id, added_by, added_at)
		SELECT rmap.target_id, 'agent', amap.target_id, ra.added_by, ra.added_at
		FROM cerebro_role_assignment ra
		JOIN cerebro_workspace_copy_map rmap
		  ON rmap.target_workspace_id = $1 AND rmap.entity_type = 'role' AND rmap.source_id = ra.role_id
		JOIN cerebro_workspace_copy_map amap
		  ON amap.target_workspace_id = $1 AND amap.entity_type = 'agent' AND amap.source_id = ra.subject_id
		WHERE ra.subject_type = 'agent'
		ON CONFLICT (role_id, subject_type, subject_id) DO NOTHING`,
		targetWorkspace)
	if err != nil {
		return 0, fmt.Errorf("relink agent role assignments: %w", err)
	}
	total += raTag.RowsAffected()

	pgmTag, err := tx.Exec(ctx, `
		INSERT INTO cerebro_project_group_member (project_id, group_id, added_by, added_at)
		SELECT pmap.target_id, gmap.target_id, pgm.added_by, pgm.added_at
		FROM cerebro_project_group_member pgm
		JOIN cerebro_workspace_copy_map gmap
		  ON gmap.target_workspace_id = $1 AND gmap.entity_type = 'group' AND gmap.source_id = pgm.group_id
		JOIN cerebro_workspace_copy_map pmap
		  ON pmap.target_workspace_id = $1 AND pmap.entity_type = 'project' AND pmap.source_id = pgm.project_id
		ON CONFLICT (project_id, group_id) DO NOTHING`,
		targetWorkspace)
	if err != nil {
		return 0, fmt.Errorf("relink project group access: %w", err)
	}
	total += pgmTag.RowsAffected()

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return total, nil
}
