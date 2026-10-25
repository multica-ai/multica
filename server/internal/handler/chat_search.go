package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Chat message search (MUL-7754's search surfaces, extended to chat).
//
// SearchIssues and SearchProjects answer workspace-visible text; chat sessions
// are readable only by their creator (loadChatSessionForUser), so this search
// is creator-scoped inside the workspace instead. It deliberately does not
// re-check private-agent access: these are the member's own conversations, the
// local index could not stay in step with live permission flips, and opening a
// hit whose agent access was revoked still lands on the access-revoked banner.

// SearchChatMessageSession names the conversation a hit belongs to.
type SearchChatMessageSession struct {
	ID      string `json:"id"`
	AgentID string `json:"agent_id"`
	Title   string `json:"title"`
	Status  string `json:"status"`
}

type SearchChatMessageResponse struct {
	ID             string                   `json:"id"`
	ChatSessionID  string                   `json:"chat_session_id"`
	Role           string                   `json:"role"`
	Content        string                   `json:"content"`
	CreatedAt      string                   `json:"created_at"`
	MatchSource    string                   `json:"match_source"`
	MatchedSnippet *string                  `json:"matched_snippet"`
	Session        SearchChatMessageSession `json:"session"`
}

// SearchChatMessages searches the requesting member's own chat messages across
// every session of the workspace (active and archived), newest first.
func (h *Handler) SearchChatMessages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
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

	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	creatorUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	terms := splitSearchTerms(q)

	sqlQuery, args := buildChatMessageSearchQuery(q, terms)
	// Fill placeholder args: $1 = workspace_id, $2 = creator_id, last two = limit, offset
	args[1] = wsUUID
	args[2] = creatorUUID
	args[len(args)-2] = limit
	args[len(args)-1] = offset

	var results []chatMessageSearchRow
	err := runSearchQuery(ctx, h.TxStarter, sqlQuery, args, func(rows pgx.Rows) error {
		for rows.Next() {
			var row chatMessageSearchRow
			if err := rows.Scan(
				&row.ID,
				&row.ChatSessionID,
				&row.Role,
				&row.Content,
				&row.CreatedAt,
			); err != nil {
				return fmt.Errorf("scan: %w", err)
			}
			results = append(results, row)
		}
		return rows.Err()
	})
	if err != nil {
		if writeSearchCancellation(w, r, err) {
			return
		}
		// Statement-timeout surfaces as SQLSTATE 57014 — same fail-fast
		// contract as SearchIssues and SearchProjects (see runSearchQuery).
		if isSearchStatementTimeout(err) {
			slog.Warn("search chat messages timed out",
				"workspace_id", wsUUID,
				"query", q,
				"timeout", searchStatementTimeout)
			writeError(w, http.StatusServiceUnavailable, "search timed out; please refine your query or try again")
			return
		}
		slog.Warn("search chat messages failed", "error", err, "query", q)
		writeError(w, http.StatusInternalServerError, "failed to search chat messages")
		return
	}
	if writeSearchCancellation(w, r, ctx.Err()) {
		return
	}

	sessions := h.searchChatMessageSessions(ctx, wsUUID, creatorUUID, results)

	resp := make([]SearchChatMessageResponse, len(results))
	for i, row := range results {
		snippet := extractSnippet(row.Content, q)
		resp[i] = SearchChatMessageResponse{
			ID:             uuidToString(row.ID),
			ChatSessionID:  uuidToString(row.ChatSessionID),
			Role:           row.Role,
			Content:        row.Content,
			CreatedAt:      row.CreatedAt.Time.UTC().Format(time.RFC3339Nano),
			MatchSource:    "content",
			MatchedSnippet: &snippet,
			Session:        sessions[uuidToString(row.ChatSessionID)],
		}
	}

	if writeSearchCancellation(w, r, ctx.Err()) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"messages": resp,
	})
}

type chatMessageSearchRow struct {
	ID            pgtype.UUID
	ChatSessionID pgtype.UUID
	Role          string
	Content       string
	CreatedAt     pgtype.Timestamptz
}

// searchChatMessageSessions loads the sessions of one result page so each hit
// can name its conversation. A missing session (deleted between the search and
// this read) leaves the zero value; the hit still navigates, and the chat page
// reports the missing thread.
func (h *Handler) searchChatMessageSessions(
	ctx context.Context,
	wsUUID, creatorUUID pgtype.UUID,
	results []chatMessageSearchRow,
) map[string]SearchChatMessageSession {
	out := make(map[string]SearchChatMessageSession, len(results))
	if len(results) == 0 {
		return out
	}
	seen := make(map[pgtype.UUID]bool, len(results))
	ids := make([]pgtype.UUID, 0, len(results))
	for _, row := range results {
		if !seen[row.ChatSessionID] {
			seen[row.ChatSessionID] = true
			ids = append(ids, row.ChatSessionID)
		}
	}
	sessions, err := h.Queries.ListSearchIndexChatSessionsByIDs(ctx, db.ListSearchIndexChatSessionsByIDsParams{
		WorkspaceID: wsUUID,
		CreatorID:   creatorUUID,
		Ids:         ids,
	})
	if err != nil {
		slog.WarnContext(ctx, "search chat message sessions failed", "error", err)
		return out
	}
	for _, s := range sessions {
		out[uuidToString(s.ID)] = SearchChatMessageSession{
			ID:      uuidToString(s.ID),
			AgentID: uuidToString(s.AgentID),
			Title:   s.Title,
			Status:  s.Status,
		}
	}
	return out
}

// buildChatMessageSearchQuery mirrors buildProjectSearchQuery's phrase and
// multi-word AND matching, scoped to the creator's messages. Ranking is a
// single relevance tier ahead of recency: chat hits are read as "that
// conversation", where newest-first is the order people expect (every chat
// client's in-conversation search works this way).
func buildChatMessageSearchQuery(phrase string, terms []string) (string, []any) {
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
	phraseParam := nextArg(escapedPhrase)
	phraseContains := "'%' || " + phraseParam + " || '%'"

	wsParam := nextArg(nil)      // workspace_id placeholder
	creatorParam := nextArg(nil) // creator_id placeholder

	var termParams []string
	if len(terms) > 1 {
		for _, t := range terms {
			et := escapeLike(t)
			termParams = append(termParams, nextArg(et))
		}
	}

	// --- WHERE clause ---
	var whereParts []string

	whereParts = append(whereParts, fmt.Sprintf("LOWER(m.content) LIKE %s", phraseContains))

	if len(termParams) > 1 {
		var termConditions []string
		for _, tp := range termParams {
			termConditions = append(termConditions, fmt.Sprintf("LOWER(m.content) LIKE '%%' || %s || '%%'", tp))
		}
		whereParts = append(whereParts, "("+strings.Join(termConditions, " AND ")+")")
	}

	whereClause := "(" + strings.Join(whereParts, " OR ") + ")"

	// --- ORDER BY ranking ---
	rankExpr := fmt.Sprintf("CASE WHEN LOWER(m.content) LIKE %s THEN 0 ELSE 1 END", phraseContains)

	limitParam := nextArg(nil)
	offsetParam := nextArg(nil)

	query := fmt.Sprintf(`SELECT m.id, m.chat_session_id, m.role, m.content, m.created_at
		FROM chat_message m
		JOIN chat_session s ON s.id = m.chat_session_id
		WHERE s.workspace_id = %s AND s.creator_id = %s
		  AND m.message_kind <> 'onboarding_kickoff'
		  AND %s
		ORDER BY %s, m.created_at DESC, m.id DESC
		LIMIT %s OFFSET %s`,
		wsParam,
		creatorParam,
		whereClause,
		rankExpr,
		limitParam,
		offsetParam,
	)

	return query, args
}
