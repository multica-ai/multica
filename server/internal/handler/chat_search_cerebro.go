// CEREBRO-PATCH(chat-search-cerebro): JEH-901 server-side chat session search
// powering Cmd+K. Net-new fork file — mirrors SearchIssues / SearchProjects
// patterns (LIKE on LOWER(content), pg_bigm trigram index on chat_message.content).
package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// SearchChatSessionResponse is one hit in the Cmd+K chat-sessions group.
// The shape is intentionally smaller than ChatSessionResponse: Cmd+K only
// needs enough to render a row + open the session.
type SearchChatSessionResponse struct {
	ChatSessionID  string  `json:"chat_session_id"`
	Title          string  `json:"title"`
	AgentID        string  `json:"agent_id"`
	MatchedSnippet *string `json:"matched_snippet,omitempty"`
	CreatedAt      string  `json:"created_at"`
}

// buildChatSessionSearchQuery returns the SQL that finds chat sessions whose
// messages match the search phrase, grouped per session with the newest
// matching message's content as the snippet source. Patterns are lowercased
// in Go so SQL only needs LOWER() on the column side, matching the pg_bigm
// 1.2 GIN index on chat_message.content.
//
// Args layout (filled by caller, mirrors buildSearchQuery):
//
//	$1: lowercased+escaped phrase
//	$2: workspace_id (placeholder, set by caller)
//	$3: creator_id   (placeholder, set by caller)
//	$N..: per-term args for multi-word AND match
//	last two: limit, offset
func buildChatSessionSearchQuery(phrase string, terms []string) (string, []any) {
	phrase = strings.ToLower(phrase)
	for i, t := range terms {
		terms[i] = strings.ToLower(t)
	}

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

	wsParam := nextArg(nil)      // $2 — workspace_id
	creatorParam := nextArg(nil) // $3 — creator_id

	// Multi-word AND match: each term must appear in the same message.
	// Single-word queries are already covered by the phrase parameter.
	var termParams []string
	if len(terms) > 1 {
		for _, t := range terms {
			et := escapeLike(t)
			termParams = append(termParams, nextArg(et))
		}
	}

	var matchParts []string
	matchParts = append(matchParts, fmt.Sprintf("LOWER(m.content) LIKE %s", phraseContains))
	if len(termParams) > 1 {
		var perTerm []string
		for _, tp := range termParams {
			tc := "'%' || " + tp + " || '%'"
			perTerm = append(perTerm, fmt.Sprintf("LOWER(m.content) LIKE %s", tc))
		}
		matchParts = append(matchParts, "("+strings.Join(perTerm, " AND ")+")")
	}
	matchClause := "(" + strings.Join(matchParts, " OR ") + ")"

	limitParam := nextArg(nil)
	offsetParam := nextArg(nil)

	// DISTINCT ON picks the newest matching message per session — the snippet
	// the user sees in the Cmd+K row. The outer SELECT joins back to
	// chat_session for the metadata Cmd+K needs (title, agent, created_at).
	// COUNT(*) OVER() returns the unfiltered total for pagination.
	query := fmt.Sprintf(`WITH latest_match AS (
		SELECT DISTINCT ON (m.chat_session_id)
			m.chat_session_id,
			m.content AS matched_content,
			m.created_at AS matched_created_at
		FROM chat_message m
		JOIN chat_session s ON s.id = m.chat_session_id
		WHERE s.workspace_id = %s
			AND s.creator_id = %s
			AND %s
		ORDER BY m.chat_session_id, m.created_at DESC
	)
	SELECT s.id, s.title, s.agent_id, s.created_at,
		lm.matched_content,
		COUNT(*) OVER() AS total_count
	FROM chat_session s
	JOIN latest_match lm ON lm.chat_session_id = s.id
	WHERE s.workspace_id = %s AND s.creator_id = %s
	ORDER BY lm.matched_created_at DESC
	LIMIT %s OFFSET %s`,
		wsParam, creatorParam, matchClause,
		wsParam, creatorParam,
		limitParam, offsetParam,
	)

	return query, args
}

// SearchChatSessions returns chat sessions owned by the caller whose messages
// match the query. Results are grouped per session — the newest matching
// message's content drives the snippet. Mirrors SearchIssues / SearchProjects:
// LIKE on LOWER(content) backed by the pg_bigm GIN index from migration
// 9016_cerebro_chat_message_search_index.
func (h *Handler) SearchChatSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
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

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	creatorUUID, ok := parseUUIDOrBadRequest(w, userID, "user_id")
	if !ok {
		return
	}

	terms := splitSearchTerms(q)
	sqlQuery, args := buildChatSessionSearchQuery(q, terms)
	// Fill placeholder args: $2=workspace_id, $3=creator_id, last two = limit/offset.
	args[1] = wsUUID
	args[2] = creatorUUID
	args[len(args)-2] = limit
	args[len(args)-1] = offset

	rows, err := h.DB.Query(ctx, sqlQuery, args...)
	if err != nil {
		slog.Warn("search chat sessions failed", "error", err, "workspace_id", workspaceID, "query", q)
		writeError(w, http.StatusInternalServerError, "failed to search chat sessions")
		return
	}
	defer rows.Close()

	type chatSearchRow struct {
		session        db.ChatSession
		matchedContent string
		totalCount     int64
	}

	var results []chatSearchRow
	for rows.Next() {
		var row chatSearchRow
		var (
			id         pgtype.UUID
			title      string
			agentID    pgtype.UUID
			createdAt  pgtype.Timestamptz
			matched    string
			totalCount int64
		)
		if err := rows.Scan(&id, &title, &agentID, &createdAt, &matched, &totalCount); err != nil {
			slog.Warn("search chat sessions scan failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to search chat sessions")
			return
		}
		row.session.ID = id
		row.session.Title = title
		row.session.AgentID = agentID
		row.session.CreatedAt = createdAt
		row.matchedContent = matched
		row.totalCount = totalCount
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("search chat sessions rows error", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to search chat sessions")
		return
	}

	var total int64
	if len(results) > 0 {
		total = results[0].totalCount
	}

	resp := make([]SearchChatSessionResponse, len(results))
	for i, row := range results {
		entry := SearchChatSessionResponse{
			ChatSessionID: uuidToString(row.session.ID),
			Title:         row.session.Title,
			AgentID:       uuidToString(row.session.AgentID),
			CreatedAt:     timestampToString(row.session.CreatedAt),
		}
		if row.matchedContent != "" {
			snippet := extractSnippet(row.matchedContent, q)
			entry.MatchedSnippet = &snippet
		}
		resp[i] = entry
	}

	w.Header().Set("X-Total-Count", strconv.FormatInt(total, 10))
	writeJSON(w, http.StatusOK, map[string]any{
		"chat_sessions": resp,
		"total":         total,
	})
}
