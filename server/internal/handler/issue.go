package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	gitlabsync "github.com/multica-ai/multica/server/internal/gitlab"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gitlabapi "github.com/multica-ai/multica/server/pkg/gitlab"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// IssueResponse is the JSON response for an issue.
type IssueResponse struct {
	ID                 string                  `json:"id"`
	WorkspaceID        string                  `json:"workspace_id"`
	Number             int32                   `json:"number"`
	Identifier         string                  `json:"identifier"`
	Title              string                  `json:"title"`
	Description        *string                 `json:"description"`
	Status             string                  `json:"status"`
	Priority           string                  `json:"priority"`
	AssigneeType       *string                 `json:"assignee_type"`
	AssigneeID         *string                 `json:"assignee_id"`
	CreatorType        string                  `json:"creator_type"`
	CreatorID          string                  `json:"creator_id"`
	ParentIssueID      *string                 `json:"parent_issue_id"`
	ProjectID          *string                 `json:"project_id"`
	Position           float64                 `json:"position"`
	DueDate            *string                 `json:"due_date"`
	CreatedAt          string                  `json:"created_at"`
	UpdatedAt          string                  `json:"updated_at"`
	Reactions          []IssueReactionResponse `json:"reactions,omitempty"`
	Attachments        []AttachmentResponse    `json:"attachments,omitempty"`
}

func issueToResponse(i db.Issue, issuePrefix string) IssueResponse {
	identifier := issuePrefix + "-" + strconv.Itoa(int(i.Number))
	return IssueResponse{
		ID:            uuidToString(i.ID),
		WorkspaceID:   uuidToString(i.WorkspaceID),
		Number:        i.Number,
		Identifier:    identifier,
		Title:         i.Title,
		Description:   textToPtr(i.Description),
		Status:        i.Status,
		Priority:      i.Priority,
		AssigneeType:  textToPtr(i.AssigneeType),
		AssigneeID:    uuidToPtr(i.AssigneeID),
		CreatorType:   i.CreatorType.String,
		CreatorID:     uuidToString(i.CreatorID),
		ParentIssueID: uuidToPtr(i.ParentIssueID),
		ProjectID:     uuidToPtr(i.ProjectID),
		Position:      i.Position,
		DueDate:       timestampToPtr(i.DueDate),
		CreatedAt:     timestampToString(i.CreatedAt),
		UpdatedAt:     timestampToString(i.UpdatedAt),
	}
}

// issueListRowToResponse converts a list-query row (no description) to an IssueResponse.
func issueListRowToResponse(i db.ListIssuesRow, issuePrefix string) IssueResponse {
	identifier := issuePrefix + "-" + strconv.Itoa(int(i.Number))
	return IssueResponse{
		ID:            uuidToString(i.ID),
		WorkspaceID:   uuidToString(i.WorkspaceID),
		Number:        i.Number,
		Identifier:    identifier,
		Title:         i.Title,
		Status:        i.Status,
		Priority:      i.Priority,
		AssigneeType:  textToPtr(i.AssigneeType),
		AssigneeID:    uuidToPtr(i.AssigneeID),
		CreatorType:   i.CreatorType.String,
		CreatorID:     uuidToString(i.CreatorID),
		ParentIssueID: uuidToPtr(i.ParentIssueID),
		ProjectID:     uuidToPtr(i.ProjectID),
		Position:      i.Position,
		DueDate:       timestampToPtr(i.DueDate),
		CreatedAt:     timestampToString(i.CreatedAt),
		UpdatedAt:     timestampToString(i.UpdatedAt),
	}
}

func openIssueRowToResponse(i db.ListOpenIssuesRow, issuePrefix string) IssueResponse {
	identifier := issuePrefix + "-" + strconv.Itoa(int(i.Number))
	return IssueResponse{
		ID:            uuidToString(i.ID),
		WorkspaceID:   uuidToString(i.WorkspaceID),
		Number:        i.Number,
		Identifier:    identifier,
		Title:         i.Title,
		Status:        i.Status,
		Priority:      i.Priority,
		AssigneeType:  textToPtr(i.AssigneeType),
		AssigneeID:    uuidToPtr(i.AssigneeID),
		CreatorType:   i.CreatorType.String,
		CreatorID:     uuidToString(i.CreatorID),
		ParentIssueID: uuidToPtr(i.ParentIssueID),
		ProjectID:     uuidToPtr(i.ProjectID),
		Position:      i.Position,
		DueDate:       timestampToPtr(i.DueDate),
		CreatedAt:     timestampToString(i.CreatedAt),
		UpdatedAt:     timestampToString(i.UpdatedAt),
	}
}

// SearchIssueResponse extends IssueResponse with search metadata.
type SearchIssueResponse struct {
	IssueResponse
	MatchSource    string  `json:"match_source"`
	MatchedSnippet *string `json:"matched_snippet,omitempty"`
}

// BatchWriteResult is the continue-on-error shape returned by
// /api/issues/batch-update and /api/issues/batch-delete. HTTP 207
// Multi-Status when both succeeded and failed items are present;
// HTTP 200 when all succeeded or all failed (client inspects lists).
// Individual item failures never abort the batch.
type BatchWriteResult struct {
	Succeeded []BatchSucceeded `json:"succeeded"`
	Failed    []BatchFailed    `json:"failed"`
}

type BatchSucceeded struct {
	ID    string         `json:"id"`
	Issue *IssueResponse `json:"issue"` // nil for batch-delete
}

type BatchFailed struct {
	ID        string `json:"id"`
	ErrorCode string `json:"error_code"` // e.g. "GITLAB_403", "NOT_FOUND", "VALIDATION_FAILED"
	Message   string `json:"message"`
}

// classifyBatchError maps a GitLab-or-handler error to a stable error_code
// string for the BatchFailed response. Stability matters — clients key
// retry logic off these codes. Uses errors.Is / errors.As so the
// classification is invariant under wrapping (e.g. writeThroughError's
// "gitlab update issue failed: %w" still matches the underlying sentinel).
// Previous implementation used strings.Contains on the formatted error
// message, which misclassified e.g. a 500 whose body happened to include
// the substring "404".
func classifyBatchError(err error) (code, msg string) {
	if err == nil {
		return "", ""
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return "NOT_FOUND", "issue not found"
	}
	if errors.Is(err, gitlabapi.ErrForbidden) {
		return "GITLAB_403", err.Error()
	}
	if errors.Is(err, gitlabapi.ErrNotFound) {
		return "GITLAB_404", err.Error()
	}
	if errors.Is(err, gitlabapi.ErrUnauthorized) {
		return "GITLAB_401", err.Error()
	}
	var apiErr *gitlabapi.APIError
	if errors.As(err, &apiErr) {
		return fmt.Sprintf("GITLAB_%d", apiErr.StatusCode), err.Error()
	}
	return "WRITE_FAILED", err.Error()
}

// extractSnippet extracts a snippet of text around the first occurrence of query.
// Returns up to ~120 runes centered on the match. Uses rune-based slicing to
// avoid splitting multi-byte UTF-8 characters (important for CJK content).
func extractSnippet(content, query string) string {
	runes := []rune(content)
	lowerRunes := []rune(strings.ToLower(content))
	queryRunes := []rune(strings.ToLower(query))

	idx := -1
	if len(queryRunes) > 0 && len(lowerRunes) >= len(queryRunes) {
		for i := 0; i <= len(lowerRunes)-len(queryRunes); i++ {
			match := true
			for j := range queryRunes {
				if lowerRunes[i+j] != queryRunes[j] {
					match = false
					break
				}
			}
			if match {
				idx = i
				break
			}
		}
	}

	if idx < 0 {
		if len(runes) > 120 {
			return string(runes[:120]) + "..."
		}
		return content
	}
	start := idx - 40
	if start < 0 {
		start = 0
	}
	end := idx + len(queryRunes) + 80
	if end > len(runes) {
		end = len(runes)
	}
	snippet := string(runes[start:end])
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(runes) {
		snippet = snippet + "..."
	}
	return snippet
}

// escapeLike escapes LIKE special characters (%, _, \) in user input.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// splitSearchTerms splits a query into individual search terms, filtering empty strings.
func splitSearchTerms(q string) []string {
	fields := strings.FieldsFunc(q, func(r rune) bool {
		return unicode.IsSpace(r)
	})
	terms := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			terms = append(terms, f)
		}
	}
	return terms
}

// identifierNumberRe matches patterns like "MUL-123" or "ABC-45".
var identifierNumberRe = regexp.MustCompile(`(?i)^[a-z]+-(\d+)$`)

// parseQueryNumber extracts an issue number from the query if it looks like
// an identifier (e.g. "MUL-123") or a bare number (e.g. "123").
func parseQueryNumber(q string) (int, bool) {
	q = strings.TrimSpace(q)
	// Check for identifier pattern like "MUL-123"
	if m := identifierNumberRe.FindStringSubmatch(q); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			return n, true
		}
	}
	// Check for bare number
	if n, err := strconv.Atoi(q); err == nil && n > 0 {
		return n, true
	}
	return 0, false
}

// searchResult holds a raw row from the dynamic search query.
type searchResult struct {
	issue                 db.Issue
	totalCount            int64
	matchSource           string
	matchedCommentContent string
}

// buildSearchQuery builds a dynamic SQL query for issue search.
// It uses LOWER(column) LIKE for case-insensitive matching compatible with pg_bigm 1.2 GIN indexes.
// Search patterns are lowercased in Go to avoid redundant LOWER() on the pattern side in SQL.
func buildSearchQuery(phrase string, terms []string, queryNum int, hasNum bool, includeClosed bool) (string, []any) {
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
	phraseParam := nextArg(escapedPhrase)               // $1
	phraseContains := "'%' || " + phraseParam + " || '%'"
	phraseStartsWith := phraseParam + " || '%'"

	wsParam := nextArg(nil) // $2 — workspace_id, will be filled by caller position

	// Build per-term LIKE conditions only for multi-word search.
	// For single-word queries, the phrase parameter already covers the term.
	var termParams []string
	if len(terms) > 1 {
		for _, t := range terms {
			et := escapeLike(t)
			termParams = append(termParams, nextArg(et))
		}
	}

	// --- WHERE clause ---
	var whereParts []string

	// Full phrase match: title, description, or comment
	phraseMatch := fmt.Sprintf(
		"(LOWER(i.title) LIKE %s OR LOWER(COALESCE(i.description, '')) LIKE %s OR EXISTS (SELECT 1 FROM comment c WHERE c.issue_id = i.id AND LOWER(c.content) LIKE %s))",
		phraseContains, phraseContains, phraseContains,
	)
	whereParts = append(whereParts, phraseMatch)

	// Multi-word AND match (each term must appear somewhere)
	if len(termParams) > 1 {
		var termConditions []string
		for _, tp := range termParams {
			tc := "'%' || " + tp + " || '%'"
			termConditions = append(termConditions, fmt.Sprintf(
				"(LOWER(i.title) LIKE %s OR LOWER(COALESCE(i.description, '')) LIKE %s OR EXISTS (SELECT 1 FROM comment c WHERE c.issue_id = i.id AND LOWER(c.content) LIKE %s))",
				tc, tc, tc,
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
		whereClause += " AND i.status NOT IN ('done', 'cancelled')"
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
	rankCases = append(rankCases, fmt.Sprintf("WHEN LOWER(i.title) LIKE %s THEN 2", phraseStartsWith))

	// Tier 3: Title contains phrase
	rankCases = append(rankCases, fmt.Sprintf("WHEN LOWER(i.title) LIKE %s THEN 3", phraseContains))

	// Tier 4: Title matches all words (multi-word only)
	if len(termParams) > 1 {
		var titleTerms []string
		for _, tp := range termParams {
			titleTerms = append(titleTerms, fmt.Sprintf("LOWER(i.title) LIKE '%s' || %s || '%s'", "%", tp, "%"))
		}
		rankCases = append(rankCases, fmt.Sprintf("WHEN (%s) THEN 4", strings.Join(titleTerms, " AND ")))
	}

	// Tier 5: Description contains phrase
	rankCases = append(rankCases, fmt.Sprintf("WHEN LOWER(COALESCE(i.description, '')) LIKE %s THEN 5", phraseContains))

	// Tier 6: Description matches all words (multi-word only)
	if len(termParams) > 1 {
		var descTerms []string
		for _, tp := range termParams {
			descTerms = append(descTerms, fmt.Sprintf("LOWER(COALESCE(i.description, '')) LIKE '%s' || %s || '%s'", "%", tp, "%"))
		}
		rankCases = append(rankCases, fmt.Sprintf("WHEN (%s) THEN 6", strings.Join(descTerms, " AND ")))
	}

	rankExpr := "CASE " + strings.Join(rankCases, " ") + " ELSE 7 END"

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

	// --- match_source expression ---
	matchSourceExpr := fmt.Sprintf(`CASE
		WHEN LOWER(i.title) LIKE %s THEN 'title'
		WHEN LOWER(COALESCE(i.description, '')) LIKE %s THEN 'description'
		ELSE 'comment'
	END`, phraseContains, phraseContains)

	// For multi-word: also check if all terms match in title/description
	if len(termParams) > 1 {
		var titleTerms []string
		var descTerms []string
		for _, tp := range termParams {
			titleTerms = append(titleTerms, fmt.Sprintf("LOWER(i.title) LIKE '%s' || %s || '%s'", "%", tp, "%"))
			descTerms = append(descTerms, fmt.Sprintf("LOWER(COALESCE(i.description, '')) LIKE '%s' || %s || '%s'", "%", tp, "%"))
		}
		matchSourceExpr = fmt.Sprintf(`CASE
			WHEN LOWER(i.title) LIKE %s THEN 'title'
			WHEN (%s) THEN 'title'
			WHEN LOWER(COALESCE(i.description, '')) LIKE %s THEN 'description'
			WHEN (%s) THEN 'description'
			ELSE 'comment'
		END`,
			phraseContains, strings.Join(titleTerms, " AND "),
			phraseContains, strings.Join(descTerms, " AND "),
		)
	}

	// --- matched_comment_content subquery ---
	// Find the most recent matching comment for comment-source matches.
	commentSubquery := fmt.Sprintf(`CASE
		WHEN LOWER(i.title) LIKE %s THEN ''
		WHEN LOWER(COALESCE(i.description, '')) LIKE %s THEN ''
		ELSE COALESCE(
			(SELECT c.content FROM comment c
			 WHERE c.issue_id = i.id AND LOWER(c.content) LIKE %s
			 ORDER BY c.created_at DESC LIMIT 1),
			''
		)
	END`, phraseContains, phraseContains, phraseContains)

	// For multi-word, also find comment matching individual terms
	if len(termParams) > 1 {
		var titleTerms []string
		var descTerms []string
		var commentTerms []string
		for _, tp := range termParams {
			titleTerms = append(titleTerms, fmt.Sprintf("LOWER(i.title) LIKE '%s' || %s || '%s'", "%", tp, "%"))
			descTerms = append(descTerms, fmt.Sprintf("LOWER(COALESCE(i.description, '')) LIKE '%s' || %s || '%s'", "%", tp, "%"))
			commentTerms = append(commentTerms, fmt.Sprintf("LOWER(c.content) LIKE '%s' || %s || '%s'", "%", tp, "%"))
		}
		commentSubquery = fmt.Sprintf(`CASE
			WHEN LOWER(i.title) LIKE %s THEN ''
			WHEN (%s) THEN ''
			WHEN LOWER(COALESCE(i.description, '')) LIKE %s THEN ''
			WHEN (%s) THEN ''
			ELSE COALESCE(
				(SELECT c.content FROM comment c
				 WHERE c.issue_id = i.id AND (LOWER(c.content) LIKE %s OR (%s))
				 ORDER BY c.created_at DESC LIMIT 1),
				''
			)
		END`,
			phraseContains, strings.Join(titleTerms, " AND "),
			phraseContains, strings.Join(descTerms, " AND "),
			phraseContains, strings.Join(commentTerms, " AND "),
		)
	}

	limitParam := nextArg(nil)  // placeholder
	offsetParam := nextArg(nil) // placeholder

	query := fmt.Sprintf(`SELECT i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
		i.assignee_type, i.assignee_id, i.creator_type, i.creator_id,
		i.parent_issue_id, i.acceptance_criteria, i.context_refs, i.position,
		i.due_date, i.created_at, i.updated_at, i.number, i.project_id,
		COUNT(*) OVER() AS total_count,
		%s AS match_source,
		%s AS matched_comment_content
	FROM issue i
	WHERE i.workspace_id = %s AND %s
	ORDER BY %s, %s, i.updated_at DESC
	LIMIT %s OFFSET %s`,
		matchSourceExpr,
		commentSubquery,
		wsParam,
		whereClause,
		rankExpr,
		statusRank,
		limitParam,
		offsetParam,
	)

	return query, args
}

func (h *Handler) SearchIssues(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceID := h.resolveWorkspaceID(r)

	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q parameter is required")
		return
	}

	limit := 20
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > 50 {
		limit = 50
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}

	includeClosed := r.URL.Query().Get("include_closed") == "true"

	wsUUID := parseUUID(workspaceID)
	terms := splitSearchTerms(q)
	queryNum, hasNum := parseQueryNumber(q)

	sqlQuery, args := buildSearchQuery(q, terms, queryNum, hasNum, includeClosed)
	// Fill placeholder args: $2 = workspace_id, last two = limit, offset
	args[1] = wsUUID
	args[len(args)-2] = limit
	args[len(args)-1] = offset

	rows, err := h.DB.Query(ctx, sqlQuery, args...)
	if err != nil {
		slog.Warn("search issues failed", "error", err, "workspace_id", workspaceID, "query", q)
		writeError(w, http.StatusInternalServerError, "failed to search issues")
		return
	}
	defer rows.Close()

	var results []searchResult
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
			&sr.issue.DueDate,
			&sr.issue.CreatedAt,
			&sr.issue.UpdatedAt,
			&sr.issue.Number,
			&sr.issue.ProjectID,
			&sr.totalCount,
			&sr.matchSource,
			&sr.matchedCommentContent,
		); err != nil {
			slog.Warn("search issues scan failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to search issues")
			return
		}
		results = append(results, sr)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("search issues rows error", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to search issues")
		return
	}

	var total int64
	if len(results) > 0 {
		total = results[0].totalCount
	}

	prefix := h.getIssuePrefix(ctx, wsUUID)
	resp := make([]SearchIssueResponse, len(results))
	for i, sr := range results {
		sir := SearchIssueResponse{
			IssueResponse: issueToResponse(sr.issue, prefix),
			MatchSource:   sr.matchSource,
		}
		if sr.matchSource == "comment" && sr.matchedCommentContent != "" {
			snippet := extractSnippet(sr.matchedCommentContent, q)
			sir.MatchedSnippet = &snippet
		}
		resp[i] = sir
	}

	w.Header().Set("X-Total-Count", strconv.FormatInt(total, 10))
	writeJSON(w, http.StatusOK, map[string]any{
		"issues": resp,
		"total":  total,
	})
}

func (h *Handler) ListIssues(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	workspaceID := h.resolveWorkspaceID(r)
	wsUUID := parseUUID(workspaceID)

	// Parse optional filter params
	var priorityFilter pgtype.Text
	if p := r.URL.Query().Get("priority"); p != "" {
		priorityFilter = pgtype.Text{String: p, Valid: true}
	}
	var assigneeFilter pgtype.UUID
	if a := r.URL.Query().Get("assignee_id"); a != "" {
		assigneeFilter = parseUUID(a)
	}
	var assigneeIdsFilter []pgtype.UUID
	if ids := r.URL.Query().Get("assignee_ids"); ids != "" {
		for _, raw := range strings.Split(ids, ",") {
			if s := strings.TrimSpace(raw); s != "" {
				assigneeIdsFilter = append(assigneeIdsFilter, parseUUID(s))
			}
		}
	}
	var creatorFilter pgtype.UUID
	if c := r.URL.Query().Get("creator_id"); c != "" {
		creatorFilter = parseUUID(c)
	}
	var projectFilter pgtype.UUID
	if p := r.URL.Query().Get("project_id"); p != "" {
		projectFilter = parseUUID(p)
	}

	// open_only=true returns all non-done/cancelled issues (no limit).
	if r.URL.Query().Get("open_only") == "true" {
		issues, err := h.Queries.ListOpenIssues(ctx, db.ListOpenIssuesParams{
			WorkspaceID: wsUUID,
			Priority:    priorityFilter,
			AssigneeID:  assigneeFilter,
			AssigneeIds: assigneeIdsFilter,
			CreatorID:   creatorFilter,
			ProjectID:   projectFilter,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list issues")
			return
		}

		prefix := h.getIssuePrefix(ctx, wsUUID)
		resp := make([]IssueResponse, len(issues))
		for i, issue := range issues {
			resp[i] = openIssueRowToResponse(issue, prefix)
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"issues": resp,
			"total":  len(resp),
		})
		return
	}

	limit := 100
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			limit = v
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil {
			offset = v
		}
	}

	var statusFilter pgtype.Text
	if s := r.URL.Query().Get("status"); s != "" {
		statusFilter = pgtype.Text{String: s, Valid: true}
	}

	issues, err := h.Queries.ListIssues(ctx, db.ListIssuesParams{
		WorkspaceID: wsUUID,
		Limit:       int32(limit),
		Offset:      int32(offset),
		Status:      statusFilter,
		Priority:    priorityFilter,
		AssigneeID:  assigneeFilter,
		AssigneeIds: assigneeIdsFilter,
		CreatorID:   creatorFilter,
		ProjectID:   projectFilter,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list issues")
		return
	}

	// Get the true total count for pagination awareness.
	total, err := h.Queries.CountIssues(ctx, db.CountIssuesParams{
		WorkspaceID: wsUUID,
		Status:      statusFilter,
		Priority:    priorityFilter,
		AssigneeID:  assigneeFilter,
		AssigneeIds: assigneeIdsFilter,
		CreatorID:   creatorFilter,
		ProjectID:   projectFilter,
	})
	if err != nil {
		total = int64(len(issues))
	}

	prefix := h.getIssuePrefix(ctx, wsUUID)
	resp := make([]IssueResponse, len(issues))
	for i, issue := range issues {
		resp[i] = issueListRowToResponse(issue, prefix)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"issues": resp,
		"total":  total,
	})
}

func (h *Handler) GetIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}
	prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
	resp := issueToResponse(issue, prefix)

	// Fetch issue reactions.
	reactions, err := h.Queries.ListIssueReactions(r.Context(), issue.ID)
	if err == nil && len(reactions) > 0 {
		resp.Reactions = make([]IssueReactionResponse, len(reactions))
		for i, rx := range reactions {
			resp.Reactions[i] = issueReactionToResponse(rx)
		}
	}

	// Fetch issue-level attachments.
	attachments, err := h.Queries.ListAttachmentsByIssue(r.Context(), db.ListAttachmentsByIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err == nil && len(attachments) > 0 {
		resp.Attachments = make([]AttachmentResponse, len(attachments))
		for i, a := range attachments {
			resp.Attachments[i] = h.attachmentToResponse(a)
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListChildIssues(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}
	children, err := h.Queries.ListChildIssues(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list child issues")
		return
	}
	prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
	resp := make([]IssueResponse, len(children))
	for i, child := range children {
		resp[i] = issueToResponse(child, prefix)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"issues": resp,
	})
}

func (h *Handler) ChildIssueProgress(w http.ResponseWriter, r *http.Request) {
	wsID := h.resolveWorkspaceID(r)
	wsUUID := parseUUID(wsID)

	rows, err := h.Queries.ChildIssueProgress(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get child issue progress")
		return
	}

	type progressEntry struct {
		ParentIssueID string `json:"parent_issue_id"`
		Total         int64  `json:"total"`
		Done          int64  `json:"done"`
	}
	resp := make([]progressEntry, len(rows))
	for i, row := range rows {
		resp[i] = progressEntry{
			ParentIssueID: uuidToString(row.ParentIssueID),
			Total:         row.Total,
			Done:          row.Done,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"progress": resp,
	})
}

type CreateIssueRequest struct {
	Title              string   `json:"title"`
	Description        *string  `json:"description"`
	Status             string   `json:"status"`
	Priority           string   `json:"priority"`
	AssigneeType       *string  `json:"assignee_type"`
	AssigneeID         *string  `json:"assignee_id"`
	ParentIssueID      *string  `json:"parent_issue_id"`
	ProjectID          *string  `json:"project_id"`
	DueDate            *string  `json:"due_date"`
	AttachmentIDs      []string `json:"attachment_ids,omitempty"`
}

func (h *Handler) CreateIssue(w http.ResponseWriter, r *http.Request) {
	var req CreateIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	workspaceID := h.resolveWorkspaceID(r)

	// Get creator from context (set by auth middleware)
	creatorID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Enforce agent visibility for caller-initiated creates: private agents can
	// only be assigned by owner/admin. Uses the HTTP user-id header to judge the
	// caller; CreateIssueInternal (autopilot, service-side callers) skips this
	// check — internal callers are already authorized.
	if req.AssigneeType != nil && *req.AssigneeType == "agent" && req.AssigneeID != nil {
		if ok, msg := h.canAssignAgent(r.Context(), r, *req.AssigneeID, workspaceID); !ok {
			writeError(w, http.StatusForbidden, msg)
			return
		}
	}

	actorType, actorID := h.resolveActor(r, creatorID, workspaceID)

	resp, _, err := h.CreateIssueInternal(r.Context(), workspaceID, actorType, actorID, req)
	if err != nil {
		var wt *writeThroughError
		if errors.As(err, &wt) {
			writeError(w, writeThroughStatus(wt), wt.Error())
			return
		}
		if errors.Is(err, errCreateIssueInvalidParent) {
			writeError(w, http.StatusBadRequest, "parent issue not found in this workspace")
			return
		}
		if errors.Is(err, errCreateIssueInvalidDueDate) {
			writeError(w, http.StatusBadRequest, "invalid due_date format, expected RFC3339")
			return
		}
		slog.Warn("create issue failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to create issue: "+err.Error())
		return
	}

	slog.Info("issue created", append(logger.RequestAttrs(r), "issue_id", resp.ID, "title", resp.Title, "status", resp.Status, "workspace_id", workspaceID)...)
	writeJSON(w, http.StatusCreated, resp)
}

// Sentinel errors for CreateIssueInternal request-validation failures. HTTP
// callers map these to 400 responses; non-HTTP callers (autopilot) propagate
// them as-is.
var (
	errCreateIssueInvalidParent  = errors.New("parent issue not found in this workspace")
	errCreateIssueInvalidDueDate = errors.New("invalid due_date format, expected RFC3339")
)

// CreateIssueInternal is the HTTP-agnostic core of issue creation. It runs the
// same write-through path as the HTTP handler: when the workspace has a GitLab
// connection, create the issue in GitLab first and reconcile the cache from
// the returned representation; otherwise fall back to the direct-DB path.
//
// Callers (the HTTP handler and service-side callers such as the autopilot
// service) own:
//   - authZ / visibility checks BEFORE calling this method
//   - error classification for their transport (HTTP status, log shape)
//
// Returns (response, cache row, error). The cache row lets non-HTTP callers
// (e.g. autopilot) record follow-up mappings (autopilot_issue) without a
// second DB round-trip.
func (h *Handler) CreateIssueInternal(
	ctx context.Context,
	workspaceID, actorType, actorID string,
	req CreateIssueRequest,
) (*IssueResponse, db.Issue, error) {
	status := req.Status
	if status == "" {
		status = "todo"
	}
	priority := req.Priority
	if priority == "" {
		priority = "none"
	}

	var assigneeType pgtype.Text
	var assigneeID pgtype.UUID
	if req.AssigneeType != nil {
		assigneeType = pgtype.Text{String: *req.AssigneeType, Valid: true}
	}
	if req.AssigneeID != nil {
		assigneeID = parseUUID(*req.AssigneeID)
	}

	var parentIssueID pgtype.UUID
	var projectID pgtype.UUID
	if req.ProjectID != nil {
		projectID = parseUUID(*req.ProjectID)
	}
	if req.ParentIssueID != nil {
		parentIssueID = parseUUID(*req.ParentIssueID)
		// Validate parent exists in the same workspace.
		parent, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
			ID:          parentIssueID,
			WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil || !parent.ID.Valid {
			return nil, db.Issue{}, errCreateIssueInvalidParent
		}
		if req.ProjectID == nil {
			projectID = parent.ProjectID
		}
	}

	var dueDate pgtype.Timestamptz
	if req.DueDate != nil && *req.DueDate != "" {
		t, err := time.Parse(time.RFC3339, *req.DueDate)
		if err != nil {
			return nil, db.Issue{}, errCreateIssueInvalidDueDate
		}
		dueDate = pgtype.Timestamptz{Time: t, Valid: true}
	}

	// Phase 3a write-through: when the workspace has a GitLab connection,
	// create the issue in GitLab first, then upsert the cache row from the
	// returned representation. Falls through to legacy direct-DB path when
	// no connection exists.
	if h.GitlabEnabled && h.GitlabResolver != nil {
		wsConn, err := h.Queries.GetWorkspaceGitlabConnection(ctx, parseUUID(workspaceID))
		if err == nil {
			token, _, err := h.GitlabResolver.ResolveTokenForWrite(ctx, workspaceID, actorType, actorID)
			if err != nil {
				slog.Error("resolve gitlab token", "error", err)
				return nil, db.Issue{}, &writeThroughError{
					status: http.StatusBadGateway,
					msg:    "could not resolve gitlab token",
					err:    err,
				}
			}

			agentSlugMap, err := h.buildAgentUUIDSlugMap(ctx, parseUUID(workspaceID))
			if err != nil {
				slog.Error("agent slug map", "error", err)
				return nil, db.Issue{}, &writeThroughError{
					status: http.StatusInternalServerError,
					msg:    "build agent map failed",
					err:    err,
				}
			}

			var assigneeTypeStr, assigneeIDStr string
			if req.AssigneeType != nil {
				assigneeTypeStr = *req.AssigneeType
			}
			if req.AssigneeID != nil {
				assigneeIDStr = *req.AssigneeID
			}
			var descStr string
			if req.Description != nil {
				descStr = *req.Description
			}

			// Resolve the requested member assignee (if any) to a GitLab user ID
			// so the translator can populate GitLab's native assignee_ids. When
			// the member has no user_gitlab_connection, they're absent from the
			// map and the translator falls back to cache-only semantics.
			memberUUIDs := []string{}
			if assigneeTypeStr == "member" && assigneeIDStr != "" {
				memberUUIDs = append(memberUUIDs, assigneeIDStr)
			}
			memberGitlabMap, err := h.buildMemberGitlabUserMap(ctx, parseUUID(workspaceID), memberUUIDs)
			if err != nil {
				slog.Error("member gitlab user map", "error", err)
				return nil, db.Issue{}, &writeThroughError{
					status: http.StatusInternalServerError,
					msg:    "build member map failed",
					err:    err,
				}
			}

			glInput := gitlabsync.BuildCreateIssueInput(gitlabsync.CreateIssueRequest{
				Title:        req.Title,
				Description:  descStr,
				Status:       status,
				Priority:     priority,
				AssigneeType: assigneeTypeStr,
				AssigneeID:   assigneeIDStr,
			}, agentSlugMap, memberGitlabMap)

			glIssue, err := h.Gitlab.CreateIssue(ctx, token, wsConn.GitlabProjectID, glInput)
			if err != nil {
				slog.Error("gitlab create issue", "error", err)
				return nil, db.Issue{}, &writeThroughError{
					status: http.StatusBadGateway,
					msg:    "gitlab create issue failed",
					err:    err,
				}
			}

			// Build the inverse slug→uuid map for the read-side translator.
			agentByLabel := make(map[string]string, len(agentSlugMap))
			for uuid, slug := range agentSlugMap {
				agentByLabel[slug] = uuid
			}
			values := gitlabsync.TranslateIssue(*glIssue, &gitlabsync.TranslateContext{AgentBySlug: agentByLabel})

			// Preserve the requested member assignee in the cache when the
			// translator didn't resolve one from the GitLab response. This
			// handles the unmapped-member case (no user_gitlab_connection):
			// GitLab can't echo an assignee we never sent, but Multica should
			// still record the member assignment locally. The translator's
			// agent resolution takes precedence when a label was present.
			if values.AssigneeType == "" && assigneeTypeStr == "member" && assigneeIDStr != "" {
				values.AssigneeType = "member"
				values.AssigneeID = assigneeIDStr
			}

			// Atomically increment the workspace issue counter and cache the GitLab row.
			glTx, err := h.TxStarter.Begin(ctx)
			if err != nil {
				slog.Error("begin gitlab write-through tx", "error", err)
				return nil, db.Issue{}, &writeThroughError{
					status: http.StatusInternalServerError,
					msg:    "failed to create issue",
					err:    err,
				}
			}
			defer glTx.Rollback(ctx)

			qtxGL := h.Queries.WithTx(glTx)
			issueNumber, err := qtxGL.IncrementIssueCounter(ctx, parseUUID(workspaceID))
			if err != nil {
				slog.Error("increment issue counter (gitlab)", "error", err)
				return nil, db.Issue{}, &writeThroughError{
					status: http.StatusInternalServerError,
					msg:    "failed to create issue",
					err:    err,
				}
			}

			cacheRow, err := qtxGL.UpsertIssueFromGitlab(ctx, buildUpsertParamsFromCreate(parseUUID(workspaceID), wsConn.GitlabProjectID, *glIssue, values))
			if err != nil {
				slog.Error("upsert gitlab cache row", "error", err)
				return nil, db.Issue{}, &writeThroughError{
					status: http.StatusInternalServerError,
					msg:    "cache upsert failed",
					err:    err,
				}
			}

			// Set the workspace-scoped issue number (UpsertIssueFromGitlab always
			// inserts 0 by default; we update it here within the same transaction).
			if _, err := glTx.Exec(ctx,
				`UPDATE issue SET number = $1 WHERE id = $2`,
				issueNumber, cacheRow.ID,
			); err != nil {
				slog.Error("set issue number (gitlab)", "error", err)
				return nil, db.Issue{}, &writeThroughError{
					status: http.StatusInternalServerError,
					msg:    "failed to create issue",
					err:    err,
				}
			}
			cacheRow.Number = issueNumber

			// Patch Multica-native fields that UpsertIssueFromGitlab doesn't
			// carry (parent_issue_id, project_id). The upsert stays "from
			// GitLab"; these fields are Multica-owned and must be threaded in
			// separately so sub-issue + project links work on connected
			// workspaces. Uses the existing UpdateIssue sqlc query, passing
			// the just-upserted row's values through COALESCE/narg slots so
			// only parent/project actually change.
			if parentIssueID.Valid || projectID.Valid {
				patched, err := qtxGL.UpdateIssue(ctx, db.UpdateIssueParams{
					ID:            cacheRow.ID,
					AssigneeType:  cacheRow.AssigneeType,
					AssigneeID:    cacheRow.AssigneeID,
					DueDate:       cacheRow.DueDate,
					ParentIssueID: parentIssueID,
					ProjectID:     projectID,
				})
				if err != nil {
					slog.Error("patch parent/project on gitlab cache row", "error", err)
					return nil, db.Issue{}, &writeThroughError{
						status: http.StatusInternalServerError,
						msg:    "failed to create issue",
						err:    err,
					}
				}
				cacheRow = patched
			}

			// Link any pre-uploaded attachments to this issue inside the same
			// txn so a partial failure rolls the whole thing back.
			if len(req.AttachmentIDs) > 0 {
				attachmentUUIDs := make([]pgtype.UUID, len(req.AttachmentIDs))
				for i, id := range req.AttachmentIDs {
					attachmentUUIDs[i] = parseUUID(id)
				}
				if err := qtxGL.LinkAttachmentsToIssue(ctx, db.LinkAttachmentsToIssueParams{
					IssueID:     cacheRow.ID,
					WorkspaceID: cacheRow.WorkspaceID,
					Column3:     attachmentUUIDs,
				}); err != nil {
					slog.Error("link attachments to gitlab issue", "error", err)
					return nil, db.Issue{}, &writeThroughError{
						status: http.StatusInternalServerError,
						msg:    "failed to create issue",
						err:    err,
					}
				}
			}

			if err := glTx.Commit(ctx); err != nil {
				slog.Error("commit gitlab write-through tx", "error", err)
				return nil, db.Issue{}, &writeThroughError{
					status: http.StatusInternalServerError,
					msg:    "failed to create issue",
					err:    err,
				}
			}

			// Enqueue an agent task when the persisted cache row resolved to an
			// agent assignee. Runs AFTER the txn commits — task-enqueueing is
			// a separate concern from the GitLab/DB write (matches legacy path
			// tail: a failed enqueue must not roll back the created issue).
			if cacheRow.AssigneeType.Valid && cacheRow.AssigneeID.Valid {
				if h.shouldEnqueueAgentTask(ctx, cacheRow) {
					h.TaskService.EnqueueTaskForIssue(ctx, cacheRow)
				}
			}

			prefix := h.getIssuePrefix(ctx, cacheRow.WorkspaceID)
			resp := issueToResponse(cacheRow, prefix)

			// Mirror the legacy path: when attachments were linked, fetch them
			// so the response carries the populated Attachments slice.
			if len(req.AttachmentIDs) > 0 {
				attachments, err := h.Queries.ListAttachmentsByIssue(ctx, db.ListAttachmentsByIssueParams{
					IssueID:     cacheRow.ID,
					WorkspaceID: cacheRow.WorkspaceID,
				})
				if err == nil && len(attachments) > 0 {
					resp.Attachments = make([]AttachmentResponse, len(attachments))
					for i, a := range attachments {
						resp.Attachments[i] = h.attachmentToResponse(a)
					}
				}
			}

			h.publish(protocol.EventIssueCreated, workspaceID, actorType, actorID, map[string]any{"issue": resp})
			return &resp, cacheRow, nil
		}
		// err != nil → fall through to legacy path (most likely pgx.ErrNoRows
		// for non-connected workspaces).
	}

	// Use a transaction to atomically increment the workspace issue counter
	// and create the issue with the assigned number.
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return nil, db.Issue{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := h.Queries.WithTx(tx)
	issueNumber, err := qtx.IncrementIssueCounter(ctx, parseUUID(workspaceID))
	if err != nil {
		return nil, db.Issue{}, fmt.Errorf("increment issue counter: %w", err)
	}

	issue, err := qtx.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID:   parseUUID(workspaceID),
		Title:         req.Title,
		Description:   ptrToText(req.Description),
		Status:        status,
		Priority:      priority,
		AssigneeType:  assigneeType,
		AssigneeID:    assigneeID,
		CreatorType:   pgtype.Text{String: actorType, Valid: actorType != ""},
		CreatorID:     parseUUID(actorID),
		ParentIssueID: parentIssueID,
		Position:      0,
		DueDate:       dueDate,
		Number:        issueNumber,
		ProjectID:     projectID,
	})
	if err != nil {
		return nil, db.Issue{}, fmt.Errorf("create issue: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, db.Issue{}, fmt.Errorf("commit tx: %w", err)
	}

	// Link any pre-uploaded attachments to this issue.
	if len(req.AttachmentIDs) > 0 {
		h.linkAttachmentsByIssueIDs(ctx, issue.ID, issue.WorkspaceID, req.AttachmentIDs)
	}

	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	resp := issueToResponse(issue, prefix)

	// Fetch linked attachments so they appear in the response.
	if len(req.AttachmentIDs) > 0 {
		attachments, err := h.Queries.ListAttachmentsByIssue(ctx, db.ListAttachmentsByIssueParams{
			IssueID:     issue.ID,
			WorkspaceID: issue.WorkspaceID,
		})
		if err == nil && len(attachments) > 0 {
			resp.Attachments = make([]AttachmentResponse, len(attachments))
			for i, a := range attachments {
				resp.Attachments[i] = h.attachmentToResponse(a)
			}
		}
	}

	h.publish(protocol.EventIssueCreated, workspaceID, actorType, actorID, map[string]any{"issue": resp})

	// Enqueue agent task when an agent-assigned issue is created.
	if issue.AssigneeType.Valid && issue.AssigneeID.Valid {
		if h.shouldEnqueueAgentTask(ctx, issue) {
			h.TaskService.EnqueueTaskForIssue(ctx, issue)
		}
	}

	return &resp, issue, nil
}

// CreateIssueForAutopilot adapts CreateIssueInternal to the service-side
// IssueCreator interface (service.IssueCreator). The autopilot service calls
// this instead of talking to CreateIssue / CreateIssueInternal directly,
// which keeps it decoupled from the handler's HTTP request types.
//
// Returns the created cache row so the autopilot service can key its
// autopilot_issue mapping off the row's gitlab_iid (when the workspace is
// connected).
func (h *Handler) CreateIssueForAutopilot(
	ctx context.Context,
	workspaceID, actorType, actorID string,
	req service.AutopilotIssueCreateRequest,
) (db.Issue, error) {
	desc := req.Description
	assigneeType := req.AssigneeType
	assigneeID := req.AssigneeID
	createReq := CreateIssueRequest{
		Title:        req.Title,
		Description:  &desc,
		Status:       req.Status,
		Priority:     req.Priority,
		AssigneeType: &assigneeType,
		AssigneeID:   &assigneeID,
	}
	_, cacheRow, err := h.CreateIssueInternal(ctx, workspaceID, actorType, actorID, createReq)
	return cacheRow, err
}

// buildMemberGitlabUserMap resolves the Multica member UUIDs referenced in a
// request to their GitLab user IDs. Used by write-through handlers that need
// to set GitLab's native assignee_ids. Unmapped members (user without a PAT
// connection) are absent from the returned map — the translator falls back to
// cache-only behavior for them.
func (h *Handler) buildMemberGitlabUserMap(ctx context.Context, workspaceID pgtype.UUID, memberUUIDs []string) (map[string]int64, error) {
	out := make(map[string]int64, len(memberUUIDs))
	for _, memberUUID := range memberUUIDs {
		if memberUUID == "" {
			continue
		}
		conn, err := h.Queries.GetUserGitlabConnection(ctx, db.GetUserGitlabConnectionParams{
			UserID:      parseUUID(memberUUID),
			WorkspaceID: workspaceID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return nil, err
		}
		if conn.GitlabUserID != 0 {
			out[memberUUID] = conn.GitlabUserID
		}
	}
	return out, nil
}

// buildUpsertParamsFromCreate converts a GitLab issue + translated IssueValues
// into the sqlc UpsertIssueFromGitlab params. Used by the Phase 3a write-through path.
func buildUpsertParamsFromCreate(wsUUID pgtype.UUID, projectID int64, issue gitlabapi.Issue, values gitlabsync.IssueValues) db.UpsertIssueFromGitlabParams {
	var assigneeType pgtype.Text
	var assigneeID pgtype.UUID
	if values.AssigneeType != "" {
		assigneeType = pgtype.Text{String: values.AssigneeType, Valid: true}
		_ = assigneeID.Scan(values.AssigneeID)
	}
	desc := pgtype.Text{}
	if values.Description != "" {
		desc = pgtype.Text{String: values.Description, Valid: true}
	}
	extUpdated := pgtype.Timestamptz{}
	if values.UpdatedAt != "" {
		if t, err := time.Parse(time.RFC3339, values.UpdatedAt); err == nil {
			extUpdated = pgtype.Timestamptz{Time: t, Valid: true}
		}
	}
	return db.UpsertIssueFromGitlabParams{
		WorkspaceID:       wsUUID,
		GitlabIid:         pgtype.Int4{Int32: int32(issue.IID), Valid: true},
		GitlabProjectID:   pgtype.Int8{Int64: projectID, Valid: true},
		GitlabIssueID:     pgtype.Int8{Int64: issue.ID, Valid: issue.ID != 0},
		Title:             values.Title,
		Description:       desc,
		Status:            values.Status,
		Priority:          values.Priority,
		AssigneeType:      assigneeType,
		AssigneeID:        assigneeID,
		CreatorType:       pgtype.Text{}, // Phase 3b backfill
		CreatorID:         pgtype.UUID{},
		DueDate:           pgtype.Timestamptz{},
		ExternalUpdatedAt: extUpdated,
	}
}

// explicitClearFields captures which fields of a PATCH were present in the
// request body with a `null` value. Derived from rawFields + the typed
// request struct: when the JSON key is present but the parsed pointer is
// nil, the user meant "clear". Used to drive explicit-clear semantics in
// buildUpsertParamsForUpdate (so the cache upsert writes SQL NULL rather
// than preserving prev values).
type explicitClearFields struct {
	Assignee bool // assignee_type OR assignee_id was present-and-null
	DueDate  bool // due_date was present-and-null
}

// buildUpsertParamsForUpdate builds UpsertIssueFromGitlabParams for the PATCH
// write-through path. The key difference from buildUpsertParamsFromCreate is
// that UpsertIssueFromGitlab's DO UPDATE clause blindly overwrites
// assignee_type / assignee_id / due_date with EXCLUDED values (no COALESCE).
// On a pre-existing cache row, create-path params would wipe those fields
// because the create-path leaves them zero — the translator only sets them
// when GitLab returned an agent::<slug> label. For updates we preserve the
// prevIssue values, and only override when:
//   - The translator resolved an agent assignee from an agent::<slug> label
//     (so the GitLab-side change wins).
//   - The request explicitly sets a new due_date (rawFields makes the
//     distinction between "absent" and "null"). A non-null value lands
//     here; an explicit-null (clear.DueDate) zeroes the column via the
//     upsert's bare EXCLUDED assignment.
//   - The request explicitly clears the assignee (clear.Assignee) —
//     we skip the member-preserve branch so the upsert writes NULL.
//
// Title / description / status / priority / gitlab_* / external_updated_at
// come from the GitLab response (via values) as before — the upsert IS how
// those reconcile after GitLab accepted the edit.
func buildUpsertParamsForUpdate(
	prevIssue db.Issue,
	projectID int64,
	issue gitlabapi.Issue,
	values gitlabsync.IssueValues,
	req UpdateIssueRequest,
	clear explicitClearFields,
) db.UpsertIssueFromGitlabParams {
	// Default both to zero. We'll fill them in below per the following rules:
	//   - Agent assignment: GitLab's labels are the source of truth. If the
	//     translator resolved an agent::<slug> label on the response, use it;
	//     otherwise the agent assignment is considered removed (a prior agent
	//     label was dropped, either by this PATCH or by an outside actor).
	//   - Member assignment: cache-only in Phase 3b (translator drops it).
	//     Preserve the prev cache value so a GitLab-originated PATCH that
	//     only touches labels doesn't wipe the member assignee — EXCEPT
	//     when the request explicitly cleared the assignee, in which case
	//     the zero (NULL) is what the user wants.
	var assigneeType pgtype.Text
	var assigneeID pgtype.UUID
	dueDate := prevIssue.DueDate

	switch {
	case values.AssigneeType == "agent":
		// Agent resolved from GitLab label — the GitLab-side state wins.
		assigneeType = pgtype.Text{String: "agent", Valid: true}
		var newID pgtype.UUID
		_ = newID.Scan(values.AssigneeID)
		assigneeID = newID
	case clear.Assignee:
		// Explicit-null clear: leave the zero values so the upsert writes
		// SQL NULL for assignee_type / assignee_id (bare EXCLUDED). This
		// branch must come before the member-preserve case below so the
		// user's "Unassigned" click isn't silently reverted.
	case prevIssue.AssigneeType.String == "member":
		// Member assignee is cache-only (translator drops member assignees).
		// Preserve it so non-assignee PATCHes don't wipe it.
		assigneeType = prevIssue.AssigneeType
		assigneeID = prevIssue.AssigneeID
	default:
		// Prev was unassigned OR prev was an agent that's no longer on the
		// GitLab response (agent::<slug> label removed). In both cases the
		// desired cache state is unassigned — leave the zero values.
	}

	// due_date: explicit-null clear wins over prev preservation; otherwise
	// a non-empty new value replaces prev; otherwise we keep prev.
	switch {
	case clear.DueDate:
		dueDate = pgtype.Timestamptz{Valid: false}
	case req.DueDate != nil && *req.DueDate != "":
		if t, err := time.Parse(time.RFC3339, *req.DueDate); err == nil {
			dueDate = pgtype.Timestamptz{Time: t, Valid: true}
		}
	}

	desc := pgtype.Text{}
	if values.Description != "" {
		desc = pgtype.Text{String: values.Description, Valid: true}
	}
	extUpdated := pgtype.Timestamptz{}
	if values.UpdatedAt != "" {
		if t, err := time.Parse(time.RFC3339, values.UpdatedAt); err == nil {
			extUpdated = pgtype.Timestamptz{Time: t, Valid: true}
		}
	}
	return db.UpsertIssueFromGitlabParams{
		WorkspaceID:       prevIssue.WorkspaceID,
		GitlabIid:         pgtype.Int4{Int32: int32(issue.IID), Valid: true},
		GitlabProjectID:   pgtype.Int8{Int64: projectID, Valid: true},
		GitlabIssueID:     pgtype.Int8{Int64: issue.ID, Valid: issue.ID != 0},
		Title:             values.Title,
		Description:       desc,
		Status:            values.Status,
		Priority:          values.Priority,
		AssigneeType:      assigneeType,
		AssigneeID:        assigneeID,
		CreatorType:       prevIssue.CreatorType,
		CreatorID:         prevIssue.CreatorID,
		DueDate:           dueDate,
		ExternalUpdatedAt: extUpdated,
	}
}

type UpdateIssueRequest struct {
	Title              *string  `json:"title"`
	Description        *string  `json:"description"`
	Status             *string  `json:"status"`
	Priority           *string  `json:"priority"`
	AssigneeType       *string  `json:"assignee_type"`
	AssigneeID         *string  `json:"assignee_id"`
	Position           *float64 `json:"position"`
	DueDate            *string  `json:"due_date"`
	ParentIssueID      *string  `json:"parent_issue_id"`
	ProjectID          *string  `json:"project_id"`
}

// writeThroughError is the internal error type returned by
// updateSingleIssueWriteThrough. The `status` field lets the direct
// UpdateIssue handler map the error back to the T4 status-code contract
// (502 for GitLab-side failures, 500 for cache/tx failures). For batch
// callers the status is ignored — BatchUpdateIssues uses
// classifyBatchError on the underlying error message instead.
type writeThroughError struct {
	status int
	msg    string
	err    error
}

func (e *writeThroughError) Error() string {
	if e.err != nil {
		// Plain string concatenation — %w in fmt.Errorf is only useful
		// when the result is returned as an error (so errors.Is/As can
		// traverse), but here we immediately collapse to a string. The
		// wrapped-chain traversal is provided by Unwrap() below, which
		// lets classifyBatchError match sentinels through this wrapper.
		return e.msg + ": " + e.err.Error()
	}
	return e.msg
}

func (e *writeThroughError) Unwrap() error { return e.err }

// writeThroughStatus returns the HTTP status code for an error returned
// by updateSingleIssueWriteThrough. Falls back to 500 for non-writeThrough
// errors.
func writeThroughStatus(err error) int {
	var wt *writeThroughError
	if errors.As(err, &wt) {
		return wt.status
	}
	return http.StatusInternalServerError
}

// updateSingleIssueWriteThrough executes the Phase 3b PATCH write-through
// for a single issue: PUT /projects/:id/issues/:iid on GitLab, upsert the
// cache row from the GitLab response inside a transaction, apply
// Multica-native fields (parent_issue_id, project_id, member assignee)
// that GitLab doesn't track, and commit. Used by both UpdateIssue
// (direct handler) and BatchUpdateIssues (loops over this). Callers
// pre-resolve token/agentSlugByUUID once per request to avoid redundant
// DB + GitLab work across a batch; the wsConn row they fetched is not
// needed inside this helper because the GitLab identifiers already live
// on prevIssue.
//
// workspaceID is threaded through purely for slog logging context (the
// helper emits a warning when a GitLab-connected cache row is missing
// GitLab identifiers — a bug upstream of this handler).
//
// On success returns the built IssueResponse and the post-commit cache
// row. On error returns a *writeThroughError with a status hint that
// the direct handler maps back to an HTTP status. The batch handler
// ignores the status hint and classifies the error via
// classifyBatchError, which uses errors.Is/As against GitLab sentinels
// (ErrForbidden/ErrNotFound/ErrUnauthorized) and APIError.StatusCode —
// classification is invariant under writeThroughError's %w wrapping.
//
// rawFields is optional: when non-nil it enables explicit-null semantics
// for parent_issue_id / project_id / assignee_type / assignee_id (JSON
// null clears the field). When nil — as in the batch path — those
// fields fall back to "value present means set, absent or nil means
// leave alone" (no explicit null).
func (h *Handler) updateSingleIssueWriteThrough(
	ctx context.Context,
	prevIssue db.Issue,
	req UpdateIssueRequest,
	rawFields map[string]json.RawMessage,
	workspaceID string,
	token string,
	agentSlugByUUID map[string]string,
) (*IssueResponse, db.Issue, error) {
	oldSnap := gitlabsync.OldIssueSnapshot{
		Status:       prevIssue.Status,
		Priority:     prevIssue.Priority,
		AssigneeType: prevIssue.AssigneeType.String,
		AssigneeUUID: uuidToString(prevIssue.AssigneeID),
	}
	translatorReq := gitlabsync.UpdateIssueRequest{
		Title:        req.Title,
		Description:  req.Description,
		Status:       req.Status,
		Priority:     req.Priority,
		AssigneeType: req.AssigneeType,
		AssigneeID:   req.AssigneeID,
		DueDate:      req.DueDate,
	}
	// Explicit-null clearing. The req pointer fields arrive as nil *string
	// for both "field absent" and "field: null" — we use rawFields (the
	// raw JSON keys present in the body) to tell them apart.
	//
	//   - assignee_type / assignee_id: a PATCH of
	//     {"assignee_type": null, "assignee_id": null} must remove the
	//     agent::<slug> label on GitLab. Pass &"" so BuildUpdateIssueInput
	//     diffs to "clear".
	//   - due_date: a PATCH of {"due_date": null} must send due_date: ""
	//     to GitLab (GitLab's clear-date signal).
	//
	// We also derive explicitClearFields here so buildUpsertParamsForUpdate
	// knows to write SQL NULL via the upsert path rather than preserving
	// prevIssue values.
	var clear explicitClearFields
	if rawFields != nil {
		empty := ""
		if _, present := rawFields["assignee_type"]; present && req.AssigneeType == nil {
			translatorReq.AssigneeType = &empty
			clear.Assignee = true
		}
		if _, present := rawFields["assignee_id"]; present && req.AssigneeID == nil {
			translatorReq.AssigneeID = &empty
			clear.Assignee = true
		}
		if _, present := rawFields["due_date"]; present && req.DueDate == nil {
			translatorReq.DueDate = &empty
			clear.DueDate = true
		}
	}

	// Resolve member assignees (old + new) to GitLab user IDs so the
	// translator can populate GitLab's native assignee_ids. We include the
	// PREV member UUID so transitions off a member can participate in the
	// map (the translator doesn't use it today, but keeping both sides is
	// symmetric with Create and cheap). Unmapped members are absent from
	// the map — translator leaves AssigneeIDs nil and the handler falls
	// back to a cache-only write below.
	memberUUIDs := []string{}
	if prevIssue.AssigneeType.Valid && prevIssue.AssigneeType.String == "member" && prevIssue.AssigneeID.Valid {
		memberUUIDs = append(memberUUIDs, uuidToString(prevIssue.AssigneeID))
	}
	if req.AssigneeType != nil && *req.AssigneeType == "member" && req.AssigneeID != nil && *req.AssigneeID != "" {
		memberUUIDs = append(memberUUIDs, *req.AssigneeID)
	}
	memberGitlabMap, mapErr := h.buildMemberGitlabUserMap(ctx, prevIssue.WorkspaceID, memberUUIDs)
	if mapErr != nil {
		slog.Error("member gitlab user map", "error", mapErr)
		return nil, db.Issue{}, &writeThroughError{
			status: http.StatusInternalServerError,
			msg:    "build member map failed",
			err:    mapErr,
		}
	}
	glInput := gitlabsync.BuildUpdateIssueInput(oldSnap, translatorReq, agentSlugByUUID, memberGitlabMap)

	if !prevIssue.GitlabIid.Valid || !prevIssue.GitlabProjectID.Valid {
		// Defensive: a cache row on a GitLab-connected workspace that
		// lacks GitLab identifiers is a bug upstream of this handler.
		// Refuse the write-through rather than silently fall through.
		slog.Error("gitlab-connected workspace issue missing gitlab identifiers",
			"issue_id", uuidToString(prevIssue.ID), "workspace_id", workspaceID)
		return nil, db.Issue{}, &writeThroughError{
			status: http.StatusBadGateway,
			msg:    "issue is missing gitlab identifiers",
		}
	}

	glIssue, glErr := h.Gitlab.UpdateIssue(ctx, token, prevIssue.GitlabProjectID.Int64, int(prevIssue.GitlabIid.Int32), glInput)
	if glErr != nil {
		slog.Error("gitlab update issue", "error", glErr, "issue_id", uuidToString(prevIssue.ID))
		return nil, db.Issue{}, &writeThroughError{
			status: http.StatusBadGateway,
			msg:    "gitlab update issue failed",
			err:    glErr,
		}
	}

	// Translate the GitLab response → cache values.
	agentByLabel := make(map[string]string, len(agentSlugByUUID))
	for uuidStr, slug := range agentSlugByUUID {
		agentByLabel[slug] = uuidStr
	}
	values := gitlabsync.TranslateIssue(*glIssue, &gitlabsync.TranslateContext{AgentBySlug: agentByLabel})

	glTx, txErr := h.TxStarter.Begin(ctx)
	if txErr != nil {
		slog.Error("begin gitlab update-through tx", "error", txErr)
		return nil, db.Issue{}, &writeThroughError{
			status: http.StatusInternalServerError,
			msg:    "failed to update issue",
			err:    txErr,
		}
	}
	defer glTx.Rollback(ctx)
	qtxGL := h.Queries.WithTx(glTx)

	cacheRow, upErr := qtxGL.UpsertIssueFromGitlab(ctx,
		buildUpsertParamsForUpdate(prevIssue, prevIssue.GitlabProjectID.Int64, *glIssue, values, req, clear))
	if upErr != nil && !errors.Is(upErr, pgx.ErrNoRows) {
		slog.Error("upsert gitlab cache row on update", "error", upErr)
		return nil, db.Issue{}, &writeThroughError{
			status: http.StatusInternalServerError,
			msg:    "cache upsert failed",
			err:    upErr,
		}
	}
	if errors.Is(upErr, pgx.ErrNoRows) {
		// Clobber guard rejected the upsert (cache row's
		// external_updated_at is already >= GitLab's updated_at). A
		// concurrent webhook superseded us — keep the current cache
		// row as-is and just apply any Multica-native patches below.
		cacheRow = prevIssue
	}

	// Apply Multica-native fields that GitLab doesn't track. Pre-fill
	// the bare-narg slots (assignee_*, due_date, parent_issue_id,
	// project_id) from cacheRow so we don't overwrite anything we
	// aren't explicitly changing.
	updParams := db.UpdateIssueParams{
		ID:            cacheRow.ID,
		AssigneeType:  cacheRow.AssigneeType,
		AssigneeID:    cacheRow.AssigneeID,
		DueDate:       cacheRow.DueDate,
		ParentIssueID: cacheRow.ParentIssueID,
		ProjectID:     cacheRow.ProjectID,
	}
	touched := false
	// parent_issue_id / project_id support explicit-null only when rawFields
	// is provided (direct PATCH). Batch callers pass rawFields=nil and fall
	// back to "set when non-nil, otherwise leave alone" — explicit-null
	// clearing is a single-issue feature today.
	if rawFields != nil {
		if _, ok := rawFields["parent_issue_id"]; ok {
			if req.ParentIssueID != nil {
				updParams.ParentIssueID = parseUUID(*req.ParentIssueID)
			} else {
				updParams.ParentIssueID = pgtype.UUID{Valid: false}
			}
			touched = true
		}
		if _, ok := rawFields["project_id"]; ok {
			if req.ProjectID != nil {
				updParams.ProjectID = parseUUID(*req.ProjectID)
			} else {
				updParams.ProjectID = pgtype.UUID{Valid: false}
			}
			touched = true
		}
	} else {
		if req.ParentIssueID != nil {
			updParams.ParentIssueID = parseUUID(*req.ParentIssueID)
			touched = true
		}
		if req.ProjectID != nil {
			updParams.ProjectID = parseUUID(*req.ProjectID)
			touched = true
		}
	}
	// Member assignees (Phase 4): the translator resolves mapped members to
	// GitLab's native assignee_ids so GitLab holds the authoritative state.
	// The cache, however, keys on Multica UUIDs — not GitLab user IDs —
	// and TranslateIssue doesn't reverse-resolve on its own (it only
	// surfaces GitlabAssigneeUserID for caller-side lookup). We patch the
	// cache row from the REQUEST here so the Multica UUID lands in
	// assignee_id. This covers both:
	//   - Mapped members: GitLab received assignee_ids: [N]; we write the
	//     member UUID so the cache matches the request (reverse-resolution
	//     via user_gitlab_connection would yield the same UUID).
	//   - Unmapped members: GitLab got nothing (assignee_ids omitted),
	//     cache-only fallback preserves the member locally.
	if req.AssigneeType != nil && *req.AssigneeType == "member" && req.AssigneeID != nil && *req.AssigneeID != "" {
		updParams.AssigneeType = pgtype.Text{String: "member", Valid: true}
		updParams.AssigneeID = parseUUID(*req.AssigneeID)
		touched = true
	}
	if touched {
		updated, updErr := qtxGL.UpdateIssue(ctx, updParams)
		if updErr != nil {
			slog.Error("patch native fields on gitlab cache row", "error", updErr)
			return nil, db.Issue{}, &writeThroughError{
				status: http.StatusInternalServerError,
				msg:    "failed to update issue",
				err:    updErr,
			}
		}
		cacheRow = updated
	}

	if err := glTx.Commit(ctx); err != nil {
		slog.Error("commit gitlab update-through tx", "error", err)
		return nil, db.Issue{}, &writeThroughError{
			status: http.StatusInternalServerError,
			msg:    "failed to update issue",
			err:    err,
		}
	}

	prefix := h.getIssuePrefix(ctx, cacheRow.WorkspaceID)
	resp := issueToResponse(cacheRow, prefix)
	return &resp, cacheRow, nil
}

func (h *Handler) UpdateIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	prevIssue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}
	userID := requestUserID(r)
	workspaceID := uuidToString(prevIssue.WorkspaceID)

	// Read body as raw bytes so we can detect which fields were explicitly sent.
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	var req UpdateIssueRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Track which fields were explicitly present in JSON (even if null)
	var rawFields map[string]json.RawMessage
	json.Unmarshal(bodyBytes, &rawFields)

	// Pre-fill nullable fields (bare sqlc.narg) with current values
	params := db.UpdateIssueParams{
		ID:            prevIssue.ID,
		AssigneeType:  prevIssue.AssigneeType,
		AssigneeID:    prevIssue.AssigneeID,
		DueDate:       prevIssue.DueDate,
		ParentIssueID: prevIssue.ParentIssueID,
		ProjectID:     prevIssue.ProjectID,
	}

	// COALESCE fields — only set when explicitly provided
	if req.Title != nil {
		params.Title = pgtype.Text{String: *req.Title, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.Status != nil {
		params.Status = pgtype.Text{String: *req.Status, Valid: true}
	}
	if req.Priority != nil {
		params.Priority = pgtype.Text{String: *req.Priority, Valid: true}
	}
	if req.Position != nil {
		params.Position = pgtype.Float8{Float64: *req.Position, Valid: true}
	}
	// Nullable fields — only override when explicitly present in JSON
	if _, ok := rawFields["assignee_type"]; ok {
		if req.AssigneeType != nil {
			params.AssigneeType = pgtype.Text{String: *req.AssigneeType, Valid: true}
		} else {
			params.AssigneeType = pgtype.Text{Valid: false} // explicit null = unassign
		}
	}
	if _, ok := rawFields["assignee_id"]; ok {
		if req.AssigneeID != nil {
			params.AssigneeID = parseUUID(*req.AssigneeID)
		} else {
			params.AssigneeID = pgtype.UUID{Valid: false} // explicit null = unassign
		}
	}
	if _, ok := rawFields["due_date"]; ok {
		if req.DueDate != nil && *req.DueDate != "" {
			t, err := time.Parse(time.RFC3339, *req.DueDate)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid due_date format, expected RFC3339")
				return
			}
			params.DueDate = pgtype.Timestamptz{Time: t, Valid: true}
		} else {
			params.DueDate = pgtype.Timestamptz{Valid: false} // explicit null = clear date
		}
	}
	if _, ok := rawFields["parent_issue_id"]; ok {
		if req.ParentIssueID != nil {
			newParentID := parseUUID(*req.ParentIssueID)
			// Cannot set self as parent.
			if uuidToString(newParentID) == id {
				writeError(w, http.StatusBadRequest, "an issue cannot be its own parent")
				return
			}
			// Validate parent exists in the same workspace.
			if _, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
				ID:          newParentID,
				WorkspaceID: prevIssue.WorkspaceID,
			}); err != nil {
				writeError(w, http.StatusBadRequest, "parent issue not found in this workspace")
				return
			}
			// Cycle detection: walk up from the new parent to ensure we don't reach this issue.
			cursor := newParentID
			for depth := 0; depth < 10; depth++ {
				ancestor, err := h.Queries.GetIssue(r.Context(), cursor)
				if err != nil || !ancestor.ParentIssueID.Valid {
					break
				}
				if uuidToString(ancestor.ParentIssueID) == id {
					writeError(w, http.StatusBadRequest, "circular parent relationship detected")
					return
				}
				cursor = ancestor.ParentIssueID
			}
			params.ParentIssueID = newParentID
		} else {
			params.ParentIssueID = pgtype.UUID{Valid: false} // explicit null = remove parent
		}
	}
	if _, ok := rawFields["project_id"]; ok {
		if req.ProjectID != nil {
			params.ProjectID = parseUUID(*req.ProjectID)
		} else {
			params.ProjectID = pgtype.UUID{Valid: false}
		}
	}

	// Enforce agent visibility: private agents can only be assigned by owner/admin.
	if req.AssigneeType != nil && *req.AssigneeType == "agent" && req.AssigneeID != nil {
		if ok, msg := h.canAssignAgent(r.Context(), r, *req.AssigneeID, workspaceID); !ok {
			writeError(w, http.StatusForbidden, msg)
			return
		}
	}

	// Phase 3b write-through: when the workspace has a GitLab connection,
	// PATCH the GitLab issue first, then reconcile the cache row from the
	// returned representation. On a GitLab-connected workspace the
	// write-through is AUTHORITATIVE — on GitLab error we return a non-2xx
	// status and never fall back to the legacy direct-DB path (that would
	// produce orphaned cache rows).
	if h.GitlabEnabled && h.GitlabResolver != nil {
		_, wsErr := h.Queries.GetWorkspaceGitlabConnection(r.Context(), prevIssue.WorkspaceID)
		if wsErr == nil {
			actorType, actorID := h.resolveActor(r, userID, workspaceID)
			token, _, tokErr := h.GitlabResolver.ResolveTokenForWrite(r.Context(), workspaceID, actorType, actorID)
			if tokErr != nil {
				slog.Error("resolve gitlab token", "error", tokErr, "workspace_id", workspaceID)
				writeError(w, http.StatusBadGateway, "could not resolve gitlab token")
				return
			}

			agentSlugByUUID, agentErr := h.buildAgentUUIDSlugMap(r.Context(), prevIssue.WorkspaceID)
			if agentErr != nil {
				slog.Error("build agent slug map", "error", agentErr, "workspace_id", workspaceID)
				writeError(w, http.StatusInternalServerError, "build agent map failed")
				return
			}

			resp, cacheRow, wtErr := h.updateSingleIssueWriteThrough(
				r.Context(), prevIssue, req, rawFields,
				workspaceID, token, agentSlugByUUID,
			)
			if wtErr != nil {
				writeError(w, writeThroughStatus(wtErr), wtErr.Error())
				return
			}

			// Detect side-effect-relevant changes by comparing the post-commit
			// cache row to prevIssue. Using the cache row (not the request) is
			// deliberate — the clobber-guard branch means the request may not
			// have been applied, and we don't want to enqueue agent tasks for
			// changes that didn't stick.
			assigneeChanged := prevIssue.AssigneeType.String != cacheRow.AssigneeType.String ||
				uuidToString(prevIssue.AssigneeID) != uuidToString(cacheRow.AssigneeID)
			statusChanged := prevIssue.Status != cacheRow.Status
			priorityChanged := prevIssue.Priority != cacheRow.Priority
			descriptionChanged := textToPtr(prevIssue.Description) != resp.Description
			titleChanged := prevIssue.Title != cacheRow.Title
			prevDueDate := timestampToPtr(prevIssue.DueDate)
			dueDateChanged := prevDueDate != resp.DueDate && (prevDueDate == nil) != (resp.DueDate == nil) ||
				(prevDueDate != nil && resp.DueDate != nil && *prevDueDate != *resp.DueDate)

			h.publish(protocol.EventIssueUpdated, workspaceID, actorType, actorID, map[string]any{
				"issue":               resp,
				"assignee_changed":    assigneeChanged,
				"status_changed":      statusChanged,
				"priority_changed":    priorityChanged,
				"due_date_changed":    dueDateChanged,
				"description_changed": descriptionChanged,
				"title_changed":       titleChanged,
				"prev_title":          prevIssue.Title,
				"prev_assignee_type":  textToPtr(prevIssue.AssigneeType),
				"prev_assignee_id":    uuidToPtr(prevIssue.AssigneeID),
				"prev_status":         prevIssue.Status,
				"prev_priority":       prevIssue.Priority,
				"prev_due_date":       prevDueDate,
				"prev_description":    textToPtr(prevIssue.Description),
				"creator_type":        prevIssue.CreatorType,
				"creator_id":          uuidToString(prevIssue.CreatorID),
			})

			// Reconcile task queue when assignee changes (matches legacy).
			if assigneeChanged {
				h.TaskService.CancelTasksForIssue(r.Context(), cacheRow.ID)
				if h.shouldEnqueueAgentTask(r.Context(), cacheRow) {
					h.TaskService.EnqueueTaskForIssue(r.Context(), cacheRow)
				}
			}

			// Trigger the assigned agent when a member moves an issue out of
			// backlog (matches legacy).
			if statusChanged && !assigneeChanged && actorType == "member" &&
				prevIssue.Status == "backlog" && cacheRow.Status != "done" && cacheRow.Status != "cancelled" {
				if h.isAgentAssigneeReady(r.Context(), cacheRow) {
					h.TaskService.EnqueueTaskForIssue(r.Context(), cacheRow)
				}
			}

			// Cancel active tasks on user-initiated cancellation (matches legacy).
			if statusChanged && cacheRow.Status == "cancelled" {
				h.TaskService.CancelTasksForIssue(r.Context(), cacheRow.ID)
			}

			writeJSON(w, http.StatusOK, resp)
			return
		}
		// wsErr != nil → fall through to legacy path (most likely
		// pgx.ErrNoRows for non-connected workspaces).
	}

	issue, err := h.Queries.UpdateIssue(r.Context(), params)
	if err != nil {
		slog.Warn("update issue failed", append(logger.RequestAttrs(r), "error", err, "issue_id", id, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to update issue: "+err.Error())
		return
	}

	prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
	resp := issueToResponse(issue, prefix)
	slog.Info("issue updated", append(logger.RequestAttrs(r), "issue_id", id, "workspace_id", workspaceID)...)

	// .String returns "" for both Valid=false (NULL) cases, so two unassigned
	// rows compare equal — intended.
	assigneeChanged := (req.AssigneeType != nil || req.AssigneeID != nil) &&
		(prevIssue.AssigneeType.String != issue.AssigneeType.String || uuidToString(prevIssue.AssigneeID) != uuidToString(issue.AssigneeID))
	statusChanged := req.Status != nil && prevIssue.Status != issue.Status
	priorityChanged := req.Priority != nil && prevIssue.Priority != issue.Priority
	descriptionChanged := req.Description != nil && textToPtr(prevIssue.Description) != resp.Description
	titleChanged := req.Title != nil && prevIssue.Title != issue.Title
	prevDueDate := timestampToPtr(prevIssue.DueDate)
	dueDateChanged := prevDueDate != resp.DueDate && (prevDueDate == nil) != (resp.DueDate == nil) ||
		(prevDueDate != nil && resp.DueDate != nil && *prevDueDate != *resp.DueDate)

	// Determine actor identity: agent (via X-Agent-ID header) or member.
	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	h.publish(protocol.EventIssueUpdated, workspaceID, actorType, actorID, map[string]any{
		"issue":               resp,
		"assignee_changed":    assigneeChanged,
		"status_changed":      statusChanged,
		"priority_changed":    priorityChanged,
		"due_date_changed":    dueDateChanged,
		"description_changed": descriptionChanged,
		"title_changed":       titleChanged,
		"prev_title":          prevIssue.Title,
		"prev_assignee_type":  textToPtr(prevIssue.AssigneeType),
		"prev_assignee_id":    uuidToPtr(prevIssue.AssigneeID),
		"prev_status":         prevIssue.Status,
		"prev_priority":       prevIssue.Priority,
		"prev_due_date":       prevDueDate,
		"prev_description":    textToPtr(prevIssue.Description),
		"creator_type":        prevIssue.CreatorType,
		"creator_id":          uuidToString(prevIssue.CreatorID),
	})

	// Reconcile task queue when assignee changes.
	if assigneeChanged {
		h.TaskService.CancelTasksForIssue(r.Context(), issue.ID)

		if h.shouldEnqueueAgentTask(r.Context(), issue) {
			h.TaskService.EnqueueTaskForIssue(r.Context(), issue)
		}
	}

	// Trigger the assigned agent when a member moves an issue out of backlog.
	// Backlog acts as a parking lot — moving to an active status signals the
	// issue is ready for work.
	if statusChanged && !assigneeChanged && actorType == "member" &&
		prevIssue.Status == "backlog" && issue.Status != "done" && issue.Status != "cancelled" {
		if h.isAgentAssigneeReady(r.Context(), issue) {
			h.TaskService.EnqueueTaskForIssue(r.Context(), issue)
		}
	}

	// Cancel active tasks when the issue is cancelled by a user.
	// This is distinct from agent-managed status transitions — cancellation
	// is a user-initiated terminal action that should stop execution.
	if statusChanged && issue.Status == "cancelled" {
		h.TaskService.CancelTasksForIssue(r.Context(), issue.ID)
	}

	writeJSON(w, http.StatusOK, resp)
}

// canAssignAgent checks whether the requesting user is allowed to assign issues
// to the given agent. Private agents can only be assigned by their owner or
// workspace admins/owners.
func (h *Handler) canAssignAgent(ctx context.Context, r *http.Request, agentID, workspaceID string) (bool, string) {
	agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
		ID:          parseUUID(agentID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		return false, "agent not found"
	}
	if agent.ArchivedAt.Valid {
		return false, "cannot assign to archived agent"
	}
	if agent.Visibility != "private" {
		return true, ""
	}
	userID := requestUserID(r)
	if uuidToString(agent.OwnerID) == userID {
		return true, ""
	}
	member, err := h.getWorkspaceMember(ctx, userID, workspaceID)
	if err != nil {
		return false, "cannot assign to private agent"
	}
	if roleAllowed(member.Role, "owner", "admin") {
		return true, ""
	}
	return false, "cannot assign to private agent"
}

// shouldEnqueueAgentTask returns true when an issue creation or assignment
// should trigger the assigned agent. Backlog issues are skipped — backlog
// acts as a parking lot where issues can be pre-assigned without immediately
// triggering execution. Moving out of backlog is handled separately in
// UpdateIssue.
func (h *Handler) shouldEnqueueAgentTask(ctx context.Context, issue db.Issue) bool {
	if issue.Status == "backlog" {
		return false
	}
	return h.isAgentAssigneeReady(ctx, issue)
}

// shouldEnqueueOnComment returns true if a member comment on this issue should
// trigger the assigned agent. Fires for any status — comments are
// conversational and can happen at any stage, including after completion
// (e.g. follow-up questions on a done issue).
func (h *Handler) shouldEnqueueOnComment(ctx context.Context, issue db.Issue) bool {
	if !h.isAgentAssigneeReady(ctx, issue) {
		return false
	}
	// Coalescing queue: allow enqueue when a task is running (so the agent
	// picks up new comments on the next cycle) but skip if this agent already
	// has a pending task (natural dedup for rapid-fire comments).
	hasPending, err := h.Queries.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
		IssueID: issue.ID,
		AgentID: issue.AssigneeID,
	})
	if err != nil || hasPending {
		return false
	}
	return true
}

// isAgentAssigneeReady checks if an issue is assigned to an active agent
// with a valid runtime.
func (h *Handler) isAgentAssigneeReady(ctx context.Context, issue db.Issue) bool {
	if !issue.AssigneeType.Valid || issue.AssigneeType.String != "agent" || !issue.AssigneeID.Valid {
		return false
	}

	agent, err := h.Queries.GetAgent(ctx, issue.AssigneeID)
	if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
		return false
	}

	return true
}

// CleanupAndDeleteIssue is the exported adapter that satisfies
// gitlab.IssueDeleter for the webhook worker. The worker can't call the
// unexported method directly (and handler imports of gitlab would cycle if
// the interface lived there), so we expose a thin forwarder. No extra logic
// belongs here — all cleanup responsibilities stay in cleanupAndDeleteIssueRow.
func (h *Handler) CleanupAndDeleteIssue(ctx context.Context, issue db.Issue) error {
	return h.cleanupAndDeleteIssueRow(ctx, issue)
}

// cleanupAndDeleteIssueRow performs the Multica-side cleanup for a deleted
// issue: cancels agent tasks, fails autopilot runs, removes the cache row,
// then deletes S3 attachments. Shared between the legacy DeleteIssue path and
// the GitLab write-through branch — the write-through calls GitLab first,
// then calls this to tear down the local state.
func (h *Handler) cleanupAndDeleteIssueRow(ctx context.Context, issue db.Issue) error {
	h.TaskService.CancelTasksForIssue(ctx, issue.ID)
	// Fail any linked autopilot runs before delete (ON DELETE SET NULL clears issue_id).
	h.Queries.FailAutopilotRunsByIssue(ctx, issue.ID)

	// Collect all attachment URLs (issue-level + comment-level) before CASCADE delete.
	attachmentURLs, _ := h.Queries.ListAttachmentURLsByIssueOrComments(ctx, issue.ID)

	if err := h.Queries.DeleteIssue(ctx, issue.ID); err != nil {
		return err
	}

	h.deleteS3Objects(ctx, attachmentURLs)
	return nil
}

// deleteSingleIssueWriteThrough executes the Phase 3b DELETE write-through
// for a single issue: DELETE /projects/:id/issues/:iid on GitLab, then
// cleanupAndDeleteIssueRow for the local state. Used by both DeleteIssue
// (direct handler) and BatchDeleteIssues (loops over this). Callers
// pre-resolve token once per request to avoid redundant work; the
// workspace_gitlab_connection row they fetched is not needed here
// because the GitLab identifiers already live on the issue row.
//
// workspaceID is threaded through purely for slog logging context.
//
// On error returns a *writeThroughError with a status hint that the direct
// handler maps to an HTTP status. The batch handler ignores the status
// hint and classifies the error via classifyBatchError, which uses
// errors.Is/As against GitLab sentinels and APIError.StatusCode —
// classification is invariant under writeThroughError's %w wrapping.
//
// Note: gitlab.Client.DeleteIssue swallows 404 (already-gone issues are
// considered successfully deleted), so a missing GitLab issue will not
// surface here — we'll proceed to clean up the cache row.
func (h *Handler) deleteSingleIssueWriteThrough(
	ctx context.Context,
	issue db.Issue,
	workspaceID string,
	token string,
) error {
	// Defensive: a cache row on a GitLab-connected workspace that lacks
	// GitLab identifiers is a bug upstream of this handler. Refuse the
	// write-through rather than silently fall through.
	if !issue.GitlabIid.Valid || !issue.GitlabProjectID.Valid {
		slog.Error("gitlab-connected workspace issue missing gitlab identifiers",
			"issue_id", uuidToString(issue.ID), "workspace_id", workspaceID)
		return &writeThroughError{
			status: http.StatusBadGateway,
			msg:    "issue is missing gitlab identifiers",
		}
	}

	if err := h.Gitlab.DeleteIssue(ctx, token, issue.GitlabProjectID.Int64, int(issue.GitlabIid.Int32)); err != nil {
		slog.Error("gitlab delete issue", "error", err, "issue_id", uuidToString(issue.ID))
		return &writeThroughError{
			status: http.StatusBadGateway,
			msg:    "gitlab delete issue failed",
			err:    err,
		}
	}

	if err := h.cleanupAndDeleteIssueRow(ctx, issue); err != nil {
		slog.Error("cleanup issue row after gitlab delete", "error", err, "issue_id", uuidToString(issue.ID))
		return &writeThroughError{
			status: http.StatusInternalServerError,
			msg:    "failed to delete issue",
			err:    err,
		}
	}

	return nil
}

func (h *Handler) DeleteIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}
	userID := requestUserID(r)
	workspaceID := uuidToString(issue.WorkspaceID)

	// Phase 3b write-through: when the workspace has a GitLab connection,
	// DELETE the GitLab issue first, then tear down the cache row. On a
	// GitLab-connected workspace the write-through is AUTHORITATIVE — on
	// GitLab error we return 502 and never fall back to legacy (that would
	// leave GitLab and the cache diverged).
	if h.GitlabEnabled && h.GitlabResolver != nil {
		_, wsErr := h.Queries.GetWorkspaceGitlabConnection(r.Context(), issue.WorkspaceID)
		if wsErr == nil {
			actorType, actorID := h.resolveActor(r, userID, workspaceID)
			token, _, tokErr := h.GitlabResolver.ResolveTokenForWrite(r.Context(), workspaceID, actorType, actorID)
			if tokErr != nil {
				slog.Error("resolve gitlab token", "error", tokErr, "workspace_id", workspaceID)
				writeError(w, http.StatusBadGateway, "could not resolve gitlab token")
				return
			}

			if err := h.deleteSingleIssueWriteThrough(r.Context(), issue, workspaceID, token); err != nil {
				writeError(w, writeThroughStatus(err), err.Error())
				return
			}

			h.publish(protocol.EventIssueDeleted, workspaceID, actorType, actorID, map[string]any{"issue_id": id})
			slog.Info("issue deleted (gitlab write-through)", append(logger.RequestAttrs(r), "issue_id", id, "workspace_id", workspaceID)...)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// else: not connected → fall through to legacy
	}

	if err := h.cleanupAndDeleteIssueRow(r.Context(), issue); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete issue")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish(protocol.EventIssueDeleted, workspaceID, actorType, actorID, map[string]any{"issue_id": id})
	slog.Info("issue deleted", append(logger.RequestAttrs(r), "issue_id", id, "workspace_id", workspaceID)...)
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Batch operations
// ---------------------------------------------------------------------------

type BatchUpdateIssuesRequest struct {
	IssueIDs []string           `json:"issue_ids"`
	Updates  UpdateIssueRequest `json:"updates"`
}

func (h *Handler) BatchUpdateIssues(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	var req BatchUpdateIssuesRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.IssueIDs) == 0 {
		writeError(w, http.StatusBadRequest, "issue_ids is required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	workspaceID := h.resolveWorkspaceID(r)

	// Phase 3b write-through: when the workspace has a GitLab connection,
	// loop over each issue, attempting the PATCH write-through per item with
	// continue-on-error semantics. Returns BatchWriteResult with 207 when
	// mixed success/failure, 200 when all-success or all-failure. Non-
	// connected workspaces fall through to the legacy direct-DB batch path.
	if h.GitlabEnabled && h.GitlabResolver != nil {
		_, wsErr := h.Queries.GetWorkspaceGitlabConnection(r.Context(), parseUUID(workspaceID))
		if wsErr == nil {
			actorType, actorID := h.resolveActor(r, userID, workspaceID)
			token, _, tokErr := h.GitlabResolver.ResolveTokenForWrite(r.Context(), workspaceID, actorType, actorID)
			if tokErr != nil {
				slog.Error("resolve gitlab token for batch update", "error", tokErr, "workspace_id", workspaceID)
				writeError(w, http.StatusBadGateway, "could not resolve gitlab token")
				return
			}
			agentSlugByUUID, agentErr := h.buildAgentUUIDSlugMap(r.Context(), parseUUID(workspaceID))
			if agentErr != nil {
				slog.Error("build agent slug map for batch update", "error", agentErr, "workspace_id", workspaceID)
				writeError(w, http.StatusInternalServerError, "build agent map failed")
				return
			}

			result := BatchWriteResult{
				Succeeded: []BatchSucceeded{},
				Failed:    []BatchFailed{},
			}

			for _, issueID := range req.IssueIDs {
				prevIssue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
					ID:          parseUUID(issueID),
					WorkspaceID: parseUUID(workspaceID),
				})
				if err != nil {
					code, msg := classifyBatchError(err)
					result.Failed = append(result.Failed, BatchFailed{ID: issueID, ErrorCode: code, Message: msg})
					continue
				}

				// Enforce agent visibility per-item (matches legacy batch).
				if req.Updates.AssigneeType != nil && *req.Updates.AssigneeType == "agent" && req.Updates.AssigneeID != nil {
					if ok, reason := h.canAssignAgent(r.Context(), r, *req.Updates.AssigneeID, workspaceID); !ok {
						result.Failed = append(result.Failed, BatchFailed{
							ID:        issueID,
							ErrorCode: "FORBIDDEN_ASSIGNEE",
							Message:   reason,
						})
						continue
					}
				}

				resp, cacheRow, perr := h.updateSingleIssueWriteThrough(
					r.Context(), prevIssue, req.Updates, nil,
					workspaceID, token, agentSlugByUUID,
				)
				if perr != nil {
					code, msg := classifyBatchError(perr)
					result.Failed = append(result.Failed, BatchFailed{ID: issueID, ErrorCode: code, Message: msg})
					continue
				}

				// Per-item side-effects (subset of single-UpdateIssue). Use
				// post-commit cache row comparison — same rationale as the
				// single-issue handler: the clobber guard may reject the
				// upsert and we don't want to enqueue tasks for no-op changes.
				assigneeChanged := prevIssue.AssigneeType.String != cacheRow.AssigneeType.String ||
					uuidToString(prevIssue.AssigneeID) != uuidToString(cacheRow.AssigneeID)
				statusChanged := prevIssue.Status != cacheRow.Status

				if assigneeChanged {
					h.TaskService.CancelTasksForIssue(r.Context(), cacheRow.ID)
					if h.shouldEnqueueAgentTask(r.Context(), cacheRow) {
						h.TaskService.EnqueueTaskForIssue(r.Context(), cacheRow)
					}
				}
				if statusChanged && !assigneeChanged && actorType == "member" &&
					prevIssue.Status == "backlog" && cacheRow.Status != "done" && cacheRow.Status != "cancelled" {
					if h.isAgentAssigneeReady(r.Context(), cacheRow) {
						h.TaskService.EnqueueTaskForIssue(r.Context(), cacheRow)
					}
				}
				if statusChanged && cacheRow.Status == "cancelled" {
					h.TaskService.CancelTasksForIssue(r.Context(), cacheRow.ID)
				}

				// Emit a simpler {"issue": resp} payload per-item on the batch
				// path — intentionally omitting the prev_*/*_changed flags the
				// single-issue PATCH emits. Rationale: subscribers that need
				// fine-grained diff metadata (for activity-log authoring,
				// inbox notifications, etc.) should drive off single-issue
				// PATCH events; the batch path exists for bulk mutations where
				// "some set of issues changed" is the useful signal and
				// computing N rich payloads would balloon socket traffic.
				// Single-issue PATCH remains available for callers that need
				// the richer shape.
				h.publish(protocol.EventIssueUpdated, workspaceID, actorType, actorID, map[string]any{"issue": resp})

				result.Succeeded = append(result.Succeeded, BatchSucceeded{ID: issueID, Issue: resp})
			}

			// 207 when mixed; 200 when all-success OR all-failure (client
			// inspects the lists either way).
			status := http.StatusOK
			if len(result.Failed) > 0 && len(result.Succeeded) > 0 {
				status = http.StatusMultiStatus
			}
			slog.Info("batch update issues (gitlab write-through)",
				append(logger.RequestAttrs(r),
					"succeeded", len(result.Succeeded),
					"failed", len(result.Failed))...)
			// Response shape: BatchWriteResult = {succeeded: [...], failed: [...]}.
			// This diverges from the legacy non-connected path below which
			// returns {"updated": n} (counter only). The divergence is
			// intentional — per-item continue-on-error semantics need the
			// per-item detail — but NOT yet normalized across both paths.
			// Frontends must key off the presence of "succeeded" vs "updated".
			// Normalizing the legacy path to BatchWriteResult is a separate
			// design decision (it would change the shape non-connected
			// workspaces see today) and is out of scope for Phase 3b.
			writeJSON(w, status, result)
			return
		}
		// wsErr != nil → fall through to legacy path (non-connected workspace).
	}

	h.legacyBatchUpdateIssues(w, r, bodyBytes, req, userID, workspaceID)
}

// legacyBatchUpdateIssues is the pre-Phase-3b batch update behavior,
// preserved verbatim for workspaces without a GitLab connection. Called by
// BatchUpdateIssues after it determines the workspace is unconnected.
// Body decode + auth + workspace resolution happen once in the caller,
// so this function receives them as arguments.
func (h *Handler) legacyBatchUpdateIssues(
	w http.ResponseWriter,
	r *http.Request,
	bodyBytes []byte,
	req BatchUpdateIssuesRequest,
	userID, workspaceID string,
) {
	// Detect which fields in "updates" were explicitly set (including null).
	var rawTop map[string]json.RawMessage
	json.Unmarshal(bodyBytes, &rawTop)
	var rawUpdates map[string]json.RawMessage
	if raw, exists := rawTop["updates"]; exists {
		json.Unmarshal(raw, &rawUpdates)
	}

	updated := 0
	for _, issueID := range req.IssueIDs {
		prevIssue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          parseUUID(issueID),
			WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil {
			continue
		}

		params := db.UpdateIssueParams{
			ID:            prevIssue.ID,
			AssigneeType:  prevIssue.AssigneeType,
			AssigneeID:    prevIssue.AssigneeID,
			DueDate:       prevIssue.DueDate,
			ParentIssueID: prevIssue.ParentIssueID,
			ProjectID:     prevIssue.ProjectID,
		}

		if req.Updates.Title != nil {
			params.Title = pgtype.Text{String: *req.Updates.Title, Valid: true}
		}
		if req.Updates.Description != nil {
			params.Description = pgtype.Text{String: *req.Updates.Description, Valid: true}
		}
		if req.Updates.Status != nil {
			params.Status = pgtype.Text{String: *req.Updates.Status, Valid: true}
		}
		if req.Updates.Priority != nil {
			params.Priority = pgtype.Text{String: *req.Updates.Priority, Valid: true}
		}
		if req.Updates.Position != nil {
			params.Position = pgtype.Float8{Float64: *req.Updates.Position, Valid: true}
		}
		if _, ok := rawUpdates["assignee_type"]; ok {
			if req.Updates.AssigneeType != nil {
				params.AssigneeType = pgtype.Text{String: *req.Updates.AssigneeType, Valid: true}
			} else {
				params.AssigneeType = pgtype.Text{Valid: false}
			}
		}
		if _, ok := rawUpdates["assignee_id"]; ok {
			if req.Updates.AssigneeID != nil {
				params.AssigneeID = parseUUID(*req.Updates.AssigneeID)
			} else {
				params.AssigneeID = pgtype.UUID{Valid: false}
			}
		}
		if _, ok := rawUpdates["due_date"]; ok {
			if req.Updates.DueDate != nil && *req.Updates.DueDate != "" {
				t, err := time.Parse(time.RFC3339, *req.Updates.DueDate)
				if err != nil {
					continue
				}
				params.DueDate = pgtype.Timestamptz{Time: t, Valid: true}
			} else {
				params.DueDate = pgtype.Timestamptz{Valid: false}
			}
		}

		if _, ok := rawUpdates["parent_issue_id"]; ok {
			if req.Updates.ParentIssueID != nil {
				newParentID := parseUUID(*req.Updates.ParentIssueID)
				// Cannot set self as parent.
				if uuidToString(newParentID) == issueID {
					continue
				}
				// Validate parent exists in the same workspace.
				if _, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
					ID:          newParentID,
					WorkspaceID: prevIssue.WorkspaceID,
				}); err != nil {
					continue
				}
				// Cycle detection: walk up from the new parent to ensure we don't reach this issue.
				cycleDetected := false
				cursor := newParentID
				for depth := 0; depth < 10; depth++ {
					ancestor, err := h.Queries.GetIssue(r.Context(), cursor)
					if err != nil || !ancestor.ParentIssueID.Valid {
						break
					}
					if uuidToString(ancestor.ParentIssueID) == issueID {
						cycleDetected = true
						break
					}
					cursor = ancestor.ParentIssueID
				}
				if cycleDetected {
					continue
				}
				params.ParentIssueID = newParentID
			} else {
				params.ParentIssueID = pgtype.UUID{Valid: false}
			}
		}
		if _, ok := rawUpdates["project_id"]; ok {
			if req.Updates.ProjectID != nil {
				params.ProjectID = parseUUID(*req.Updates.ProjectID)
			} else {
				params.ProjectID = pgtype.UUID{Valid: false}
			}
		}

		// Enforce agent visibility for batch assignment.
		if req.Updates.AssigneeType != nil && *req.Updates.AssigneeType == "agent" && req.Updates.AssigneeID != nil {
			if ok, _ := h.canAssignAgent(r.Context(), r, *req.Updates.AssigneeID, workspaceID); !ok {
				continue
			}
		}

		issue, err := h.Queries.UpdateIssue(r.Context(), params)
		if err != nil {
			slog.Warn("batch update issue failed", "issue_id", issueID, "error", err)
			continue
		}

		prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
		resp := issueToResponse(issue, prefix)
		actorType, actorID := h.resolveActor(r, userID, workspaceID)

		assigneeChanged := (req.Updates.AssigneeType != nil || req.Updates.AssigneeID != nil) &&
			(prevIssue.AssigneeType.String != issue.AssigneeType.String || uuidToString(prevIssue.AssigneeID) != uuidToString(issue.AssigneeID))
		statusChanged := req.Updates.Status != nil && prevIssue.Status != issue.Status
		priorityChanged := req.Updates.Priority != nil && prevIssue.Priority != issue.Priority

		h.publish(protocol.EventIssueUpdated, workspaceID, actorType, actorID, map[string]any{
			"issue":            resp,
			"assignee_changed": assigneeChanged,
			"status_changed":   statusChanged,
			"priority_changed": priorityChanged,
		})

		if assigneeChanged {
			h.TaskService.CancelTasksForIssue(r.Context(), issue.ID)
			if h.shouldEnqueueAgentTask(r.Context(), issue) {
				h.TaskService.EnqueueTaskForIssue(r.Context(), issue)
			}
		}

		// Trigger agent when moving out of backlog (batch).
		if statusChanged && !assigneeChanged && actorType == "member" &&
			prevIssue.Status == "backlog" && issue.Status != "done" && issue.Status != "cancelled" {
			if h.isAgentAssigneeReady(r.Context(), issue) {
				h.TaskService.EnqueueTaskForIssue(r.Context(), issue)
			}
		}

		// Cancel active tasks when the issue is cancelled by a user.
		if statusChanged && issue.Status == "cancelled" {
			h.TaskService.CancelTasksForIssue(r.Context(), issue.ID)
		}

		updated++
	}

	slog.Info("batch update issues", append(logger.RequestAttrs(r), "count", updated)...)
	// Response shape: {"updated": n}. Diverges from the connected write-through
	// path above which returns BatchWriteResult {succeeded, failed}. This
	// legacy shape is preserved verbatim (pre-Phase-3b behaviour) and is NOT
	// yet normalized across the two paths — normalization would change the
	// contract non-connected workspaces see today and is out of scope here.
	writeJSON(w, http.StatusOK, map[string]any{"updated": updated})
}

type BatchDeleteIssuesRequest struct {
	IssueIDs []string `json:"issue_ids"`
}

func (h *Handler) BatchDeleteIssues(w http.ResponseWriter, r *http.Request) {
	var req BatchDeleteIssuesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.IssueIDs) == 0 {
		writeError(w, http.StatusBadRequest, "issue_ids is required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	workspaceID := h.resolveWorkspaceID(r)

	// Phase 3b write-through: when the workspace has a GitLab connection,
	// loop over each issue, attempting the DELETE write-through per item with
	// continue-on-error semantics. Returns BatchWriteResult with 207 when
	// mixed success/failure, 200 when all-success or all-failure. Non-
	// connected workspaces fall through to the legacy direct-DB batch path.
	if h.GitlabEnabled && h.GitlabResolver != nil {
		_, wsErr := h.Queries.GetWorkspaceGitlabConnection(r.Context(), parseUUID(workspaceID))
		if wsErr == nil {
			actorType, actorID := h.resolveActor(r, userID, workspaceID)
			token, _, tokErr := h.GitlabResolver.ResolveTokenForWrite(r.Context(), workspaceID, actorType, actorID)
			if tokErr != nil {
				slog.Error("resolve gitlab token for batch delete", "error", tokErr, "workspace_id", workspaceID)
				writeError(w, http.StatusBadGateway, "could not resolve gitlab token")
				return
			}

			result := BatchWriteResult{
				Succeeded: []BatchSucceeded{},
				Failed:    []BatchFailed{},
			}

			for _, issueID := range req.IssueIDs {
				issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
					ID:          parseUUID(issueID),
					WorkspaceID: parseUUID(workspaceID),
				})
				if err != nil {
					code, msg := classifyBatchError(err)
					result.Failed = append(result.Failed, BatchFailed{ID: issueID, ErrorCode: code, Message: msg})
					continue
				}

				if perr := h.deleteSingleIssueWriteThrough(r.Context(), issue, workspaceID, token); perr != nil {
					code, msg := classifyBatchError(perr)
					result.Failed = append(result.Failed, BatchFailed{ID: issueID, ErrorCode: code, Message: msg})
					continue
				}

				h.publish(protocol.EventIssueDeleted, workspaceID, actorType, actorID, map[string]any{"issue_id": issueID})
				result.Succeeded = append(result.Succeeded, BatchSucceeded{ID: issueID})
			}

			// 207 when mixed; 200 when all-success OR all-failure (client
			// inspects the lists either way).
			status := http.StatusOK
			if len(result.Failed) > 0 && len(result.Succeeded) > 0 {
				status = http.StatusMultiStatus
			}
			slog.Info("batch delete issues (gitlab write-through)",
				append(logger.RequestAttrs(r),
					"succeeded", len(result.Succeeded),
					"failed", len(result.Failed))...)
			// Response shape: BatchWriteResult = {succeeded: [...], failed: [...]}
			// (with Succeeded.Issue == nil for batch-delete). Diverges from the
			// legacy non-connected path below which returns {"deleted": n}.
			// Divergence is intentional (per-item continue-on-error semantics)
			// and NOT yet normalized — mirrors the batch-update split above.
			writeJSON(w, status, result)
			return
		}
		// wsErr != nil → fall through to legacy path (non-connected workspace).
	}

	h.legacyBatchDeleteIssues(w, r, req, userID, workspaceID)
}

// legacyBatchDeleteIssues is the pre-Phase-3b batch delete behavior,
// preserved verbatim for workspaces without a GitLab connection. Called by
// BatchDeleteIssues after it determines the workspace is unconnected.
// Body decode + auth + workspace resolution happen once in the caller,
// so this function receives them as arguments.
func (h *Handler) legacyBatchDeleteIssues(
	w http.ResponseWriter,
	r *http.Request,
	req BatchDeleteIssuesRequest,
	userID, workspaceID string,
) {
	deleted := 0
	for _, issueID := range req.IssueIDs {
		issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          parseUUID(issueID),
			WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil {
			continue
		}

		h.TaskService.CancelTasksForIssue(r.Context(), issue.ID)
		h.Queries.FailAutopilotRunsByIssue(r.Context(), issue.ID)

		// Collect attachment URLs before CASCADE delete to clean up S3 objects.
		attachmentURLs, _ := h.Queries.ListAttachmentURLsByIssueOrComments(r.Context(), issue.ID)

		if err := h.Queries.DeleteIssue(r.Context(), issue.ID); err != nil {
			slog.Warn("batch delete issue failed", "issue_id", issueID, "error", err)
			continue
		}

		h.deleteS3Objects(r.Context(), attachmentURLs)

		actorType, actorID := h.resolveActor(r, userID, workspaceID)
		h.publish(protocol.EventIssueDeleted, workspaceID, actorType, actorID, map[string]any{"issue_id": issueID})
		deleted++
	}

	slog.Info("batch delete issues", append(logger.RequestAttrs(r), "count", deleted)...)
	// Response shape: {"deleted": n}. Diverges from the connected write-through
	// path above which returns BatchWriteResult {succeeded, failed}. Preserved
	// verbatim (pre-Phase-3b behaviour) and NOT yet normalized across the two
	// paths — same rationale as batch-update.
	writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}
