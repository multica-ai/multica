package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service/channel"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ChannelMessageResponse is the JSON shape returned for channel messages.
type ChannelMessageResponse struct {
	ID              string  `json:"id"`
	ChannelID       string  `json:"channel_id"`
	AuthorType      string  `json:"author_type"`
	AuthorID        string  `json:"author_id"`
	Content         string  `json:"content"`
	ParentMessageID *string `json:"parent_message_id"`
	EditedAt        *string `json:"edited_at"`
	DeletedAt       *string `json:"deleted_at"`
	CreatedAt       string  `json:"created_at"`
}

func channelMessageToResponse(m db.ChannelMessage) ChannelMessageResponse {
	resp := ChannelMessageResponse{
		ID:         uuidToString(m.ID),
		ChannelID:  uuidToString(m.ChannelID),
		AuthorType: m.AuthorType,
		AuthorID:   uuidToString(m.AuthorID),
		Content:    m.Content,
		CreatedAt:  timestampToString(m.CreatedAt),
	}
	if m.ParentMessageID.Valid {
		s := uuidToString(m.ParentMessageID)
		resp.ParentMessageID = &s
	}
	if m.EditedAt.Valid {
		s := timestampToString(m.EditedAt)
		resp.EditedAt = &s
	}
	if m.DeletedAt.Valid {
		s := timestampToString(m.DeletedAt)
		resp.DeletedAt = &s
	}
	return resp
}

// ListChannelMessages handles GET /api/channels/{channelId}/messages.
//
// Query params:
//   - `before` — RFC3339 timestamp; returns messages strictly older. Default
//     (omitted) returns the newest page.
//   - `limit` — integer 1..200; default 50.
//   - `include_threaded` — when "true", returns the full stream including
//     thread replies. Default omits replies (top-level only).
//
// Cursor pagination is by `created_at` rather than offset/limit because
// channels see frequent inserts at the head and offset would skip rows.
func (h *Handler) ListChannelMessages(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := h.requireChannelsEnabled(w, r)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	channelUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "channelId"), "channel id")
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, uuidToString(wsID))
	actorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor id")
	if !ok {
		return
	}
	if _, ok := h.requireChannelAccess(w, r, channelUUID, wsID, channel.Actor{Type: actorType, ID: actorUUID}); !ok {
		return
	}

	q := r.URL.Query()
	params := channel.ListMessagesParams{ChannelID: channelUUID}
	if v := q.Get("before"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid before parameter; expected RFC3339")
			return
		}
		ts := pgtype.Timestamptz{Time: t, Valid: true}
		params.BeforeCreatedAt = &ts
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "invalid limit parameter")
			return
		}
		params.Limit = int32(n)
	}
	if q.Get("include_threaded") == "true" {
		params.IncludeThreaded = true
	}

	msgs, err := h.ChannelMessageService.List(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list messages")
		return
	}
	out := make([]ChannelMessageResponse, len(msgs))
	for i, m := range msgs {
		out[i] = channelMessageToResponse(m)
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateChannelMessageRequest is the JSON body for POST /api/channels/{channelId}/messages.
type CreateChannelMessageRequest struct {
	Content         string  `json:"content"`
	ParentMessageID *string `json:"parent_message_id"`
}

// CreateChannelMessage handles POST /api/channels/{channelId}/messages.
//
// Side-effects (handler-only, per spec §4 service-handler split):
//   - publishes EventChannelMessage on the workspace bus
//   - for every `@member` mention, writes an inbox_item with
//     type='channel_mention' and the channel/message refs in `details`
//
// Phase 1 deliberately does NOT enqueue agent tasks for `@agent` mentions —
// that's Phase 3's job. Mentions still render in the UI (markdown), they
// just don't trigger tasks yet.
func (h *Handler) CreateChannelMessage(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := h.requireChannelsEnabled(w, r)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	channelUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "channelId"), "channel id")
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, uuidToString(wsID))
	actorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor id")
	if !ok {
		return
	}
	ch, ok := h.requireChannelAccess(w, r, channelUUID, wsID, channel.Actor{Type: actorType, ID: actorUUID})
	if !ok {
		return
	}

	var req CreateChannelMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := channel.CreateMessageParams{
		ChannelID: channelUUID,
		Author:    channel.Actor{Type: actorType, ID: actorUUID},
		Content:   req.Content,
	}
	if req.ParentMessageID != nil {
		parentUUID, ok := parseUUIDOrBadRequest(w, *req.ParentMessageID, "parent_message_id")
		if !ok {
			return
		}
		// Verify the parent belongs to this channel — we don't want
		// cross-channel thread reuse.
		parent, err := h.ChannelMessageService.Get(r.Context(), parentUUID)
		if err != nil || parent.ChannelID.Bytes != channelUUID.Bytes {
			writeError(w, http.StatusBadRequest, "invalid parent_message_id")
			return
		}
		params.ParentMessageID = &parentUUID
	}

	msg, err := h.ChannelMessageService.Create(r.Context(), params)
	if err != nil {
		status, msgStr := channelErrorStatus(err)
		writeError(w, status, msgStr)
		return
	}

	resp := channelMessageToResponse(msg)
	h.publish(protocol.EventChannelMessage, uuidToString(wsID), actorType, actorID, map[string]any{
		"channel_id":   uuidToString(channelUUID),
		"channel_name": ch.Name,
		"message":      resp,
	})

	// Mention → inbox. Best-effort: a failure to write inbox entries should
	// not 500 the message-create. We log and move on.
	go h.fanOutChannelMentions(context.Background(), wsID, ch, msg, actorType, actorID)

	writeJSON(w, http.StatusCreated, resp)
}

// fanOutChannelMentions writes inbox_item rows for every @member mention in
// a freshly-created channel message. Run in a goroutine because the user
// already has their 201 response — failing to notify isn't worth blocking
// the request.
//
// Phase 1 only handles `@member` mentions. `@agent` mentions are parsed
// (and rendered in the UI) but don't trigger tasks yet — see Phase 3.
//
// `@all` mentions: spec doesn't pin behavior. We skip them in Phase 1
// rather than fan out to every workspace member; a follow-up can decide
// whether they should produce inbox entries or live as render-only.
func (h *Handler) fanOutChannelMentions(ctx context.Context, workspaceID pgtype.UUID, ch db.Channel, msg db.ChannelMessage, authorType, authorID string) {
	mentions := util.ParseMentions(msg.Content)
	if len(mentions) == 0 {
		return
	}
	authorUUID, err := util.ParseUUID(authorID)
	if err != nil {
		slog.Warn("fanOutChannelMentions: invalid author id", "author_id", authorID, "error", err)
		return
	}
	channelIDStr := uuidToString(ch.ID)
	messageIDStr := uuidToString(msg.ID)
	displayName := ch.DisplayName
	if displayName == "" {
		displayName = ch.Name
	}

	for _, m := range mentions {
		if m.Type != channel.ActorMember {
			// Phase 1: agent mentions render but don't enqueue or write inbox.
			// @all is also skipped pending design.
			continue
		}
		if authorType == channel.ActorMember && m.ID == authorID {
			// Don't notify self.
			continue
		}
		recipientUUID, err := util.ParseUUID(m.ID)
		if err != nil {
			continue
		}
		details, err := json.Marshal(map[string]any{
			"channel_id":           channelIDStr,
			"channel_name":         ch.Name,
			"channel_display_name": ch.DisplayName,
			"message_id":           messageIDStr,
			"message_kind":         ch.Kind,
		})
		if err != nil {
			slog.Warn("fanOutChannelMentions: marshal details", "error", err)
			continue
		}
		_, err = h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			WorkspaceID:   workspaceID,
			RecipientType: channel.ActorMember,
			RecipientID:   recipientUUID,
			Type:          "channel_mention",
			Severity:      "info",
			IssueID:       pgtype.UUID{}, // not an issue
			Title:         "#" + displayName,
			Body:          pgtype.Text{String: snippet(msg.Content, 200), Valid: true},
			ActorType:     pgtype.Text{String: authorType, Valid: true},
			ActorID:       authorUUID,
			Details:       details,
		})
		if err != nil {
			slog.Warn("fanOutChannelMentions: create inbox", "recipient_id", m.ID, "error", err)
		}
	}
}

// snippet returns the first n runes of s, ending at a word boundary if one
// is reachable within 20% of the limit. Used for inbox notification bodies
// where a long message would dominate the inbox UI.
func snippet(s string, n int) string {
	if len(s) <= n {
		return s
	}
	runes := []rune(s)
	if len(runes) <= n {
		return string(runes)
	}
	cut := runes[:n]
	// Walk back up to 20% looking for whitespace.
	max := n - n/5
	for i := len(cut) - 1; i >= max; i-- {
		switch cut[i] {
		case ' ', '\n', '\t':
			return string(cut[:i]) + "…"
		}
	}
	return string(cut) + "…"
}
