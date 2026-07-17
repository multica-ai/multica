package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issueguard"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	agentpkg "github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// IssueResponse is the JSON response for an issue.
type IssueResponse struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	Number        int32   `json:"number"`
	Identifier    string  `json:"identifier"`
	Title         string  `json:"title"`
	Description   *string `json:"description"`
	Status        string  `json:"status"`
	Priority      string  `json:"priority"`
	AssigneeType  *string `json:"assignee_type"`
	AssigneeID    *string `json:"assignee_id"`
	CreatorType   string  `json:"creator_type"`
	CreatorID     string  `json:"creator_id"`
	ParentIssueID *string `json:"parent_issue_id"`
	ProjectID     *string `json:"project_id"`
	Position      float64 `json:"position"`
	// Stage groups sub-issues under the same parent into ordered barrier
	// groups (null = unstaged). See issue_child_done.go for how a closed
	// stage gates the child-done -> parent wake.
	Stage     *int32  `json:"stage"`
	StartDate *string `json:"start_date"`
	DueDate   *string `json:"due_date"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
	// Metadata is the per-issue KV map (see issue_metadata.go). Always emitted
	// (empty object when unset) so frontend code can `issue.metadata[key]`
	// without nil-guarding the parent field.
	Metadata map[string]any `json:"metadata"`
	// Properties is the custom-property value bag keyed by property definition
	// UUID (see property.go). Always emitted, mirroring Metadata.
	Properties  map[string]any          `json:"properties"`
	Reactions   []IssueReactionResponse `json:"reactions,omitempty"`
	Attachments []AttachmentResponse    `json:"attachments,omitempty"`
	// Labels are bulk-attached by list/detail endpoints so the client can render
	// chips without an N+1 round-trip per row. Pointer + omitempty so paths that
	// don't load labels (e.g. UpdateIssue, batch UpdateIssues, the issue:updated
	// WS broadcast) emit no `labels` field at all — the client merge then
	// preserves whatever labels are already in cache. nil pointer = "field
	// absent, do not touch"; non-nil (incl. empty slice) = authoritative list.
	Labels *[]LabelResponse `json:"labels,omitempty"`
}

// validIssueStatuses / validIssuePriorities mirror the CHECK constraints on
// the issue table. Write handlers pre-validate these so callers get a clean
// 400 with the allowed values instead of a database CHECK violation bubbling
// up as a 500.
var validIssueStatuses = []string{"backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"}
var validIssuePriorities = []string{"urgent", "high", "medium", "low", "none"}

func validateIssueEnum(w http.ResponseWriter, field, value string, allowed []string) bool {
	for _, a := range allowed {
		if value == a {
			return true
		}
	}
	writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid %s %q; valid values: %s", field, value, strings.Join(allowed, ", ")))
	return false
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
		Stage:         int4ToPtr(i.Stage),
		StartDate:     dateToPtr(i.StartDate),
		DueDate:       dateToPtr(i.DueDate),
		CreatedAt:     timestampToString(i.CreatedAt),
		UpdatedAt:     timestampToString(i.UpdatedAt),
		Metadata:      parseIssueMetadata(i.Metadata),
		Properties:    parseIssueProperties(i.Properties),
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
		Stage:         int4ToPtr(i.Stage),
		StartDate:     dateToPtr(i.StartDate),
		DueDate:       dateToPtr(i.DueDate),
		CreatedAt:     timestampToString(i.CreatedAt),
		UpdatedAt:     timestampToString(i.UpdatedAt),
		Metadata:      parseIssueMetadata(i.Metadata),
		Properties:    parseIssueProperties(i.Properties),
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
			ID:           uuidToString(r.ID),
			WorkspaceID:  uuidToString(r.WorkspaceID),
			ResourceType: r.ResourceType,
			Name:         r.Name,
			Description:  r.Description,
			Color:        r.Color,
			CreatedAt:    timestampToString(r.CreatedAt),
			UpdatedAt:    timestampToString(r.UpdatedAt),
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
		Stage:         int4ToPtr(i.Stage),
		StartDate:     dateToPtr(i.StartDate),
		DueDate:       dateToPtr(i.DueDate),
		CreatedAt:     timestampToString(i.CreatedAt),
		UpdatedAt:     timestampToString(i.UpdatedAt),
		Metadata:      parseIssueMetadata(i.Metadata),
		Properties:    parseIssueProperties(i.Properties),
	}
}

type IssueAssigneeGroupResponse struct {
	ID           string          `json:"id"`
	AssigneeType *string         `json:"assignee_type"`
	AssigneeID   *string         `json:"assignee_id"`
	Issues       []IssueResponse `json:"issues"`
	Total        int64           `json:"total"`
}

type GroupedIssuesResponse struct {
	Groups []IssueAssigneeGroupResponse `json:"groups"`
}

type groupedIssueRow struct {
	db.ListIssuesRow
	GroupTotal int64
}

func assigneeGroupID(assigneeType pgtype.Text, assigneeID pgtype.UUID) string {
	if assigneeType.Valid && assigneeID.Valid {
		return "assignee:" + assigneeType.String + ":" + uuidToString(assigneeID)
	}
	return "assignee:unassigned"
}

// SearchIssueResponse extends IssueResponse with search metadata.
type SearchIssueResponse struct {
	IssueResponse
	MatchSource               string  `json:"match_source"`
	MatchedSnippet            *string `json:"matched_snippet,omitempty"`
	MatchedDescriptionSnippet *string `json:"matched_description_snippet,omitempty"`
	MatchedCommentSnippet     *string `json:"matched_comment_snippet,omitempty"`
}

// extractSnippet extracts a snippet of text around the first occurrence of query.
// Returns up to ~120 runes centered on the match. Uses rune-based slicing to
// avoid splitting multi-byte UTF-8 characters (important for CJK content).
// For multi-word queries, tries phrase match first; if not found, locates the
// earliest occurring individual term and centers the snippet around it.
func extractSnippet(content, query string) string {
	runes := []rune(content)
	lowerRunes := []rune(strings.ToLower(content))
	queryRunes := []rune(strings.ToLower(query))

	idx := findRuneSubstring(lowerRunes, queryRunes)

	// If phrase not found, try individual terms for multi-word queries.
	matchLen := len(queryRunes)
	if idx < 0 {
		terms := strings.Fields(strings.ToLower(query))
		if len(terms) > 1 {
			earliest := -1
			earliestLen := 0
			for _, term := range terms {
				termRunes := []rune(term)
				pos := findRuneSubstring(lowerRunes, termRunes)
				if pos >= 0 && (earliest < 0 || pos < earliest) {
					earliest = pos
					earliestLen = len(termRunes)
				}
			}
			if earliest >= 0 {
				idx = earliest
				matchLen = earliestLen
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
	end := idx + matchLen + 80
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

// findRuneSubstring returns the index of needle in haystack, or -1 if not found.
func findRuneSubstring(haystack, needle []rune) int {
	if len(needle) == 0 || len(haystack) < len(needle) {
		return -1
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// descriptionContains checks if the description text contains the search phrase or all terms.
func descriptionContains(desc pgtype.Text, phrase string, terms []string) bool {
	if !desc.Valid || desc.String == "" {
		return false
	}
	lower := strings.ToLower(desc.String)
	if strings.Contains(lower, strings.ToLower(phrase)) {
		return true
	}
	if len(terms) > 1 {
		for _, t := range terms {
			if !strings.Contains(lower, strings.ToLower(t)) {
				return false
			}
		}
		return true
	}
	return false
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
// LIKE patterns are pre-built in Go (e.g. "%html%") so pg_bigm can extract bigrams from a single parameter value.
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
		i.start_date, i.due_date, i.created_at, i.updated_at, i.number, i.project_id,
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
	// Fill placeholder args: $4 = workspace_id, last two = limit, offset
	args[3] = wsUUID
	args[len(args)-2] = limit
	args[len(args)-1] = offset

	var results []searchResult
	err := runSearchQuery(ctx, h.TxStarter, sqlQuery, args, func(rows pgx.Rows) error {
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
				&sr.issue.Number,
				&sr.issue.ProjectID,
				&sr.totalCount,
				&sr.matchSource,
				&sr.matchedCommentContent,
			); err != nil {
				return fmt.Errorf("scan: %w", err)
			}
			results = append(results, sr)
		}
		return rows.Err()
	})
	if err != nil {
		// Statement-timeout surfaces as SQLSTATE 57014. Return a 503
		// so the frontend can distinguish a timeout ("try a more
		// specific query") from a generic 500. This is the fail-fast
		// path when GIN search indexes are absent or the database is
		// overloaded; see runSearchQuery header for context.
		if isSearchStatementTimeout(err) {
			slog.Warn("search issues timed out",
				"workspace_id", workspaceID,
				"query", q,
				"timeout", searchStatementTimeout)
			writeError(w, http.StatusServiceUnavailable, "search timed out; please refine your query or try again")
			return
		}
		slog.Warn("search issues failed", "error", err, "workspace_id", workspaceID, "query", q)
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
		// Always populate comment snippet when a matching comment exists
		if sr.matchedCommentContent != "" {
			snippet := extractSnippet(sr.matchedCommentContent, q)
			sir.MatchedCommentSnippet = &snippet
			// Keep backward compat: also set MatchedSnippet for comment-source matches
			if sr.matchSource == "comment" {
				sir.MatchedSnippet = &snippet
			}
		}
		// Populate description snippet when description matches
		if sr.matchSource == "description" || descriptionContains(sr.issue.Description, q, terms) {
			if sr.issue.Description.Valid && sr.issue.Description.String != "" {
				snippet := extractSnippet(sr.issue.Description.String, q)
				sir.MatchedDescriptionSnippet = &snippet
			}
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
	// involves_user_id widens the assignee filter to surface issues where the
	// user is the indirect assignee (their owned agent, or a squad they belong
	// to / lead / have an agent inside). Direct member-assignment is excluded
	// by design — that is the meaning of `assignee_id` (tab 1), and tab 3 must
	// be disjoint from tab 1.
	var involvesUserFilter pgtype.UUID
	if u := r.URL.Query().Get("involves_user_id"); u != "" {
		id, ok := parseUUIDOrBadRequest(w, u, "involves_user_id")
		if !ok {
			return
		}
		involvesUserFilter = id
	}

	metadataFilter, ok := parseMetadataFilterParam(w, r.URL.Query().Get("metadata"))
	if !ok {
		return
	}
	propertiesFilter, ok := parsePropertiesFilterParam(w, r.URL.Query().Get("properties"))
	if !ok {
		return
	}
	dateFilter, ok := parseIssueDateFilter(w, r.URL.Query())
	if !ok {
		return
	}

	// open_only=true returns all non-done/cancelled issues (no limit).
	if r.URL.Query().Get("open_only") == "true" {
		// Serialize the parsed AND-of-ORs groups into the single jsonb param
		// the static query unrolls (see properties_filter in ListOpenIssues).
		var openPropertiesFilter []byte
		if len(propertiesFilter) > 0 {
			marshaled, marshalErr := json.Marshal(propertiesFilter)
			if marshalErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to list issues")
				return
			}
			openPropertiesFilter = marshaled
		}
		issues, err := h.Queries.ListOpenIssues(ctx, db.ListOpenIssuesParams{
			WorkspaceID:      wsUUID,
			Priority:         priorityFilter,
			AssigneeID:       assigneeFilter,
			AssigneeIds:      assigneeIdsFilter,
			CreatorID:        creatorFilter,
			ProjectID:        projectFilter,
			InvolvesUserID:   involvesUserFilter,
			MetadataFilter:   metadataFilter,
			PropertiesFilter: openPropertiesFilter,
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
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > 100 {
		limit = 100
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}

	var statusFilter pgtype.Text
	if s := r.URL.Query().Get("status"); s != "" {
		statusFilter = pgtype.Text{String: s, Valid: true}
	}

	// assignee_types narrows the list to issues assigned to the given actor
	// kinds (member / agent / squad). Mirrors the same param on
	// ListGroupedIssues so the workspace Members/Agents tabs can filter
	// server-side instead of post-filtering loaded pages on the client.
	assigneeTypesFilter := splitCommaParam(r.URL.Query().Get("assignee_types"))
	for _, assigneeType := range assigneeTypesFilter {
		if !isIssueActorType(assigneeType) {
			writeError(w, http.StatusBadRequest, "invalid assignee_types")
			return
		}
	}

	// scheduled=true restricts the result to issues that have at least one of
	// start_date / due_date set. Used by the Project Gantt view, which only
	// renders schedulable rows and shouldn't pay for the full project list.
	var scheduledFilter pgtype.Bool
	if r.URL.Query().Get("scheduled") == "true" {
		scheduledFilter = pgtype.Bool{Bool: true, Valid: true}
	}

	// Parse sort and direction params for dynamic ORDER BY.
	// Manual sort (position) is always ASC — direction is ignored because
	// the user defines order through drag-and-drop, reversing it has no
	// product meaning.
	sortCol := "position"
	sortIsExpr := false
	sortIsProperty := false
	if s := r.URL.Query().Get("sort"); s != "" {
		switch s {
		case "position", "title", "created_at", "start_date", "due_date":
			sortCol = s
		case "priority":
			sortCol = "CASE i.priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 WHEN 'low' THEN 3 ELSE 4 END"
			sortIsExpr = true
		default:
			// property:<definitionId> sorts by the custom-property value
			// (typed expression); unknown/archived definitions degrade to
			// position order instead of erroring stale clients.
			expr, handled, sortErr := h.propertySortExpr(r, workspaceID, s)
			if !handled {
				writeError(w, http.StatusBadRequest, "invalid sort value")
				return
			}
			if sortErr != nil {
				if sortErr.Error() == "invalid sort value" || sortErr.Error() == "invalid workspace id" {
					writeError(w, http.StatusBadRequest, sortErr.Error())
					return
				}
				slog.Warn("propertySortExpr failed", append(logger.RequestAttrs(r), "error", sortErr)...)
				writeError(w, http.StatusInternalServerError, "failed to resolve sort")
				return
			}
			if expr != "" {
				sortCol = expr
				sortIsExpr = true
				sortIsProperty = true
			}
		}
	}
	sortDir := "ASC"
	if sortCol != "position" {
		if d := r.URL.Query().Get("direction"); d != "" {
			switch strings.ToLower(d) {
			case "asc":
				sortDir = "ASC"
			case "desc":
				sortDir = "DESC"
			default:
				writeError(w, http.StatusBadRequest, "invalid direction value")
				return
			}
		}
	}

	// Build dynamic SQL — same approach as ListGroupedIssues.
	where := []string{"i.workspace_id = $1"}
	args := []any{wsUUID}
	addArg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}

	if statusFilter.Valid {
		where = append(where, fmt.Sprintf("i.status = %s", addArg(statusFilter.String)))
	}
	if priorityFilter.Valid {
		where = append(where, fmt.Sprintf("i.priority = %s", addArg(priorityFilter.String)))
	}
	if assigneeFilter.Valid {
		where = append(where, fmt.Sprintf("i.assignee_id = %s::uuid", addArg(assigneeFilter)))
	}
	if len(assigneeIdsFilter) > 0 {
		where = append(where, fmt.Sprintf("i.assignee_id = ANY(%s::uuid[])", addArg(assigneeIdsFilter)))
	}
	if len(assigneeTypesFilter) > 0 {
		where = append(where, fmt.Sprintf("i.assignee_type = ANY(%s::text[])", addArg(assigneeTypesFilter)))
	}
	if creatorFilter.Valid {
		where = append(where, fmt.Sprintf("i.creator_id = %s::uuid", addArg(creatorFilter)))
	}
	if projectFilter.Valid {
		where = append(where, fmt.Sprintf("i.project_id = %s::uuid", addArg(projectFilter)))
	}
	if scheduledFilter.Valid {
		where = append(where, "(i.start_date IS NOT NULL OR i.due_date IS NOT NULL)")
	}
	if metadataFilter != nil {
		where = append(where, fmt.Sprintf("i.metadata @> %s::jsonb", addArg(string(metadataFilter))))
	}
	if propertiesFilter != nil {
		where = append(where, propertiesFilterPredicate(propertiesFilter, addArg))
	}
	where = appendIssueDateFilter(where, addArg, dateFilter)
	if involvesUserFilter.Valid {
		ref := addArg(involvesUserFilter)
		where = append(where, fmt.Sprintf(`(
    (i.assignee_type = 'agent' AND i.assignee_id IN (
       SELECT a.id FROM agent a
        WHERE a.workspace_id = $1
          AND a.owner_id     = %[1]s::uuid
    ))
    OR (i.assignee_type = 'squad' AND i.assignee_id IN (
       SELECT sm.squad_id
         FROM squad_member sm
         JOIN squad s ON s.id = sm.squad_id
        WHERE s.workspace_id = $1
          AND sm.member_type = 'member'
          AND sm.member_id   = %[1]s::uuid
       UNION
       SELECT s.id
         FROM squad s
         JOIN agent a ON a.id = s.leader_id
        WHERE s.workspace_id = $1
          AND a.workspace_id = $1
          AND a.owner_id     = %[1]s::uuid
       UNION
       SELECT sm.squad_id
         FROM squad_member sm
         JOIN squad s ON s.id = sm.squad_id
         JOIN agent a ON a.id = sm.member_id
        WHERE s.workspace_id = $1
          AND sm.member_type = 'agent'
          AND a.workspace_id = $1
          AND a.owner_id     = %[1]s::uuid
    ))
)`, ref))
	}

	whereSql := strings.Join(where, " AND ")

	// Build ORDER BY clause.
	orderBy := sortCol
	if !sortIsExpr {
		orderBy = "i." + sortCol
	}
	orderBy += " " + sortDir
	if sortCol == "start_date" || sortCol == "due_date" || sortIsProperty {
		// Property values are sparse: issues without one sort last in both
		// directions (mirrors the client comparator).
		orderBy += " NULLS LAST"
	}
	orderBy += ", i.created_at DESC"

	offsetRef := addArg(int64(offset))
	limitRef := addArg(int64(limit))

	query := fmt.Sprintf(`SELECT i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
       i.assignee_type, i.assignee_id, i.creator_type, i.creator_id,
       i.parent_issue_id, i.position, i.start_date, i.due_date, i.created_at, i.updated_at, i.number, i.project_id, i.metadata, i.stage, i.properties
FROM issue i
WHERE %s
ORDER BY %s
LIMIT %s OFFSET %s`, whereSql, orderBy, limitRef, offsetRef)

	rows, err := h.DB.Query(ctx, query, args...)
	if err != nil {
		slog.Warn("ListIssues query failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list issues")
		return
	}
	defer rows.Close()

	var issues []db.ListIssuesRow
	for rows.Next() {
		var row db.ListIssuesRow
		if err := rows.Scan(
			&row.ID,
			&row.WorkspaceID,
			&row.Title,
			&row.Description,
			&row.Status,
			&row.Priority,
			&row.AssigneeType,
			&row.AssigneeID,
			&row.CreatorType,
			&row.CreatorID,
			&row.ParentIssueID,
			&row.Position,
			&row.StartDate,
			&row.DueDate,
			&row.CreatedAt,
			&row.UpdatedAt,
			&row.Number,
			&row.ProjectID,
			&row.Metadata,
			&row.Stage,
			&row.Properties,
		); err != nil {
			slog.Warn("ListIssues scan failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to list issues")
			return
		}
		issues = append(issues, row)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("ListIssues rows failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list issues")
		return
	}

	// Get the true total count for pagination awareness.
	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM issue i WHERE %s`, whereSql)
	// Count query uses the same args minus the OFFSET and LIMIT params (last two added).
	countArgs := args[:len(args)-2]
	var total int64
	if err := h.DB.QueryRow(ctx, countQuery, countArgs...).Scan(&total); err != nil {
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

type issueActorFilter struct {
	actorType string
	actorID   pgtype.UUID
}

type issueDateFilter struct {
	column string
	start  time.Time
	end    time.Time
}

func parseIssueDateFilter(w http.ResponseWriter, values url.Values) (*issueDateFilter, bool) {
	field := strings.TrimSpace(values.Get("date_field"))
	startRaw := strings.TrimSpace(values.Get("date_start"))
	endRaw := strings.TrimSpace(values.Get("date_end"))
	if field == "" && startRaw == "" && endRaw == "" {
		return nil, true
	}
	if field == "" || startRaw == "" || endRaw == "" {
		writeError(w, http.StatusBadRequest, "date_field, date_start, and date_end are required together")
		return nil, false
	}

	column := ""
	switch field {
	case "created_at":
		column = "created_at"
	case "updated_at":
		column = "updated_at"
	default:
		writeError(w, http.StatusBadRequest, "invalid date_field")
		return nil, false
	}

	start, err := time.Parse(time.RFC3339Nano, startRaw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid date_start")
		return nil, false
	}
	end, err := time.Parse(time.RFC3339Nano, endRaw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid date_end")
		return nil, false
	}
	if !start.Before(end) {
		writeError(w, http.StatusBadRequest, "date_start must be before date_end")
		return nil, false
	}

	return &issueDateFilter{column: column, start: start, end: end}, true
}

func appendIssueDateFilter(where []string, addArg func(any) string, filter *issueDateFilter) []string {
	if filter == nil {
		return where
	}
	startRef := addArg(filter.start)
	endRef := addArg(filter.end)
	return append(where, fmt.Sprintf(
		"i.%s >= %s AND i.%s < %s",
		filter.column,
		startRef,
		filter.column,
		endRef,
	))
}

func splitCommaParam(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func isIssueActorType(s string) bool {
	return s == "member" || s == "agent" || s == "squad"
}

func parseUUIDParamList(w http.ResponseWriter, raw, fieldName string) ([]pgtype.UUID, bool) {
	parts := splitCommaParam(raw)
	if len(parts) == 0 {
		return nil, true
	}
	ids := make([]pgtype.UUID, 0, len(parts))
	for _, part := range parts {
		id, ok := parseUUIDOrBadRequest(w, part, fieldName)
		if !ok {
			return nil, false
		}
		ids = append(ids, id)
	}
	return ids, true
}

func parseActorFilterList(w http.ResponseWriter, raw, fieldName string) ([]issueActorFilter, bool) {
	parts := splitCommaParam(raw)
	if len(parts) == 0 {
		return nil, true
	}
	filters := make([]issueActorFilter, 0, len(parts))
	for _, part := range parts {
		pieces := strings.SplitN(part, ":", 2)
		if len(pieces) != 2 || !isIssueActorType(pieces[0]) || strings.TrimSpace(pieces[1]) == "" {
			writeError(w, http.StatusBadRequest, "invalid "+fieldName)
			return nil, false
		}
		id, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(pieces[1]), fieldName)
		if !ok {
			return nil, false
		}
		filters = append(filters, issueActorFilter{
			actorType: pieces[0],
			actorID:   id,
		})
	}
	return filters, true
}

func (h *Handler) ListGroupedIssues(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if h.DB == nil {
		writeError(w, http.StatusInternalServerError, "database is unavailable")
		return
	}

	groupBy := r.URL.Query().Get("group_by")
	if groupBy == "" {
		groupBy = "assignee"
	}
	if groupBy != "assignee" {
		writeError(w, http.StatusBadRequest, "unsupported group_by")
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	limit := 50
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > 100 {
		limit = 100
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v > 0 {
			offset = v
		}
	}

	where := []string{"i.workspace_id = $1"}
	args := []any{wsUUID}
	addArg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}

	statuses := splitCommaParam(r.URL.Query().Get("statuses"))
	if len(statuses) == 0 {
		statuses = splitCommaParam(r.URL.Query().Get("status"))
	}
	if len(statuses) > 0 {
		where = append(where, fmt.Sprintf("i.status = ANY(%s::text[])", addArg(statuses)))
	}

	priorities := splitCommaParam(r.URL.Query().Get("priorities"))
	if len(priorities) == 0 {
		priorities = splitCommaParam(r.URL.Query().Get("priority"))
	}
	if len(priorities) > 0 {
		where = append(where, fmt.Sprintf("i.priority = ANY(%s::text[])", addArg(priorities)))
	}

	assigneeTypes := splitCommaParam(r.URL.Query().Get("assignee_types"))
	if len(assigneeTypes) > 0 {
		for _, assigneeType := range assigneeTypes {
			if !isIssueActorType(assigneeType) {
				writeError(w, http.StatusBadRequest, "invalid assignee_types")
				return
			}
		}
		where = append(where, fmt.Sprintf("i.assignee_type = ANY(%s::text[])", addArg(assigneeTypes)))
	}

	if raw := r.URL.Query().Get("assignee_id"); raw != "" {
		id, ok := parseUUIDOrBadRequest(w, raw, "assignee_id")
		if !ok {
			return
		}
		where = append(where, fmt.Sprintf("i.assignee_id = %s::uuid", addArg(id)))
	}
	if raw := r.URL.Query().Get("assignee_ids"); raw != "" {
		ids, ok := parseUUIDParamList(w, raw, "assignee_ids")
		if !ok {
			return
		}
		if len(ids) > 0 {
			where = append(where, fmt.Sprintf("i.assignee_id = ANY(%s::uuid[])", addArg(ids)))
		}
	}
	if raw := r.URL.Query().Get("creator_id"); raw != "" {
		id, ok := parseUUIDOrBadRequest(w, raw, "creator_id")
		if !ok {
			return
		}
		where = append(where, fmt.Sprintf("i.creator_id = %s::uuid", addArg(id)))
	}
	if raw := r.URL.Query().Get("project_id"); raw != "" {
		id, ok := parseUUIDOrBadRequest(w, raw, "project_id")
		if !ok {
			return
		}
		where = append(where, fmt.Sprintf("i.project_id = %s::uuid", addArg(id)))
	}
	if filter, ok := parseMetadataFilterParam(w, r.URL.Query().Get("metadata")); !ok {
		return
	} else if filter != nil {
		where = append(where, fmt.Sprintf("i.metadata @> %s::jsonb", addArg(string(filter))))
	}
	if filter, ok := parsePropertiesFilterParam(w, r.URL.Query().Get("properties")); !ok {
		return
	} else if filter != nil {
		where = append(where, propertiesFilterPredicate(filter, addArg))
	}
	// Mirror the involves_user_id 4-branch UNION from sqlc's ListIssues /
	// ListOpenIssues / CountIssues. ListGroupedIssues is a hand-written dynamic
	// SQL builder that does not share parameters with sqlc, so the fragment is
	// re-implemented here in lock-step. Member-direct assignment is excluded by
	// design: that semantics belongs to tab 1 (`assignee_id`), and tab 3 must
	// stay disjoint from tab 1.
	if raw := r.URL.Query().Get("involves_user_id"); raw != "" {
		id, ok := parseUUIDOrBadRequest(w, raw, "involves_user_id")
		if !ok {
			return
		}
		ref := addArg(id)
		where = append(where, fmt.Sprintf(`(
    (i.assignee_type = 'agent' AND i.assignee_id IN (
       SELECT a.id FROM agent a
        WHERE a.workspace_id = $1
          AND a.owner_id     = %[1]s::uuid
    ))
    OR (i.assignee_type = 'squad' AND i.assignee_id IN (
       SELECT sm.squad_id
         FROM squad_member sm
         JOIN squad s ON s.id = sm.squad_id
        WHERE s.workspace_id = $1
          AND sm.member_type = 'member'
          AND sm.member_id   = %[1]s::uuid
       UNION
       SELECT s.id
         FROM squad s
         JOIN agent a ON a.id = s.leader_id
        WHERE s.workspace_id = $1
          AND a.workspace_id = $1
          AND a.owner_id     = %[1]s::uuid
       UNION
       SELECT sm.squad_id
         FROM squad_member sm
         JOIN squad s ON s.id = sm.squad_id
         JOIN agent a ON a.id = sm.member_id
        WHERE s.workspace_id = $1
          AND sm.member_type = 'agent'
          AND a.workspace_id = $1
          AND a.owner_id     = %[1]s::uuid
    ))
)`, ref))
	}

	assigneeFilters, ok := parseActorFilterList(w, r.URL.Query().Get("assignee_filters"), "assignee_filters")
	if !ok {
		return
	}
	includeNoAssignee := r.URL.Query().Get("include_no_assignee") == "true"
	if len(assigneeFilters) > 0 || includeNoAssignee {
		ors := make([]string, 0, len(assigneeFilters)+1)
		for _, filter := range assigneeFilters {
			ors = append(ors, fmt.Sprintf(
				"(i.assignee_type = %s::text AND i.assignee_id = %s::uuid)",
				addArg(filter.actorType),
				addArg(filter.actorID),
			))
		}
		if includeNoAssignee {
			ors = append(ors, "(i.assignee_type IS NULL AND i.assignee_id IS NULL)")
		}
		where = append(where, "("+strings.Join(ors, " OR ")+")")
	}

	creatorFilters, ok := parseActorFilterList(w, r.URL.Query().Get("creator_filters"), "creator_filters")
	if !ok {
		return
	}
	if len(creatorFilters) > 0 {
		ors := make([]string, 0, len(creatorFilters))
		for _, filter := range creatorFilters {
			ors = append(ors, fmt.Sprintf(
				"(i.creator_type = %s::text AND i.creator_id = %s::uuid)",
				addArg(filter.actorType),
				addArg(filter.actorID),
			))
		}
		where = append(where, "("+strings.Join(ors, " OR ")+")")
	}

	projectIDs, ok := parseUUIDParamList(w, r.URL.Query().Get("project_ids"), "project_ids")
	if !ok {
		return
	}
	includeNoProject := r.URL.Query().Get("include_no_project") == "true"
	if len(projectIDs) > 0 || includeNoProject {
		ors := make([]string, 0, 2)
		if len(projectIDs) > 0 {
			ors = append(ors, fmt.Sprintf("i.project_id = ANY(%s::uuid[])", addArg(projectIDs)))
		}
		if includeNoProject {
			ors = append(ors, "i.project_id IS NULL")
		}
		where = append(where, "("+strings.Join(ors, " OR ")+")")
	}

	labelIDs, ok := parseUUIDParamList(w, r.URL.Query().Get("label_ids"), "label_ids")
	if !ok {
		return
	}
	if len(labelIDs) > 0 {
		where = append(where, fmt.Sprintf(
			"EXISTS (SELECT 1 FROM issue_to_label itl WHERE itl.issue_id = i.id AND itl.label_id = ANY(%s::uuid[]))",
			addArg(labelIDs),
		))
	}

	dateFilter, ok := parseIssueDateFilter(w, r.URL.Query())
	if !ok {
		return
	}
	where = appendIssueDateFilter(where, addArg, dateFilter)

	if groupAssigneeType := r.URL.Query().Get("group_assignee_type"); groupAssigneeType != "" {
		if groupAssigneeType == "none" {
			where = append(where, "(i.assignee_type IS NULL AND i.assignee_id IS NULL)")
		} else {
			if !isIssueActorType(groupAssigneeType) {
				writeError(w, http.StatusBadRequest, "invalid group_assignee_type")
				return
			}
			rawID := r.URL.Query().Get("group_assignee_id")
			if rawID == "" {
				writeError(w, http.StatusBadRequest, "invalid group_assignee_id")
				return
			}
			assigneeID, ok := parseUUIDOrBadRequest(w, rawID, "group_assignee_id")
			if !ok {
				return
			}
			where = append(where, fmt.Sprintf(
				"(i.assignee_type = %s::text AND i.assignee_id = %s::uuid)",
				addArg(groupAssigneeType),
				addArg(assigneeID),
			))
		}
	}

	sortCol := "position"
	sortIsExpr := false
	sortIsProperty := false
	if s := r.URL.Query().Get("sort"); s != "" {
		switch s {
		case "position", "title", "created_at", "start_date", "due_date":
			sortCol = s
		case "priority":
			sortCol = "CASE i.priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 WHEN 'low' THEN 3 ELSE 4 END"
			sortIsExpr = true
		default:
			// property:<definitionId> sorts by the custom-property value
			// (typed expression); unknown/archived definitions degrade to
			// position order instead of erroring stale clients.
			expr, handled, sortErr := h.propertySortExpr(r, workspaceID, s)
			if !handled {
				writeError(w, http.StatusBadRequest, "invalid sort value")
				return
			}
			if sortErr != nil {
				if sortErr.Error() == "invalid sort value" || sortErr.Error() == "invalid workspace id" {
					writeError(w, http.StatusBadRequest, sortErr.Error())
					return
				}
				slog.Warn("propertySortExpr failed", append(logger.RequestAttrs(r), "error", sortErr)...)
				writeError(w, http.StatusInternalServerError, "failed to resolve sort")
				return
			}
			if expr != "" {
				sortCol = expr
				sortIsExpr = true
				sortIsProperty = true
			}
		}
	}
	sortDir := "ASC"
	if sortCol != "position" {
		if d := r.URL.Query().Get("direction"); d != "" {
			switch strings.ToLower(d) {
			case "asc":
				sortDir = "ASC"
			case "desc":
				sortDir = "DESC"
			default:
				writeError(w, http.StatusBadRequest, "invalid direction value")
				return
			}
		}
	}

	intraGroupOrder := sortCol
	if !sortIsExpr {
		intraGroupOrder = "i." + sortCol
	}
	intraGroupOrder += " " + sortDir
	if sortCol == "start_date" || sortCol == "due_date" || sortIsProperty {
		intraGroupOrder += " NULLS LAST"
	}
	intraGroupOrder += ", i.created_at DESC"

	offsetRef := addArg(int64(offset))
	limitRef := addArg(int64(limit))
	query := fmt.Sprintf(`
WITH ranked AS (
	SELECT
		i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
		i.assignee_type, i.assignee_id, i.creator_type, i.creator_id,
		i.parent_issue_id, i.position, i.start_date, i.due_date, i.created_at, i.updated_at,
		i.number, i.project_id, i.metadata, i.stage, i.properties,
		COUNT(*) OVER (PARTITION BY i.assignee_type, i.assignee_id) AS group_total,
		ROW_NUMBER() OVER (
			PARTITION BY i.assignee_type, i.assignee_id
			ORDER BY %s
		) AS rn
	FROM issue i
	WHERE %s
)
SELECT
	id, workspace_id, title, description, status, priority,
	assignee_type, assignee_id, creator_type, creator_id,
	parent_issue_id, position, start_date, due_date, created_at, updated_at,
	number, project_id, metadata, stage, properties, group_total
FROM ranked
WHERE rn > %s AND rn <= %s + %s
ORDER BY
	CASE assignee_type
		WHEN 'member' THEN 0
		WHEN 'agent' THEN 1
		WHEN 'squad' THEN 2
		ELSE 3
	END,
	assignee_type NULLS LAST,
	assignee_id NULLS LAST,
	rn`, intraGroupOrder, strings.Join(where, " AND "), offsetRef, offsetRef, limitRef)

	rows, err := h.DB.Query(ctx, query, args...)
	if err != nil {
		slog.Warn("ListGroupedIssues query failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list grouped issues")
		return
	}
	defer rows.Close()

	groupedRows := []groupedIssueRow{}
	for rows.Next() {
		var row groupedIssueRow
		if err := rows.Scan(
			&row.ID,
			&row.WorkspaceID,
			&row.Title,
			&row.Description,
			&row.Status,
			&row.Priority,
			&row.AssigneeType,
			&row.AssigneeID,
			&row.CreatorType,
			&row.CreatorID,
			&row.ParentIssueID,
			&row.Position,
			&row.StartDate,
			&row.DueDate,
			&row.CreatedAt,
			&row.UpdatedAt,
			&row.Number,
			&row.ProjectID,
			&row.Metadata,
			&row.Stage,
			&row.Properties,
			&row.GroupTotal,
		); err != nil {
			slog.Warn("ListGroupedIssues scan failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to list grouped issues")
			return
		}
		groupedRows = append(groupedRows, row)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("ListGroupedIssues rows failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list grouped issues")
		return
	}

	ids := make([]pgtype.UUID, len(groupedRows))
	for i, row := range groupedRows {
		ids[i] = row.ID
	}
	labelsMap := h.labelsByIssue(ctx, wsUUID, ids)
	prefix := h.getIssuePrefix(ctx, wsUUID)

	groups := []IssueAssigneeGroupResponse{}
	groupIndex := map[string]int{}
	for _, row := range groupedRows {
		groupID := assigneeGroupID(row.AssigneeType, row.AssigneeID)
		idx, exists := groupIndex[groupID]
		if !exists {
			idx = len(groups)
			groupIndex[groupID] = idx
			groups = append(groups, IssueAssigneeGroupResponse{
				ID:           groupID,
				AssigneeType: textToPtr(row.AssigneeType),
				AssigneeID:   uuidToPtr(row.AssigneeID),
				Issues:       []IssueResponse{},
				Total:        row.GroupTotal,
			})
		}

		issue := issueListRowToResponse(row.ListIssuesRow, prefix)
		labels := labelsMap[issue.ID]
		if labels == nil {
			labels = []LabelResponse{}
		}
		issue.Labels = &labels
		groups[idx].Issues = append(groups[idx].Issues, issue)
	}

	writeJSON(w, http.StatusOK, GroupedIssuesResponse{Groups: groups})
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

// Cap on the number of parents we'll fan-out children for in one request.
// Swimlane's visible-lane count is naturally bounded by what fits on screen
// (typically <= 50), but cap explicitly so a malicious caller can't ANY()
// across the whole workspace's issue set in a single round trip.
const listChildrenByParentsLimit = 200

// ListChildrenByParents returns the union of children for the
// provided parent ids. Replaces the N-call fan-out Swimlane would otherwise
// have to make on mount (one /issues/:id/children per visible parent lane).
//
// Workspace scope is enforced at the query level — any parent_id that doesn't
// belong to the caller's workspace simply yields zero children, so callers
// can't probe parents across workspace boundaries.
func (h *Handler) ListChildrenByParents(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	raw := r.URL.Query().Get("parent_ids")
	if raw == "" {
		// Empty input is a no-op response (not an error) — simplifies the
		// client which calls this unconditionally on Swimlane mount even
		// when there are zero visible parent lanes.
		writeJSON(w, http.StatusOK, map[string]any{"issues": []IssueResponse{}})
		return
	}

	parts := strings.Split(raw, ",")
	if len(parts) > listChildrenByParentsLimit {
		writeError(w, http.StatusBadRequest, "too many parent_ids")
		return
	}
	parentIDs := make([]pgtype.UUID, 0, len(parts))
	for _, s := range parts {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		id, ok := parseUUIDOrBadRequest(w, s, "parent_ids")
		if !ok {
			return
		}
		parentIDs = append(parentIDs, id)
	}
	if len(parentIDs) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"issues": []IssueResponse{}})
		return
	}

	children, err := h.Queries.ListChildrenByParents(r.Context(), db.ListChildrenByParentsParams{
		WorkspaceID: wsUUID,
		ParentIds:   parentIDs,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list child issues")
		return
	}
	prefix := h.getIssuePrefix(r.Context(), wsUUID)
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
// user picks an actor (agent or squad) in the modal and types one line of
// natural language; the server validates the actor's reachability up front,
// queues a quick-create task, and returns 202 immediately. The agent
// translates the prompt into a `multica issue create` invocation in the
// background; success and failure both surface as inbox notifications to
// the requester.
//
// Exactly one of AgentID / SquadID is required. When SquadID is set, the
// task is enqueued against the squad's leader agent and the leader receives
// the same Operating Protocol briefing it would for an issue assigned to
// the squad, so it can choose to delegate to a squad member as usual.
//
// ProjectID is optional and lets the modal target a specific project so
// the agent's `multica issue create` invocation passes `--project <uuid>`
// instead of letting it default. The frontend remembers the user's last
// pick per workspace, so frequent users skip retyping "in project X".
//
// ParentIssueID is optional and is set by the "Add sub issue" entry point
// when the modal is opened from an existing issue. The agent passes it
// through as `--parent <uuid>` so the new issue is filed as a sub-issue,
// keeping the sub-issue intent of the entry point regardless of whether
// the user submits via manual or agent mode.
type QuickCreateIssueRequest struct {
	AgentID       string   `json:"agent_id,omitempty"`
	SquadID       string   `json:"squad_id,omitempty"`
	Prompt        string   `json:"prompt"`
	Priority      string   `json:"priority,omitempty"`
	DueDate       string   `json:"due_date,omitempty"`
	ProjectID     string   `json:"project_id,omitempty"`
	ParentIssueID string   `json:"parent_issue_id,omitempty"`
	AttachmentIDs []string `json:"attachment_ids,omitempty"`
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
	priority := strings.ToLower(strings.TrimSpace(req.Priority))
	if priority != "" && priority != "urgent" && priority != "high" && priority != "medium" && priority != "low" {
		writeError(w, http.StatusBadRequest, "priority must be one of: urgent, high, medium, low")
		return
	}
	dueDate := strings.TrimSpace(req.DueDate)
	if dueDate != "" {
		parsed, err := util.ParseCalendarDate(dueDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid due_date format, expected YYYY-MM-DD")
			return
		}
		dueDate = parsed.Time.Format("2006-01-02")
	}

	hasAgent := strings.TrimSpace(req.AgentID) != ""
	hasSquad := strings.TrimSpace(req.SquadID) != ""
	if hasAgent == hasSquad {
		writeError(w, http.StatusBadRequest, "exactly one of agent_id or squad_id is required")
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

	// Resolve the actor to the agent that will actually run the task. For
	// agent picks that's the agent itself; for squad picks it's the squad's
	// leader agent. The leader receives a squad-leader briefing on dispatch
	// (see daemon.go), matching the behavior of an issue assigned to the
	// squad — picking a squad here is functionally "ask the squad leader to
	// create this issue, on behalf of the squad".
	var agentUUID pgtype.UUID
	var squadUUID pgtype.UUID
	if hasSquad {
		var ok bool
		squadUUID, ok = parseUUIDOrBadRequest(w, req.SquadID, "squad_id")
		if !ok {
			return
		}
		squad, err := h.Queries.GetSquadInWorkspace(r.Context(), db.GetSquadInWorkspaceParams{
			ID:          squadUUID,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "squad not found")
			return
		}
		if squad.ArchivedAt.Valid {
			writeError(w, http.StatusBadRequest, "squad is archived")
			return
		}
		agentUUID = squad.LeaderID
	} else {
		var ok bool
		agentUUID, ok = parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
		if !ok {
			return
		}
	}

	// Reuse the same workspace-membership / archived / private-agent
	// ownership rules as `validateAssigneePair` so a user can't POST a
	// private agent_id they shouldn't be able to dispatch (the frontend
	// filters them out, but the handler is the trust boundary). Squad
	// picks reach this with the resolved leader agent; the same rules
	// apply — a private leader behind a squad the user can't reach
	// should still be rejected.
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
	// handling, quick-create attachment binding, no-retry on partial failure).
	// Older daemons either double-create issues on partial CLI failures, drop
	// attachment bindings, or mishandle pasted screenshot URLs; fail closed
	// before enqueuing rather than surface the breakage as an inbox failure
	// twenty seconds later. Dev-built
	// daemons (git-describe shape) are exempted inside CheckMinCLIVersion
	// so `make daemon` works without weakening staging or production.
	if status, payload := h.checkQuickCreateDaemonVersion(r.Context(), agent.RuntimeID); status != 0 {
		writeJSON(w, status, payload)
		return
	}
	if priority != "" || dueDate != "" {
		if status, payload := h.checkQuickCreateDaemonVersionAtLeast(
			r.Context(), agent.RuntimeID, agentpkg.MinQuickCreateFieldsCLIVersion,
		); status != 0 {
			writeJSON(w, status, payload)
			return
		}
	}

	attachmentIDs, ok := parseUUIDSliceOrBadRequest(w, req.AttachmentIDs, "attachment_ids")
	if !ok {
		return
	}

	// Optional project_id — validate it belongs to the same workspace before
	// pinning the task to it. The handler is the trust boundary; the frontend
	// already only shows projects from the active workspace, but we re-check
	// here so a forged request can't smuggle a foreign project ID through.
	var projectUUID pgtype.UUID
	if strings.TrimSpace(req.ProjectID) != "" {
		pid, ok := parseUUIDOrBadRequest(w, req.ProjectID, "project_id")
		if !ok {
			return
		}
		if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
			ID:          pid,
			WorkspaceID: wsUUID,
		}); err != nil {
			writeError(w, http.StatusBadRequest, "project not found")
			return
		}
		projectUUID = pid
	}

	// Optional parent_issue_id — validate same-workspace membership just like
	// the regular CreateIssue path. Frontend seeds this from the "Add sub
	// issue" entry, but the handler re-checks so a forged request can't
	// smuggle a foreign parent UUID through.
	var parentIssueUUID pgtype.UUID
	if strings.TrimSpace(req.ParentIssueID) != "" {
		pid, ok := parseUUIDOrBadRequest(w, req.ParentIssueID, "parent_issue_id")
		if !ok {
			return
		}
		parent, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          pid,
			WorkspaceID: wsUUID,
		})
		if err != nil || !parent.ID.Valid {
			writeError(w, http.StatusBadRequest, "parent issue not found in this workspace")
			return
		}
		parentIssueUUID = pid
	}

	task, err := h.TaskService.EnqueueQuickCreateTask(r.Context(), wsUUID, requesterUUID, agentUUID, squadUUID, prompt, priority, dueDate, projectUUID, parentIssueUUID, attachmentIDs)
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
//	  "min_version":     "0.2.21",
//	  "runtime_id":      "<uuid>"
//	}
func (h *Handler) checkQuickCreateDaemonVersion(ctx context.Context, runtimeID pgtype.UUID) (int, map[string]any) {
	return h.checkQuickCreateDaemonVersionAtLeast(ctx, runtimeID, agentpkg.MinQuickCreateCLIVersion)
}

func (h *Handler) checkQuickCreateDaemonVersionAtLeast(ctx context.Context, runtimeID pgtype.UUID, minimum string) (int, map[string]any) {
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
	switch err := agentpkg.CheckMinCLIVersionFor(current, minimum); {
	case err == nil:
		return 0, nil
	case errors.Is(err, agentpkg.ErrCLIVersionMissing), errors.Is(err, agentpkg.ErrCLIVersionTooOld):
		return http.StatusUnprocessableEntity, map[string]any{
			"code":            "daemon_version_unsupported",
			"current_version": current,
			"min_version":     minimum,
			"runtime_id":      uuidToString(runtimeID),
		}
	default:
		// Defensive fall-through: unknown error from the version check is
		// also fail-closed, since the gate exists precisely because we
		// can't trust older daemons with this flow.
		return http.StatusUnprocessableEntity, map[string]any{
			"code":            "daemon_version_unsupported",
			"current_version": current,
			"min_version":     minimum,
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
	Stage         *int32   `json:"stage,omitempty"`
	StartDate     *string  `json:"start_date"`
	DueDate       *string  `json:"due_date"`
	AttachmentIDs []string `json:"attachment_ids,omitempty"`
	// LabelIDs are issue-scoped labels to attach to the new issue in the same
	// transaction as the create. Unknown or non-issue ids are rejected with
	// 400 (service.ErrIssueLabelNotFound) rather than silently dropped.
	LabelIDs []string `json:"label_ids,omitempty"`
	// OriginType / OriginID stamp the new issue with its provenance so
	// platform-internal flows can deterministically locate it later. Only
	// trusted callers should set these — currently the daemon CLI passes
	// them through for quick-create tasks (origin_type=quick_create,
	// origin_id=agent_task_queue.id).
	OriginType *string `json:"origin_type,omitempty"`
	OriginID   *string `json:"origin_id,omitempty"`

	AllowDuplicate bool `json:"allow_duplicate,omitempty"`
}

func duplicateIssueMessage(issue IssueResponse) string {
	return issueguard.DuplicateMessage(issue.Identifier, issue.Title, issue.Status)
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

	status := req.Status
	if status == "" {
		status = "todo"
	}
	priority := req.Priority
	if priority == "" {
		priority = "none"
	}
	if !validateIssueEnum(w, "status", status, validIssueStatuses) {
		return
	}
	if !validateIssueEnum(w, "priority", priority, validIssuePriorities) {
		return
	}
	if req.Stage != nil && *req.Stage < 1 {
		writeError(w, http.StatusBadRequest, "stage must be >= 1")
		return
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
	}
	// Cross-workspace parent / project existence is enforced inside
	// IssueService.Create (atomically with the create), so every entry
	// point — HTTP, Lark, future MCP — gets the same boundary check
	// without duplicating the lookup here.

	attachmentIDs, ok := parseUUIDSliceOrBadRequest(w, req.AttachmentIDs, "attachment_ids")
	if !ok {
		return
	}

	labelIDs, ok := parseUUIDSliceOrBadRequest(w, req.LabelIDs, "label_ids")
	if !ok {
		return
	}

	var startDate pgtype.Date
	if req.StartDate != nil && *req.StartDate != "" {
		d, err := util.ParseCalendarDate(*req.StartDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid start_date format, expected YYYY-MM-DD")
			return
		}
		startDate = d
	}

	var dueDate pgtype.Date
	if req.DueDate != nil && *req.DueDate != "" {
		d, err := util.ParseCalendarDate(*req.DueDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid due_date format, expected YYYY-MM-DD")
			return
		}
		dueDate = d
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
	} else if creatorType == "agent" {
		// MUL-4305: an agent creating an issue via the ordinary create path
		// carries no explicit origin, which historically left the new issue
		// unattributed. Any run later derived from it (agent assignment,
		// squad-leader trigger) then lost the top-of-chain human originator,
		// so A2A @-mentions from those runs failed the canInvokeAgent gate
		// against private agents. Stamp the acting task as the issue's origin
		// so resolveOriginatorForIssueTask can inherit its originator — the
		// same trick CreateComment uses with comment.source_task_id (MUL-4015).
		//
		// The task id is taken from the SERVER-trusted X-Task-ID: resolveActor
		// only returns creatorType=="agent" when either X-Actor-Source=task_token
		// (the auth middleware bound X-Agent-ID/X-Task-ID from the mat_ token and
		// stripped any client value) or the X-Agent-ID/X-Task-ID pair was
		// validated against the DB. A member-forged X-Task-ID never reaches here
		// because it would have resolved to creatorType=="member". We still
		// re-check the task belongs to the acting agent before trusting it.
		if taskIDHeader := r.Header.Get("X-Task-ID"); taskIDHeader != "" {
			if taskUUID, perr := util.ParseUUID(taskIDHeader); perr == nil {
				if task, terr := h.Queries.GetAgentTask(r.Context(), taskUUID); terr == nil && uuidToString(task.AgentID) == actualCreatorID {
					originType = pgtype.Text{String: "agent_create", Valid: true}
					originID = taskUUID
				}
			}
		}
	}

	// Prefix is workspace-level; pre-compute once so both the broadcast
	// payload builder and the HTTP response share the same value.
	prefix := h.getIssuePrefix(r.Context(), wsUUID)

	// Analytics agent ID: assignee agent when the issue is being assigned
	// to an agent, otherwise the creator agent for agent-authored issues.
	// Resolved here (not in the service) because creator identity is HTTP-side.
	analyticsAgentID := ""
	if assigneeType.Valid && assigneeType.String == "agent" {
		analyticsAgentID = uuidToString(assigneeID)
	}
	if creatorType == "agent" && analyticsAgentID == "" {
		analyticsAgentID = actualCreatorID
	}

	buildAttachmentResponses := func(atts []db.Attachment) []AttachmentResponse {
		if len(atts) == 0 {
			return nil
		}
		out := make([]AttachmentResponse, len(atts))
		for i, a := range atts {
			out[i] = h.attachmentToResponse(a)
		}
		return out
	}

	res, err := h.IssueService.Create(r.Context(), service.IssueCreateParams{
		WorkspaceID:    wsUUID,
		Title:          req.Title,
		Description:    ptrToText(req.Description),
		Status:         status,
		Priority:       priority,
		AssigneeType:   assigneeType,
		AssigneeID:     assigneeID,
		CreatorType:    creatorType,
		CreatorID:      parseUUID(actualCreatorID),
		ParentIssueID:  parentIssueID,
		ProjectID:      projectID,
		StartDate:      startDate,
		DueDate:        dueDate,
		OriginType:     originType,
		OriginID:       originID,
		Stage:          ptrToInt4(req.Stage),
		AttachmentIDs:  attachmentIDs,
		LabelIDs:       labelIDs,
		AllowDuplicate: req.AllowDuplicate,
	}, service.IssueCreateOpts{
		ActorID:          actualCreatorID,
		AnalyticsAgentID: analyticsAgentID,
		Platform:         func() string { p, _, _ := middleware.ClientMetadataFromContext(r.Context()); return p }(),
		BroadcastPayload: func(issue db.Issue, atts []db.Attachment, labels []db.IssueLabel) map[string]any {
			payload := issueToResponse(issue, prefix)
			payload.Attachments = buildAttachmentResponses(atts)
			// Carry the authoritative label snapshot so every online client
			// renders the new issue already labeled. Non-nil (even empty)
			// pointer = authoritative list; the old flow's separate
			// issue_labels:changed broadcast is gone.
			labelResponses := labelsToResponse(labels)
			payload.Labels = &labelResponses
			return map[string]any{"issue": payload}
		},
	})

	if errors.Is(err, service.ErrActiveDuplicate) {
		dup := *res.DuplicateIssue
		existing := issueToResponse(dup, h.getIssuePrefix(r.Context(), dup.WorkspaceID))
		writeJSON(w, http.StatusConflict, map[string]any{
			"code":  "active_duplicate_issue",
			"error": duplicateIssueMessage(existing),
			"issue": existing,
		})
		return
	}
	if errors.Is(err, service.ErrParentIssueNotFound) {
		writeError(w, http.StatusBadRequest, "parent issue not found in this workspace")
		return
	}
	if errors.Is(err, service.ErrProjectNotFound) {
		writeError(w, http.StatusBadRequest, "project not found in this workspace")
		return
	}
	if errors.Is(err, service.ErrIssueLabelNotFound) {
		writeError(w, http.StatusBadRequest, "one or more labels not found in this workspace")
		return
	}
	if err != nil {
		slog.Warn("create issue failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to create issue: "+err.Error())
		return
	}

	issue := res.Issue
	slog.Info("issue created", append(logger.RequestAttrs(r), "issue_id", uuidToString(issue.ID), "title", issue.Title, "status", issue.Status, "workspace_id", workspaceID)...)

	resp := issueToResponse(issue, prefix)
	resp.Attachments = buildAttachmentResponses(res.Attachments)
	// Echo the authoritative labels attached in the create transaction. Always
	// non-nil (empty slice when none) so a newer client can tell the backend
	// understood label_ids and skip its legacy post-create attach fallback.
	labelResponses := labelsToResponse(res.Labels)
	resp.Labels = &labelResponses
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
	StartDate     *string  `json:"start_date"`
	DueDate       *string  `json:"due_date"`
	ParentIssueID *string  `json:"parent_issue_id"`
	ProjectID     *string  `json:"project_id"`
	Stage         *int32   `json:"stage"`
	// AttachmentIDs lets the description editor bind newly uploaded files to
	// this issue so they surface in `GET /api/issues/:id/attachments` and the
	// editor's preview Eye keeps working past a refresh. Existing bindings
	// are idempotent — re-sending the same id is a no-op.
	AttachmentIDs []string `json:"attachment_ids"`
	// SuppressRun, when true, applies the assignee/status change as usual but
	// skips starting the agent run this write would otherwise trigger
	// ("暂时不启动" — MUL-3375). It is not an undo: the change takes effect and
	// the issue can be run later via manual run/rerun. Optional; omitted or
	// false keeps today's behavior. Mirrors comment suppress_agent_ids.
	SuppressRun bool `json:"suppress_run,omitempty"`
	// HandoffNote is an optional free-text instruction injected into the run's
	// opening context when this write starts an agent/squad run ("交接说明" —
	// MUL-3375). Only consumed when a run actually starts: SuppressRun=true or
	// a parked/non-triggering write drops it. Never fabricates a comment.
	HandoffNote string `json:"handoff_note,omitempty"`
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
		StartDate:     prevIssue.StartDate,
		DueDate:       prevIssue.DueDate,
		ParentIssueID: prevIssue.ParentIssueID,
		ProjectID:     prevIssue.ProjectID,
		Stage:         prevIssue.Stage,
	}

	// COALESCE fields — only set when explicitly provided
	if req.Title != nil {
		params.Title = pgtype.Text{String: *req.Title, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.Status != nil {
		if !validateIssueEnum(w, "status", *req.Status, validIssueStatuses) {
			return
		}
		params.Status = pgtype.Text{String: *req.Status, Valid: true}
	}
	if req.Priority != nil {
		if !validateIssueEnum(w, "priority", *req.Priority, validIssuePriorities) {
			return
		}
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
	if _, ok := rawFields["start_date"]; ok {
		if req.StartDate != nil && *req.StartDate != "" {
			d, err := util.ParseCalendarDate(*req.StartDate)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid start_date format, expected YYYY-MM-DD")
				return
			}
			params.StartDate = d
		} else {
			params.StartDate = pgtype.Date{Valid: false} // explicit null = clear date
		}
	}
	if _, ok := rawFields["due_date"]; ok {
		if req.DueDate != nil && *req.DueDate != "" {
			d, err := util.ParseCalendarDate(*req.DueDate)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid due_date format, expected YYYY-MM-DD")
				return
			}
			params.DueDate = d
		} else {
			params.DueDate = pgtype.Date{Valid: false} // explicit null = clear date
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
	if _, ok := rawFields["stage"]; ok {
		if req.Stage != nil {
			if *req.Stage < 1 {
				writeError(w, http.StatusBadRequest, "stage must be >= 1")
				return
			}
			params.Stage = pgtype.Int4{Int32: *req.Stage, Valid: true}
		} else {
			params.Stage = pgtype.Int4{Valid: false} // explicit null = unstage
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

	attachmentIDs, ok := parseUUIDSliceOrBadRequest(w, req.AttachmentIDs, "attachment_ids")
	if !ok {
		return
	}

	issue, err := h.Queries.UpdateIssue(r.Context(), params)
	if err != nil {
		slog.Warn("update issue failed", append(logger.RequestAttrs(r), "error", err, "issue_id", id, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to update issue: "+err.Error())
		return
	}

	if len(attachmentIDs) > 0 {
		h.linkAttachmentsByIssueIDs(r.Context(), issue.ID, issue.WorkspaceID, attachmentIDs)
	}

	prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
	resp := issueToResponse(issue, prefix)
	slog.Info("issue updated", append(logger.RequestAttrs(r), "issue_id", id, "workspace_id", workspaceID)...)

	assigneeChanged := (req.AssigneeType != nil || req.AssigneeID != nil) &&
		(prevIssue.AssigneeType.String != issue.AssigneeType.String || uuidToString(prevIssue.AssigneeID) != uuidToString(issue.AssigneeID))
	statusChanged := req.Status != nil && prevIssue.Status != issue.Status
	priorityChanged := req.Priority != nil && prevIssue.Priority != issue.Priority
	// project_changed gates the client's per-project issue-list refetch the way
	// status/assignee flags gate theirs. Without it the client must diff
	// project_id against its own cache, which breaks once an optimistic local
	// move has overwritten the cached value (MUL-3669 / #4548).
	projectChanged := req.ProjectID != nil && uuidToString(prevIssue.ProjectID) != uuidToString(issue.ProjectID)
	descriptionChanged := req.Description != nil && textToPtr(prevIssue.Description) != resp.Description
	titleChanged := req.Title != nil && prevIssue.Title != issue.Title
	prevStartDate := dateToPtr(prevIssue.StartDate)
	startDateChanged := prevStartDate != resp.StartDate && (prevStartDate == nil) != (resp.StartDate == nil) ||
		(prevStartDate != nil && resp.StartDate != nil && *prevStartDate != *resp.StartDate)
	prevDueDate := dateToPtr(prevIssue.DueDate)
	dueDateChanged := prevDueDate != resp.DueDate && (prevDueDate == nil) != (resp.DueDate == nil) ||
		(prevDueDate != nil && resp.DueDate != nil && *prevDueDate != *resp.DueDate)

	// Determine actor identity: agent (via X-Agent-ID header) or member.
	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	h.publish(protocol.EventIssueUpdated, workspaceID, actorType, actorID, map[string]any{
		"issue":               resp,
		"assignee_changed":    assigneeChanged,
		"status_changed":      statusChanged,
		"priority_changed":    priorityChanged,
		"project_changed":     projectChanged,
		"start_date_changed":  startDateChanged,
		"due_date_changed":    dueDateChanged,
		"description_changed": descriptionChanged,
		"title_changed":       titleChanged,
		"prev_title":          prevIssue.Title,
		"prev_assignee_type":  textToPtr(prevIssue.AssigneeType),
		"prev_assignee_id":    uuidToPtr(prevIssue.AssigneeID),
		"prev_status":         prevIssue.Status,
		"prev_priority":       prevIssue.Priority,
		"prev_start_date":     prevStartDate,
		"prev_due_date":       prevDueDate,
		"prev_description":    textToPtr(prevIssue.Description),
		"creator_type":        prevIssue.CreatorType,
		"creator_id":          uuidToString(prevIssue.CreatorID),
	})

	// Reconcile the task queue. Whether this write starts an agent run — and
	// for whom (agent assignee or squad leader) — is decided by the single
	// WillEnqueueRun predicate, shared verbatim with the preview endpoint so
	// the two never drift (MUL-3375).
	//
	// A reassignment intentionally does NOT cancel existing tasks on the issue
	// (#4963 / MUL-4113). The previous "cancel every active task on the issue"
	// was too coarse: it silently dropped unrelated in-flight work (a
	// mention-triggered run for another agent, a squad task) with no requeue,
	// and it self-cancelled a run that reassigned the issue from inside itself.
	// Ownership handoff no longer implies interruption; the new assignee's run,
	// if any, is enqueued by WillEnqueueRun below and runs alongside whatever
	// was already in flight. No status change — not even → cancelled — cancels
	// active tasks: a user clicking "cancel" on an issue has no expectation that
	// it stops in-flight agent runs, so that implicit coupling is gone
	// (MUL-4465). Deleting an issue still cancels its tasks (see DeleteIssue),
	// because the tasks' owning issue ceases to exist.
	if trigger, ok := h.IssueService.WillEnqueueRun(r.Context(),
		service.IssueTriggerInput{
			Issue:           issue,
			PrevStatus:      prevIssue.Status,
			AssigneeChanged: assigneeChanged,
			StatusChanged:   statusChanged,
		},
		h.issueTriggerWriteProbe(r, actorType, issue),
	); ok && !req.SuppressRun {
		h.dispatchIssueRun(r.Context(), issue, trigger, actorType, actorID, req.HandoffNote)
	}

	// Platform-driven parent notification: when this issue transitions into
	// `done` and has a parent, post a top-level system comment on the parent
	// (MUL-2538 — replaces the agent-prompt rule that caused self-mention
	// loops in PR #2918). The helper guards on transition + parent state and
	// fails best-effort.
	if statusChanged {
		h.notifyParentOfChildDone(r.Context(), prevIssue, issue)
	}

	writeJSON(w, http.StatusOK, resp)
}

// validateAssigneePair verifies the (assignee_type, assignee_id) pair refers
// to an existing entity in the workspace. For agent assignees it also rejects
// archived agents and runs the private-agent gate via canAccessPrivateAgent
// — assigning an issue is a task-producing surface, so it must use the same
// predicate as chat / @-mention / history. Agent callers (X-Agent-ID) bypass
// the gate so A2A flows can still hand work off to private agents.
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
		actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
		if !h.canInvokeAgent(ctx, agent, actorType, actorID, h.invokeOriginatorFromRequest(r, actorType, actorID), workspaceID) {
			return http.StatusForbidden, "cannot assign to private agent"
		}
		return 0, ""
	case "squad":
		squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
			ID:          assigneeID,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			return http.StatusBadRequest, "assignee_id does not refer to a squad in this workspace"
		}
		if squad.ArchivedAt.Valid {
			return http.StatusBadRequest, "cannot assign to an archived squad"
		}
		leader, err := h.Queries.GetAgent(ctx, squad.LeaderID)
		if err != nil || leader.ArchivedAt.Valid {
			return http.StatusBadRequest, "squad leader is archived; cannot assign to this squad"
		}
		actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
		if !h.canInvokeAgent(ctx, leader, actorType, actorID, h.invokeOriginatorFromRequest(r, actorType, actorID), workspaceID) {
			return http.StatusForbidden, "cannot assign to squad with private leader"
		}
		return 0, ""
	default:
		return http.StatusBadRequest, "assignee_type must be 'member', 'agent', or 'squad'"
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

// shouldEnqueueAssigneeFallback returns true when comment routing can fall back
// to the issue's assigned agent. Fires for any status — comments are
// conversational and can happen at any stage, including after completion
// (e.g. follow-up questions on a done issue).
//
// Mirrors the private-agent gate that resolveMentionedAgentCommentTriggers applies on the
// @mention path: once an owner/admin assigns a private agent to an issue, the
// agent's UUID is "welded" onto the issue and remains visible to every member
// who can view it. Without this check any of those members could dispatch a new
// task to the private agent simply by commenting (#3300).
func (h *Handler) shouldEnqueueAssigneeFallback(ctx context.Context, issue db.Issue, actorType, actorID string, opts commentTriggerComputeOptions) bool {
	_, hasPending, ok := h.assigneeFallbackAgent(ctx, issue, actorType, actorID, opts)
	return ok && !hasPending
}

func (h *Handler) assigneeFallbackAgent(ctx context.Context, issue db.Issue, actorType, actorID string, opts commentTriggerComputeOptions) (db.Agent, bool, bool) {
	if !issue.AssigneeType.Valid || issue.AssigneeType.String != "agent" || !issue.AssigneeID.Valid {
		return db.Agent{}, false, false
	}
	agent, err := h.Queries.GetAgent(ctx, issue.AssigneeID)
	if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
		return db.Agent{}, false, false
	}
	if !h.canInvokeAgent(ctx, agent, actorType, actorID, opts.effectiveInvoker(), uuidToString(issue.WorkspaceID)) {
		return db.Agent{}, false, false
	}
	// Coalescing queue: pending is still a valid route target, but callers
	// that actually enqueue tasks use this flag to avoid piling on duplicates.
	hasPending, err := h.hasPendingTaskForIssueAndAgent(ctx, issue.ID, issue.AssigneeID, opts)
	if err != nil {
		return db.Agent{}, false, false
	}
	return agent, hasPending, true
}

// isAgentRunningOnIssue reports whether the calling agent's current task
// (identified by X-Task-ID) is running for the exact issue being promoted.
// That is the only true self-loop on backlog→active: the agent flipping
// the same issue its own task is executing for would immediately re-enqueue
// itself, complete the run, flip again, and so on.
//
// Same-agent cross-issue handoff (Agent A finishing a task on issue I1 then
// promoting issue I2 — even when I2 is also assigned to A) is NOT a loop
// and must fire; that is the documented serial sub-task chain. Member
// actors never match.
//
// X-Task-ID is guaranteed to be present and consistent when actorType is
// "agent": resolveActor demotes the actor to "member" otherwise (handler.go
// resolveActor). We still recheck defensively — a future caller could pass
// agent identity through a different path.
func (h *Handler) isAgentRunningOnIssue(r *http.Request, actorType string, issue db.Issue) bool {
	if actorType != "agent" {
		return false
	}
	taskIDStr := r.Header.Get("X-Task-ID")
	if taskIDStr == "" {
		return false
	}
	taskUUID, err := util.ParseUUID(taskIDStr)
	if err != nil {
		return false
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskUUID)
	if err != nil {
		return false
	}
	if !task.IssueID.Valid {
		return false
	}
	return uuidToString(task.IssueID) == uuidToString(issue.ID)
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

	err := h.Queries.DeleteIssue(r.Context(), db.DeleteIssueParams{
		ID:          issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
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
		for _, k := range []string{"assignee_type", "assignee_id", "start_date", "due_date", "parent_issue_id", "project_id", "stage"} {
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
	if req.Updates.Status != nil {
		if !validateIssueEnum(w, "status", *req.Updates.Status, validIssueStatuses) {
			return
		}
	}
	if req.Updates.Priority != nil {
		if !validateIssueEnum(w, "priority", *req.Updates.Priority, validIssuePriorities) {
			return
		}
	}

	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	updated := 0
	// Children that transitioned into a terminal status this batch, collected so
	// the parent/stage notification is evaluated once against the final state
	// after the loop (MUL-4155) rather than per-child mid-batch.
	var childDoneCompleted []db.Issue
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
			StartDate:     prevIssue.StartDate,
			DueDate:       prevIssue.DueDate,
			ParentIssueID: prevIssue.ParentIssueID,
			ProjectID:     prevIssue.ProjectID,
			Stage:         prevIssue.Stage,
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
				assigneeUUID, err := util.ParseUUID(*req.Updates.AssigneeID)
				if err != nil {
					continue
				}
				params.AssigneeID = assigneeUUID
			} else {
				params.AssigneeID = pgtype.UUID{Valid: false}
			}
		}
		if _, ok := rawUpdates["start_date"]; ok {
			if req.Updates.StartDate != nil && *req.Updates.StartDate != "" {
				d, err := util.ParseCalendarDate(*req.Updates.StartDate)
				if err != nil {
					continue
				}
				params.StartDate = d
			} else {
				params.StartDate = pgtype.Date{Valid: false}
			}
		}
		if _, ok := rawUpdates["due_date"]; ok {
			if req.Updates.DueDate != nil && *req.Updates.DueDate != "" {
				d, err := util.ParseCalendarDate(*req.Updates.DueDate)
				if err != nil {
					continue
				}
				params.DueDate = d
			} else {
				params.DueDate = pgtype.Date{Valid: false}
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
		if _, ok := rawUpdates["stage"]; ok {
			if req.Updates.Stage != nil {
				if *req.Updates.Stage < 1 {
					continue
				}
				params.Stage = pgtype.Int4{Int32: *req.Updates.Stage, Valid: true}
			} else {
				params.Stage = pgtype.Int4{Valid: false} // explicit null = unstage
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
		projectChanged := req.Updates.ProjectID != nil && uuidToString(prevIssue.ProjectID) != uuidToString(issue.ProjectID)

		h.publish(protocol.EventIssueUpdated, workspaceID, actorType, actorID, map[string]any{
			"issue":            resp,
			"assignee_changed": assigneeChanged,
			"status_changed":   statusChanged,
			"priority_changed": priorityChanged,
			"project_changed":  projectChanged,
		})

		// Reassignment does not cancel existing tasks (#4963 / MUL-4113) —
		// mirrors UpdateIssue. See that handler for the rationale.
		//
		// Same single predicate as UpdateIssue — batch must not grow its own
		// copy of the enqueue rule (the historical source of four-entry-point
		// drift, MUL-3375). suppress_run applies batch-wide.
		if trigger, ok := h.IssueService.WillEnqueueRun(r.Context(),
			service.IssueTriggerInput{
				Issue:           issue,
				PrevStatus:      prevIssue.Status,
				AssigneeChanged: assigneeChanged,
				StatusChanged:   statusChanged,
			},
			h.issueTriggerWriteProbe(r, actorType, issue),
		); ok && !req.Updates.SuppressRun {
			h.dispatchIssueRun(r.Context(), issue, trigger, actorType, actorID, req.Updates.HandoffNote)
		}

		// No status change — not even → cancelled — cancels active tasks here,
		// mirroring UpdateIssue (MUL-4465). See that handler for the rationale.

		// Platform-driven parent notification, mirrored from UpdateIssue
		// (MUL-2538) but DEFERRED to after the loop. Evaluating the stage
		// barrier here, per-child, would read a mid-batch sibling snapshot and
		// fire a stale "advance Stage N+1" wake when one batch closes several
		// stages at once (MUL-4155). Collect the terminal transitions and let
		// notifyParentsOfBatchChildDone below evaluate each parent once against
		// the batch's final committed state. Same transition guard as
		// notifyParentOfChildDone: a non-terminal -> terminal move on a child.
		if statusChanged && issue.ParentIssueID.Valid &&
			!isTerminalChildStatus(prevIssue.Status) && isTerminalChildStatus(issue.Status) {
			childDoneCompleted = append(childDoneCompleted, issue)
		}

		updated++
	}

	// Aggregate parent/stage notification over the whole batch's final state so
	// each affected parent gets at most one accurate comment + wake, independent
	// of issue_ids order (MUL-4155). Best-effort; failure does not abort the
	// batch. Single-issue UpdateIssue is unchanged and still notifies inline.
	h.notifyParentsOfBatchChildDone(r.Context(), childDoneCompleted)

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

		if err := h.Queries.DeleteIssue(r.Context(), db.DeleteIssueParams{
			ID:          issue.ID,
			WorkspaceID: issue.WorkspaceID,
		}); err != nil {
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
