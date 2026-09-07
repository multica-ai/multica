package handler

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

type searchParityRow struct {
	id                    string
	matchSource           string
	matchedCommentContent string
}

// TestBuildSearchQuery_CandidateFirstParity compares the candidate-first query
// with the exact parent-commit buildSearchQuery against the same database rows.
// The matrix deliberately covers matches spread across fields and comments,
// because those are the cases most likely to be changed accidentally by a
// candidate aggregation. The legacy query has no final ID tie-breaker, so every
// matching fixture has a distinct updated_at; deterministic ties are tested as
// a new candidate-first property rather than attributed to legacy behavior.
func TestBuildSearchQuery_CandidateFirstParity(t *testing.T) {
	token := fmt.Sprintf("mulparity%d", time.Now().UnixNano())
	baseTime := time.Now().Add(-time.Hour).UTC()

	exactTitle := token + " exact title"
	exactID := dbfx.Issue(t, exactTitle, testutil.Cols{
		"description": nil,
		"updated_at":  baseTime.Add(time.Minute),
	})
	dbfx.Issue(t, token+" phrase starts here", testutil.Cols{
		"description": "",
		"updated_at":  baseTime.Add(2 * time.Minute),
	})
	dbfx.Issue(t, "prefix "+token+" phrase contained", testutil.Cols{
		"description": "unrelated",
		"updated_at":  baseTime.Add(3 * time.Minute),
	})
	descriptionID := dbfx.Issue(t, "description source "+token, testutil.Cols{
		"description": "the " + token + " description phrase lives here",
		"updated_at":  baseTime.Add(4 * time.Minute),
	})

	crossFieldID := dbfx.Issue(t, token+" title-part", testutil.Cols{
		"description": "cross-part",
		"updated_at":  baseTime.Add(5 * time.Minute),
	})
	dbfx.Comment(t, crossFieldID, "fields-part", testutil.Cols{
		"created_at": baseTime.Add(5 * time.Minute),
	})

	sameCommentID := dbfx.Issue(t, "same-comment source "+token, testutil.Cols{
		"updated_at": baseTime.Add(6 * time.Minute),
	})
	dbfx.Comment(t, sameCommentID, token+" same gap all", testutil.Cols{
		"created_at": baseTime.Add(6 * time.Minute),
	})

	splitCommentID := dbfx.Issue(t, "split-comment source "+token, testutil.Cols{
		"updated_at": baseTime.Add(7 * time.Minute),
	})
	dbfx.Comment(t, splitCommentID, token, testutil.Cols{"created_at": baseTime.Add(7 * time.Minute)})
	dbfx.Comment(t, splitCommentID, "same", testutil.Cols{"created_at": baseTime.Add(8 * time.Minute)})
	dbfx.Comment(t, splitCommentID, "all", testutil.Cols{"created_at": baseTime.Add(9 * time.Minute)})

	latestCommentID := dbfx.Issue(t, "latest-comment source", testutil.Cols{
		"updated_at": baseTime.Add(10 * time.Minute),
	})
	dbfx.Comment(t, latestCommentID, token+" snippet older", testutil.Cols{
		"created_at": baseTime.Add(10 * time.Minute),
	})
	latestComment := token + " snippet newer"
	dbfx.Comment(t, latestCommentID, latestComment, testutil.Cols{
		"created_at": baseTime.Add(11 * time.Minute),
	})

	dbfx.Issue(t, "special characters", testutil.Cols{
		"description": token + ` 100% under_score path\segment`,
		"updated_at":  baseTime.Add(12 * time.Minute),
	})
	dbfx.Issue(t, token+" done result", testutil.Cols{
		"status":     "done",
		"updated_at": baseTime.Add(13 * time.Minute),
	})
	dbfx.Issue(t, token+" custom terminal", testutil.Cols{
		"status":     "custom_done",
		"updated_at": baseTime.Add(14 * time.Minute),
	})
	dbfx.Issue(t, token+" cancelled exact", testutil.Cols{
		"status":     "cancelled",
		"updated_at": baseTime.Add(15 * time.Minute),
	})

	foreignWorkspaceID := dbfx.Workspace(t, "Search parity foreign", token+"-foreign")
	foreignIssueID := dbfx.Issue(t, token+" foreign issue", testutil.Cols{
		"workspace_id": foreignWorkspaceID,
		"updated_at":   baseTime.Add(16 * time.Minute),
	})
	dbfx.Comment(t, foreignIssueID, token+" foreign comment", testutil.Cols{
		"workspace_id": foreignWorkspaceID,
		"created_at":   baseTime.Add(16 * time.Minute),
	})

	var exactNumber int
	if err := testPool.QueryRow(context.Background(), `SELECT number FROM issue WHERE id = $1`, exactID).Scan(&exactNumber); err != nil {
		t.Fatalf("load exact issue number: %v", err)
	}

	cases := []struct {
		name          string
		phrase        string
		queryNum      int
		hasNum        bool
		includeClosed bool
		terminalKeys  []string
		limit         int
		offset        int
	}{
		{name: "single term", phrase: token, terminalKeys: []string{"done", "cancelled", "custom_done"}, limit: 50},
		{name: "two word phrase and all terms", phrase: token + " phrase", terminalKeys: []string{"done", "cancelled", "custom_done"}, limit: 50},
		{name: "three terms across title description and comment", phrase: token + " cross-part fields-part", terminalKeys: []string{"done", "cancelled", "custom_done"}, limit: 50},
		{name: "same comment versus split comments", phrase: token + " same all", terminalKeys: []string{"done", "cancelled", "custom_done"}, limit: 50},
		{name: "latest matching comment", phrase: token + " snippet", terminalKeys: []string{"done", "cancelled", "custom_done"}, limit: 50},
		{name: "identifier", phrase: fmt.Sprintf("HAN-%d", exactNumber), queryNum: exactNumber, hasNum: true, includeClosed: true, limit: 50},
		{name: "bare issue number", phrase: fmt.Sprint(exactNumber), queryNum: exactNumber, hasNum: true, includeClosed: true, limit: 50},
		{name: "exact title", phrase: exactTitle, includeClosed: true, limit: 50},
		{name: "percent escape", phrase: token + " 100%", includeClosed: true, limit: 50},
		{name: "underscore escape", phrase: token + " under_score", includeClosed: true, limit: 50},
		{name: "backslash escape", phrase: token + ` path\segment`, includeClosed: true, limit: 50},
		{name: "exclude terminal statuses", phrase: token, terminalKeys: []string{"done", "cancelled", "custom_done"}, limit: 50},
		{name: "include closed and cancelled demotion", phrase: token, includeClosed: true, limit: 50},
		{name: "limit and offset", phrase: token, includeClosed: true, limit: 3, offset: 2},
		{name: "no results", phrase: token + " absent-value", includeClosed: true, limit: 50},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			terms := splitSearchTerms(tt.phrase)
			legacyQuery, legacyArgs := buildLegacySearchQueryForParity(
				tt.phrase, append([]string(nil), terms...), tt.queryNum, tt.hasNum, tt.includeClosed, tt.terminalKeys,
			)
			legacyArgs[3] = testWorkspaceID
			legacyArgs[len(legacyArgs)-2] = tt.limit
			legacyArgs[len(legacyArgs)-1] = tt.offset

			candidateQuery, candidateArgs := buildSearchQuery(
				tt.phrase, append([]string(nil), terms...), tt.queryNum, tt.hasNum, tt.includeClosed, tt.terminalKeys,
			)
			candidateArgs[3] = testWorkspaceID
			candidateArgs[len(candidateArgs)-2] = tt.limit
			candidateArgs[len(candidateArgs)-1] = tt.offset

			legacyRows := runLegacySearchForParity(t, legacyQuery, legacyArgs)
			candidateRows := runCandidateSearchForParity(t, candidateQuery, candidateArgs)
			if !reflect.DeepEqual(candidateRows, legacyRows) {
				t.Fatalf("candidate-first rows differ from legacy semantics\ncandidate: %#v\nlegacy:    %#v", candidateRows, legacyRows)
			}
		})
	}

	// Legacy ordering is undefined when two matching comments have the same
	// created_at. Candidate-first makes that case deterministic by choosing the
	// greater UUID, matching the timeline's established (created_at, id) order.
	tiedSnippetIssueID := dbfx.Issue(t, "tied snippet source", testutil.Cols{
		"updated_at": baseTime.Add(17 * time.Minute),
	})
	firstCommentID, secondCommentID := uuid.NewString(), uuid.NewString()
	lowCommentID, highCommentID := firstCommentID, secondCommentID
	if lowCommentID > highCommentID {
		lowCommentID, highCommentID = highCommentID, lowCommentID
	}
	tiedCommentTime := baseTime.Add(17 * time.Minute)
	dbfx.Comment(t, tiedSnippetIssueID, token+" tied snippet low", testutil.Cols{
		"id":         lowCommentID,
		"created_at": tiedCommentTime,
	})
	highCommentContent := token + " tied snippet high"
	dbfx.Comment(t, tiedSnippetIssueID, highCommentContent, testutil.Cols{
		"id":         highCommentID,
		"created_at": tiedCommentTime,
	})
	tiedRows := runBuiltSearchForParity(t, token+" tied snippet", true, nil, 50, 0)
	if row, ok := findSearchParityRow(tiedRows, tiedSnippetIssueID); !ok {
		t.Fatal("tied-comment match is missing")
	} else if row.matchedCommentContent != highCommentContent {
		t.Fatalf("tied-comment snippet = %q, want greater-ID comment %q", row.matchedCommentContent, highCommentContent)
	}

	splitRows := runBuiltSearchForParity(t, token+" same all", true, nil, 50, 0)
	if row, ok := findSearchParityRow(splitRows, splitCommentID); !ok {
		t.Fatal("split-comment cross-comment match is missing")
	} else if row.matchSource != "comment" || row.matchedCommentContent != "" {
		t.Fatalf("split-comment match = %#v, want comment source with no same-comment snippet", row)
	}
	if row, ok := findSearchParityRow(splitRows, sameCommentID); !ok {
		t.Fatal("same-comment all-terms match is missing")
	} else if row.matchedCommentContent == "" {
		t.Fatalf("same-comment all-terms match has no snippet: %#v", row)
	}

	crossRows := runBuiltSearchForParity(t, token+" cross-part fields-part", true, nil, 50, 0)
	if row, ok := findSearchParityRow(crossRows, crossFieldID); !ok {
		t.Fatal("cross-field all-terms match is missing")
	} else if row.matchSource != "comment" {
		t.Fatalf("cross-field match source = %q, want legacy fallback comment", row.matchSource)
	}

	latestRows := runBuiltSearchForParity(t, token+" snippet", true, nil, 50, 0)
	if row, ok := findSearchParityRow(latestRows, latestCommentID); !ok {
		t.Fatal("latest-comment match is missing")
	} else if row.matchedCommentContent != latestComment {
		t.Fatalf("matched comment = %q, want latest %q", row.matchedCommentContent, latestComment)
	}

	descriptionRows := runBuiltSearchForParity(t, token+" description phrase", true, nil, 50, 0)
	if row, ok := findSearchParityRow(descriptionRows, descriptionID); !ok {
		t.Fatal("description phrase match is missing")
	} else if row.matchSource != "description" {
		t.Fatalf("description match source = %q, want description", row.matchSource)
	}
}

func runBuiltSearchForParity(t *testing.T, phrase string, includeClosed bool, terminalKeys []string, limit, offset int) []searchParityRow {
	t.Helper()
	terms := splitSearchTerms(phrase)
	queryNum, hasNum := parseQueryNumber(phrase)
	query, args := buildSearchQuery(phrase, terms, queryNum, hasNum, includeClosed, terminalKeys)
	args[3] = testWorkspaceID
	args[len(args)-2] = limit
	args[len(args)-1] = offset
	return runCandidateSearchForParity(t, query, args)
}

func findSearchParityRow(rows []searchParityRow, id string) (searchParityRow, bool) {
	for _, row := range rows {
		if row.id == id {
			return row, true
		}
	}
	return searchParityRow{}, false
}

func runCandidateSearchForParity(t *testing.T, query string, args []any) []searchParityRow {
	return runSearchForParity(t, "candidate", query, args)
}

func runLegacySearchForParity(t *testing.T, query string, args []any) []searchParityRow {
	return runSearchForParity(t, "legacy", query, args)
}

func runSearchForParity(t *testing.T, label, query string, args []any) []searchParityRow {
	t.Helper()
	rows, err := testPool.Query(context.Background(), query, args...)
	if err != nil {
		t.Fatalf("run %s search: %v\n%s", label, err, query)
	}
	defer rows.Close()

	var result []searchParityRow
	for rows.Next() {
		var sr searchResult
		if err := rows.Scan(
			&sr.issue.ID,
			&sr.issue.WorkspaceID,
			&sr.issue.Title,
			&sr.issue.Description,
			&sr.issue.Status,
			&sr.issue.Priority,
			&sr.issue.AssigneeType,
			&sr.issue.AssigneeID,
			&sr.issue.CreatorType,
			&sr.issue.CreatorID,
			&sr.issue.ParentIssueID,
			&sr.issue.AcceptanceCriteria,
			&sr.issue.ContextRefs,
			&sr.issue.Position,
			&sr.issue.StartDate,
			&sr.issue.DueDate,
			&sr.issue.CreatedAt,
			&sr.issue.UpdatedAt,
			&sr.issue.LastActivityAt,
			&sr.issue.Number,
			&sr.issue.ProjectID,
			&sr.issue.Revision,
			&sr.matchSource,
			&sr.matchedCommentContent,
		); err != nil {
			t.Fatalf("scan %s row: %v", label, err)
		}
		result = append(result, searchParityRow{
			id:                    uuidToString(sr.issue.ID),
			matchSource:           sr.matchSource,
			matchedCommentContent: sr.matchedCommentContent,
		})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s rows: %v", label, err)
	}
	return result
}

// buildLegacySearchQueryForParity is the parent commit's buildSearchQuery copied
// verbatim except for its name. Keeping the original implementation here avoids
// proving parity against a re-derived oracle that could share the new query's
// assumptions. This remains test-only; production has no legacy fallback path.
func buildLegacySearchQueryForParity(phrase string, terms []string, queryNum int, hasNum bool, includeClosed bool, terminalStatusKeys []string) (string, []any) {
	// Lowercase in Go so SQL only needs LOWER() on the column side.
	phrase = strings.ToLower(phrase)
	for i, t := range terms {
		terms[i] = strings.ToLower(t)
	}

	// Parameter index tracker
	argIdx := 1
	args := []any{}
	nextArg := func(val any) string {
		args = append(args, val)
		s := fmt.Sprintf("$%d", argIdx)
		argIdx++
		return s
	}

	escapedPhrase := escapeLike(phrase)
	// $1: exact phrase (for exact title match)
	phraseParam := nextArg(escapedPhrase)
	// $2: "%phrase%" (contains pattern — pre-built for pg_bigm index usage)
	phraseContainsParam := nextArg("%" + escapedPhrase + "%")
	// $3: "phrase%" (starts-with pattern)
	phraseStartsWithParam := nextArg(escapedPhrase + "%")

	wsParam := nextArg(nil) // $4 — workspace_id, will be filled by caller position

	// Build per-term LIKE conditions only for multi-word search.
	var termContainsParams []string
	if len(terms) > 1 {
		for _, t := range terms {
			et := escapeLike(t)
			termContainsParams = append(termContainsParams, nextArg("%"+et+"%"))
		}
	}

	// --- WHERE clause ---
	var whereParts []string

	// Full phrase match: title, description, or comment.
	//
	// The comment EXISTS subquery is deliberately correlated on BOTH
	// c.issue_id = i.id AND c.workspace_id = wsParam. The workspace_id
	// filter is not strictly necessary for correctness (comment.workspace_id
	// is FK-consistent with its issue's workspace), but it is critical for
	// the planner. Without it, Postgres rewrites the correlated EXISTS
	// into a hashed subplan that materializes every comment in the entire
	// `comment` table matching the LIKE — for common tokens like "search"
	// this can be hundreds of thousands of rows, blowing out work_mem into
	// a lossy bitmap and taking 30+ seconds. With the workspace_id
	// constant duplicated into the subquery, the hashed set collapses to
	// this workspace's comments and the plan uses the supporting
	// idx_comment_workspace (migration 135). See MUL-4059 EXPLAIN reports.
	phraseMatch := fmt.Sprintf(
		"(LOWER(i.title) LIKE %s OR LOWER(COALESCE(i.description, '')) LIKE %s OR EXISTS (SELECT 1 FROM comment c WHERE c.issue_id = i.id AND c.workspace_id = %s AND LOWER(c.content) LIKE %s))",
		phraseContainsParam, phraseContainsParam, wsParam, phraseContainsParam,
	)
	whereParts = append(whereParts, phraseMatch)

	// Multi-word AND match (each term must appear somewhere). Same
	// workspace_id-in-subquery contract as above.
	if len(termContainsParams) > 1 {
		var termConditions []string
		for _, tp := range termContainsParams {
			termConditions = append(termConditions, fmt.Sprintf(
				"(LOWER(i.title) LIKE %s OR LOWER(COALESCE(i.description, '')) LIKE %s OR EXISTS (SELECT 1 FROM comment c WHERE c.issue_id = i.id AND c.workspace_id = %s AND LOWER(c.content) LIKE %s))",
				tp, tp, wsParam, tp,
			))
		}
		whereParts = append(whereParts, "("+strings.Join(termConditions, " AND ")+")")
	}

	// Number match
	numParam := ""
	if hasNum {
		numParam = nextArg(queryNum)
		whereParts = append(whereParts, fmt.Sprintf("i.number = %s", numParam))
	}

	whereClause := "(" + strings.Join(whereParts, " OR ") + ")"

	if !includeClosed {
		// Negate only known terminal keys so an unknown legacy key remains
		// searchable instead of disappearing from the default result set.
		terminalStatusesParam := nextArg(terminalStatusKeys)
		whereClause += fmt.Sprintf(" AND NOT (i.status = ANY(%s::text[]))", terminalStatusesParam)
	}

	// --- ORDER BY clause ---
	// Build ranking CASE with fine-grained tiers.
	var rankCases []string

	// Tier 0: Identifier exact match
	if hasNum {
		rankCases = append(rankCases, fmt.Sprintf("WHEN i.number = %s THEN 0", numParam))
	}

	// Tier 1: Exact title match
	rankCases = append(rankCases, fmt.Sprintf("WHEN LOWER(i.title) = %s THEN 1", phraseParam))

	// Tier 2: Title starts with phrase
	rankCases = append(rankCases, fmt.Sprintf("WHEN LOWER(i.title) LIKE %s THEN 2", phraseStartsWithParam))

	// Tier 3: Title contains phrase
	rankCases = append(rankCases, fmt.Sprintf("WHEN LOWER(i.title) LIKE %s THEN 3", phraseContainsParam))

	// Tier 4: Title matches all words (multi-word only)
	if len(termContainsParams) > 1 {
		var titleTerms []string
		for _, tp := range termContainsParams {
			titleTerms = append(titleTerms, fmt.Sprintf("LOWER(i.title) LIKE %s", tp))
		}
		rankCases = append(rankCases, fmt.Sprintf("WHEN (%s) THEN 4", strings.Join(titleTerms, " AND ")))
	}

	// Tier 5: Description contains phrase
	rankCases = append(rankCases, fmt.Sprintf("WHEN LOWER(COALESCE(i.description, '')) LIKE %s THEN 5", phraseContainsParam))

	// Tier 6: Description matches all words (multi-word only)
	if len(termContainsParams) > 1 {
		var descTerms []string
		for _, tp := range termContainsParams {
			descTerms = append(descTerms, fmt.Sprintf("LOWER(COALESCE(i.description, '')) LIKE %s", tp))
		}
		rankCases = append(rankCases, fmt.Sprintf("WHEN (%s) THEN 6", strings.Join(descTerms, " AND ")))
	}

	// Tier 7: Comment contains phrase. Same workspace_id-in-subquery
	// contract as the WHERE clause; see the phraseMatch comment above.
	rankCases = append(rankCases, fmt.Sprintf("WHEN EXISTS (SELECT 1 FROM comment c WHERE c.issue_id = i.id AND c.workspace_id = %s AND LOWER(c.content) LIKE %s) THEN 7", wsParam, phraseContainsParam))

	// Tier 8: Comment matches all words (multi-word only)
	if len(termContainsParams) > 1 {
		var commentTerms []string
		for _, tp := range termContainsParams {
			commentTerms = append(commentTerms, fmt.Sprintf("LOWER(c.content) LIKE %s", tp))
		}
		rankCases = append(rankCases, fmt.Sprintf("WHEN EXISTS (SELECT 1 FROM comment c WHERE c.issue_id = i.id AND c.workspace_id = %s AND (%s)) THEN 8", wsParam, strings.Join(commentTerms, " AND ")))
	}

	rankExpr := "CASE " + strings.Join(rankCases, " ") + " ELSE 9 END"

	// Status priority: active issues first
	statusRank := `CASE i.status
		WHEN 'in_progress' THEN 0
		WHEN 'in_review' THEN 1
		WHEN 'todo' THEN 2
		WHEN 'blocked' THEN 3
		WHEN 'backlog' THEN 4
		WHEN 'done' THEN 5
		WHEN 'cancelled' THEN 6
		ELSE 7
	END`

	// Cancelled issues are abandoned work. statusRank alone cannot keep them
	// down because it is only a tie-breaker within one relevance tier: a
	// cancelled issue whose title matches the phrase exactly (tier 1) still
	// outranks an in_progress issue that merely contains it (tier 3), and a
	// workspace with many cancelled issues can fill the whole LIMIT window and
	// push live work off the page entirely. So demote cancelled ahead of
	// rankExpr — they sort after every other match and are the first rows the
	// LIMIT drops. Unlike 'done', which is finished work worth referencing,
	// cancelled work was thrown away. The exception is a direct hit: an exact
	// identifier or exact title means the user is targeting that one issue and
	// knows what they asked for.
	//
	// The title half reuses tier 1's predicate verbatim, including its quirk:
	// phraseParam is escapeLike'd, so a title containing _ or % never compares
	// equal and is not treated as a direct hit. Such an issue is still returned
	// by number; keeping the two predicates identical matters more than working
	// around an escaping bug that belongs with tier 1.
	directHitParts := []string{fmt.Sprintf("LOWER(i.title) = %s", phraseParam)}
	if hasNum {
		directHitParts = append(directHitParts, fmt.Sprintf("i.number = %s", numParam))
	}
	cancelledRank := fmt.Sprintf(
		"CASE WHEN i.status = 'cancelled' AND NOT (%s) THEN 1 ELSE 0 END",
		strings.Join(directHitParts, " OR "),
	)

	// --- match_source expression ---
	matchSourceExpr := fmt.Sprintf(`CASE
		WHEN LOWER(i.title) LIKE %s THEN 'title'
		WHEN LOWER(COALESCE(i.description, '')) LIKE %s THEN 'description'
		ELSE 'comment'
	END`, phraseContainsParam, phraseContainsParam)

	// For multi-word: also check if all terms match in title/description
	if len(termContainsParams) > 1 {
		var titleTerms []string
		var descTerms []string
		for _, tp := range termContainsParams {
			titleTerms = append(titleTerms, fmt.Sprintf("LOWER(i.title) LIKE %s", tp))
			descTerms = append(descTerms, fmt.Sprintf("LOWER(COALESCE(i.description, '')) LIKE %s", tp))
		}
		matchSourceExpr = fmt.Sprintf(`CASE
			WHEN LOWER(i.title) LIKE %s THEN 'title'
			WHEN (%s) THEN 'title'
			WHEN LOWER(COALESCE(i.description, '')) LIKE %s THEN 'description'
			WHEN (%s) THEN 'description'
			ELSE 'comment'
		END`,
			phraseContainsParam, strings.Join(titleTerms, " AND "),
			phraseContainsParam, strings.Join(descTerms, " AND "),
		)
	}

	// --- matched_comment_content subquery ---
	// Always return matching comment content regardless of match_source,
	// so frontend can display comment snippet alongside title/description matches.
	// The c.workspace_id filter mirrors the WHERE clause: without it,
	// the planner can pick a global comment scan that ignores workspace
	// scoping.
	commentSubquery := fmt.Sprintf(`COALESCE(
		(SELECT c.content FROM comment c
		 WHERE c.issue_id = i.id AND c.workspace_id = %s AND LOWER(c.content) LIKE %s
		 ORDER BY c.created_at DESC LIMIT 1),
		''
	)`, wsParam, phraseContainsParam)

	if len(termContainsParams) > 1 {
		var commentTerms []string
		for _, tp := range termContainsParams {
			commentTerms = append(commentTerms, fmt.Sprintf("LOWER(c.content) LIKE %s", tp))
		}
		commentSubquery = fmt.Sprintf(`COALESCE(
			(SELECT c.content FROM comment c
			 WHERE c.issue_id = i.id AND c.workspace_id = %s AND (LOWER(c.content) LIKE %s OR (%s))
			 ORDER BY c.created_at DESC LIMIT 1),
			''
		)`, wsParam, phraseContainsParam, strings.Join(commentTerms, " AND "))
	}

	limitParam := nextArg(nil)  // placeholder
	offsetParam := nextArg(nil) // placeholder

	query := fmt.Sprintf(`SELECT i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
		i.assignee_type, i.assignee_id, i.creator_type, i.creator_id,
		i.parent_issue_id, i.acceptance_criteria, i.context_refs, i.position,
		i.start_date, i.due_date, i.created_at, i.updated_at, i.last_activity_at, i.number, i.project_id,
		i.revision,
		%s AS match_source,
		%s AS matched_comment_content
	FROM issue i
	WHERE i.workspace_id = %s AND %s
	ORDER BY %s, %s, %s, i.updated_at DESC
	LIMIT %s OFFSET %s`,
		matchSourceExpr,
		commentSubquery,
		wsParam,
		whereClause,
		cancelledRank,
		rankExpr,
		statusRank,
		limitParam,
		offsetParam,
	)

	return query, args
}
