package handler

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

type searchParityRow struct {
	id                    string
	matchSource           string
	matchedCommentContent string
}

// TestBuildSearchQuery_CandidateFirstParity compares the candidate-first query
// with the legacy correlated-subquery semantics against the same database rows.
// The matrix deliberately covers matches spread across fields and comments,
// because those are the cases most likely to be changed accidentally by a
// candidate aggregation.
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
				tt.phrase, terms, tt.queryNum, tt.hasNum, tt.includeClosed, tt.terminalKeys,
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
	t.Helper()
	rows, err := testPool.Query(context.Background(), query, args...)
	if err != nil {
		t.Fatalf("run candidate search: %v\n%s", err, query)
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
			t.Fatalf("scan candidate row: %v", err)
		}
		result = append(result, searchParityRow{
			id:                    uuidToString(sr.issue.ID),
			matchSource:           sr.matchSource,
			matchedCommentContent: sr.matchedCommentContent,
		})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("candidate rows: %v", err)
	}
	return result
}

func runLegacySearchForParity(t *testing.T, query string, args []any) []searchParityRow {
	t.Helper()
	rows, err := testPool.Query(context.Background(), query, args...)
	if err != nil {
		t.Fatalf("run legacy search: %v\n%s", err, query)
	}
	defer rows.Close()

	var result []searchParityRow
	for rows.Next() {
		var row searchParityRow
		if err := rows.Scan(&row.id, &row.matchSource, &row.matchedCommentContent); err != nil {
			t.Fatalf("scan legacy row: %v", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("legacy rows: %v", err)
	}
	return result
}

// buildLegacySearchQueryForParity is a compact test oracle for the query shape
// that preceded candidate-first search. It intentionally keeps the repeated
// correlated comment EXISTS checks so the production query can be compared
// with the old behavior without retaining a legacy path at runtime.
func buildLegacySearchQueryForParity(phrase string, terms []string, queryNum int, hasNum bool, includeClosed bool, terminalStatusKeys []string) (string, []any) {
	phrase = strings.ToLower(phrase)
	for i := range terms {
		terms[i] = strings.ToLower(terms[i])
	}

	args := []any{}
	nextArg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}

	escapedPhrase := escapeLike(phrase)
	phraseParam := nextArg(escapedPhrase)
	phraseContainsParam := nextArg("%" + escapedPhrase + "%")
	phraseStartsWithParam := nextArg(escapedPhrase + "%")
	workspaceParam := nextArg(nil)

	termPatterns := make([]string, 0, len(terms))
	for _, term := range terms {
		termPatterns = append(termPatterns, "%"+escapeLike(term)+"%")
	}
	termPatternsParam := ""
	if len(termPatterns) > 1 {
		termPatternsParam = nextArg(termPatterns)
	}

	numberParam := ""
	if hasNum {
		numberParam = nextArg(queryNum)
	}

	whereParts := []string{fmt.Sprintf(`
		LOWER(i.title) LIKE %[1]s
		OR LOWER(COALESCE(i.description, '')) LIKE %[1]s
		OR EXISTS (
			SELECT 1 FROM comment c
			WHERE c.issue_id = i.id
			  AND c.workspace_id = %[2]s
			  AND LOWER(c.content) LIKE %[1]s
		)`, phraseContainsParam, workspaceParam)}
	if termPatternsParam != "" {
		whereParts = append(whereParts, fmt.Sprintf(`NOT EXISTS (
			SELECT 1
			FROM unnest(%[1]s::text[]) AS term(pattern)
			WHERE NOT (
				LOWER(i.title) LIKE term.pattern
				OR LOWER(COALESCE(i.description, '')) LIKE term.pattern
				OR EXISTS (
					SELECT 1 FROM comment c
					WHERE c.issue_id = i.id
					  AND c.workspace_id = %[2]s
					  AND LOWER(c.content) LIKE term.pattern
				)
			)
		)`, termPatternsParam, workspaceParam))
	}
	if hasNum {
		whereParts = append(whereParts, "i.number = "+numberParam)
	}
	whereClause := "(" + strings.Join(whereParts, " OR ") + ")"
	if !includeClosed {
		terminalStatusesParam := nextArg(terminalStatusKeys)
		whereClause += fmt.Sprintf(" AND NOT (i.status = ANY(%s::text[]))", terminalStatusesParam)
	}

	rankCases := []string{}
	if hasNum {
		rankCases = append(rankCases, "WHEN i.number = "+numberParam+" THEN 0")
	}
	rankCases = append(rankCases,
		"WHEN LOWER(i.title) = "+phraseParam+" THEN 1",
		"WHEN LOWER(i.title) LIKE "+phraseStartsWithParam+" THEN 2",
		"WHEN LOWER(i.title) LIKE "+phraseContainsParam+" THEN 3",
	)
	if termPatternsParam != "" {
		rankCases = append(rankCases, fmt.Sprintf(`WHEN NOT EXISTS (
			SELECT 1 FROM unnest(%s::text[]) AS term(pattern)
			WHERE LOWER(i.title) NOT LIKE term.pattern
		) THEN 4`, termPatternsParam))
	}
	rankCases = append(rankCases, "WHEN LOWER(COALESCE(i.description, '')) LIKE "+phraseContainsParam+" THEN 5")
	if termPatternsParam != "" {
		rankCases = append(rankCases, fmt.Sprintf(`WHEN NOT EXISTS (
			SELECT 1 FROM unnest(%s::text[]) AS term(pattern)
			WHERE LOWER(COALESCE(i.description, '')) NOT LIKE term.pattern
		) THEN 6`, termPatternsParam))
	}
	rankCases = append(rankCases, fmt.Sprintf(`WHEN EXISTS (
		SELECT 1 FROM comment c
		WHERE c.issue_id = i.id AND c.workspace_id = %s
		  AND LOWER(c.content) LIKE %s
	) THEN 7`, workspaceParam, phraseContainsParam))
	if termPatternsParam != "" {
		rankCases = append(rankCases, fmt.Sprintf(`WHEN EXISTS (
			SELECT 1 FROM comment c
			WHERE c.issue_id = i.id AND c.workspace_id = %[1]s
			  AND NOT EXISTS (
				SELECT 1 FROM unnest(%[2]s::text[]) AS term(pattern)
				WHERE LOWER(c.content) NOT LIKE term.pattern
			  )
		) THEN 8`, workspaceParam, termPatternsParam))
	}
	rankExpr := "CASE " + strings.Join(rankCases, " ") + " ELSE 9 END"

	directHitParts := []string{"LOWER(i.title) = " + phraseParam}
	if hasNum {
		directHitParts = append(directHitParts, "i.number = "+numberParam)
	}
	cancelledRank := fmt.Sprintf(
		"CASE WHEN i.status = 'cancelled' AND NOT (%s) THEN 1 ELSE 0 END",
		strings.Join(directHitParts, " OR "),
	)
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

	matchSource := fmt.Sprintf(`CASE
		WHEN LOWER(i.title) LIKE %[1]s THEN 'title'
		WHEN LOWER(COALESCE(i.description, '')) LIKE %[1]s THEN 'description'
		ELSE 'comment'
	END`, phraseContainsParam)
	if termPatternsParam != "" {
		matchSource = fmt.Sprintf(`CASE
			WHEN LOWER(i.title) LIKE %[1]s THEN 'title'
			WHEN NOT EXISTS (
				SELECT 1 FROM unnest(%[2]s::text[]) AS term(pattern)
				WHERE LOWER(i.title) NOT LIKE term.pattern
			) THEN 'title'
			WHEN LOWER(COALESCE(i.description, '')) LIKE %[1]s THEN 'description'
			WHEN NOT EXISTS (
				SELECT 1 FROM unnest(%[2]s::text[]) AS term(pattern)
				WHERE LOWER(COALESCE(i.description, '')) NOT LIKE term.pattern
			) THEN 'description'
			ELSE 'comment'
		END`, phraseContainsParam, termPatternsParam)
	}

	commentPredicate := "LOWER(c.content) LIKE " + phraseContainsParam
	if termPatternsParam != "" {
		commentPredicate += fmt.Sprintf(` OR NOT EXISTS (
			SELECT 1 FROM unnest(%s::text[]) AS term(pattern)
			WHERE LOWER(c.content) NOT LIKE term.pattern
		)`, termPatternsParam)
	}
	commentContent := fmt.Sprintf(`COALESCE((
		SELECT c.content FROM comment c
		WHERE c.issue_id = i.id AND c.workspace_id = %s
		  AND (%s)
		ORDER BY c.created_at DESC
		LIMIT 1
	), '')`, workspaceParam, commentPredicate)

	limitParam := nextArg(nil)
	offsetParam := nextArg(nil)
	query := fmt.Sprintf(`SELECT i.id::text, %s AS match_source, %s AS matched_comment_content
	FROM issue i
	WHERE i.workspace_id = %s AND %s
	ORDER BY %s, %s, %s, i.updated_at DESC, i.id ASC
	LIMIT %s OFFSET %s`,
		matchSource,
		commentContent,
		workspaceParam,
		whereClause,
		cancelledRank,
		rankExpr,
		statusRank,
		limitParam,
		offsetParam,
	)
	return query, args
}
