// CEREBRO-PATCH(workspace-copy): TECH-3732 — rewrite internal references in
// copied text so they point at the copies, not the originals.
//
// Structural links (issue->parent, issue->project) are healed by
// RelinkIssueRelations. But references written *inside* free text are not:
//
//   - mention links to other issues: `[label](mention://issue/<source-uuid>)`
//     still carry the source issue's UUID, so clicking them jumps back into
//     the source workspace.
//   - bare identifier tokens like "TECH-123" in a description or comment still
//     read as the source number (the copy gets a fresh number, e.g. FIR-7).
//
// RewriteInternalReferences walks every issue copied into the target and, for
// the descriptions + comment bodies, rewrites both forms to their copied
// counterparts using the copy map. Only references whose target was actually
// copied are rewritten; everything else is left untouched. Idempotent: a second
// pass finds nothing left to change. Never touches the source workspace.
package workspacecopy

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// RewriteResult reports how many copied rows had references rewritten.
type RewriteResult struct {
	IssuesRewritten   int64 `json:"issues_rewritten,omitempty"`
	CommentsRewritten int64 `json:"comments_rewritten,omitempty"`
}

var nonAlphaRe = regexp.MustCompile(`[^a-zA-Z]`)

// issuePrefixForTx returns a workspace's issue_prefix, falling back to the
// upstream rule (first 3 alpha chars of the name, "WS" if none) when the column
// is empty — mirrors handler.generateIssuePrefix so rewritten identifiers match
// exactly what the UI renders.
func issuePrefixForTx(ctx context.Context, tx pgx.Tx, ws pgtype.UUID) (string, error) {
	var prefix, name string
	if err := tx.QueryRow(ctx, `SELECT issue_prefix, name FROM workspace WHERE id = $1`, ws).Scan(&prefix, &name); err != nil {
		return "", fmt.Errorf("load workspace prefix: %w", err)
	}
	if prefix != "" {
		return prefix, nil
	}
	letters := strings.ToUpper(nonAlphaRe.ReplaceAllString(name, ""))
	if letters == "" {
		return "WS", nil
	}
	if len(letters) > 3 {
		letters = letters[:3]
	}
	return letters, nil
}

// issueMapping is one source->target issue row from the copy map, enriched with
// the source workspace so we can resolve the source identifier prefix.
type issueMapping struct {
	sourceWorkspace pgtype.UUID
	sourceID        pgtype.UUID
	targetID        pgtype.UUID
	sourceNumber    int32
	targetNumber    int32
}

// RewriteInternalReferences rewrites mention-link UUIDs and bare identifier
// tokens inside the descriptions + comments of every issue copied into the
// target workspace, so internal references resolve to the copies.
func (s *Store) RewriteInternalReferences(ctx context.Context, targetWorkspace pgtype.UUID) (RewriteResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return RewriteResult{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	// every issue-shaped mapping for this target (issue/channel/dm all stored
	// under entity_type 'issue').
	rows, err := tx.Query(ctx, `
		SELECT source_workspace_id, source_id, target_id, source_number, target_number
		FROM cerebro_workspace_copy_map
		WHERE target_workspace_id = $1 AND entity_type = 'issue'`,
		targetWorkspace)
	if err != nil {
		return RewriteResult{}, fmt.Errorf("load issue mappings: %w", err)
	}
	var mappings []issueMapping
	for rows.Next() {
		var m issueMapping
		var srcNum, tgtNum pgtype.Int4
		if err := rows.Scan(&m.sourceWorkspace, &m.sourceID, &m.targetID, &srcNum, &tgtNum); err != nil {
			rows.Close()
			return RewriteResult{}, fmt.Errorf("scan mapping: %w", err)
		}
		m.sourceNumber = srcNum.Int32
		m.targetNumber = tgtNum.Int32
		mappings = append(mappings, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return RewriteResult{}, fmt.Errorf("iterate mappings: %w", err)
	}
	if len(mappings) == 0 {
		if err := tx.Commit(ctx); err != nil {
			return RewriteResult{}, fmt.Errorf("commit: %w", err)
		}
		return RewriteResult{}, nil
	}

	targetPrefix, err := issuePrefixForTx(ctx, tx, targetWorkspace)
	if err != nil {
		return RewriteResult{}, err
	}

	// Build the rewriter:
	//
	//   - mentionReplacer: global, literal `mention://issue/<src>` ->
	//     `mention://issue/<tgt>` for every copied issue. UUID-exact, so it is
	//     unambiguous and always idempotent regardless of prefixes.
	//   - byWorkspace: per *source workspace*, an identifier-token rewriter
	//     (PREFIX-<num> -> targetPrefix-<targetNum>). Scoping per source
	//     workspace — not just per prefix — keeps two source workspaces that
	//     share a prefix from colliding on the same number. A token is only
	//     rewritten using the map of the issue's own source workspace.
	//
	// Identifier-token rewriting is SKIPPED when a source workspace's prefix
	// equals the target prefix: a rewritten token would then carry the same
	// prefix and re-match the regex on a later pass (relink/cascade re-run),
	// double-rewriting and corrupting the reference. In that case the prefix
	// is identical anyway, so only the number would change — ambiguous and not
	// worth the idempotency hazard. Mention-link UUIDs still heal in that case.
	var mentionPairs []string
	type wsRewriter struct {
		re   *regexp.Regexp
		nums map[int]string // srcNum -> target identifier
	}
	byWorkspace := map[[16]byte]*wsRewriter{}
	srcPrefixCache := map[[16]byte]string{}
	for _, m := range mappings {
		srcUUID := uuid.UUID(m.sourceID.Bytes).String()
		tgtUUID := uuid.UUID(m.targetID.Bytes).String()
		mentionPairs = append(mentionPairs,
			"mention://issue/"+srcUUID, "mention://issue/"+tgtUUID)

		srcPrefix, ok := srcPrefixCache[m.sourceWorkspace.Bytes]
		if !ok {
			srcPrefix, err = issuePrefixForTx(ctx, tx, m.sourceWorkspace)
			if err != nil {
				return RewriteResult{}, err
			}
			srcPrefixCache[m.sourceWorkspace.Bytes] = srcPrefix
		}
		if srcPrefix == "" || strings.EqualFold(srcPrefix, targetPrefix) ||
			m.sourceNumber == 0 || m.targetNumber == 0 {
			continue
		}
		wr := byWorkspace[m.sourceWorkspace.Bytes]
		if wr == nil {
			wr = &wsRewriter{
				re:   regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(srcPrefix) + `-(\d+)\b`),
				nums: map[int]string{},
			}
			byWorkspace[m.sourceWorkspace.Bytes] = wr
		}
		wr.nums[int(m.sourceNumber)] = fmt.Sprintf("%s-%d", targetPrefix, m.targetNumber)
	}
	mentionReplacer := strings.NewReplacer(mentionPairs...)

	rewrite := func(sourceWorkspace [16]byte, text string) string {
		if text == "" {
			return text
		}
		// 1. identifier tokens (mention labels + bare references), using only
		//    this issue's source workspace map. Numbers absent from the map are
		//    left as-is. Skipped entirely for equal-prefix source workspaces.
		if wr := byWorkspace[sourceWorkspace]; wr != nil {
			text = wr.re.ReplaceAllStringFunc(text, func(match string) string {
				dash := strings.LastIndex(match, "-")
				if dash < 0 {
					return match
				}
				n, convErr := strconv.Atoi(match[dash+1:])
				if convErr != nil {
					return match
				}
				if tgt, ok := wr.nums[n]; ok {
					return tgt
				}
				return match
			})
		}
		// 2. mention-link hrefs (UUID-exact, unambiguous).
		return mentionReplacer.Replace(text)
	}

	// rewrite each copied issue description + its comments.
	var issuesRewritten, commentsRewritten int64
	for _, m := range mappings {
		var desc pgtype.Text
		if err := tx.QueryRow(ctx, `SELECT description FROM issue WHERE id = $1`, m.targetID).Scan(&desc); err != nil {
			return RewriteResult{}, fmt.Errorf("load target description: %w", err)
		}
		if desc.Valid {
			if nv := rewrite(m.sourceWorkspace.Bytes, desc.String); nv != desc.String {
				if _, err := tx.Exec(ctx, `UPDATE issue SET description = $1, updated_at = now() WHERE id = $2`, nv, m.targetID); err != nil {
					return RewriteResult{}, fmt.Errorf("update description: %w", err)
				}
				issuesRewritten++
			}
		}

		crows, err := tx.Query(ctx, `SELECT id, content FROM comment WHERE issue_id = $1`, m.targetID)
		if err != nil {
			return RewriteResult{}, fmt.Errorf("load target comments: %w", err)
		}
		type cmt struct {
			id      pgtype.UUID
			content string
		}
		var comments []cmt
		for crows.Next() {
			var c cmt
			if err := crows.Scan(&c.id, &c.content); err != nil {
				crows.Close()
				return RewriteResult{}, fmt.Errorf("scan comment: %w", err)
			}
			comments = append(comments, c)
		}
		crows.Close()
		if err := crows.Err(); err != nil {
			return RewriteResult{}, fmt.Errorf("iterate comments: %w", err)
		}
		for _, c := range comments {
			if nv := rewrite(m.sourceWorkspace.Bytes, c.content); nv != c.content {
				if _, err := tx.Exec(ctx, `UPDATE comment SET content = $1, updated_at = now() WHERE id = $2`, nv, c.id); err != nil {
					return RewriteResult{}, fmt.Errorf("update comment: %w", err)
				}
				commentsRewritten++
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return RewriteResult{}, fmt.Errorf("commit: %w", err)
	}
	return RewriteResult{IssuesRewritten: issuesRewritten, CommentsRewritten: commentsRewritten}, nil
}
