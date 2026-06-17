// CEREBRO-PATCH(workspace-copy): TECH-3582 — copy artifacts (documents, notes,
// reports, plans, decisions, diagrams) into the target workspace.
//
// An artifact is scoped to exactly one of: an issue, a project, or the
// workspace (both scope columns null). Issue- and project-scoped artifacts are
// carried alongside their parent when that issue/project is copied; the
// workspace-scoped ones plus the folder tree are carried by the bulk pass
// (CopyWorkspaceArtifacts). Copy is non-destructive: the source is untouched and
// every copy is recorded in cerebro_workspace_copy_map so re-runs are idempotent.
package workspacecopy

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// sourceArtifact is one artifact row read from the source workspace.
type sourceArtifact struct {
	id            pgtype.UUID
	kind          string
	title         string
	body          string
	metadata      []byte
	authorType    string
	authorID      pgtype.UUID
	folderID      pgtype.UUID
	format        string
	fileURL       pgtype.Text
	fileSizeBytes pgtype.Int8
	createdAt     pgtype.Timestamptz
}

// artifactScope is the target scope a copied artifact is attached to. Exactly
// one of projectID / issueID is set for project/issue scope; both zero means
// workspace scope.
type artifactScope struct {
	projectID pgtype.UUID
	issueID   pgtype.UUID
}

// copyIssueArtifactsTx copies every artifact attached to sourceIssue onto the
// copied targetIssue. Returns the count newly copied.
func copyIssueArtifactsTx(ctx context.Context, tx pgx.Tx, runID, sourceWorkspace, targetWorkspace, sourceIssue, targetIssue pgtype.UUID) (int64, error) {
	rows, err := loadArtifacts(ctx, tx, sourceWorkspace, "issue_id = $2", sourceIssue)
	if err != nil {
		return 0, err
	}
	return insertArtifacts(ctx, tx, runID, sourceWorkspace, targetWorkspace, rows, artifactScope{issueID: targetIssue})
}

// copyProjectArtifactsTx copies every artifact attached to sourceProject onto
// the copied targetProject.
func copyProjectArtifactsTx(ctx context.Context, tx pgx.Tx, runID, sourceWorkspace, targetWorkspace, sourceProject, targetProject pgtype.UUID) (int64, error) {
	rows, err := loadArtifacts(ctx, tx, sourceWorkspace, "project_id = $2", sourceProject)
	if err != nil {
		return 0, err
	}
	return insertArtifacts(ctx, tx, runID, sourceWorkspace, targetWorkspace, rows, artifactScope{projectID: targetProject})
}

// CopyWorkspaceArtifacts copies the workspace-scoped artifacts (those attached
// to neither an issue nor a project) and the whole folder tree from
// sourceWorkspace into targetWorkspace. Folders are copied parents-first and
// remapped; each artifact's folder_id is rewritten to its copied folder.
// Idempotent and non-destructive. This is the bulk ("run once") pass.
func (s *Store) CopyWorkspaceArtifacts(ctx context.Context, runID, sourceWorkspace, targetWorkspace pgtype.UUID) (CopyResult, error) {
	return s.inTx(ctx, func(tx pgx.Tx) (CopyResult, error) {
		if err := copyArtifactFoldersTx(ctx, tx, runID, sourceWorkspace, targetWorkspace); err != nil {
			return CopyResult{}, err
		}
		rows, err := loadArtifacts(ctx, tx, sourceWorkspace, "project_id IS NULL AND issue_id IS NULL", pgtype.UUID{})
		if err != nil {
			return CopyResult{}, err
		}
		copied, err := insertArtifacts(ctx, tx, runID, sourceWorkspace, targetWorkspace, rows, artifactScope{})
		if err != nil {
			return CopyResult{}, err
		}
		return CopyResult{EntityType: "workspace_artifacts", CascadeCopied: copied}, nil
	})
}

// loadArtifacts reads the source artifacts matching whereSQL. When whereSQL
// references $2, pass the bind value in whereArg; otherwise whereArg is ignored.
func loadArtifacts(ctx context.Context, tx pgx.Tx, sourceWorkspace pgtype.UUID, whereSQL string, whereArg pgtype.UUID) ([]sourceArtifact, error) {
	q := `SELECT id, kind, title, body, metadata, author_type, author_id, folder_id,
	             format, file_url, file_size_bytes, created_at
	      FROM artifact WHERE workspace_id = $1 AND ` + whereSQL
	args := []any{sourceWorkspace}
	if whereArg.Valid {
		args = append(args, whereArg)
	}
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("load artifacts: %w", err)
	}
	defer rows.Close()
	var out []sourceArtifact
	for rows.Next() {
		var a sourceArtifact
		if err := rows.Scan(&a.id, &a.kind, &a.title, &a.body, &a.metadata, &a.authorType,
			&a.authorID, &a.folderID, &a.format, &a.fileURL, &a.fileSizeBytes, &a.createdAt); err != nil {
			return nil, fmt.Errorf("scan artifact: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate artifacts: %w", err)
	}
	return out, nil
}

// insertArtifacts copies each source artifact into the target workspace at the
// given scope, remapping folder_id (if the folder was copied, else cleared) and
// an agent author_id (if the agent was copied). Idempotent via the copy map.
func insertArtifacts(ctx context.Context, tx pgx.Tx, runID, sourceWorkspace, targetWorkspace pgtype.UUID, arts []sourceArtifact, scope artifactScope) (int64, error) {
	var copied int64
	for _, a := range arts {
		if _, done, err := lookupMapping(ctx, tx, targetWorkspace, "artifact", a.id); err != nil {
			return copied, err
		} else if done {
			continue
		}

		// folder_id: remap if the folder was copied; otherwise drop it (folder
		// is an orthogonal grouping, safe to clear when not carried).
		folderID := pgtype.UUID{}
		if a.folderID.Valid {
			if mapped, ok, err := lookupMapping(ctx, tx, targetWorkspace, "artifact_folder", a.folderID); err != nil {
				return copied, err
			} else if ok {
				folderID = mapped
			}
		}

		// agent authors are workspace-specific; remap to the copied agent if it
		// exists. member authors are shared across workspaces — keep as-is.
		authorID := a.authorID
		if a.authorType == "agent" {
			if mapped, ok, err := lookupMapping(ctx, tx, targetWorkspace, "agent", a.authorID); err != nil {
				return copied, err
			} else if ok {
				authorID = mapped
			}
		}

		var targetID pgtype.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO artifact (id, workspace_id, project_id, issue_id, kind, title, body, metadata,
			                      author_type, author_id, folder_id, format, file_url, file_size_bytes,
			                      created_at, updated_at)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, now())
			RETURNING id`,
			targetWorkspace, scope.projectID, scope.issueID, a.kind, a.title, a.body, a.metadata,
			a.authorType, authorID, folderID, a.format, a.fileURL, a.fileSizeBytes, a.createdAt).Scan(&targetID); err != nil {
			return copied, fmt.Errorf("copy artifact: %w", err)
		}
		if err := recordMapping(ctx, tx, runID, sourceWorkspace, targetWorkspace, "artifact", a.id, targetID, nil, nil); err != nil {
			return copied, err
		}
		// a note is an artifact with a cerebro_note extension row; carry it (plus
		// versions / comments / references / shares) when present.
		if err := copyNoteExtensionTx(ctx, tx, targetWorkspace, a.id, targetID); err != nil {
			return copied, err
		}
		copied++
	}
	return copied, nil
}

// copyNoteExtensionTx carries the cerebro_note extension of a copied artifact —
// the note row itself plus its versions, comments (threads rebuilt), references
// and shares. A no-op when the source artifact is not a note. The note is copied
// unlocked (locked_by/locked_at cleared) so the target owns a fresh edit lock.
func copyNoteExtensionTx(ctx context.Context, tx pgx.Tx, targetWorkspace, sourceArtifact, targetArtifact pgtype.UUID) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM cerebro_note WHERE artifact_id = $1)`, sourceArtifact).Scan(&exists); err != nil {
		return fmt.Errorf("probe note: %w", err)
	}
	if !exists {
		return nil
	}
	// note row (keyed by artifact_id; copied unlocked). ON CONFLICT keeps a re-run idempotent.
	if _, err := tx.Exec(ctx, `
		INSERT INTO cerebro_note (artifact_id, owner_id, visibility, pinned, pinned_at, created_at, updated_at)
		SELECT $1, n.owner_id, n.visibility, n.pinned, n.pinned_at, n.created_at, now()
		FROM cerebro_note n WHERE n.artifact_id = $2
		ON CONFLICT (artifact_id) DO NOTHING`,
		targetArtifact, sourceArtifact); err != nil {
		return fmt.Errorf("copy note: %w", err)
	}
	// versions (skip ones already present for the target note, by version_no)
	if _, err := tx.Exec(ctx, `
		INSERT INTO cerebro_note_version (id, note_id, version_no, title, body, byte_size, reason, label,
		                                  author_type, author_id, created_at, updated_at)
		SELECT gen_random_uuid(), $1, v.version_no, v.title, v.body, v.byte_size, v.reason, v.label,
		       v.author_type, v.author_id, v.created_at, now()
		FROM cerebro_note_version v
		WHERE v.note_id = $2
		  AND NOT EXISTS (SELECT 1 FROM cerebro_note_version tv WHERE tv.note_id = $1 AND tv.version_no = v.version_no)`,
		targetArtifact, sourceArtifact); err != nil {
		return fmt.Errorf("copy note versions: %w", err)
	}
	// comments with reply threading rebuilt via a source→target id map.
	rows, err := tx.Query(ctx, `
		WITH pairs AS (
			SELECT c.id AS src_id, c.thread_root_id AS src_root, gen_random_uuid() AS tgt_id,
			       c.kind, c.body, c.anchor_quote, c.anchor_prefix, c.anchor_suffix, c.anchor_start, c.anchor_end,
			       c.suggestion_text, c.suggestion_state, c.resolved, c.resolved_by, c.resolved_at,
			       c.author_type, c.author_id, c.created_at
			FROM cerebro_note_comment c WHERE c.note_id = $2
		), ins AS (
			INSERT INTO cerebro_note_comment (id, note_id, thread_root_id, kind, body, anchor_quote, anchor_prefix,
			                                  anchor_suffix, anchor_start, anchor_end, suggestion_text, suggestion_state,
			                                  resolved, resolved_by, resolved_at, author_type, author_id, created_at, updated_at)
			SELECT tgt_id, $1, NULL, kind, body, anchor_quote, anchor_prefix, anchor_suffix, anchor_start, anchor_end,
			       suggestion_text, suggestion_state, resolved, resolved_by, resolved_at, author_type, author_id, created_at, now()
			FROM pairs
		)
		SELECT src_id, tgt_id, src_root FROM pairs`,
		targetArtifact, sourceArtifact)
	if err != nil {
		return fmt.Errorf("copy note comments: %w", err)
	}
	idMap := make(map[[16]byte]pgtype.UUID)
	type link struct{ child, root pgtype.UUID }
	var roots []link
	for rows.Next() {
		var srcID, tgtID, srcRoot pgtype.UUID
		if err := rows.Scan(&srcID, &tgtID, &srcRoot); err != nil {
			rows.Close()
			return fmt.Errorf("scan note comment: %w", err)
		}
		idMap[srcID.Bytes] = tgtID
		if srcRoot.Valid {
			roots = append(roots, link{child: tgtID, root: srcRoot})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate note comments: %w", err)
	}
	var childIDs, rootIDs []pgtype.UUID
	for _, l := range roots {
		if mapped, ok := idMap[l.root.Bytes]; ok {
			childIDs = append(childIDs, l.child)
			rootIDs = append(rootIDs, mapped)
		}
	}
	if len(childIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE cerebro_note_comment AS c SET thread_root_id = m.root_id
			FROM (SELECT unnest($1::uuid[]) AS child_id, unnest($2::uuid[]) AS root_id) m
			WHERE c.id = m.child_id`,
			childIDs, rootIDs); err != nil {
			return fmt.Errorf("rewrite note comment threads: %w", err)
		}
	}
	// references (fresh ids)
	if _, err := tx.Exec(ctx, `
		INSERT INTO cerebro_note_reference (id, note_id, object, type, ref_id, label, url, metadata,
		                                    created_by_type, created_by_id, created_at, updated_at)
		SELECT gen_random_uuid(), $1, r.object, r.type, r.ref_id, r.label, r.url, r.metadata,
		       r.created_by_type, r.created_by_id, r.created_at, now()
		FROM cerebro_note_reference r WHERE r.note_id = $2`,
		targetArtifact, sourceArtifact); err != nil {
		return fmt.Errorf("copy note references: %w", err)
	}
	// shares (by user; users are shared across workspaces)
	if _, err := tx.Exec(ctx, `
		INSERT INTO cerebro_note_share (artifact_id, user_id, created_at)
		SELECT $1, s.user_id, s.created_at FROM cerebro_note_share s WHERE s.artifact_id = $2
		ON CONFLICT DO NOTHING`,
		targetArtifact, sourceArtifact); err != nil {
		return fmt.Errorf("copy note shares: %w", err)
	}
	return nil
}

// copyArtifactFoldersTx copies the source workspace's artifact folder tree into
// the target, parents-first, remapping parent_id to the copied parent. A folder
// with a name that already exists at the same parent in the target is reused
// (mapped to the existing one) rather than duplicated. Idempotent.
func copyArtifactFoldersTx(ctx context.Context, tx pgx.Tx, runID, sourceWorkspace, targetWorkspace pgtype.UUID) error {
	// depth-ordered so a parent is always processed before its children.
	rows, err := tx.Query(ctx, `
		WITH RECURSIVE tree AS (
			SELECT id, parent_id, name, 0 AS depth
			FROM artifact_folder WHERE workspace_id = $1 AND parent_id IS NULL
			UNION ALL
			SELECT f.id, f.parent_id, f.name, t.depth + 1
			FROM artifact_folder f JOIN tree t ON f.parent_id = t.id
		)
		SELECT id, parent_id, name FROM tree ORDER BY depth`,
		sourceWorkspace)
	if err != nil {
		return fmt.Errorf("load folders: %w", err)
	}
	type folder struct {
		id, parent pgtype.UUID
		name       string
	}
	var folders []folder
	for rows.Next() {
		var f folder
		if err := rows.Scan(&f.id, &f.parent, &f.name); err != nil {
			rows.Close()
			return fmt.Errorf("scan folder: %w", err)
		}
		folders = append(folders, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate folders: %w", err)
	}

	for _, f := range folders {
		if _, done, err := lookupMapping(ctx, tx, targetWorkspace, "artifact_folder", f.id); err != nil {
			return err
		} else if done {
			continue
		}
		// remap parent to its copied counterpart (root stays root).
		parent := pgtype.UUID{}
		if f.parent.Valid {
			if mapped, ok, err := lookupMapping(ctx, tx, targetWorkspace, "artifact_folder", f.parent); err != nil {
				return err
			} else if ok {
				parent = mapped
			}
		}
		// reuse an existing same-name folder under the same parent if present
		// (the target's UNIQUE(workspace_id, parent_id, name) would otherwise
		// reject a duplicate); else create it.
		var targetID pgtype.UUID
		err := tx.QueryRow(ctx, `
			SELECT id FROM artifact_folder
			WHERE workspace_id = $1 AND name = $2
			  AND parent_id IS NOT DISTINCT FROM $3`,
			targetWorkspace, f.name, parent).Scan(&targetID)
		if err == pgx.ErrNoRows {
			if err := tx.QueryRow(ctx, `
				INSERT INTO artifact_folder (id, workspace_id, parent_id, name)
				VALUES (gen_random_uuid(), $1, $2, $3) RETURNING id`,
				targetWorkspace, parent, f.name).Scan(&targetID); err != nil {
				return fmt.Errorf("copy folder: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("lookup folder: %w", err)
		}
		if err := recordMapping(ctx, tx, runID, sourceWorkspace, targetWorkspace, "artifact_folder", f.id, targetID, nil, nil); err != nil {
			return err
		}
	}
	return nil
}
