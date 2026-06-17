// CEREBRO-PATCH(workspace-copy): TECH-3742 — one-time "foundation" bulk copiers
// (run first, under Settings) plus the per-item skill copier.
//
// The foundation is everything a workspace needs to exist BEFORE its agents,
// projects, issues, chats and autopilots are carried over: labels, skills (the
// governance `skill` rows + every version + file), connections + their stored
// credentials, the role/group/permission graph, the GitHub installation links,
// and the workspace settings rows. Copying it first means agent skill-bindings,
// autopilot/agent group access and project leads all have something to resolve
// against when the per-item copies run.
//
// Every copier is non-destructive (NEW rows in the target, source untouched) and
// idempotent via cerebro_workspace_copy_map. Name clashes on uniquely-named
// entities (skill, connection, group) reuse the existing same-name target row
// rather than duplicating — the safe default for a merge; per-item skill copy
// can override that with an explicit conflict policy (see conflict.go).
package workspacecopy

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// FoundationResult summarizes a one-time foundation bulk copy.
type FoundationResult struct {
	Labels          int64 `json:"labels_copied"`
	Skills          int64 `json:"skills_copied"`
	SkillVersions   int64 `json:"skill_versions_copied"`
	SkillFiles      int64 `json:"skill_files_copied"`
	Connections     int64 `json:"connections_copied"`
	Credentials     int64 `json:"credentials_copied"`
	Roles           int64 `json:"roles_copied"`
	Groups          int64 `json:"groups_copied"`
	RoleAssignments int64 `json:"role_assignments_copied"`
	Grants          int64 `json:"grants_copied"`
	GitHub          int64 `json:"github_installations_copied"`
	Settings        int64 `json:"settings_rows_copied"`
}

// CopyFoundation runs every foundation bulk copier in one transaction, in the
// order their cross-references require (roles and groups before the workspace
// grants that remap onto them; everything before agents and projects are copied
// in a later per-item pass, with agent/project-scoped access healed afterward by
// RelinkGroupAccess). Idempotent and non-destructive.
func (s *Store) CopyFoundation(ctx context.Context, runID, sourceWorkspace, targetWorkspace pgtype.UUID) (FoundationResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return FoundationResult{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var res FoundationResult
	if res.Labels, err = copyWorkspaceLabelsTx(ctx, tx, runID, sourceWorkspace, targetWorkspace); err != nil {
		return FoundationResult{}, err
	}
	sk, sv, sf, err := copyWorkspaceSkillsTx(ctx, tx, runID, sourceWorkspace, targetWorkspace, conflictSkip)
	if err != nil {
		return FoundationResult{}, err
	}
	res.Skills, res.SkillVersions, res.SkillFiles = sk, sv, sf
	if res.Connections, res.Credentials, err = copyConnectionsAndCredentialsTx(ctx, tx, runID, sourceWorkspace, targetWorkspace); err != nil {
		return FoundationResult{}, err
	}
	if res.Roles, res.Groups, res.RoleAssignments, res.Grants, err = copyRolesGroupsPermissionsTx(ctx, tx, runID, sourceWorkspace, targetWorkspace); err != nil {
		return FoundationResult{}, err
	}
	if res.GitHub, err = copyGitHubTx(ctx, tx, runID, sourceWorkspace, targetWorkspace); err != nil {
		return FoundationResult{}, err
	}
	if res.Settings, err = copyWorkspaceSettingsTx(ctx, tx, runID, sourceWorkspace, targetWorkspace); err != nil {
		return FoundationResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return FoundationResult{}, fmt.Errorf("commit: %w", err)
	}
	return res, nil
}

// --- labels ----------------------------------------------------------------

// copyWorkspaceLabelsTx copies every label in the source workspace into the
// target (reusing same-name target labels), so a later issue copy finds its
// labels already present. Returns the count of labels processed.
func copyWorkspaceLabelsTx(ctx context.Context, tx pgx.Tx, runID, sourceWorkspace, targetWorkspace pgtype.UUID) (int64, error) {
	rows, err := tx.Query(ctx, `SELECT id FROM issue_label WHERE workspace_id = $1`, sourceWorkspace)
	if err != nil {
		return 0, fmt.Errorf("list labels: %w", err)
	}
	ids, err := collectUUIDRows(rows)
	if err != nil {
		return 0, err
	}
	var n int64
	for _, id := range ids {
		if _, err := ensureLabelTx(ctx, tx, runID, sourceWorkspace, targetWorkspace, id); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// --- skills -----------------------------------------------------------------

// CopySkill copies one skill (row + versions + files) into the target with the
// given name-clash policy. Idempotent and non-destructive.
func (s *Store) CopySkill(ctx context.Context, runID, targetWorkspace, sourceSkill pgtype.UUID, policy conflictPolicy) (CopyResult, error) {
	return s.inTx(ctx, func(tx pgx.Tx) (CopyResult, error) {
		// resolve the source workspace so the mapping records the right origin.
		var sourceWorkspace pgtype.UUID
		if err := tx.QueryRow(ctx, `SELECT workspace_id FROM skill WHERE id = $1`, sourceSkill).Scan(&sourceWorkspace); err != nil {
			return CopyResult{}, fmt.Errorf("load skill workspace: %w", err)
		}
		v, f, copied, err := copySkillTx(ctx, tx, runID, sourceWorkspace, targetWorkspace, sourceSkill, policy)
		if err != nil {
			return CopyResult{}, err
		}
		target, _, err := lookupMapping(ctx, tx, targetWorkspace, "skill", sourceSkill)
		if err != nil {
			return CopyResult{}, err
		}
		return CopyResult{
			EntityType:    "skill",
			SourceID:      sourceSkill,
			TargetID:      target,
			CascadeCopied: v + f,
			AlreadyDone:   !copied,
		}, nil
	})
}

// copyWorkspaceSkillsTx copies every skill in the source workspace. Returns the
// (skills, versions, files) copied. policy decides name-clash behaviour.
func copyWorkspaceSkillsTx(ctx context.Context, tx pgx.Tx, runID, sourceWorkspace, targetWorkspace pgtype.UUID, policy conflictPolicy) (int64, int64, int64, error) {
	rows, err := tx.Query(ctx, `SELECT id FROM skill WHERE workspace_id = $1`, sourceWorkspace)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("list skills: %w", err)
	}
	ids, err := collectUUIDRows(rows)
	if err != nil {
		return 0, 0, 0, err
	}
	var skills, versions, files int64
	for _, id := range ids {
		v, f, copied, err := copySkillTx(ctx, tx, runID, sourceWorkspace, targetWorkspace, id, policy)
		if err != nil {
			return skills, versions, files, err
		}
		if copied {
			skills++
		}
		versions += v
		files += f
	}
	return skills, versions, files, nil
}

// copySkillTx copies one skill row plus its versions and files into the target.
// On a name clash it applies policy (skip → reuse existing target skill, no new
// row; overwrite → not supported for skills, treated as skip; keepBoth → append
// a numeric suffix). Idempotent via the copy map. Returns versions/files copied
// and whether a NEW skill row was created.
func copySkillTx(ctx context.Context, tx pgx.Tx, runID, sourceWorkspace, targetWorkspace, sourceSkill pgtype.UUID, policy conflictPolicy) (int64, int64, bool, error) {
	if existing, done, err := lookupMapping(ctx, tx, targetWorkspace, "skill", sourceSkill); err != nil {
		return 0, 0, false, err
	} else if done {
		// already copied: still ensure versions/files are present (idempotent).
		v, f, err := copySkillChildrenTx(ctx, tx, sourceSkill, existing)
		return v, f, false, err
	}

	var name string
	if err := tx.QueryRow(ctx, `SELECT name FROM skill WHERE id = $1`, sourceSkill).Scan(&name); err != nil {
		return 0, 0, false, fmt.Errorf("load skill: %w", err)
	}

	// name-clash handling against the target's UNIQUE(workspace_id, name).
	var existingTarget pgtype.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM skill WHERE workspace_id = $1 AND name = $2`, targetWorkspace, name).Scan(&existingTarget)
	switch {
	case err == nil:
		switch policy {
		case conflictKeepBoth:
			name, err = uniqueName(ctx, tx, "skill", targetWorkspace, name)
			if err != nil {
				return 0, 0, false, err
			}
		default: // skip / overwrite → reuse the existing skill, copy children into it
			if err := recordMapping(ctx, tx, runID, sourceWorkspace, targetWorkspace, "skill", sourceSkill, existingTarget, nil, nil); err != nil {
				return 0, 0, false, err
			}
			v, f, err := copySkillChildrenTx(ctx, tx, sourceSkill, existingTarget)
			return v, f, false, err
		}
	case err != pgx.ErrNoRows:
		return 0, 0, false, fmt.Errorf("find skill: %w", err)
	}

	var targetID pgtype.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO skill (id, workspace_id, name, description, content, config, created_by,
		                   created_at, updated_at, owner_id, approver_ids, current_version, metadata)
		SELECT gen_random_uuid(), $1, $2, s.description, s.content, s.config, s.created_by,
		       s.created_at, now(), s.owner_id, s.approver_ids, s.current_version, s.metadata
		FROM skill s WHERE s.id = $3
		RETURNING id`,
		targetWorkspace, name, sourceSkill).Scan(&targetID); err != nil {
		return 0, 0, false, fmt.Errorf("copy skill: %w", err)
	}
	if err := recordMapping(ctx, tx, runID, sourceWorkspace, targetWorkspace, "skill", sourceSkill, targetID, nil, nil); err != nil {
		return 0, 0, false, err
	}
	v, f, err := copySkillChildrenTx(ctx, tx, sourceSkill, targetID)
	return v, f, true, err
}

// copySkillChildrenTx copies skill_version + skill_file rows for a source skill
// onto the target skill, skipping versions/files already present (idempotent).
func copySkillChildrenTx(ctx context.Context, tx pgx.Tx, sourceSkill, targetSkill pgtype.UUID) (int64, int64, error) {
	vTag, err := tx.Exec(ctx, `
		INSERT INTO skill_version (id, skill_id, version, content, files, description, created_by, created_at)
		SELECT gen_random_uuid(), $1, v.version, v.content, v.files, v.description, v.created_by, v.created_at
		FROM skill_version v
		WHERE v.skill_id = $2
		  AND NOT EXISTS (SELECT 1 FROM skill_version tv WHERE tv.skill_id = $1 AND tv.version = v.version)`,
		targetSkill, sourceSkill)
	if err != nil {
		return 0, 0, fmt.Errorf("copy skill versions: %w", err)
	}
	fTag, err := tx.Exec(ctx, `
		INSERT INTO skill_file (id, skill_id, path, content, created_at, updated_at)
		SELECT gen_random_uuid(), $1, f.path, f.content, f.created_at, now()
		FROM skill_file f
		WHERE f.skill_id = $2
		  AND NOT EXISTS (SELECT 1 FROM skill_file tf WHERE tf.skill_id = $1 AND tf.path = f.path)`,
		targetSkill, sourceSkill)
	if err != nil {
		return 0, 0, fmt.Errorf("copy skill files: %w", err)
	}
	return vTag.RowsAffected(), fTag.RowsAffected(), nil
}

// --- connections + credentials ---------------------------------------------

// copyConnectionsAndCredentialsTx copies workspace_connection rows and the
// stored cerebro_credential rows. Credentials are encrypted with a single
// instance-wide master key (credentials.KeyEnvVar), not a per-workspace key, so
// the encrypted_value bytea decrypts unchanged in the target. Credential
// bindings are remapped to copied resources where possible. Returns
// (connections, credentials) copied.
func copyConnectionsAndCredentialsTx(ctx context.Context, tx pgx.Tx, runID, sourceWorkspace, targetWorkspace pgtype.UUID) (int64, int64, error) {
	// connections (reuse same-name target connection on clash)
	connRows, err := tx.Query(ctx, `SELECT id, name FROM workspace_connection WHERE workspace_id = $1`, sourceWorkspace)
	if err != nil {
		return 0, 0, fmt.Errorf("list connections: %w", err)
	}
	type named struct {
		id   pgtype.UUID
		name string
	}
	var conns []named
	for connRows.Next() {
		var c named
		if err := connRows.Scan(&c.id, &c.name); err != nil {
			connRows.Close()
			return 0, 0, fmt.Errorf("scan connection: %w", err)
		}
		conns = append(conns, c)
	}
	connRows.Close()
	if err := connRows.Err(); err != nil {
		return 0, 0, fmt.Errorf("iterate connections: %w", err)
	}

	var connCopied int64
	for _, c := range conns {
		if _, done, err := lookupMapping(ctx, tx, targetWorkspace, "connection", c.id); err != nil {
			return connCopied, 0, err
		} else if done {
			continue
		}
		var targetID pgtype.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM workspace_connection WHERE workspace_id = $1 AND name = $2`, targetWorkspace, c.name).Scan(&targetID)
		if err == pgx.ErrNoRows {
			if err := tx.QueryRow(ctx, `
				INSERT INTO workspace_connection (id, workspace_id, name, display_name, type, url, internal,
				                                  auth_config, endpoint_permissions, enabled, created_at, updated_at, tools)
				SELECT gen_random_uuid(), $1, wc.name, wc.display_name, wc.type, wc.url, wc.internal,
				       wc.auth_config, wc.endpoint_permissions, wc.enabled, wc.created_at, now(), wc.tools
				FROM workspace_connection wc WHERE wc.id = $2
				RETURNING id`,
				targetWorkspace, c.id).Scan(&targetID); err != nil {
				return connCopied, 0, fmt.Errorf("copy connection: %w", err)
			}
			connCopied++
		} else if err != nil {
			return connCopied, 0, fmt.Errorf("find connection: %w", err)
		}
		if err := recordMapping(ctx, tx, runID, sourceWorkspace, targetWorkspace, "connection", c.id, targetID, nil, nil); err != nil {
			return connCopied, 0, err
		}
	}

	// credentials (reuse same type+name target credential on clash)
	credRows, err := tx.Query(ctx, `SELECT id, type, name FROM cerebro_credential WHERE workspace_id = $1`, sourceWorkspace)
	if err != nil {
		return connCopied, 0, fmt.Errorf("list credentials: %w", err)
	}
	type cred struct {
		id        pgtype.UUID
		typ, name string
	}
	var creds []cred
	for credRows.Next() {
		var c cred
		if err := credRows.Scan(&c.id, &c.typ, &c.name); err != nil {
			credRows.Close()
			return connCopied, 0, fmt.Errorf("scan credential: %w", err)
		}
		creds = append(creds, c)
	}
	credRows.Close()
	if err := credRows.Err(); err != nil {
		return connCopied, 0, fmt.Errorf("iterate credentials: %w", err)
	}

	var credCopied int64
	for _, c := range creds {
		if _, done, err := lookupMapping(ctx, tx, targetWorkspace, "credential", c.id); err != nil {
			return connCopied, credCopied, err
		} else if done {
			continue
		}
		var targetID pgtype.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM cerebro_credential WHERE workspace_id = $1 AND type = $2 AND name = $3`,
			targetWorkspace, c.typ, c.name).Scan(&targetID)
		if err == pgx.ErrNoRows {
			if err := tx.QueryRow(ctx, `
				INSERT INTO cerebro_credential (id, workspace_id, type, name, description, encrypted_value,
				                                value_hint, metadata, created_by_type, created_by_id,
				                                created_at, updated_at, expires_at, last_rotated_at)
				SELECT gen_random_uuid(), $1, cc.type, cc.name, cc.description, cc.encrypted_value,
				       cc.value_hint, cc.metadata, cc.created_by_type, cc.created_by_id,
				       cc.created_at, now(), cc.expires_at, cc.last_rotated_at
				FROM cerebro_credential cc WHERE cc.id = $2
				RETURNING id`,
				targetWorkspace, c.id).Scan(&targetID); err != nil {
				return connCopied, credCopied, fmt.Errorf("copy credential: %w", err)
			}
			credCopied++
		} else if err != nil {
			return connCopied, credCopied, fmt.Errorf("find credential: %w", err)
		}
		if err := recordMapping(ctx, tx, runID, sourceWorkspace, targetWorkspace, "credential", c.id, targetID, nil, nil); err != nil {
			return connCopied, credCopied, err
		}
		// bindings: remap resource_id to the copied resource (connection/agent)
		// when we have a mapping for it; otherwise carry the binding unchanged so
		// a same-id resource that exists in both workspaces still resolves.
		if err := copyCredentialBindingsTx(ctx, tx, targetWorkspace, c.id, targetID); err != nil {
			return connCopied, credCopied, err
		}
	}

	return connCopied, credCopied, nil
}

// copyCredentialBindingsTx copies cerebro_credential_binding rows for a source
// credential onto the target credential, remapping resource_id to a copied
// connection or agent when one exists. Idempotent (skips bindings already
// present for the target credential + resource).
func copyCredentialBindingsTx(ctx context.Context, tx pgx.Tx, targetWorkspace, sourceCred, targetCred pgtype.UUID) error {
	rows, err := tx.Query(ctx, `SELECT resource_type, resource_id, created_by_type, created_by_id FROM cerebro_credential_binding WHERE credential_id = $1`, sourceCred)
	if err != nil {
		return fmt.Errorf("list credential bindings: %w", err)
	}
	type binding struct {
		resType, byType string
		resID, byID     pgtype.UUID
	}
	var bindings []binding
	for rows.Next() {
		var b binding
		if err := rows.Scan(&b.resType, &b.resID, &b.byType, &b.byID); err != nil {
			rows.Close()
			return fmt.Errorf("scan binding: %w", err)
		}
		bindings = append(bindings, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate bindings: %w", err)
	}
	for _, b := range bindings {
		resID := b.resID
		// remap connection / agent resource ids to the copied counterpart.
		entity := ""
		switch b.resType {
		case "connection":
			entity = "connection"
		case "agent":
			entity = "agent"
		}
		if entity != "" {
			if mapped, ok, err := lookupMapping(ctx, tx, targetWorkspace, entity, b.resID); err != nil {
				return err
			} else if ok {
				resID = mapped
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO cerebro_credential_binding (id, credential_id, resource_type, resource_id, created_by_type, created_by_id, created_at)
			SELECT gen_random_uuid(), $1, $2, $3, $4, $5, now()
			WHERE NOT EXISTS (
				SELECT 1 FROM cerebro_credential_binding
				WHERE credential_id = $1 AND resource_type = $2 AND resource_id = $3
			)`,
			targetCred, b.resType, resID, b.byType, b.byID); err != nil {
			return fmt.Errorf("copy credential binding: %w", err)
		}
	}
	return nil
}

// --- github -----------------------------------------------------------------

// copyGitHubTx links the source workspace's GitHub installations into the
// target. A github_installation.installation_id is GLOBALLY unique (one GitHub
// App install ↔ one row), so the same installation cannot live in two
// workspaces at once and a non-destructive copy can only:
//   - map to the target's own row when it already links that installation, or
//   - (when the installation is free, i.e. not linked anywhere) create the link.
//
// An installation still linked to the SOURCE (or any other workspace) is left
// alone and must be reconnected in the target after the merge — the same
// re-pair pattern used for agent runtimes. Returns the count of rows linked in
// the target (0 when every installation is still held elsewhere).
func copyGitHubTx(ctx context.Context, tx pgx.Tx, runID, sourceWorkspace, targetWorkspace pgtype.UUID) (int64, error) {
	rows, err := tx.Query(ctx, `SELECT id, installation_id FROM github_installation WHERE workspace_id = $1`, sourceWorkspace)
	if err != nil {
		return 0, fmt.Errorf("list github installations: %w", err)
	}
	type inst struct {
		id     pgtype.UUID
		instID int64
	}
	var insts []inst
	for rows.Next() {
		var in inst
		if err := rows.Scan(&in.id, &in.instID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan installation: %w", err)
		}
		insts = append(insts, in)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate installations: %w", err)
	}

	var copied int64
	for _, in := range insts {
		if _, done, err := lookupMapping(ctx, tx, targetWorkspace, "github_installation", in.id); err != nil {
			return copied, err
		} else if done {
			continue
		}
		// where does this installation_id currently live?
		var holderWS, holderID pgtype.UUID
		err := tx.QueryRow(ctx, `SELECT workspace_id, id FROM github_installation WHERE installation_id = $1`, in.instID).Scan(&holderWS, &holderID)
		if err != nil && err != pgx.ErrNoRows {
			return copied, fmt.Errorf("locate installation: %w", err)
		}
		switch {
		case err == nil && holderWS == targetWorkspace:
			// target already links it → map to its row.
			if err := recordMapping(ctx, tx, runID, sourceWorkspace, targetWorkspace, "github_installation", in.id, holderID, nil, nil); err != nil {
				return copied, err
			}
		case err == nil:
			// still held by the source / another workspace → cannot duplicate;
			// must be reconnected in the target after the merge. Skip silently.
			continue
		default:
			// installation free → create the link in the target.
			var targetID pgtype.UUID
			if err := tx.QueryRow(ctx, `
				INSERT INTO github_installation (id, workspace_id, installation_id, account_login, account_type,
				                                 account_avatar_url, connected_by_id, created_at, updated_at)
				SELECT gen_random_uuid(), $1, gi.installation_id, gi.account_login, gi.account_type,
				       gi.account_avatar_url, gi.connected_by_id, gi.created_at, now()
				FROM github_installation gi WHERE gi.id = $2
				RETURNING id`,
				targetWorkspace, in.id).Scan(&targetID); err != nil {
				return copied, fmt.Errorf("copy github installation: %w", err)
			}
			if err := recordMapping(ctx, tx, runID, sourceWorkspace, targetWorkspace, "github_installation", in.id, targetID, nil, nil); err != nil {
				return copied, err
			}
			copied++
		}
	}
	return copied, nil
}

// --- settings ---------------------------------------------------------------

// copyWorkspaceSettingsTx copies the workspace-scoped settings rows
// (cerebro_workspace_settings + cerebro_workspace_auth_settings) into the
// target, only when the target does not already have them (a workspace's own
// settings win; a merge never clobbers the survivor's configured values).
// Returns the count of settings rows inserted.
func copyWorkspaceSettingsTx(ctx context.Context, tx pgx.Tx, runID, sourceWorkspace, targetWorkspace pgtype.UUID) (int64, error) {
	var n int64
	wsTag, err := tx.Exec(ctx, `
		INSERT INTO cerebro_workspace_settings (workspace_id, display_currency, updated_at, updated_by,
		                                        wakeup_max_self_per_issue, wakeup_min_interval_minutes)
		SELECT $1, ws.display_currency, now(), ws.updated_by, ws.wakeup_max_self_per_issue, ws.wakeup_min_interval_minutes
		FROM cerebro_workspace_settings ws WHERE ws.workspace_id = $2
		ON CONFLICT (workspace_id) DO NOTHING`,
		targetWorkspace, sourceWorkspace)
	if err != nil {
		return n, fmt.Errorf("copy workspace settings: %w", err)
	}
	n += wsTag.RowsAffected()

	authTag, err := tx.Exec(ctx, `
		INSERT INTO cerebro_workspace_auth_settings (workspace_id, google_signup_domains, default_role,
		                                             created_at, updated_at, updated_by_user_id, google_workspace_sync_enabled)
		SELECT $1, a.google_signup_domains, a.default_role, a.created_at, now(), a.updated_by_user_id, a.google_workspace_sync_enabled
		FROM cerebro_workspace_auth_settings a WHERE a.workspace_id = $2
		ON CONFLICT (workspace_id) DO NOTHING`,
		targetWorkspace, sourceWorkspace)
	if err != nil {
		return n, fmt.Errorf("copy auth settings: %w", err)
	}
	n += authTag.RowsAffected()
	return n, nil
}

// collectUUIDRows scans a single-column UUID result set, closing rows.
func collectUUIDRows(rows pgx.Rows) ([]pgtype.UUID, error) {
	defer rows.Close()
	var ids []pgtype.UUID
	for rows.Next() {
		var id pgtype.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ids: %w", err)
	}
	return ids, nil
}
