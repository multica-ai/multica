package note

// FIR-2022 — full-text search over documents (every artifact kind: report /
// plan / decision / diagram / note) AND their comments. This mirrors the
// issue-search pipeline (server/internal/cerebro/search + handler/search_cerebro.go)
// but targets the artifact + cerebro_note_comment tables and applies the same
// per-document access rule the Notes/Documents API already enforces
// (CanUserSeeNote / CanUserCommentOnArtifact in note.sql):
//
//   folder chain visible to the user  AND
//   ( plain document (no cerebro_note row)  OR  owner  OR  workspace-visible
//     OR  shared-with-this-user )
//
// The user is X-User-ID. For an agent caller that is the agent's owner (the
// auth middleware maps an agent token's owner onto X-User-ID), so an agent sees
// exactly what its owner may see — no private note ever leaks into agent search.
//
// Text matching: FTS on title (A) / body (B) via the generated search_tsv, a
// trigram fallback on title/body for spelling tolerance, and an FTS EXISTS over
// the document's comments. Comment matches stay strict FTS (no trigram OR) for
// the same planner reason documented in cerebro/search/query.go (FIR-2664).

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrosearch "github.com/multica-ai/multica/server/internal/cerebro/search"
)

// validSearchKind gates the optional kind filter on /api/notes/search.
var validSearchKind = map[string]bool{
	"report":   true,
	"plan":     true,
	"decision": true,
	"diagram":  true,
	"note":     true,
}

// NoteSearchResult is one matched document. MatchSource is "title", "body" or
// "comment"; Snippet is a short excerpt of whatever carried the strongest
// signal so the caller can show "why this matched". MatchedCommentSnippet is
// populated whenever a comment matched, even when the title also matched, so a
// title hit can still surface the comment that mentioned the query.
type NoteSearchResult struct {
	ID                    string  `json:"id"`
	WorkspaceID           string  `json:"workspace_id"`
	Kind                  string  `json:"kind"`
	Title                 string  `json:"title"`
	FolderID              *string `json:"folder_id"`
	IssueID               *string `json:"issue_id"`
	ProjectID             *string `json:"project_id"`
	OwnerID               *string `json:"owner_id"`
	Visibility            *string `json:"visibility"`
	UpdatedAt             string  `json:"updated_at"`
	MatchSource           string  `json:"match_source"`
	Snippet               string  `json:"snippet,omitempty"`
	MatchedCommentSnippet *string `json:"matched_comment_snippet,omitempty"`
}

// NoteSearchResponse is the envelope returned by GET /api/notes/search.
type NoteSearchResponse struct {
	Results []NoteSearchResult `json:"results"`
	Total   int64              `json:"total"`
}

// SearchNotes handles GET /api/notes/search?q=&kind=&limit=&offset=.
func (h *Handler) SearchNotes(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ownerUUID, ok := h.scope(w, r, userID)
	if !ok {
		return
	}
	if h.Pool == nil {
		writeError(w, http.StatusServiceUnavailable, "search is not available")
		return
	}

	q := r.URL.Query()
	text := strings.TrimSpace(q.Get("q"))
	if text == "" {
		writeError(w, http.StatusBadRequest, "q parameter is required")
		return
	}
	kind := strings.ToLower(strings.TrimSpace(q.Get("kind")))
	if kind != "" && !validSearchKind[kind] {
		writeError(w, http.StatusBadRequest, "invalid kind")
		return
	}

	limit := 20
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 50 {
		limit = 50
	}
	offset := 0
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	sql, args := buildNoteSearchSQL(noteSearchInput{
		WorkspaceID: wsUUID,
		UserID:      ownerUUID,
		Text:        text,
		Kind:        kind,
		Limit:       limit,
		Offset:      offset,
	})

	// Run in a tx so we can SET LOCAL the pg_trgm word-similarity threshold for
	// the `<%` operator (same as the issue search — see search_cerebro.go).
	ctx := r.Context()
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		slog.Warn("note search begin tx failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to search notes")
		return
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL pg_trgm.word_similarity_threshold = "+cerebrosearch.TrigramThreshold); err != nil {
		slog.Warn("note search set trgm threshold failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to search notes")
		return
	}
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		slog.Warn("note search query failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to search notes")
		return
	}
	defer rows.Close()

	results := make([]NoteSearchResult, 0, limit)
	var total int64
	for rows.Next() {
		var (
			id, wsID, folderID, issueID, projectID, ownerID  pgtype.UUID
			kindVal, title, visibility, matchSource, comment pgtype.Text
			updatedAt                                        pgtype.Timestamptz
			body                                             pgtype.Text
			totalCount                                       int64
		)
		if err := rows.Scan(
			&id, &wsID, &kindVal, &title, &body, &folderID, &issueID, &projectID,
			&ownerID, &visibility, &updatedAt, &totalCount, &matchSource, &comment,
		); err != nil {
			slog.Warn("note search scan failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to search notes")
			return
		}
		total = totalCount
		res := NoteSearchResult{
			ID:          uuidStr(id),
			WorkspaceID: uuidStr(wsID),
			Kind:        kindVal.String,
			Title:       title.String,
			FolderID:    uuidPtr(folderID),
			IssueID:     uuidPtr(issueID),
			ProjectID:   uuidPtr(projectID),
			OwnerID:     uuidPtr(ownerID),
			MatchSource: matchSource.String,
			UpdatedAt:   tsStr(updatedAt),
		}
		if visibility.Valid {
			v := visibility.String
			res.Visibility = &v
		}
		// Snippet: from the field that carried the match.
		switch matchSource.String {
		case "body":
			res.Snippet = noteSnippet(body.String, text)
		case "comment":
			res.Snippet = noteSnippet(comment.String, text)
		default:
			res.Snippet = noteSnippet(title.String, text)
		}
		// A comment match is always surfaced separately so a title/body hit can
		// still show the comment that mentioned the query.
		if comment.Valid && comment.String != "" {
			cs := noteSnippet(comment.String, text)
			res.MatchedCommentSnippet = &cs
		}
		results = append(results, res)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("note search rows error", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to search notes")
		return
	}

	w.Header().Set("X-Total-Count", strconv.FormatInt(total, 10))
	writeJSON(w, http.StatusOK, NoteSearchResponse{Results: results, Total: total})
}

// noteSearchInput captures the resolved request for buildNoteSearchSQL.
type noteSearchInput struct {
	WorkspaceID pgtype.UUID
	UserID      pgtype.UUID
	Text        string
	Kind        string // "" = all kinds
	Limit       int
	Offset      int
}

// buildNoteSearchSQL assembles the FTS SELECT + bound args. Every value is
// filled before return (no unbound placeholders), matching cerebro/search.Build.
func buildNoteSearchSQL(in noteSearchInput) (string, []any) {
	args := []any{}
	next := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	wsParam := next(in.WorkspaceID)
	userParam := next(in.UserID)
	tsParam := next(in.Text)   // websearch_to_tsquery
	trgmParam := next(in.Text) // similarity / <% / LIKE
	rawParam := next(in.Text)  // exact-title compare
	startsParam := next(in.Text + "%")

	// Access rule — identical to CanUserCommentOnArtifact (note.sql): folder
	// chain visible AND (plain doc OR owner OR workspace OR shared-with-user).
	access := fmt.Sprintf(`cerebro_artifact_folder_visible(a.folder_id, %[1]s)
		AND (
			n.artifact_id IS NULL
			OR n.owner_id = %[1]s
			OR n.visibility = 'workspace'
			OR (n.visibility = 'shared' AND EXISTS (
				SELECT 1 FROM cerebro_note_share s
				WHERE s.artifact_id = n.artifact_id AND s.user_id = %[1]s))
		)`, userParam)

	// Text predicates: FTS on title+body, trigram fallback on title/body, and
	// strict-FTS EXISTS over the document's comments.
	textPred := fmt.Sprintf(`(
		a.search_tsv @@ websearch_to_tsquery('simple', %[1]s)
		OR %[2]s <%% lower(a.title)
		OR %[2]s <%% lower(coalesce(a.body, ''))
		OR EXISTS (
			SELECT 1 FROM cerebro_note_comment c
			WHERE c.note_id = a.id
			  AND c.search_tsv @@ websearch_to_tsquery('simple', %[1]s)
		)
	)`, tsParam, trgmParam)

	where := []string{
		fmt.Sprintf("a.workspace_id = %s", wsParam),
		access,
		textPred,
	}
	if in.Kind != "" {
		where = append(where, fmt.Sprintf("a.kind = %s", next(in.Kind)))
	}

	// match_source: which field carried the strongest signal.
	matchSource := fmt.Sprintf(`CASE
		WHEN a.search_tsv @@ websearch_to_tsquery('simple', %[1]s) THEN
			CASE
				WHEN lower(a.title) LIKE '%%' || lower(%[2]s) || '%%' THEN 'title'
				WHEN lower(coalesce(a.body, '')) LIKE '%%' || lower(%[2]s) || '%%' THEN 'body'
				ELSE 'title'
			END
		WHEN %[2]s <%% lower(a.title) THEN 'title'
		WHEN %[2]s <%% lower(coalesce(a.body, '')) THEN 'body'
		ELSE 'comment'
	END`, tsParam, trgmParam)

	// matched comment body (best-ranked) — strict FTS, mirrors the EXISTS.
	matchedComment := fmt.Sprintf(`COALESCE((
		SELECT c.body FROM cerebro_note_comment c
		WHERE c.note_id = a.id
		  AND c.search_tsv @@ websearch_to_tsquery('simple', %[1]s)
		ORDER BY ts_rank(c.search_tsv, websearch_to_tsquery('simple', %[1]s)) DESC NULLS LAST,
		         c.created_at DESC
		LIMIT 1
	), '')`, tsParam)

	// Composite relevance score.
	order := fmt.Sprintf(`(
		(CASE WHEN lower(a.title) = lower(%[3]s) THEN 50.0 ELSE 0.0 END)
		+ (CASE WHEN lower(a.title) LIKE lower(%[4]s) THEN 10.0 ELSE 0.0 END)
		+ (ts_rank(a.search_tsv, websearch_to_tsquery('simple', %[1]s)) * 10.0)
		+ COALESCE((
			SELECT MAX(ts_rank(c.search_tsv, websearch_to_tsquery('simple', %[1]s))) * 5.0
			FROM cerebro_note_comment c WHERE c.note_id = a.id
		), 0.0)
		+ (word_similarity(%[2]s, lower(a.title)) * 3.0)
		+ (word_similarity(%[2]s, lower(coalesce(a.body, ''))) * 1.0)
	) DESC, a.updated_at DESC`, tsParam, trgmParam, rawParam, startsParam)

	limitParam := next(in.Limit)
	offsetParam := next(in.Offset)

	sql := fmt.Sprintf(`SELECT a.id, a.workspace_id, a.kind, a.title, a.body,
		a.folder_id, a.issue_id, a.project_id,
		n.owner_id, n.visibility, a.updated_at,
		COUNT(*) OVER() AS total_count,
		%s AS match_source,
		%s AS matched_comment_body
	FROM artifact a
	LEFT JOIN cerebro_note n ON n.artifact_id = a.id
	WHERE %s
	ORDER BY %s
	LIMIT %s OFFSET %s`,
		matchSource,
		matchedComment,
		strings.Join(where, " AND "),
		order,
		limitParam,
		offsetParam,
	)
	return sql, args
}

// noteSnippet returns a short excerpt of text centred on the first query term,
// falling back to the head of the text when no term is found. It works on runes
// so multi-byte Danish characters (æøå) are never split mid-character.
func noteSnippet(text, query string) string {
	const window = 160
	clean := []rune(strings.Join(strings.Fields(text), " "))
	if len(clean) == 0 {
		return ""
	}
	lowerStr := strings.ToLower(string(clean))
	idx := -1
	for _, term := range strings.Fields(strings.ToLower(query)) {
		t := strings.TrimSpace(term)
		if t == "" {
			continue
		}
		// Byte offset → rune offset so windowing stays rune-aligned.
		if at := strings.Index(lowerStr, t); at >= 0 {
			idx = len([]rune(lowerStr[:at]))
			break
		}
	}
	if idx < 0 {
		if len(clean) <= window {
			return string(clean)
		}
		return string(clean[:window]) + "…"
	}
	start := idx - window/2
	if start < 0 {
		start = 0
	}
	end := start + window
	if end > len(clean) {
		end = len(clean)
	}
	snippet := string(clean[start:end])
	if start > 0 {
		snippet = "…" + snippet
	}
	if end < len(clean) {
		snippet = snippet + "…"
	}
	return snippet
}
