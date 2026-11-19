package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Doc search follows the same contract as issue and project search: a
// substring match on the lowered phrase or on every whitespace-separated term,
// ranked title hits first, run under the shared search timeout. It is the
// server answer clients fall back to, and the one agents use through
// `multica doc search`.

const (
	docSearchDefaultLimit = 20
	docSearchMaxLimit     = 50
	// Runes of content returned around the first hit. Docs can hold 2 MB of
	// Markdown; the snippet is cut from this window, never from the whole body.
	docSearchWindowBefore = 200
	docSearchWindowLength = 600
)

// SearchDocResponse is a doc summary plus where the query matched.
type SearchDocResponse struct {
	DocSummaryResponse
	// "title" or "content": the strongest field that matched.
	MatchSource string `json:"match_source"`
	// A short excerpt around the match, for content hits only.
	MatchedSnippet *string `json:"matched_snippet,omitempty"`
}

// buildDocSearchQuery builds the doc search SQL. Placeholders the caller
// fills: the workspace id at args[1], limit and offset as the last two.
func buildDocSearchQuery(phrase string, terms []string) (string, []any) {
	phrase = strings.ToLower(phrase)
	lowered := make([]string, len(terms))
	for i, t := range terms {
		lowered[i] = strings.ToLower(t)
	}

	argIdx := 1
	args := []any{}
	nextArg := func(val any) string {
		args = append(args, val)
		s := fmt.Sprintf("$%d", argIdx)
		argIdx++
		return s
	}

	phraseParam := nextArg(escapeLike(phrase))
	wsParam := nextArg(nil)
	contains := func(p string) string { return "'%' || " + p + " || '%'" }
	title := "LOWER(d.title)"
	content := "LOWER(d.content)"

	var termParams []string
	if len(lowered) > 1 {
		for _, t := range lowered {
			termParams = append(termParams, nextArg(escapeLike(t)))
		}
	}

	where := fmt.Sprintf("(%s LIKE %s OR %s LIKE %s)", title, contains(phraseParam), content, contains(phraseParam))
	allTermsInTitle := ""
	if len(termParams) > 1 {
		var each, inTitle []string
		for _, tp := range termParams {
			each = append(each, fmt.Sprintf("(%s LIKE %s OR %s LIKE %s)", title, contains(tp), content, contains(tp)))
			inTitle = append(inTitle, fmt.Sprintf("%s LIKE %s", title, contains(tp)))
		}
		where = "(" + where + " OR (" + strings.Join(each, " AND ") + "))"
		allTermsInTitle = "(" + strings.Join(inTitle, " AND ") + ")"
	}

	// strpos and the exact-title tier take the raw (unescaped) phrase; LIKE
	// takes the escaped one, so a title with % or _ still ranks as exact.
	rawPhrase := nextArg(phrase)
	rankCases := []string{
		fmt.Sprintf("WHEN %s = %s THEN 0", title, rawPhrase),
		fmt.Sprintf("WHEN %s LIKE %s || '%%' THEN 1", title, phraseParam),
		fmt.Sprintf("WHEN %s LIKE %s THEN 2", title, contains(phraseParam)),
	}
	matchSource := fmt.Sprintf("WHEN %s LIKE %s THEN 'title'", title, contains(phraseParam))
	if allTermsInTitle != "" {
		rankCases = append(rankCases, fmt.Sprintf("WHEN %s THEN 3", allTermsInTitle))
		matchSource += fmt.Sprintf(" WHEN %s THEN 'title'", allTermsInTitle)
	}
	rankCases = append(rankCases, fmt.Sprintf("WHEN %s LIKE %s THEN 4", content, contains(phraseParam)))
	rank := "CASE " + strings.Join(rankCases, " ") + " ELSE 5 END"

	// A multi-term content hit anchors on the first term; the Go snippet then
	// centers on the earliest.
	anchor := rawPhrase
	if len(lowered) > 1 {
		anchor = nextArg(lowered[0])
	}
	window := fmt.Sprintf(
		`substring(d.content from greatest(
			CASE WHEN strpos(%[1]s, %[2]s) > 0 THEN strpos(%[1]s, %[2]s) ELSE strpos(%[1]s, %[3]s) END - %[4]d, 1
		) for %[5]d)`,
		content, rawPhrase, anchor, docSearchWindowBefore, docSearchWindowLength,
	)

	limitParam := nextArg(nil)
	offsetParam := nextArg(nil)
	query := fmt.Sprintf(`SELECT d.id, d.workspace_id, d.folder_id, d.title, d.revision, d.version_count,
		d.created_by_type, d.created_by_id, d.updated_by_type, d.updated_by_id, d.created_at, d.updated_at,
		CASE %s ELSE 'content' END AS match_source,
		%s AS content_window
	FROM doc d
	WHERE d.workspace_id = %s AND %s
	ORDER BY %s, d.updated_at DESC
	LIMIT %s OFFSET %s`,
		matchSource, window, wsParam, where, rank, limitParam, offsetParam)
	return query, args
}

func (h *Handler) SearchDocs(w http.ResponseWriter, r *http.Request) {
	_, wsUUID, workspaceID, ok := h.docWorkspace(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeError(w, http.StatusBadRequest, "q parameter is required")
		return
	}
	limit := docSearchDefaultLimit
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = min(v, docSearchMaxLimit)
	}
	offset := 0
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v >= 0 {
		offset = v
	}

	sqlQuery, args := buildDocSearchQuery(q, splitSearchTerms(q))
	args[1] = wsUUID
	args[len(args)-2] = limit
	args[len(args)-1] = offset

	resp := []SearchDocResponse{}
	err := runSearchQuery(ctx, h.TxStarter, sqlQuery, args, func(rows pgx.Rows) error {
		for rows.Next() {
			var (
				id, ws, folder, createdBy, updatedBy pgtype.UUID
				createdAt, updatedAt                 pgtype.Timestamptz
				title, createdType, updatedType      string
				matchSource, window                  string
				revision                             int64
				versionCount                         int32
			)
			if err := rows.Scan(&id, &ws, &folder, &title, &revision, &versionCount,
				&createdType, &createdBy, &updatedType, &updatedBy, &createdAt, &updatedAt,
				&matchSource, &window); err != nil {
				return fmt.Errorf("scan: %w", err)
			}
			hit := SearchDocResponse{
				DocSummaryResponse: DocSummaryResponse{
					ID: uuidToString(id), WorkspaceID: uuidToString(ws), FolderID: uuidToPtr(folder),
					Title: title, Revision: revision, VersionCount: versionCount,
					CreatedByType: createdType, CreatedByID: uuidToString(createdBy),
					UpdatedByType: updatedType, UpdatedByID: uuidToString(updatedBy),
					CreatedAt: timestampToString(createdAt), UpdatedAt: timestampToString(updatedAt),
				},
				MatchSource: matchSource,
			}
			if matchSource == "content" && window != "" {
				snippet := extractSnippet(window, q)
				hit.MatchedSnippet = &snippet
			}
			resp = append(resp, hit)
		}
		return rows.Err()
	})
	if err != nil {
		if writeQueryCancellation(w, r, err) {
			return
		}
		if isSearchStatementTimeout(err) {
			slog.Warn("search docs timed out", "workspace_id", workspaceID, "query", q, "timeout", searchStatementTimeout)
			writeError(w, http.StatusServiceUnavailable, "search timed out; please refine your query or try again")
			return
		}
		slog.Warn("search docs failed", "error", err, "workspace_id", workspaceID, "query", q)
		writeError(w, http.StatusInternalServerError, "failed to search docs")
		return
	}
	if writeQueryCancellation(w, r, ctx.Err()) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"docs": resp})
}
