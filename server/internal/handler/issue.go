package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"errors"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// IssueResponse is the JSON response for an issue.
type IssueResponse struct {
	ID            string                  `json:"id"`
	WorkspaceID   string                  `json:"workspace_id"`
	Number        int32                   `json:"number"`
	Identifier    string                  `json:"identifier"`
	Title         string                  `json:"title"`
	Description   *string                 `json:"description"`
	Status        string                  `json:"status"`
	Priority      string                  `json:"priority"`
	AssigneeType  *string                 `json:"assignee_type"`
	AssigneeID    *string                 `json:"assignee_id"`
	CreatorType   string                  `json:"creator_type"`
	CreatorID     string                  `json:"creator_id"`
	ParentIssueID *string                 `json:"parent_issue_id"`
	ProjectID     *string                 `json:"project_id"`
	Position      float64                 `json:"position"`
	DueDate       *string                 `json:"due_date"`
	CreatedAt     string                  `json:"created_at"`
	UpdatedAt     string                  `json:"updated_at"`
	Reactions     []IssueReactionResponse `json:"reactions,omitempty"`
	Attachments   []AttachmentResponse    `json:"attachments,omitempty"`
	// Labels are bulk-attached by list/detail endpoints so the client can render
	// chips without an N+1 round-trip per row. Pointer + omitempty so paths that
	// don't load labels (e.g. UpdateIssue, batch UpdateIssues, the issue:updated
	// WS broadcast) emit no `labels` field at all — the client merge then
	// preserves whatever labels are already in cache. nil pointer = "field
	// absent, do not touch"; non-nil (incl. empty slice) = authoritative list.
	Labels *[]LabelResponse `json:"labels,omitempty"`
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
		CreatorType:   i.CreatorType,
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
		Description:   textToPtr(i.Description),
		Status:        i.Status,
		Priority:      i.Priority,
		AssigneeType:  textToPtr(i.AssigneeType),
		AssigneeID:    uuidToPtr(i.AssigneeID),
		CreatorType:   i.CreatorType,
		CreatorID:     uuidToString(i.CreatorID),
		ParentIssueID: uuidToPtr(i.ParentIssueID),
		ProjectID:     uuidToPtr(i.ProjectID),
		Position:      i.Position,
		DueDate:       timestampToPtr(i.DueDate),
		CreatedAt:     timestampToString(i.CreatedAt),
		UpdatedAt:     timestampToString(i.UpdatedAt),
	}
}

// labelsByIssue bulk-loads labels for the given issue IDs and returns a map
// keyed by issue UUID string. On error or empty input, returns an empty map —
// label rendering is non-critical and we'd rather serve issues without labels
// than fail the whole list call.
func (h *Handler) labelsByIssue(ctx context.Context, wsUUID pgtype.UUID, issueIDs []pgtype.UUID) map[string][]LabelResponse {
	out := map[string][]LabelResponse{}
	if len(issueIDs) == 0 {
		return out
	}
	rows, err := h.Queries.ListLabelsForIssues(ctx, db.ListLabelsForIssuesParams{
		IssueIds:    issueIDs,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		slog.Warn("ListLabelsForIssues failed", "error", err)
		return out
	}
	for _, r := range rows {
		issueID := uuidToString(r.IssueID)
		out[issueID] = append(out[issueID], LabelResponse{
			ID:          uuidToString(r.ID),
			WorkspaceID: uuidToString(r.WorkspaceID),
			Name:        r.Name,
			Color:       r.Color,
			CreatedAt:   timestampToString(r.CreatedAt),
			UpdatedAt:   timestampToString(r.UpdatedAt),
		})
	}
	return out
}

func openIssueRowToResponse(i db.ListOpenIssuesRow, issuePrefix string) IssueResponse {
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
		CreatorType:   i.CreatorType,
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
	phraseParam := nextArg(escapedPhrase) // $1
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

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
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
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	// Parse optional filter params. Malformed UUIDs in filters return 400 —
	// silently coercing them to a zero UUID would mask a client bug and let
	// the query return an empty result set (or worse, match a NULL row).
	var priorityFilter pgtype.Text
	if p := r.URL.Query().Get("priority"); p != "" {
		priorityFilter = pgtype.Text{String: p, Valid: true}
	}
	var assigneeFilter pgtype.UUID
	if a := r.URL.Query().Get("assignee_id"); a != "" {
		id, ok := parseUUIDOrBadRequest(w, a, "assignee_id")
		if !ok {
			return
		}
		assigneeFilter = id
	}
	var assigneeIdsFilter []pgtype.UUID
	if ids := r.URL.Query().Get("assignee_ids"); ids != "" {
		for _, raw := range strings.Split(ids, ",") {
			if s := strings.TrimSpace(raw); s != "" {
				id, ok := parseUUIDOrBadRequest(w, s, "assignee_ids")
				if !ok {
					return
				}
				assigneeIdsFilter = append(assigneeIdsFilter, id)
			}
		}
	}
	var creatorFilter pgtype.UUID
	if c := r.URL.Query().Get("creator_id"); c != "" {
		id, ok := parseUUIDOrBadRequest(w, c, "creator_id")
		if !ok {
			return
		}
		creatorFilter = id
	}
	var projectFilter pgtype.UUID
	if p := r.URL.Query().Get("project_id"); p != "" {
		id, ok := parseUUIDOrBadRequest(w, p, "project_id")
		if !ok {
			return
		}
		projectFilter = id
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
		ids := make([]pgtype.UUID, len(issues))
		for i, issue := range issues {
			ids[i] = issue.ID
		}
		labelsMap := h.labelsByIssue(ctx, wsUUID, ids)
		resp := make([]IssueResponse, len(issues))
		for i, issue := range issues {
			resp[i] = openIssueRowToResponse(issue, prefix)
			labels := labelsMap[resp[i].ID]
			if labels == nil {
				labels = []LabelResponse{}
			}
			resp[i].Labels = &labels
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
	ids := make([]pgtype.UUID, len(issues))
	for i, issue := range issues {
		ids[i] = issue.ID
	}
	labelsMap := h.labelsByIssue(ctx, wsUUID, ids)
	resp := make([]IssueResponse, len(issues))
	for i, issue := range issues {
		resp[i] = issueListRowToResponse(issue, prefix)
		labels := labelsMap[resp[i].ID]
		if labels == nil {
			labels = []LabelResponse{}
		}
		resp[i].Labels = &labels
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
	detailLabels := h.labelsByIssue(r.Context(), issue.WorkspaceID, []pgtype.UUID{issue.ID})[uuidToString(issue.ID)]
	if detailLabels == nil {
		detailLabels = []LabelResponse{}
	}
	resp.Labels = &detailLabels

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
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}

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

// QuickCreateIssueRequest is the body for POST /api/issues/quick-create. The
// user picks an agent in the modal and types one line of natural language;
// the server validates the agent's reachability up front, queues a quick-
// create task, and returns 202 immediately. The agent translates the prompt
// into a `multica issue create` invocation in the background; success and
// failure both surface as inbox notifications to the requester.
type QuickCreateIssueRequest struct {
	AgentID string `json:"agent_id"`
	Prompt  string `json:"prompt"`
}

// QuickCreateIssueResponse echoes the queued task id so the frontend can
// correlate the eventual inbox item, even though completion is fully async.
type QuickCreateIssueResponse struct {
	TaskID string `json:"task_id"`
}

func (h *Handler) QuickCreateIssue(w http.ResponseWriter, r *http.Request) {
	var req QuickCreateIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		writeError(w, http.StatusBadRequest, "prompt is required")
		return
	}
	agentUUID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
	if !ok {
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	requesterID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	requesterUUID, ok := parseUUIDOrBadRequest(w, requesterID, "requester_id")
	if !ok {
		return
	}

	// Reuse the same workspace-membership / archived / private-agent
	// ownership rules as `validateAssigneePair` so a user can't POST a
	// private agent_id they shouldn't be able to dispatch (the frontend
	// filters them out, but the handler is the trust boundary).
	if status, msg := h.validateAssigneePair(
		r.Context(), r, workspaceID,
		pgtype.Text{String: "agent", Valid: true},
		agentUUID,
	); status != 0 {
		writeError(w, status, msg)
		return
	}

	// Re-load the agent for the runtime liveness check below. Safe by
	// construction: validateAssigneePair just confirmed it exists in this
	// workspace and the caller has visibility.
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          agentUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if !agent.RuntimeID.Valid {
		writeAgentUnavailable(w, "agent has no runtime")
		return
	}
	if !h.isRuntimeOnline(r.Context(), agent.RuntimeID) {
		writeAgentUnavailable(w, "agent's runtime is offline")
		return
	}

	// Daemon CLI version gate. The agent-side prompt + create-flow rely on
	// behaviors introduced in MinQuickCreateCLIVersion (URL attachment
	// handling, no-retry on partial failure). Older daemons either
	// double-create issues on partial CLI failures or mishandle pasted
	// screenshot URLs; fail closed before enqueuing rather than surface
	// the breakage as an inbox failure twenty seconds later.
	if status, payload := h.checkQuickCreateDaemonVersion(r.Context(), agent.RuntimeID); status != 0 {
		writeJSON(w, status, payload)
		return
	}

	task, err := h.TaskService.EnqueueQuickCreateTask(r.Context(), wsUUID, requesterUUID, agentUUID, prompt)
	if err != nil {
		slog.Warn("quick-create enqueue failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to enqueue quick-create task")
		return
	}

	writeJSON(w, http.StatusAccepted, QuickCreateIssueResponse{TaskID: uuidToString(task.ID)})
}

// writeAgentUnavailable returns 422 with a stable error code so the modal
// can show a "switch agent" hint without parsing the human-readable reason.
func writeAgentUnavailable(w http.ResponseWriter, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	json.NewEncoder(w).Encode(map[string]any{
		"code":   "agent_unavailable",
		"reason": reason,
	})
}

// isRuntimeOnline returns true when the given runtime is currently
// reachable (status == "online"). Quick-create rejects submissions whose
// agent's runtime is offline so the user gets immediate feedback in the
// modal instead of an inbox failure twenty seconds later.
func (h *Handler) isRuntimeOnline(ctx context.Context, runtimeID pgtype.UUID) bool {
	rt, err := h.Queries.GetAgentRuntime(ctx, runtimeID)
	if err != nil {
		return false
	}
	return rt.Status == "online"
}

// checkQuickCreateDaemonVersion enforces MinQuickCreateCLIVersion against the
// CLI version the daemon reported at registration time (stored on the runtime
// row's metadata.cli_version). Returns (0, nil) when the version is
// acceptable, otherwise (status, payload) ready to hand to writeJSON.
//
// Failure shape is stable so the modal can branch on the `code` field and
// surface a "needs upgrade" hint that points at the specific runtime:
//
//	422 {
//	  "code": "daemon_version_unsupported",
//	  "current_version": "0.2.18" | "",
//	  "min_version":     "0.2.20",
//	  "runtime_id":      "<uuid>"
//	}
func (h *Handler) checkQuickCreateDaemonVersion(ctx context.Context, runtimeID pgtype.UUID) (int, map[string]any) {
	rt, err := h.Queries.GetAgentRuntime(ctx, runtimeID)
	if err != nil {
		// Runtime row vanished between the online check and here — treat
		// as unavailable rather than wedging the request on a 500.
		return http.StatusUnprocessableEntity, map[string]any{
			"code":   "agent_unavailable",
			"reason": "agent's runtime is no longer registered",
		}
	}
	current := readRuntimeCLIVersion(rt.Metadata)
	switch err := agent.CheckMinCLIVersion(current); {
	case err == nil:
		return 0, nil
	case errors.Is(err, agent.ErrCLIVersionMissing), errors.Is(err, agent.ErrCLIVersionTooOld):
		return http.StatusUnprocessableEntity, map[string]any{
			"code":            "daemon_version_unsupported",
			"current_version": current,
			"min_version":     agent.MinQuickCreateCLIVersion,
			"runtime_id":      uuidToString(runtimeID),
		}
	default:
		// Defensive fall-through: unknown error from the version check is
		// also fail-closed, since the gate exists precisely because we
		// can't trust older daemons with this flow.
		return http.StatusUnprocessableEntity, map[string]any{
			"code":            "daemon_version_unsupported",
			"current_version": current,
			"min_version":     agent.MinQuickCreateCLIVersion,
			"runtime_id":      uuidToString(runtimeID),
		}
	}
}

// readRuntimeCLIVersion pulls metadata.cli_version off a runtime row. The
// metadata column is JSONB on the wire; the daemon stores the multica CLI
// version under that key during registration (see DaemonRegister).
func readRuntimeCLIVersion(metadata []byte) string {
	if len(metadata) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(metadata, &m); err != nil {
		return ""
	}
	if v, ok := m["cli_version"].(string); ok {
		return v
	}
	return ""
}

type CreateIssueRequest struct {
	Title         string   `json:"title"`
	Description   *string  `json:"description"`
	Status        string   `json:"status"`
	Priority      string   `json:"priority"`
	AssigneeType  *string  `json:"assignee_type"`
	AssigneeID    *string  `json:"assignee_id"`
	ParentIssueID *string  `json:"parent_issue_id"`
	ProjectID     *string  `json:"project_id"`
	DueDate       *string  `json:"due_date"`
	AttachmentIDs []string `json:"attachment_ids,omitempty"`
	// OriginType / OriginID stamp the new issue with its provenance so
	// platform-internal flows can deterministically locate it later. Only
	// trusted callers should set these — currently the daemon CLI passes
	// them through for quick-create tasks (origin_type=quick_create,
	// origin_id=agent_task_queue.id).
	OriginType *string `json:"origin_type,omitempty"`
	OriginID   *string `json:"origin_id,omitempty"`
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
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	// Get creator from context (set by auth middleware)
	creatorID, ok := requireUserID(w, r)
	if !ok {
		return
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
		id, ok := parseUUIDOrBadRequest(w, *req.AssigneeID, "assignee_id")
		if !ok {
			return
		}
		assigneeID = id
	}

	if status, msg := h.validateAssigneePair(r.Context(), r, workspaceID, assigneeType, assigneeID); status != 0 {
		writeError(w, status, msg)
		return
	}

	var parentIssueID pgtype.UUID
	var projectID pgtype.UUID
	if req.ProjectID != nil {
		id, ok := parseUUIDOrBadRequest(w, *req.ProjectID, "project_id")
		if !ok {
			return
		}
		projectID = id
	}
	if req.ParentIssueID != nil {
		id, ok := parseUUIDOrBadRequest(w, *req.ParentIssueID, "parent_issue_id")
		if !ok {
			return
		}
		parentIssueID = id
		// Validate parent exists in the same workspace.
		parent, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          parentIssueID,
			WorkspaceID: wsUUID,
		})
		if err != nil || !parent.ID.Valid {
			writeError(w, http.StatusBadRequest, "parent issue not found in this workspace")
			return
		}
		if req.ProjectID == nil {
			projectID = parent.ProjectID
		}
	}

	status, err := h.resolveIssueStatusKey(r.Context(), wsUUID, projectID, req.Status)
	if err != nil {
		statusCode, msg := issueWorkflowErrorResponse(err)
		writeError(w, statusCode, msg)
		return
	}

	attachmentIDs, ok := parseUUIDSliceOrBadRequest(w, req.AttachmentIDs, "attachment_ids")
	if !ok {
		return
	}

	var dueDate pgtype.Timestamptz
	if req.DueDate != nil && *req.DueDate != "" {
		t, err := time.Parse(time.RFC3339, *req.DueDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid due_date format, expected RFC3339")
			return
		}
		dueDate = pgtype.Timestamptz{Time: t, Valid: true}
	}

	// Use a transaction to atomically increment the workspace issue counter
	// and create the issue with the assigned number.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create issue")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)
	issueNumber, err := qtx.IncrementIssueCounter(r.Context(), wsUUID)
	if err != nil {
		slog.Warn("increment issue counter failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to create issue")
		return
	}

	// Determine creator identity: agent (via X-Agent-ID header) or member.
	creatorType, actualCreatorID := h.resolveActor(r, creatorID, workspaceID)

	// Optional origin stamping (quick-create / autopilot). Only the
	// allowed origin types are accepted; anything else is rejected so a
	// rogue caller can't mint arbitrary origin labels. Both fields must
	// be provided together.
	var originType pgtype.Text
	var originID pgtype.UUID
	if req.OriginType != nil || req.OriginID != nil {
		if req.OriginType == nil || req.OriginID == nil {
			writeError(w, http.StatusBadRequest, "origin_type and origin_id must be provided together")
			return
		}
		switch *req.OriginType {
		case "quick_create":
			// Allowed — daemon CLI passes this through from a quick-create task.
		default:
			writeError(w, http.StatusBadRequest, "unsupported origin_type")
			return
		}
		oid, ok := parseUUIDOrBadRequest(w, *req.OriginID, "origin_id")
		if !ok {
			return
		}
		originType = pgtype.Text{String: *req.OriginType, Valid: true}
		originID = oid
	}

	var issue db.Issue
	if originType.Valid {
		issue, err = qtx.CreateIssueWithOrigin(r.Context(), db.CreateIssueWithOriginParams{
			WorkspaceID:   wsUUID,
			Title:         req.Title,
			Description:   ptrToText(req.Description),
			Status:        status,
			Priority:      priority,
			AssigneeType:  assigneeType,
			AssigneeID:    assigneeID,
			CreatorType:   creatorType,
			CreatorID:     parseUUID(actualCreatorID),
			ParentIssueID: parentIssueID,
			Position:      0,
			DueDate:       dueDate,
			Number:        issueNumber,
			ProjectID:     projectID,
			OriginType:    originType,
			OriginID:      originID,
		})
	} else {
		issue, err = qtx.CreateIssue(r.Context(), db.CreateIssueParams{
			WorkspaceID:   wsUUID,
			Title:         req.Title,
			Description:   ptrToText(req.Description),
			Status:        status,
			Priority:      priority,
			AssigneeType:  assigneeType,
			AssigneeID:    assigneeID,
			CreatorType:   creatorType,
			CreatorID:     parseUUID(actualCreatorID),
			ParentIssueID: parentIssueID,
			Position:      0,
			DueDate:       dueDate,
			Number:        issueNumber,
			ProjectID:     projectID,
		})
	}
	if err != nil {
		slog.Warn("create issue failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to create issue: "+err.Error())
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create issue")
		return
	}

	// Link any pre-uploaded attachments to this issue.
	if len(attachmentIDs) > 0 {
		h.linkAttachmentsByIssueIDs(r.Context(), issue.ID, issue.WorkspaceID, attachmentIDs)
	}

	prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
	resp := issueToResponse(issue, prefix)

	// Fetch linked attachments so they appear in the response.
	if len(attachmentIDs) > 0 {
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
	}

	slog.Info("issue created", append(logger.RequestAttrs(r), "issue_id", uuidToString(issue.ID), "title", issue.Title, "status", issue.Status, "workspace_id", workspaceID)...)
	h.publish(protocol.EventIssueCreated, workspaceID, creatorType, actualCreatorID, map[string]any{"issue": resp})

	// Enqueue agent task when an agent-assigned issue is created.
	if issue.AssigneeType.Valid && issue.AssigneeID.Valid {
		if h.shouldEnqueueAgentTask(r.Context(), issue) {
			h.TaskService.EnqueueTaskForIssue(r.Context(), issue)
		}
	}

	writeJSON(w, http.StatusCreated, resp)
}

type UpdateIssueRequest struct {
	Title         *string  `json:"title"`
	Description   *string  `json:"description"`
	Status        *string  `json:"status"`
	Priority      *string  `json:"priority"`
	AssigneeType  *string  `json:"assignee_type"`
	AssigneeID    *string  `json:"assignee_id"`
	Position      *float64 `json:"position"`
	DueDate       *string  `json:"due_date"`
	ParentIssueID *string  `json:"parent_issue_id"`
	ProjectID     *string  `json:"project_id"`
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
		status, err := h.resolveIssueStatusKey(r.Context(), prevIssue.WorkspaceID, params.ProjectID, *req.Status)
		if err != nil {
			statusCode, msg := issueWorkflowErrorResponse(err)
			writeError(w, statusCode, msg)
			return
		}
		params.Status = pgtype.Text{String: status, Valid: true}
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
			id, ok := parseUUIDOrBadRequest(w, *req.AssigneeID, "assignee_id")
			if !ok {
				return
			}
			params.AssigneeID = id
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
			newParentID, ok := parseUUIDOrBadRequest(w, *req.ParentIssueID, "parent_issue_id")
			if !ok {
				return
			}
			// Cannot set self as parent. Compare against prevIssue.ID (the
			// resolved entity), not the raw URL string — `id` may be an
			// identifier like "MUL-7".
			if newParentID == prevIssue.ID {
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
				if ancestor.ParentIssueID == prevIssue.ID {
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
			projectUUID, ok := parseUUIDOrBadRequest(w, *req.ProjectID, "project_id")
			if !ok {
				return
			}
			params.ProjectID = projectUUID
		} else {
			params.ProjectID = pgtype.UUID{Valid: false}
		}
	}

	// Validate the resulting (assignee_type, assignee_id) pair when the caller
	// touches either field. Existing data on the issue is left alone if the
	// caller is not changing it.
	_, touchedType := rawFields["assignee_type"]
	_, touchedID := rawFields["assignee_id"]
	if touchedType || touchedID {
		if status, msg := h.validateAssigneePair(r.Context(), r, workspaceID, params.AssigneeType, params.AssigneeID); status != 0 {
			writeError(w, status, msg)
			return
		}
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

// validateAssigneePair verifies the (assignee_type, assignee_id) pair refers
// to an existing entity in the workspace. For agent assignees it also enforces
// visibility (private agents are only assignable by their owner or by
// workspace admins/owners) and rejects archived agents.
//
// Returns (statusCode, errorMessage). statusCode == 0 means the pair is valid;
// callers should treat any non-zero status as a rejection and surface it back
// to the client.
func (h *Handler) validateAssigneePair(ctx context.Context, r *http.Request, workspaceID string, assigneeType pgtype.Text, assigneeID pgtype.UUID) (int, string) {
	// Both unset → unassigned issue, valid.
	if !assigneeType.Valid && !assigneeID.Valid {
		return 0, ""
	}
	// Exactly one of type/id provided → callers must always pair them.
	if assigneeType.Valid != assigneeID.Valid {
		return http.StatusBadRequest, "assignee_type and assignee_id must be provided together"
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return http.StatusBadRequest, "invalid workspace_id"
	}
	switch assigneeType.String {
	case "member":
		if _, err := h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
			UserID:      assigneeID,
			WorkspaceID: wsUUID,
		}); err != nil {
			return http.StatusBadRequest, "assignee_id does not refer to a member of this workspace"
		}
		return 0, ""
	case "agent":
		agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
			ID:          assigneeID,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			return http.StatusBadRequest, "assignee_id does not refer to an agent of this workspace"
		}
		if agent.ArchivedAt.Valid {
			return http.StatusBadRequest, "cannot assign to archived agent"
		}
		if agent.Visibility == "private" {
			userID := requestUserID(r)
			if uuidToString(agent.OwnerID) != userID {
				member, err := h.getWorkspaceMember(ctx, userID, workspaceID)
				if err != nil || !roleAllowed(member.Role, "owner", "admin") {
					return http.StatusForbidden, "cannot assign to private agent"
				}
			}
		}
		return 0, ""
	default:
		return http.StatusBadRequest, "assignee_type must be 'member' or 'agent'"
	}
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

func (h *Handler) DeleteIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}

	h.TaskService.CancelTasksForIssue(r.Context(), issue.ID)
	// Fail any linked autopilot runs before delete (ON DELETE SET NULL clears issue_id).
	h.Queries.FailAutopilotRunsByIssue(r.Context(), issue.ID)

	// Collect all attachment URLs (issue-level + comment-level) before CASCADE delete.
	attachmentURLs, _ := h.Queries.ListAttachmentURLsByIssueOrComments(r.Context(), issue.ID)

	err := h.Queries.DeleteIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete issue")
		return
	}

	h.deleteS3Objects(r.Context(), attachmentURLs)
	userID := requestUserID(r)
	actorType, actorID := h.resolveActor(r, userID, uuidToString(issue.WorkspaceID))
	// Always emit the resolved UUID — frontend caches key by UUID, so an
	// identifier-style payload ("MUL-123") would leave stale entries on
	// other clients after an identifier-path delete.
	resolvedID := uuidToString(issue.ID)
	h.publish(protocol.EventIssueDeleted, uuidToString(issue.WorkspaceID), actorType, actorID, map[string]any{"issue_id": resolvedID})
	slog.Info("issue deleted", append(logger.RequestAttrs(r), "issue_id", resolvedID, "workspace_id", uuidToString(issue.WorkspaceID))...)
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

	// Detect which fields in "updates" were explicitly set (including null).
	var rawTop map[string]json.RawMessage
	json.Unmarshal(bodyBytes, &rawTop)
	var rawUpdates map[string]json.RawMessage
	if raw, exists := rawTop["updates"]; exists {
		json.Unmarshal(raw, &rawUpdates)
	}

	// Short-circuit when no mutation field is present in `updates`. Without
	// this, the loop below runs N no-op UPDATEs (every if-guard skips, every
	// COALESCE preserves the existing value) and reports `{"updated": N}` —
	// the response cheerfully claims success while nothing changed. Most
	// real-world cases that hit this path are caller mistakes (status placed
	// at the top level, "update" misspelled as singular). Telling the truth
	// here — `{"updated": 0}` — keeps the wire shape stable while making the
	// count match reality. See multica-ai/multica#1660.
	hasMutation := req.Updates.Title != nil ||
		req.Updates.Description != nil ||
		req.Updates.Status != nil ||
		req.Updates.Priority != nil ||
		req.Updates.Position != nil
	if !hasMutation {
		for _, k := range []string{"assignee_type", "assignee_id", "due_date", "parent_issue_id", "project_id"} {
			if _, ok := rawUpdates[k]; ok {
				hasMutation = true
				break
			}
		}
	}
	if !hasMutation {
		writeJSON(w, http.StatusOK, map[string]any{"updated": 0})
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	updated := 0
	for _, issueID := range req.IssueIDs {
		issueUUID, err := util.ParseUUID(issueID)
		if err != nil {
			continue
		}
		prevIssue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          issueUUID,
			WorkspaceID: wsUUID,
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
			status, err := h.resolveIssueStatusKey(r.Context(), prevIssue.WorkspaceID, params.ProjectID, *req.Updates.Status)
			if err != nil {
				continue
			}
			params.Status = pgtype.Text{String: status, Valid: true}
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
				assigneeUUID, err := util.ParseUUID(*req.Updates.AssigneeID)
				if err != nil {
					continue
				}
				params.AssigneeID = assigneeUUID
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
				newParentID, err := util.ParseUUID(*req.Updates.ParentIssueID)
				if err != nil {
					continue
				}
				// Cannot set self as parent.
				if newParentID == prevIssue.ID {
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
					if ancestor.ParentIssueID == prevIssue.ID {
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
				projectUUID, err := util.ParseUUID(*req.Updates.ProjectID)
				if err != nil {
					continue
				}
				params.ProjectID = projectUUID
			} else {
				params.ProjectID = pgtype.UUID{Valid: false}
			}
		}

		// Validate the resulting assignee pair when this batch update touches
		// either assignee field. Skip the issue silently on failure.
		_, batchTouchedType := rawUpdates["assignee_type"]
		_, batchTouchedID := rawUpdates["assignee_id"]
		if batchTouchedType || batchTouchedID {
			if status, _ := h.validateAssigneePair(r.Context(), r, workspaceID, params.AssigneeType, params.AssigneeID); status != 0 {
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
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	deleted := 0
	for _, issueID := range req.IssueIDs {
		issueUUID, err := util.ParseUUID(issueID)
		if err != nil {
			continue
		}
		issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          issueUUID,
			WorkspaceID: wsUUID,
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

		// Always emit the resolved UUID — frontend caches key by UUID.
		actorType, actorID := h.resolveActor(r, userID, workspaceID)
		h.publish(protocol.EventIssueDeleted, workspaceID, actorType, actorID, map[string]any{"issue_id": uuidToString(issue.ID)})
		deleted++
	}

	slog.Info("batch delete issues", append(logger.RequestAttrs(r), "count", deleted)...)
	writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}
