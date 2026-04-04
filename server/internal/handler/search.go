package handler

import (
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var issueNumberPattern = regexp.MustCompile(`^[A-Za-z]+-(\d+)$`)

type SearchResponse struct {
	Issues []SearchIssueResult `json:"issues"`
}

type SearchIssueResult struct {
	ID           string  `json:"id"`
	Number       int32   `json:"number"`
	Identifier   string  `json:"identifier"`
	Title        string  `json:"title"`
	Status       string  `json:"status"`
	Priority     string  `json:"priority"`
	AssigneeType *string `json:"assignee_type"`
	AssigneeID   *string `json:"assignee_id"`
}

func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceID := resolveWorkspaceID(r)

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, SearchResponse{Issues: []SearchIssueResult{}})
		return
	}

	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 50 {
			limit = v
		}
	}

	// Build tsquery: split on whitespace, join with & for AND semantics.
	// Each word gets :* suffix for prefix matching.
	words := strings.Fields(q)
	tsqueryParts := make([]string, 0, len(words))
	for _, w := range words {
		cleaned := sanitizeTsqueryWord(w)
		if cleaned != "" {
			tsqueryParts = append(tsqueryParts, cleaned+":*")
		}
	}
	tsquery := strings.Join(tsqueryParts, " & ")
	if tsquery == "" {
		tsquery = "___empty___" // won't match anything, let ILIKE handle it
	}

	// Extract issue number if query looks like "MUL-42" or just "42".
	var issueNumber pgtype.Int4
	if matches := issueNumberPattern.FindStringSubmatch(q); len(matches) == 2 {
		if n, err := strconv.Atoi(matches[1]); err == nil {
			issueNumber = pgtype.Int4{Int32: int32(n), Valid: true}
		}
	} else if n, err := strconv.Atoi(strings.TrimSpace(q)); err == nil {
		issueNumber = pgtype.Int4{Int32: int32(n), Valid: true}
	}

	issues, err := h.Queries.SearchIssues(ctx, db.SearchIssuesParams{
		WorkspaceID:  parseUUID(workspaceID),
		Tsquery:      tsquery,
		IlikePattern: escapeLikePattern(q),
		IssueNumber:  issueNumber,
		MaxResults:   int32(limit),
	})
	if err != nil {
		slog.Warn("search failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}

	prefix := h.getIssuePrefix(ctx, parseUUID(workspaceID))
	results := make([]SearchIssueResult, len(issues))
	for i, issue := range issues {
		results[i] = SearchIssueResult{
			ID:           uuidToString(issue.ID),
			Number:       issue.Number,
			Identifier:   prefix + "-" + strconv.Itoa(int(issue.Number)),
			Title:        issue.Title,
			Status:       issue.Status,
			Priority:     issue.Priority,
			AssigneeType: textToPtr(issue.AssigneeType),
			AssigneeID:   uuidToPtr(issue.AssigneeID),
		}
	}

	writeJSON(w, http.StatusOK, SearchResponse{Issues: results})
}

func sanitizeTsqueryWord(w string) string {
	var b strings.Builder
	for _, r := range w {
		switch r {
		case '\'', '!', '&', '|', '(', ')', ':', '*', '<', '>':
			// skip tsquery special characters
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// escapeLikePattern escapes LIKE/ILIKE wildcard characters (%, _) and the
// escape character itself (\) so user input is treated as literal text.
func escapeLikePattern(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}
