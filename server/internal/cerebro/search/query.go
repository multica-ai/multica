package search

import (
	"fmt"
	"strings"
)

// Query is the FIR-2595 FTS-based replacement for the LIKE-driven SQL in
// handler.buildSearchQuery. It returns the full SELECT and the parameter
// slice (no placeholder slots — every value is filled before return) ready
// to hand to pgx Query.
type Query struct {
	SQL  string
	Args []any
	// HasText is true when free-text was supplied (and thus tsquery / trgm
	// rankers contribute to ORDER BY). When false (filters-only search), the
	// handler sorts by updated_at DESC alone.
	HasText bool
}

// QueryInput captures everything the handler resolved from the request:
// workspace, parsed free-text, filters mapped to UUIDs / enum values, plus
// pagination. Each filter slice is OR-within, AND-across (matches at least
// one value per active filter category).
type QueryInput struct {
	WorkspaceID  any // pgtype.UUID
	Text         string
	QueryNumber  int
	HasNumber    bool
	StatusValues []string // already expanded via StatusMap
	// FromUserIDs are user_ids resolved from `from:` filters. Matches issue
	// creators OR authors of any comment on the issue.
	FromUserIDs []any // []pgtype.UUID
	// FromAgentIDs are agent_ids resolved from `from:` filters.
	FromAgentIDs []any
	// AssigneeUserIDs / AssigneeAgentIDs are resolved from `assignee:`.
	AssigneeUserIDs  []any
	AssigneeAgentIDs []any
	// AssigneeUnassigned set when assignee:none / assignee:unassigned was
	// requested. Matches issues with NULL assignee_id.
	AssigneeUnassigned bool
	// ProjectIDs are resolved from `project:`.
	ProjectIDs []any
	// HasAttachment narrows to issues with at least one attachment.
	HasAttachment bool

	IncludeClosed bool
	Limit         int
	Offset        int
}

// trigramThreshold is the pg_trgm similarity cutoff for fuzzy fallback.
// 0.3 matches Postgres' default `set_limit()` and lets minor typos / endings
// through without flooding results with weak matches.
const trigramThreshold = 0.3

// buildFilterClauses returns the AND-ed WHERE predicates that the inline
// filters (status / from / assignee / project / has) contribute, given the
// `next` parameter allocator. Workspace and text predicates are emitted by
// the caller because their treatment differs between the FTS Build and the
// hybrid CTE in BuildHybrid.
//
// Returning a [][]string{(or-group)} lets the caller decide how to compose
// the groups; the current callers always join with " AND ".
func buildFilterClauses(in QueryInput, next func(any) string) []string {
	var out []string

	if len(in.StatusValues) > 0 {
		params := make([]string, 0, len(in.StatusValues))
		for _, s := range in.StatusValues {
			params = append(params, next(s))
		}
		out = append(out, fmt.Sprintf("i.status IN (%s)", strings.Join(params, ",")))
	} else if !in.IncludeClosed {
		out = append(out, "i.status NOT IN ('done', 'cancelled')")
	}

	if len(in.FromUserIDs) > 0 || len(in.FromAgentIDs) > 0 {
		var fromOrs []string
		if len(in.FromUserIDs) > 0 {
			ps := paramList(next, in.FromUserIDs)
			fromOrs = append(fromOrs, fmt.Sprintf("(i.creator_type = 'member' AND i.creator_id IN (%s))", ps))
			fromOrs = append(fromOrs, fmt.Sprintf(`EXISTS (
				SELECT 1 FROM comment c
				WHERE c.issue_id = i.id AND c.author_type = 'member' AND c.author_id IN (%s)
			)`, ps))
		}
		if len(in.FromAgentIDs) > 0 {
			ps := paramList(next, in.FromAgentIDs)
			fromOrs = append(fromOrs, fmt.Sprintf("(i.creator_type = 'agent' AND i.creator_id IN (%s))", ps))
			fromOrs = append(fromOrs, fmt.Sprintf(`EXISTS (
				SELECT 1 FROM comment c
				WHERE c.issue_id = i.id AND c.author_type = 'agent' AND c.author_id IN (%s)
			)`, ps))
		}
		out = append(out, "("+strings.Join(fromOrs, " OR ")+")")
	}

	if in.AssigneeUnassigned ||
		len(in.AssigneeUserIDs) > 0 || len(in.AssigneeAgentIDs) > 0 {
		var assignOrs []string
		if in.AssigneeUnassigned {
			assignOrs = append(assignOrs, "i.assignee_id IS NULL")
		}
		if len(in.AssigneeUserIDs) > 0 {
			ps := paramList(next, in.AssigneeUserIDs)
			assignOrs = append(assignOrs, fmt.Sprintf("(i.assignee_type = 'member' AND i.assignee_id IN (%s))", ps))
		}
		if len(in.AssigneeAgentIDs) > 0 {
			ps := paramList(next, in.AssigneeAgentIDs)
			assignOrs = append(assignOrs, fmt.Sprintf("(i.assignee_type = 'agent' AND i.assignee_id IN (%s))", ps))
		}
		out = append(out, "("+strings.Join(assignOrs, " OR ")+")")
	}

	if len(in.ProjectIDs) > 0 {
		ps := paramList(next, in.ProjectIDs)
		out = append(out, fmt.Sprintf("i.project_id IN (%s)", ps))
	}

	if in.HasAttachment {
		out = append(out, "EXISTS (SELECT 1 FROM attachment a WHERE a.issue_id = i.id)")
	}

	return out
}

// Build produces the final SQL + arg list. It assembles the query in stages
// so each filter section stays readable; pgx handles parameter binding.
func Build(in QueryInput) Query {
	args := []any{}
	next := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	// $1 = workspace_id (always present)
	wsParam := next(in.WorkspaceID)

	// $2..$3 = text params. We always allocate them so the SQL is shape-
	// stable. When in.Text == "" they bind to empty strings and the tsquery
	// branch falls through harmlessly.
	tsParam := next(in.Text)       // for websearch_to_tsquery
	trgmParam := next(in.Text)     // for similarity()
	rawParam := next(in.Text)      // for exact-title compare
	startsParam := next(in.Text + "%")

	hasText := strings.TrimSpace(in.Text) != ""

	// --- WHERE clause: text predicates ---
	var textPreds []string
	if hasText {
		// FTS hit on title+description (the GENERATED column)
		textPreds = append(textPreds, fmt.Sprintf("i.search_tsv @@ websearch_to_tsquery('simple', %s)", tsParam))
		// Trigram fallback on title / description for spell-tolerance
		textPreds = append(textPreds, fmt.Sprintf("word_similarity(%s, lower(i.title)) > %.2f", trgmParam, trigramThreshold))
		textPreds = append(textPreds, fmt.Sprintf("word_similarity(%s, lower(coalesce(i.description, ''))) > %.2f", trgmParam, trigramThreshold))
		// FTS or trigram hit on a comment of this issue
		textPreds = append(textPreds, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM comment c
			WHERE c.issue_id = i.id
			  AND (c.search_tsv @@ websearch_to_tsquery('simple', %s)
			       OR word_similarity(%s, lower(c.content)) > %.2f)
		)`, tsParam, trgmParam, trigramThreshold))
	}

	// Optional identifier-number match.
	var numParam string
	if in.HasNumber {
		numParam = next(in.QueryNumber)
		// Number match is an OR alongside text — so `MUL-42 deploy` still
		// matches MUL-42 even if it doesn't FTS-match "deploy".
		textPreds = append(textPreds, fmt.Sprintf("i.number = %s", numParam))
	}

	whereParts := []string{fmt.Sprintf("i.workspace_id = %s", wsParam)}
	if len(textPreds) > 0 {
		whereParts = append(whereParts, "("+strings.Join(textPreds, " OR ")+")")
	}

	// --- WHERE clause: filters ---
	whereParts = append(whereParts, buildFilterClauses(in, next)...)
	whereClause := strings.Join(whereParts, " AND ")

	// --- ORDER BY ---
	//
	// Composite relevance score combines four signals:
	//   * identifier-number match — overrides everything (tier 0)
	//   * exact-title match — strong boost
	//   * ts_rank on title+description (weights set in the migration)
	//   * comment ts_rank — same scale, weaker because the comment lives
	//     on the row, not the issue body
	//   * trigram similarity — fuzzy fallback so typos still surface
	//
	// Without free text we sort by updated_at — the filter set already did
	// the relevance work.
	var orderExpr string
	if hasText {
		var orderParts []string
		if in.HasNumber {
			orderParts = append(orderParts, fmt.Sprintf("(CASE WHEN i.number = %s THEN 100.0 ELSE 0.0 END)", numParam))
		}
		orderParts = append(orderParts,
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
		orderExpr = "(" + strings.Join(orderParts, " + ") + ") DESC, i.updated_at DESC"
	} else {
		orderExpr = "i.updated_at DESC"
	}

	// --- match_source: which field carried the strongest signal ---
	matchSourceExpr := "'title'"
	if hasText {
		matchSourceExpr = fmt.Sprintf(`CASE
			WHEN i.search_tsv @@ websearch_to_tsquery('simple', %s) THEN
				CASE
					WHEN lower(i.title) LIKE '%%' || lower(%s) || '%%' THEN 'title'
					WHEN lower(coalesce(i.description, '')) LIKE '%%' || lower(%s) || '%%' THEN 'description'
					ELSE 'title'
				END
			ELSE 'comment'
		END`, tsParam, trgmParam, trgmParam)
	}

	// --- matched_comment_content: best-ranked matching comment (snippet
	// source). Always populated when free text was supplied so the UI can
	// show a comment snippet next to title-only matches.
	matchedCommentExpr := "''"
	if hasText {
		matchedCommentExpr = fmt.Sprintf(`COALESCE((
			SELECT c.content FROM comment c
			WHERE c.issue_id = i.id
			  AND (c.search_tsv @@ websearch_to_tsquery('simple', %s)
			       OR word_similarity(%s, lower(c.content)) > %.2f)
			ORDER BY ts_rank(c.search_tsv, websearch_to_tsquery('simple', %s)) DESC NULLS LAST,
			         c.created_at DESC
			LIMIT 1
		), '')`, tsParam, trgmParam, trigramThreshold, tsParam)
	}

	// --- pagination ---
	limitParam := next(in.Limit)
	offsetParam := next(in.Offset)

	sql := fmt.Sprintf(`SELECT i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
		i.assignee_type, i.assignee_id, i.creator_type, i.creator_id,
		i.parent_issue_id, i.acceptance_criteria, i.context_refs, i.position,
		i.start_date, i.due_date, i.created_at, i.updated_at, i.number, i.project_id,
		COUNT(*) OVER() AS total_count,
		%s AS match_source,
		%s AS matched_comment_content
	FROM issue i
	WHERE %s
	ORDER BY %s
	LIMIT %s OFFSET %s`,
		matchSourceExpr,
		matchedCommentExpr,
		whereClause,
		orderExpr,
		limitParam,
		offsetParam,
	)

	return Query{SQL: sql, Args: args, HasText: hasText}
}

func paramList(next func(any) string, values []any) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = next(v)
	}
	return strings.Join(parts, ",")
}
