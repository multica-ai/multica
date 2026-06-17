// Package workspacecopy implements the non-destructive "copy workspace contents
// into another workspace" extension (TECH-3582).
//
// Copy, not move: every operation creates NEW rows in the target workspace and
// never deletes or mutates the source. The source workspace remains a full,
// read-only safety net. Each copied entity gets a fresh UUID; an entry in
// cerebro_workspace_copy_map records source->target so (a) re-runs are idempotent,
// (b) internal references can be rewritten to the copied targets, and (c) a
// forwarding pointer can be left on the source for old bookmarks.
//
// CEREBRO-PATCH(workspace-copy): TECH-3582 fork-specific workspace copy engine.
package workspacecopy

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNameConflict is returned when an entity (e.g. agent) cannot be copied
// because the target workspace already has one with the same name. The caller
// resolves it via the keep / rename / overwrite policy (a later slice).
var ErrNameConflict = errors.New("name already exists in target workspace")

// ErrAgentNotCopied is returned when a chat is copied before its agent has been
// copied into the target workspace. Copy agents first.
var ErrAgentNotCopied = errors.New("chat's agent has not been copied into the target workspace yet")

// ErrAssigneeNotCopied is returned when an autopilot's required assignee (an
// agent) has not yet been copied into the target workspace. Copy the agent
// first. Squad assignees are out of scope and must be reassigned manually.
var ErrAssigneeNotCopied = errors.New("autopilot assignee has not been copied into the target workspace yet")

// Store is the database layer for the workspace-copy engine.
type Store struct {
	pool *pgxpool.Pool
}

// New constructs a Store bound to the given pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// CopyResult summarizes one entity copy.
type CopyResult struct {
	EntityType   string      `json:"entity_type"`
	SourceID     pgtype.UUID `json:"source_id"`
	TargetID     pgtype.UUID `json:"target_id"`
	SourceNumber int32       `json:"source_number,omitempty"`
	TargetNumber int32       `json:"target_number,omitempty"`
	Comments     int64       `json:"comments_copied,omitempty"`
	Reactions    int64       `json:"reactions_copied,omitempty"`
	Labels       int64       `json:"labels_copied,omitempty"`
	Attachments  int64       `json:"attachments_copied,omitempty"`
	AlreadyDone  bool        `json:"already_copied,omitempty"`
	// CascadeCopied counts the descendant items copied alongside this root in a
	// cascade copy (sub-issues for an issue, issues for a project). Zero for a
	// plain single-item copy.
	CascadeCopied int64 `json:"cascade_copied,omitempty"`
}

// CopyIssue copies one issue (kind issue/channel/dm) with its comments,
// reactions and labels into targetWorkspace, non-destructively. Idempotent.
func (s *Store) CopyIssue(ctx context.Context, runID, targetWorkspace, sourceIssue pgtype.UUID) (CopyResult, error) {
	return s.inTx(ctx, func(tx pgx.Tx) (CopyResult, error) {
		return copyIssueTx(ctx, tx, runID, targetWorkspace, sourceIssue)
	})
}

// CopyAgent copies one agent into targetWorkspace "parked" (no runtime); the
// owner re-pairs a runtime there via the guide. Instructions and all settings
// travel on the agent row. Returns ErrNameConflict if the name is taken.
func (s *Store) CopyAgent(ctx context.Context, runID, targetWorkspace, sourceAgent pgtype.UUID) (CopyResult, error) {
	return s.inTx(ctx, func(tx pgx.Tx) (CopyResult, error) {
		return copyAgentTx(ctx, tx, runID, targetWorkspace, sourceAgent)
	})
}

// inTx runs fn in a transaction and commits on success.
func (s *Store) inTx(ctx context.Context, fn func(pgx.Tx) (CopyResult, error)) (CopyResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return CopyResult{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)
	res, err := fn(tx)
	if err != nil {
		return CopyResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CopyResult{}, fmt.Errorf("commit: %w", err)
	}
	return res, nil
}

// lookupMapping returns the already-copied target id for (entity, source) in the
// target workspace, or (zero, false) if not yet copied.
func lookupMapping(ctx context.Context, tx pgx.Tx, targetWorkspace pgtype.UUID, entity string, source pgtype.UUID) (pgtype.UUID, bool, error) {
	var target pgtype.UUID
	err := tx.QueryRow(ctx, `
		SELECT target_id FROM cerebro_workspace_copy_map
		WHERE target_workspace_id = $1 AND entity_type = $2 AND source_id = $3`,
		targetWorkspace, entity, source).Scan(&target)
	if err == nil {
		return target, true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, false, nil
	}
	return pgtype.UUID{}, false, fmt.Errorf("mapping lookup: %w", err)
}

// recordMapping writes the source->target mapping row.
func recordMapping(ctx context.Context, tx pgx.Tx, runID, sourceWorkspace, targetWorkspace pgtype.UUID, entity string, source, target pgtype.UUID, srcNum, tgtNum *int32) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO cerebro_workspace_copy_map
		    (run_id, source_workspace_id, target_workspace_id, entity_type, source_id, target_id, source_number, target_number)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		runID, sourceWorkspace, targetWorkspace, entity, source, target, srcNum, tgtNum)
	if err != nil {
		return fmt.Errorf("record mapping (%s): %w", entity, err)
	}
	return nil
}

func copyIssueTx(ctx context.Context, tx pgx.Tx, runID, targetWorkspace, sourceIssue pgtype.UUID) (CopyResult, error) {
	if existing, done, err := lookupMapping(ctx, tx, targetWorkspace, "issue", sourceIssue); err != nil {
		return CopyResult{}, err
	} else if done {
		return CopyResult{EntityType: "issue", SourceID: sourceIssue, TargetID: existing, AlreadyDone: true}, nil
	}

	// source workspace + number (for the mapping/forwarding pointer), plus the
	// project and parent-issue links to remap.
	var sourceWorkspace pgtype.UUID
	var sourceNumber int32
	var srcProject, srcParent pgtype.UUID
	if err := tx.QueryRow(ctx, `SELECT workspace_id, number, project_id, parent_issue_id FROM issue WHERE id = $1`, sourceIssue).
		Scan(&sourceWorkspace, &sourceNumber, &srcProject, &srcParent); err != nil {
		return CopyResult{}, fmt.Errorf("load source issue: %w", err)
	}

	// Remap the project and parent-issue links to their copied counterparts if
	// those were already copied into the target; otherwise leave null. A copy
	// done in a different order (child before parent, issue before project) is
	// healed afterwards by RelinkIssueRelations, so links never point into the
	// source workspace and never silently vanish.
	tgtProject, err := remapIfCopied(ctx, tx, targetWorkspace, "project", srcProject)
	if err != nil {
		return CopyResult{}, err
	}
	tgtParent, err := remapIfCopied(ctx, tx, targetWorkspace, "issue", srcParent)
	if err != nil {
		return CopyResult{}, err
	}

	// reserve next number in target (atomic)
	var targetNumber int32
	if err := tx.QueryRow(ctx,
		`UPDATE workspace SET issue_counter = issue_counter + 1 WHERE id = $1 RETURNING issue_counter`,
		targetWorkspace).Scan(&targetNumber); err != nil {
		return CopyResult{}, fmt.Errorf("reserve number: %w", err)
	}

	// copy the issue row (fresh id). project_id / parent_issue_id carry the remapped
	// targets so a copied issue lands in its copied project and under its copied
	// parent. is_private / classification / position travel with the row so a copied
	// issue keeps its privacy flag, governance label and ordering (else they silently
	// reset to public / 'internal' / 0).
	var targetID pgtype.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO issue (id, workspace_id, title, description, status, priority, kind,
		                   creator_type, creator_id, assignee_type, assignee_id, number,
		                   position, is_private, classification, project_id, parent_issue_id,
		                   created_at, updated_at)
		SELECT gen_random_uuid(), $1, i.title, i.description, i.status, i.priority, i.kind,
		       i.creator_type, i.creator_id, i.assignee_type, i.assignee_id, $2,
		       i.position, i.is_private, i.classification, $4, $5, i.created_at, now()
		FROM issue i WHERE i.id = $3
		RETURNING id`,
		targetWorkspace, targetNumber, sourceIssue, tgtProject, tgtParent).Scan(&targetID); err != nil {
		return CopyResult{}, fmt.Errorf("copy issue: %w", err)
	}

	// comments (fresh ids). We capture each source->target comment id so we can
	// (a) rebuild the parent_id reply threading and (b) remap comment-scoped
	// attachments. type + classification travel so status/system comments keep
	// their kind and classified comments keep their label.
	commentMap, commentsCopied, err := copyCommentsTx(ctx, tx, targetWorkspace, sourceIssue, targetID)
	if err != nil {
		return CopyResult{}, err
	}

	// reactions (new issue id, target workspace)
	rTag, err := tx.Exec(ctx, `
		INSERT INTO issue_reaction (issue_id, workspace_id, actor_type, actor_id, emoji, created_at)
		SELECT $1, $2, r.actor_type, r.actor_id, r.emoji, r.created_at
		FROM issue_reaction r WHERE r.issue_id = $3`,
		targetID, targetWorkspace, sourceIssue)
	if err != nil {
		return CopyResult{}, fmt.Errorf("copy reactions: %w", err)
	}

	// labels: map each source label to a target label (by name; create if missing),
	// then link the copied issue to the target label.
	srcLabels, err := tx.Query(ctx, `SELECT label_id FROM issue_to_label WHERE issue_id = $1`, sourceIssue)
	if err != nil {
		return CopyResult{}, fmt.Errorf("list issue labels: %w", err)
	}
	var labelIDs []pgtype.UUID
	for srcLabels.Next() {
		var lid pgtype.UUID
		if err := srcLabels.Scan(&lid); err != nil {
			srcLabels.Close()
			return CopyResult{}, fmt.Errorf("scan label: %w", err)
		}
		labelIDs = append(labelIDs, lid)
	}
	srcLabels.Close()
	var labelsCopied int64
	for _, lid := range labelIDs {
		targetLabel, err := ensureLabelTx(ctx, tx, runID, sourceWorkspace, targetWorkspace, lid)
		if err != nil {
			return CopyResult{}, err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO issue_to_label (issue_id, label_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			targetID, targetLabel); err != nil {
			return CopyResult{}, fmt.Errorf("link label: %w", err)
		}
		labelsCopied++
	}

	// attachments: both issue-level (issue_id set) and comment-level (comment_id
	// set) attachments for this issue, with issue_id/comment_id remapped to the
	// copies. The blob url is shared (a copy references the same stored file).
	attachCopied, err := copyAttachmentsTx(ctx, tx, targetWorkspace, sourceIssue, targetID, commentMap)
	if err != nil {
		return CopyResult{}, err
	}

	if err := recordMapping(ctx, tx, runID, sourceWorkspace, targetWorkspace, "issue", sourceIssue, targetID, &sourceNumber, &targetNumber); err != nil {
		return CopyResult{}, err
	}

	return CopyResult{
		EntityType:   "issue",
		SourceID:     sourceIssue,
		TargetID:     targetID,
		SourceNumber: sourceNumber,
		TargetNumber: targetNumber,
		Comments:     commentsCopied,
		Reactions:    rTag.RowsAffected(),
		Labels:       labelsCopied,
		Attachments:  attachCopied,
	}, nil
}

// copyCommentsTx copies all comments of sourceIssue onto targetIssue, preserving
// reply threading. It returns a source->target comment id map (used to remap
// comment-scoped attachments) and the number of comments copied.
//
// A single CTE generates each target id alongside its source id so the INSERT and
// the returned pairs see identical UUIDs (the CTE is materialized — it is
// referenced twice and uses the volatile gen_random_uuid()). A second pass rewrites
// parent_id: a reply's parent_id is set to the copied parent so threads survive.
func copyCommentsTx(ctx context.Context, tx pgx.Tx, targetWorkspace, sourceIssue, targetIssue pgtype.UUID) (map[[16]byte]pgtype.UUID, int64, error) {
	rows, err := tx.Query(ctx, `
		WITH pairs AS (
			SELECT c.id AS src_id, c.parent_id AS src_parent, gen_random_uuid() AS tgt_id,
			       c.author_type, c.author_id, c.content, c.type, c.classification, c.created_at
			FROM comment c WHERE c.issue_id = $3
		), ins AS (
			INSERT INTO comment (id, issue_id, workspace_id, author_type, author_id, content, type, classification, created_at, updated_at)
			SELECT tgt_id, $1, $2, author_type, author_id, content, type, classification, created_at, now()
			FROM pairs
		)
		SELECT src_id, tgt_id, src_parent FROM pairs`,
		targetIssue, targetWorkspace, sourceIssue)
	if err != nil {
		return nil, 0, fmt.Errorf("copy comments: %w", err)
	}
	defer rows.Close()

	idMap := make(map[[16]byte]pgtype.UUID)
	type link struct{ child, parent pgtype.UUID }
	var srcParents []link
	for rows.Next() {
		var srcID, tgtID, srcParent pgtype.UUID
		if err := rows.Scan(&srcID, &tgtID, &srcParent); err != nil {
			return nil, 0, fmt.Errorf("scan comment pair: %w", err)
		}
		idMap[srcID.Bytes] = tgtID
		if srcParent.Valid {
			srcParents = append(srcParents, link{child: tgtID, parent: srcParent})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate comment pairs: %w", err)
	}

	// rewrite threading: map each reply's parent to the copied parent comment.
	var childIDs, parentIDs []pgtype.UUID
	for _, l := range srcParents {
		if mappedParent, ok := idMap[l.parent.Bytes]; ok {
			childIDs = append(childIDs, l.child)
			parentIDs = append(parentIDs, mappedParent)
		}
	}
	if len(childIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE comment AS c SET parent_id = m.parent_id
			FROM (SELECT unnest($1::uuid[]) AS child_id, unnest($2::uuid[]) AS parent_id) m
			WHERE c.id = m.child_id`,
			childIDs, parentIDs); err != nil {
			return nil, 0, fmt.Errorf("rewrite comment threads: %w", err)
		}
	}
	return idMap, int64(len(idMap)), nil
}

// copyAttachmentsTx copies the attachments of sourceIssue onto the copied issue,
// remapping issue_id to targetIssue and comment_id to the copied comment (via
// commentMap). Attachments whose comment is not in the map (shouldn't happen for
// this issue) are skipped to avoid dangling references.
func copyAttachmentsTx(ctx context.Context, tx pgx.Tx, targetWorkspace, sourceIssue, targetIssue pgtype.UUID, commentMap map[[16]byte]pgtype.UUID) (int64, error) {
	rows, err := tx.Query(ctx, `
		SELECT issue_id, comment_id, uploader_type, uploader_id, filename, url, content_type, size_bytes, created_at
		FROM attachment
		WHERE issue_id = $1
		   OR comment_id IN (SELECT id FROM comment WHERE issue_id = $1)`,
		sourceIssue)
	if err != nil {
		return 0, fmt.Errorf("list attachments: %w", err)
	}
	type att struct {
		issueID, commentID     pgtype.UUID
		uploaderType, filename string
		uploaderID             pgtype.UUID
		url, contentType       string
		sizeBytes              int64
		createdAt              pgtype.Timestamptz
	}
	var atts []att
	for rows.Next() {
		var a att
		if err := rows.Scan(&a.issueID, &a.commentID, &a.uploaderType, &a.uploaderID,
			&a.filename, &a.url, &a.contentType, &a.sizeBytes, &a.createdAt); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan attachment: %w", err)
		}
		atts = append(atts, a)
	}
	rows.Close()

	var copied int64
	for _, a := range atts {
		// remap issue_id (issue-level attachment) and comment_id (comment-level)
		newIssue := pgtype.UUID{}
		if a.issueID.Valid {
			newIssue = targetIssue
		}
		newComment := pgtype.UUID{}
		if a.commentID.Valid {
			mapped, ok := commentMap[a.commentID.Bytes]
			if !ok {
				continue // comment not part of this issue's copy; skip to avoid dangling ref
			}
			newComment = mapped
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO attachment (id, workspace_id, issue_id, comment_id, uploader_type, uploader_id,
			                        filename, url, content_type, size_bytes, created_at)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			targetWorkspace, newIssue, newComment, a.uploaderType, a.uploaderID,
			a.filename, a.url, a.contentType, a.sizeBytes, a.createdAt); err != nil {
			return 0, fmt.Errorf("copy attachment: %w", err)
		}
		copied++
	}
	return copied, nil
}

// remapIfCopied returns the copied target id for (entity, src) if src is set and
// has already been copied into the target workspace; otherwise a null UUID.
func remapIfCopied(ctx context.Context, tx pgx.Tx, targetWorkspace pgtype.UUID, entity string, src pgtype.UUID) (pgtype.UUID, error) {
	if !src.Valid {
		return pgtype.UUID{}, nil
	}
	mapped, ok, err := lookupMapping(ctx, tx, targetWorkspace, entity, src)
	if err != nil {
		return pgtype.UUID{}, err
	}
	if ok {
		return mapped, nil
	}
	return pgtype.UUID{}, nil
}

// ensureLabelTx maps a source label to a label in the target workspace by name,
// creating the target label if it does not exist. Idempotent via the copy map.
func ensureLabelTx(ctx context.Context, tx pgx.Tx, runID, sourceWorkspace, targetWorkspace, sourceLabel pgtype.UUID) (pgtype.UUID, error) {
	if existing, done, err := lookupMapping(ctx, tx, targetWorkspace, "label", sourceLabel); err != nil {
		return pgtype.UUID{}, err
	} else if done {
		return existing, nil
	}

	var name, color string
	if err := tx.QueryRow(ctx, `SELECT name, color FROM issue_label WHERE id = $1`, sourceLabel).Scan(&name, &color); err != nil {
		return pgtype.UUID{}, fmt.Errorf("load label: %w", err)
	}

	// reuse an existing same-name label in the target if present
	var targetLabel pgtype.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM issue_label WHERE workspace_id = $1 AND name = $2`, targetWorkspace, name).Scan(&targetLabel)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.QueryRow(ctx,
			`INSERT INTO issue_label (id, workspace_id, name, color) VALUES (gen_random_uuid(), $1, $2, $3) RETURNING id`,
			targetWorkspace, name, color).Scan(&targetLabel); err != nil {
			return pgtype.UUID{}, fmt.Errorf("create label: %w", err)
		}
	} else if err != nil {
		return pgtype.UUID{}, fmt.Errorf("find label: %w", err)
	}

	if err := recordMapping(ctx, tx, runID, sourceWorkspace, targetWorkspace, "label", sourceLabel, targetLabel, nil, nil); err != nil {
		return pgtype.UUID{}, err
	}
	return targetLabel, nil
}

func copyAgentTx(ctx context.Context, tx pgx.Tx, runID, targetWorkspace, sourceAgent pgtype.UUID) (CopyResult, error) {
	if existing, done, err := lookupMapping(ctx, tx, targetWorkspace, "agent", sourceAgent); err != nil {
		return CopyResult{}, err
	} else if done {
		return CopyResult{EntityType: "agent", SourceID: sourceAgent, TargetID: existing, AlreadyDone: true}, nil
	}

	var sourceWorkspace pgtype.UUID
	var name string
	if err := tx.QueryRow(ctx, `SELECT workspace_id, name FROM agent WHERE id = $1`, sourceAgent).Scan(&sourceWorkspace, &name); err != nil {
		return CopyResult{}, fmt.Errorf("load source agent: %w", err)
	}

	// name-conflict guard (UNIQUE(workspace_id, name)); resolved by policy slice.
	var dup int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM agent WHERE workspace_id = $1 AND name = $2`, targetWorkspace, name).Scan(&dup); err != nil {
		return CopyResult{}, fmt.Errorf("name check: %w", err)
	}
	if dup > 0 {
		return CopyResult{}, fmt.Errorf("%w: agent %q", ErrNameConflict, name)
	}

	// copy the agent row "parked": runtime_id NULL, status offline, not archived.
	// instructions + all settings columns travel with the row.
	var targetID pgtype.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO agent (id, workspace_id, name, avatar_url, runtime_mode, runtime_config, visibility,
		                   status, max_concurrent_tasks, owner_id, description, runtime_id, instructions,
		                   custom_env, custom_args, mcp_config, model, persona_sandbox,
		                   thinking_level, created_at, updated_at)
		SELECT gen_random_uuid(), $1, a.name, a.avatar_url, a.runtime_mode, a.runtime_config, a.visibility,
		       'offline', a.max_concurrent_tasks, a.owner_id, a.description, NULL, a.instructions,
		       a.custom_env, a.custom_args, a.mcp_config, a.model, a.persona_sandbox,
		       a.thinking_level, a.created_at, now()
		FROM agent a WHERE a.id = $2
		RETURNING id`,
		targetWorkspace, sourceAgent).Scan(&targetID); err != nil {
		return CopyResult{}, fmt.Errorf("copy agent: %w", err)
	}

	if err := recordMapping(ctx, tx, runID, sourceWorkspace, targetWorkspace, "agent", sourceAgent, targetID, nil, nil); err != nil {
		return CopyResult{}, err
	}

	return CopyResult{EntityType: "agent", SourceID: sourceAgent, TargetID: targetID}, nil
}
