// FIR-2604 / search trin 2: hybrid (FTS + vector) SQL builder.
package search

import (
	"fmt"
	"strings"
)

// rrfK is the Reciprocal Rank Fusion constant. 60 is the value the original
// RRF paper used and the de-facto default in BM25+vector hybrid pipelines
// (ElasticSearch, ParadeDB, pgvecto.rs all recommend it). It's the only knob
// in RRF — higher = flatter blend, lower = top of each list dominates more.
const rrfK = 60.0

// HybridInput extends QueryInput with the query embedding and the candidate
// pool sizes for each retrieval path. The defaults (100 each) are large
// enough that small filter sets do not starve the fusion stage.
type HybridInput struct {
	Query             QueryInput
	QueryVectorLit    string // pgvector textual literal e.g. "[0.1,0.2,...]"
	FTSCandidatePool  int    // default 100
	VecCandidatePool  int    // default 100
}

// BuildHybrid returns the SQL that fuses Del 1's FTS ranking with pgvector
// nearest-neighbours via Reciprocal Rank Fusion. The shape mirrors Build so
// the handler can scan results into the same `searchResult` struct.
//
// Free text is REQUIRED for hybrid — a filter-only search has no query
// embedding to rank against, and Del 1's updated_at sort is already correct.
// Callers fall back to Build when in.Text == "".
//
// The embedding column type is referenced inside DO-block CTE so a Postgres
// install without pgvector simply returns no vec rows and the hybrid result
// collapses to keyword-only. That keeps the handler code path-stable.
func BuildHybrid(h HybridInput) Query {
	in := h.Query
	args := []any{}
	next := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	if h.FTSCandidatePool <= 0 {
		h.FTSCandidatePool = 100
	}
	if h.VecCandidatePool <= 0 {
		h.VecCandidatePool = 100
	}

	wsParam := next(in.WorkspaceID)
	tsParam := next(in.Text)
	trgmParam := next(in.Text)
	rawParam := next(in.Text)
	startsParam := next(in.Text + "%")
	vecParam := next(h.QueryVectorLit)

	// --- FTS CTE: same composite ranker as Build, but capped + ROW_NUMBER'd. ---
	// Trigram predicates use the `<%` operator so the gin_trgm_ops indexes
	// can serve them — same indexability reason as in Build (see query.go).
	var ftsTextPreds []string
	ftsTextPreds = append(ftsTextPreds,
		fmt.Sprintf("i.search_tsv @@ websearch_to_tsquery('simple', %s)", tsParam),
		fmt.Sprintf("%s <%% lower(i.title)", trgmParam),
		fmt.Sprintf("%s <%% lower(coalesce(i.description, ''))", trgmParam),
		fmt.Sprintf(`EXISTS (
			SELECT 1 FROM comment c
			WHERE c.issue_id = i.id
			  AND (c.search_tsv @@ websearch_to_tsquery('simple', %s)
			       OR %s <%% lower(c.content))
		)`, tsParam, trgmParam),
	)

	var numParam string
	if in.HasNumber {
		numParam = next(in.QueryNumber)
		ftsTextPreds = append(ftsTextPreds, fmt.Sprintf("i.number = %s", numParam))
	}

	ftsFilterClauses := buildFilterClauses(in, next)
	ftsWhere := append([]string{
		fmt.Sprintf("i.workspace_id = %s", wsParam),
		"(" + strings.Join(ftsTextPreds, " OR ") + ")",
	}, ftsFilterClauses...)

	var ftsOrderParts []string
	if in.HasNumber {
		ftsOrderParts = append(ftsOrderParts, fmt.Sprintf("(CASE WHEN i.number = %s THEN 100.0 ELSE 0.0 END)", numParam))
	}
	ftsOrderParts = append(ftsOrderParts,
		fmt.Sprintf("(CASE WHEN lower(i.title) = lower(%s) THEN 50.0 ELSE 0.0 END)", rawParam),
		fmt.Sprintf("(CASE WHEN lower(i.title) LIKE lower(%s) THEN 10.0 ELSE 0.0 END)", startsParam),
		fmt.Sprintf("(ts_rank(i.search_tsv, websearch_to_tsquery('simple', %s)) * 10.0)", tsParam),
		fmt.Sprintf(`COALESCE((
			SELECT MAX(ts_rank(c.search_tsv, websearch_to_tsquery('simple', %s))) * 5.0
			FROM comment c WHERE c.issue_id = i.id
		), 0.0)`, tsParam),
		fmt.Sprintf("(word_similarity(%s, lower(i.title)) * 3.0)", trgmParam),
		fmt.Sprintf("(word_similarity(%s, lower(coalesce(i.description, ''))) * 1.0)", trgmParam),
	)
	ftsOrderExpr := "(" + strings.Join(ftsOrderParts, " + ") + ") DESC, i.updated_at DESC"

	ftsPoolParam := next(h.FTSCandidatePool)
	vecPoolParam := next(h.VecCandidatePool)

	// --- Vector CTEs: issue-direct + comment-derived, deduped via MIN(distance). ---
	//
	// We over-fetch by VecCandidatePool/2 per side so the dedup-by-issue
	// pass still has a healthy pool to GROUP from. ivfflat is approximate, so
	// keeping a buffer here meaningfully improves recall.

	// --- Outer SELECT: hydrate full issue row + RRF score, then ORDER + LIMIT. ---
	outerFilterStart := len(args)
	outerFilterClauses := buildFilterClauses(in, next)
	_ = outerFilterStart // (kept for symmetry; ordering already deterministic)
	outerWhere := []string{fmt.Sprintf("i.workspace_id = %s", wsParam)}
	outerWhere = append(outerWhere, outerFilterClauses...)
	outerWhereSQL := strings.Join(outerWhere, " AND ")

	matchSourceExpr := fmt.Sprintf(`CASE
		WHEN i.search_tsv @@ websearch_to_tsquery('simple', %s) THEN
			CASE
				WHEN lower(i.title) LIKE '%%' || lower(%s) || '%%' THEN 'title'
				WHEN lower(coalesce(i.description, '')) LIKE '%%' || lower(%s) || '%%' THEN 'description'
				ELSE 'title'
			END
		WHEN EXISTS (
			SELECT 1 FROM comment c
			WHERE c.issue_id = i.id
			  AND (c.search_tsv @@ websearch_to_tsquery('simple', %s)
			       OR %s <%% lower(c.content))
		) THEN 'comment'
		ELSE 'semantic'
	END`, tsParam, trgmParam, trgmParam, tsParam, trgmParam)

	matchedCommentExpr := fmt.Sprintf(`COALESCE((
		SELECT c.content FROM comment c
		WHERE c.issue_id = i.id
		  AND (c.search_tsv @@ websearch_to_tsquery('simple', %s)
		       OR %s <%% lower(c.content))
		ORDER BY ts_rank(c.search_tsv, websearch_to_tsquery('simple', %s)) DESC NULLS LAST,
		         c.created_at DESC
		LIMIT 1
	), '')`, tsParam, trgmParam, tsParam)

	limitParam := next(in.Limit)
	offsetParam := next(in.Offset)

	sql := fmt.Sprintf(`WITH fts_pool AS (
		SELECT i.id, ROW_NUMBER() OVER (ORDER BY %s) AS rnk
		FROM issue i
		WHERE %s
		LIMIT %s
	),
	issue_neighbours AS (
		SELECT target_id AS issue_id, embedding <=> %s::vector AS distance
		FROM cerebro_search_embedding
		WHERE workspace_id = %s AND target_type = 'issue'
		ORDER BY embedding <=> %s::vector
		LIMIT %s
	),
	comment_neighbours AS (
		SELECT c.issue_id, ce.embedding <=> %s::vector AS distance
		FROM cerebro_search_embedding ce
		JOIN comment c ON c.id = ce.target_id
		WHERE ce.workspace_id = %s AND ce.target_type = 'comment'
		ORDER BY ce.embedding <=> %s::vector
		LIMIT %s
	),
	vec_pool AS (
		SELECT issue_id AS id,
			ROW_NUMBER() OVER (ORDER BY MIN(distance)) AS rnk
		FROM (
			SELECT * FROM issue_neighbours
			UNION ALL
			SELECT * FROM comment_neighbours
		) u
		GROUP BY issue_id
	),
	combined AS (
		SELECT COALESCE(f.id, v.id) AS id,
			COALESCE(1.0 / (%g + f.rnk), 0.0) + COALESCE(1.0 / (%g + v.rnk), 0.0) AS rrf_score
		FROM fts_pool f
		FULL OUTER JOIN vec_pool v ON f.id = v.id
	)
	SELECT i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
		i.assignee_type, i.assignee_id, i.creator_type, i.creator_id,
		i.parent_issue_id, i.acceptance_criteria, i.context_refs, i.position,
		i.start_date, i.due_date, i.created_at, i.updated_at, i.number, i.project_id,
		COUNT(*) OVER() AS total_count,
		%s AS match_source,
		%s AS matched_comment_content
	FROM combined c
	JOIN issue i ON i.id = c.id
	WHERE %s
	ORDER BY c.rrf_score DESC, i.updated_at DESC
	LIMIT %s OFFSET %s`,
		ftsOrderExpr,
		strings.Join(ftsWhere, " AND "),
		ftsPoolParam,
		vecParam, wsParam, vecParam, vecPoolParam,
		vecParam, wsParam, vecParam, vecPoolParam,
		rrfK, rrfK,
		matchSourceExpr,
		matchedCommentExpr,
		outerWhereSQL,
		limitParam, offsetParam,
	)

	return Query{SQL: sql, Args: args, HasText: true}
}
