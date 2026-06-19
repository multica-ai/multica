// CEREBRO-PATCH(channel-message-search): FIR-407 — per-channel/DM message search.
//
// Net-new fork file. Searches the messages (comments) of a single channel or
// DM via the Postgres full-text index added in migration 9055
// (comment.search_tsv) plus a plain substring fallback, so a user can find an
// old link or note in a long conversation without scrolling. Scoped to one
// conversation and access-controlled the same way GetChannel is (the requester
// must be a subscriber), which is exactly what makes private DMs searchable
// only by their participants.
package handler

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ChannelMessageSearchResult is one matching message in a channel/DM.
type ChannelMessageSearchResult struct {
	ID         string  `json:"id"`
	ParentID   *string `json:"parent_id"`
	AuthorType string  `json:"author_type"`
	AuthorID   string  `json:"author_id"`
	Content    string  `json:"content"`
	Snippet    string  `json:"snippet"`
	CreatedAt  string  `json:"created_at"`
}

// ChannelMessageSearchResponse is the payload for GET
// /api/channels/{id}/messages/search. Messages are returned in chronological
// order (oldest first) so the frontend can step through them in the same order
// they appear in the conversation. Total is the full match count before the
// limit is applied.
type ChannelMessageSearchResponse struct {
	Messages []ChannelMessageSearchResult `json:"messages"`
	Total    int64                        `json:"total"`
}

const (
	channelSearchDefaultLimit = 100
	channelSearchMaxLimit     = 500
)

// SearchChannelMessages handles GET /api/channels/{id}/messages/search?q=...
func (h *Handler) SearchChannelMessages(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	id := chi.URLParam(r, "id")

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	channelUUID, ok := parseUUIDOrBadRequest(w, id, "id")
	if !ok {
		return
	}

	// Resolve the channel and enforce access — identical rule to GetChannel:
	// it must be a channel/DM and the caller must be a subscriber. A private
	// DM only has its two participants as subscribers, so this is what scopes
	// DM search to the people in the conversation.
	issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID:          channelUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	if issue.Kind != ChannelKindChannel && issue.Kind != ChannelKindDM {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	subscribed, err := h.Queries.IsIssueSubscriber(r.Context(), db.IsIssueSubscriberParams{
		IssueID:  issue.ID,
		UserType: "member",
		UserID:   parseUUID(userID),
	})
	if err != nil || !subscribed {
		writeError(w, http.StatusForbidden, "not a participant of this channel")
		return
	}

	rawQ := strings.TrimSpace(r.URL.Query().Get("q"))
	if rawQ == "" {
		writeError(w, http.StatusBadRequest, "q parameter is required")
		return
	}

	limit := channelSearchDefaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, parseErr := strconv.Atoi(v); parseErr == nil && n > 0 {
			limit = n
		}
	}
	if limit > channelSearchMaxLimit {
		limit = channelSearchMaxLimit
	}

	likePattern := "%" + escapeLikePattern(strings.ToLower(rawQ)) + "%"

	// FTS (token match, language-neutral 'simple' config) OR a plain substring
	// fallback so exact phrases and URL fragments — which the tsvector
	// tokenizer splits awkwardly — still match. No trigram operator here so the
	// query never depends on pg_trgm being installed (migration 9055 makes the
	// trigram indexes best-effort). count(*) OVER() carries the pre-limit total.
	const sql = `
SELECT c.id, c.parent_id, c.author_type, c.author_id, c.content, c.created_at,
       count(*) OVER() AS total
FROM comment c
WHERE c.issue_id = $1
  AND c.workspace_id = $2
  AND c.type = 'comment'
  AND (
    c.search_tsv @@ websearch_to_tsquery('simple', $3)
    OR lower(c.content) LIKE $4 ESCAPE '\'
  )
ORDER BY c.created_at ASC, c.id ASC
LIMIT $5`

	rows, err := h.DB.Query(r.Context(), sql, issue.ID, wsUUID, rawQ, likePattern, limit)
	if err != nil {
		slog.Warn("channel message search failed", "error", err, "channel_id", id)
		writeError(w, http.StatusInternalServerError, "failed to search messages")
		return
	}
	defer rows.Close()

	messages := make([]ChannelMessageSearchResult, 0)
	var total int64
	for rows.Next() {
		var (
			cID       pgtype.UUID
			parentID  pgtype.UUID
			authType  string
			authID    pgtype.UUID
			content   string
			createdAt pgtype.Timestamptz
			rowTotal  int64
		)
		if scanErr := rows.Scan(&cID, &parentID, &authType, &authID, &content, &createdAt, &rowTotal); scanErr != nil {
			slog.Warn("channel message search scan failed", "error", scanErr, "channel_id", id)
			writeError(w, http.StatusInternalServerError, "failed to search messages")
			return
		}
		total = rowTotal
		messages = append(messages, ChannelMessageSearchResult{
			ID:         uuidToString(cID),
			ParentID:   uuidToPtr(parentID),
			AuthorType: authType,
			AuthorID:   uuidToString(authID),
			Content:    content,
			Snippet:    extractSnippet(content, rawQ),
			CreatedAt:  timestampToString(createdAt),
		})
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		slog.Warn("channel message search rows error", "error", rowsErr, "channel_id", id)
		writeError(w, http.StatusInternalServerError, "failed to search messages")
		return
	}

	w.Header().Set("X-Total-Count", strconv.FormatInt(total, 10))
	writeJSON(w, http.StatusOK, ChannelMessageSearchResponse{Messages: messages, Total: total})
}

// escapeLikePattern escapes the LIKE metacharacters so a query containing %,
// _, or \ is matched literally (the SQL uses ESCAPE '\').
func escapeLikePattern(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// ---------------------------------------------------------------------------
// Workspace-admin cross-conversation search (FIR-407, agent-coaching follow-up)
// ---------------------------------------------------------------------------

// AdminChannelMessageSearchResult is one matching message together with the
// conversation it belongs to, so an admin (or the coaching agent) can see where
// it came from and jump to it.
type AdminChannelMessageSearchResult struct {
	ID           string  `json:"id"`
	ChannelID    string  `json:"channel_id"`
	ChannelKind  string  `json:"channel_kind"`
	ChannelTitle string  `json:"channel_title"`
	ParentID     *string `json:"parent_id"`
	AuthorType   string  `json:"author_type"`
	AuthorID     string  `json:"author_id"`
	Content      string  `json:"content"`
	Snippet      string  `json:"snippet"`
	CreatedAt    string  `json:"created_at"`
}

// AdminChannelMessageSearchResponse is the payload for the workspace-wide admin
// search. Newest match first (it is a results list, not an in-conversation
// scroll). Total is the full match count before the limit.
type AdminChannelMessageSearchResponse struct {
	Messages []AdminChannelMessageSearchResult `json:"messages"`
	Total    int64                             `json:"total"`
}

// SearchAllChannelMessages handles GET /api/cerebro/channel-messages/search?q=
//
// Workspace-admin/owner only: it searches messages across EVERY channel and DM
// in the workspace — including private DMs the caller is not part of — so the
// coaching agent (and, later, brain) can review the whole conversation history.
// This deliberately bypasses the per-channel subscriber check that scopes the
// normal in-conversation search; the admin/owner gate is the access boundary.
func (h *Handler) SearchAllChannelMessages(w http.ResponseWriter, r *http.Request) {
	member, hasMember := ctxMember(r.Context())
	if !hasMember || !isWorkspaceAdmin(member) {
		writeError(w, http.StatusForbidden, "workspace admin or owner required")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	rawQ := strings.TrimSpace(r.URL.Query().Get("q"))
	if rawQ == "" {
		writeError(w, http.StatusBadRequest, "q parameter is required")
		return
	}

	limit := channelSearchDefaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, parseErr := strconv.Atoi(v); parseErr == nil && n > 0 {
			limit = n
		}
	}
	if limit > channelSearchMaxLimit {
		limit = channelSearchMaxLimit
	}

	likePattern := "%" + escapeLikePattern(strings.ToLower(rawQ)) + "%"

	const sql = `
SELECT c.id, c.issue_id, i.kind, i.title, c.parent_id, c.author_type, c.author_id, c.content, c.created_at,
       count(*) OVER() AS total
FROM comment c
JOIN issue i ON i.id = c.issue_id
WHERE c.workspace_id = $1
  AND i.kind IN ('channel', 'dm')
  AND c.type = 'comment'
  AND (
    c.search_tsv @@ websearch_to_tsquery('simple', $2)
    OR lower(c.content) LIKE $3 ESCAPE '\'
  )
ORDER BY c.created_at DESC, c.id DESC
LIMIT $4`

	rows, err := h.DB.Query(r.Context(), sql, wsUUID, rawQ, likePattern, limit)
	if err != nil {
		slog.Warn("admin channel message search failed", "error", err, "workspace_id", workspaceID)
		writeError(w, http.StatusInternalServerError, "failed to search messages")
		return
	}
	defer rows.Close()

	messages := make([]AdminChannelMessageSearchResult, 0)
	var total int64
	for rows.Next() {
		var (
			cID       pgtype.UUID
			issueID   pgtype.UUID
			kind      string
			title     string
			parentID  pgtype.UUID
			authType  string
			authID    pgtype.UUID
			content   string
			createdAt pgtype.Timestamptz
			rowTotal  int64
		)
		if scanErr := rows.Scan(&cID, &issueID, &kind, &title, &parentID, &authType, &authID, &content, &createdAt, &rowTotal); scanErr != nil {
			slog.Warn("admin channel message search scan failed", "error", scanErr, "workspace_id", workspaceID)
			writeError(w, http.StatusInternalServerError, "failed to search messages")
			return
		}
		total = rowTotal
		messages = append(messages, AdminChannelMessageSearchResult{
			ID:           uuidToString(cID),
			ChannelID:    uuidToString(issueID),
			ChannelKind:  kind,
			ChannelTitle: title,
			ParentID:     uuidToPtr(parentID),
			AuthorType:   authType,
			AuthorID:     uuidToString(authID),
			Content:      content,
			Snippet:      extractSnippet(content, rawQ),
			CreatedAt:    timestampToString(createdAt),
		})
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		slog.Warn("admin channel message search rows error", "error", rowsErr, "workspace_id", workspaceID)
		writeError(w, http.StatusInternalServerError, "failed to search messages")
		return
	}

	w.Header().Set("X-Total-Count", strconv.FormatInt(total, 10))
	writeJSON(w, http.StatusOK, AdminChannelMessageSearchResponse{Messages: messages, Total: total})
}

// ---------------------------------------------------------------------------
// Per-user cross-conversation search (FIR-407, global-search follow-up)
// ---------------------------------------------------------------------------

// SearchMyChannelMessages handles GET /api/cerebro/my/channel-messages/search?q=
//
// Powers the global Cmd+K search: it searches messages across EVERY channel and
// DM the caller participates in (is a subscriber of), so a user can find an old
// link or note from any of their conversations in one place — without first
// opening the conversation. It is the personal, access-scoped counterpart to the
// admin-only SearchAllChannelMessages: the subscriber filter is the access
// boundary, so a private DM only surfaces for its participants. Same FTS index
// (comment.search_tsv) plus the substring fallback, newest match first.
func (h *Handler) SearchMyChannelMessages(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	rawQ := strings.TrimSpace(r.URL.Query().Get("q"))
	if rawQ == "" {
		writeError(w, http.StatusBadRequest, "q parameter is required")
		return
	}

	limit := channelSearchDefaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, parseErr := strconv.Atoi(v); parseErr == nil && n > 0 {
			limit = n
		}
	}
	if limit > channelSearchMaxLimit {
		limit = channelSearchMaxLimit
	}

	likePattern := "%" + escapeLikePattern(strings.ToLower(rawQ)) + "%"

	const sql = `
SELECT c.id, c.issue_id, i.kind, i.title, c.parent_id, c.author_type, c.author_id, c.content, c.created_at,
       count(*) OVER() AS total
FROM comment c
JOIN issue i ON i.id = c.issue_id
WHERE c.workspace_id = $1
  AND i.kind IN ('channel', 'dm')
  AND c.type = 'comment'
  AND EXISTS (
    SELECT 1 FROM issue_subscriber s
    WHERE s.issue_id = i.id AND s.user_type = 'member' AND s.user_id = $2
  )
  AND (
    c.search_tsv @@ websearch_to_tsquery('simple', $3)
    OR lower(c.content) LIKE $4 ESCAPE '\'
  )
ORDER BY c.created_at DESC, c.id DESC
LIMIT $5`

	rows, err := h.DB.Query(r.Context(), sql, wsUUID, parseUUID(userID), rawQ, likePattern, limit)
	if err != nil {
		slog.Warn("my channel message search failed", "error", err, "workspace_id", workspaceID)
		writeError(w, http.StatusInternalServerError, "failed to search messages")
		return
	}
	defer rows.Close()

	messages := make([]AdminChannelMessageSearchResult, 0)
	var total int64
	for rows.Next() {
		var (
			cID       pgtype.UUID
			issueID   pgtype.UUID
			kind      string
			title     string
			parentID  pgtype.UUID
			authType  string
			authID    pgtype.UUID
			content   string
			createdAt pgtype.Timestamptz
			rowTotal  int64
		)
		if scanErr := rows.Scan(&cID, &issueID, &kind, &title, &parentID, &authType, &authID, &content, &createdAt, &rowTotal); scanErr != nil {
			slog.Warn("my channel message search scan failed", "error", scanErr, "workspace_id", workspaceID)
			writeError(w, http.StatusInternalServerError, "failed to search messages")
			return
		}
		total = rowTotal
		messages = append(messages, AdminChannelMessageSearchResult{
			ID:           uuidToString(cID),
			ChannelID:    uuidToString(issueID),
			ChannelKind:  kind,
			ChannelTitle: title,
			ParentID:     uuidToPtr(parentID),
			AuthorType:   authType,
			AuthorID:     uuidToString(authID),
			Content:      content,
			Snippet:      extractSnippet(content, rawQ),
			CreatedAt:    timestampToString(createdAt),
		})
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		slog.Warn("my channel message search rows error", "error", rowsErr, "workspace_id", workspaceID)
		writeError(w, http.StatusInternalServerError, "failed to search messages")
		return
	}

	w.Header().Set("X-Total-Count", strconv.FormatInt(total, 10))
	writeJSON(w, http.StatusOK, AdminChannelMessageSearchResponse{Messages: messages, Total: total})
}
