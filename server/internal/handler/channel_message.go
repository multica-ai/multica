package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/service/channel"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ChannelReactionResponse is the JSON shape for one emoji reaction. Mirrors
// the issue-comment ReactionResponse but scoped to channel_message rather
// than comment.
type ChannelReactionResponse struct {
	ID               string `json:"id"`
	ChannelMessageID string `json:"channel_message_id"`
	ActorType        string `json:"actor_type"`
	ActorID          string `json:"actor_id"`
	Emoji            string `json:"emoji"`
	CreatedAt        string `json:"created_at"`
}

func channelReactionToResponse(r db.ChannelMessageReaction) ChannelReactionResponse {
	return ChannelReactionResponse{
		ID:               uuidToString(r.ID),
		ChannelMessageID: uuidToString(r.ChannelMessageID),
		ActorType:        r.ActorType,
		ActorID:          uuidToString(r.ActorID),
		Emoji:            r.Emoji,
		CreatedAt:        timestampToString(r.CreatedAt),
	}
}

// ChannelMessageResponse is the JSON shape returned for channel messages.
// Reactions and thread-reply count are populated by the list/get handlers
// in batched fetches; the create handler returns an empty Reactions slice
// and ThreadReplyCount=0 since neither can exist for a brand-new row.
type ChannelMessageResponse struct {
	ID               string                    `json:"id"`
	ChannelID        string                    `json:"channel_id"`
	AuthorType       string                    `json:"author_type"`
	AuthorID         string                    `json:"author_id"`
	Content          string                    `json:"content"`
	ParentMessageID  *string                   `json:"parent_message_id"`
	EditedAt         *string                   `json:"edited_at"`
	DeletedAt        *string                   `json:"deleted_at"`
	CreatedAt        string                    `json:"created_at"`
	Reactions        []ChannelReactionResponse `json:"reactions"`
	ThreadReplyCount int32                     `json:"thread_reply_count"`
	// Phase 5b — attachments hydrated from metadata.attachments JSONB
	// (a string array of attachment ids). The list/get/thread handlers
	// fetch attachment rows in one batched query and preserve the
	// order from metadata. Empty array on rows without attachments.
	Attachments []AttachmentResponse `json:"attachments"`
}

func channelMessageToResponse(m db.ChannelMessage) ChannelMessageResponse {
	resp := ChannelMessageResponse{
		ID:          uuidToString(m.ID),
		ChannelID:   uuidToString(m.ChannelID),
		AuthorType:  m.AuthorType,
		AuthorID:    uuidToString(m.AuthorID),
		Content:     m.Content,
		CreatedAt:   timestampToString(m.CreatedAt),
		Reactions:   []ChannelReactionResponse{},
		Attachments: []AttachmentResponse{},
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

// channelMessageAttachmentIDs decodes metadata.attachments from a
// channel_message row. Returns nil when the metadata is missing or
// malformed; callers treat that as "no attachments".
func channelMessageAttachmentIDs(m db.ChannelMessage) []pgtype.UUID {
	if len(m.Metadata) == 0 {
		return nil
	}
	var meta struct {
		Attachments []string `json:"attachments"`
	}
	if err := json.Unmarshal(m.Metadata, &meta); err != nil || len(meta.Attachments) == 0 {
		return nil
	}
	out := make([]pgtype.UUID, 0, len(meta.Attachments))
	for _, s := range meta.Attachments {
		u, err := util.ParseUUID(s)
		if err != nil {
			continue
		}
		out = append(out, u)
	}
	return out
}

// groupChannelAttachments hydrates attachment rows for a list of channel
// messages by joining the per-message metadata.attachments arrays into a
// single ListAttachmentsByIDs call. Order is preserved per message based
// on the metadata array, so the UI renders attachments in the order the
// author selected them.
func (h *Handler) groupChannelAttachments(r *http.Request, workspaceID pgtype.UUID, msgs []db.ChannelMessage) map[string][]AttachmentResponse {
	if len(msgs) == 0 {
		return nil
	}
	// Collect every attachment id across all messages, plus per-message
	// id-order for later reordering.
	perMessage := make(map[string][]pgtype.UUID, len(msgs))
	all := make([]pgtype.UUID, 0)
	seen := make(map[[16]byte]struct{})
	for _, m := range msgs {
		ids := channelMessageAttachmentIDs(m)
		if len(ids) == 0 {
			continue
		}
		perMessage[uuidToString(m.ID)] = ids
		for _, id := range ids {
			if _, dup := seen[id.Bytes]; dup {
				continue
			}
			seen[id.Bytes] = struct{}{}
			all = append(all, id)
		}
	}
	if len(all) == 0 {
		return nil
	}
	rows, err := h.Queries.ListAttachmentsByIDs(r.Context(), db.ListAttachmentsByIDsParams{
		Column1:     all,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return nil
	}
	byID := make(map[string]db.Attachment, len(rows))
	for _, a := range rows {
		byID[uuidToString(a.ID)] = a
	}
	out := make(map[string][]AttachmentResponse, len(perMessage))
	for mid, ids := range perMessage {
		ordered := make([]AttachmentResponse, 0, len(ids))
		for _, id := range ids {
			if a, ok := byID[uuidToString(id)]; ok {
				ordered = append(ordered, h.attachmentToResponse(a))
			}
		}
		if len(ordered) > 0 {
			out[mid] = ordered
		}
	}
	return out
}

// validateChannelAttachmentIDs is the create-time guard: every id the
// client claims to attach must (1) parse, (2) exist in the same
// workspace, (3) have been uploaded by the calling actor. The third
// constraint stops a member from "borrowing" another user's
// attachment by submitting its id as their own.
func (h *Handler) validateChannelAttachmentIDs(ctx context.Context, ids []string, workspaceID pgtype.UUID, uploaderType, uploaderID string) ([]pgtype.UUID, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	uploaderUUID, err := util.ParseUUID(uploaderID)
	if err != nil {
		return nil, fmt.Errorf("invalid uploader id: %w", err)
	}
	parsed := make([]pgtype.UUID, 0, len(ids))
	for _, s := range ids {
		u, err := util.ParseUUID(s)
		if err != nil {
			return nil, fmt.Errorf("invalid attachment id: %w", err)
		}
		parsed = append(parsed, u)
	}
	rows, err := h.Queries.ListAttachmentsByIDs(ctx, db.ListAttachmentsByIDsParams{
		Column1:     parsed,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return nil, err
	}
	if len(rows) != len(parsed) {
		return nil, fmt.Errorf("one or more attachments not found in this workspace")
	}
	for _, a := range rows {
		if a.UploaderType != uploaderType || a.UploaderID.Bytes != uploaderUUID.Bytes {
			return nil, fmt.Errorf("attachment %s was not uploaded by you", uuidToString(a.ID))
		}
	}
	return parsed, nil
}

// groupChannelReactions fetches all reactions for the given message ids
// in one query and groups them by message id. Mirrors groupReactions for
// issue comments. Returns a nil map when no message ids supplied.
func (h *Handler) groupChannelReactions(r *http.Request, messageIDs []pgtype.UUID) map[string][]ChannelReactionResponse {
	if len(messageIDs) == 0 {
		return nil
	}
	rows, err := h.Queries.ListChannelMessageReactionsByMessageIDs(r.Context(), messageIDs)
	if err != nil {
		// Treat a fetch failure as "no reactions" — callers render the
		// timeline without reaction chips rather than 500ing.
		return nil
	}
	out := make(map[string][]ChannelReactionResponse, len(messageIDs))
	for _, rx := range rows {
		mid := uuidToString(rx.ChannelMessageID)
		out[mid] = append(out[mid], channelReactionToResponse(rx))
	}
	return out
}

// groupChannelThreadReplyCounts batches the per-parent reply counts for
// a list of (potentially) parent messages. Returns the count keyed by
// parent message id; a missing key means zero replies.
func (h *Handler) groupChannelThreadReplyCounts(r *http.Request, parentIDs []pgtype.UUID) map[string]int32 {
	if len(parentIDs) == 0 {
		return nil
	}
	rows, err := h.Queries.CountThreadRepliesByMessageIDs(r.Context(), parentIDs)
	if err != nil {
		return nil
	}
	out := make(map[string]int32, len(rows))
	for _, row := range rows {
		out[uuidToString(row.ParentID)] = row.ReplyCount
	}
	return out
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

	// Batch the reaction and thread-reply-count fetches so the timeline
	// query stays cheap even on busy channels. Both are best-effort — a
	// failure renders the messages without the badge rather than 500ing.
	ids := make([]pgtype.UUID, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	reactions := h.groupChannelReactions(r, ids)
	replyCounts := h.groupChannelThreadReplyCounts(r, ids)
	attachments := h.groupChannelAttachments(r, wsID, msgs)

	out := make([]ChannelMessageResponse, len(msgs))
	for i, m := range msgs {
		resp := channelMessageToResponse(m)
		mid := uuidToString(m.ID)
		if rs, ok := reactions[mid]; ok && len(rs) > 0 {
			resp.Reactions = rs
		}
		if c, ok := replyCounts[mid]; ok {
			resp.ThreadReplyCount = c
		}
		if atts, ok := attachments[mid]; ok && len(atts) > 0 {
			resp.Attachments = atts
		}
		out[i] = resp
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateChannelMessageRequest is the JSON body for POST /api/channels/{channelId}/messages.
type CreateChannelMessageRequest struct {
	Content         string   `json:"content"`
	ParentMessageID *string  `json:"parent_message_id"`
	// Phase 5b — attachment ids the client uploaded via /api/upload-file
	// before submitting the message. Stored in
	// channel_message.metadata.attachments JSONB array. The handler
	// validates each id exists, lives in the same workspace, and was
	// uploaded by the same actor before stamping it.
	AttachmentIDs []string `json:"attachment_ids,omitempty"`
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

	// Phase 5b — validate + stamp attachment ids before insert. The
	// metadata JSONB carries the array as ["uuid1", "uuid2", ...] so the
	// hydrator on read can do one batched query.
	var validatedAttachments []pgtype.UUID
	if len(req.AttachmentIDs) > 0 {
		var err error
		validatedAttachments, err = h.validateChannelAttachmentIDs(r.Context(), req.AttachmentIDs, wsID, actorType, actorID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	params := channel.CreateMessageParams{
		ChannelID: channelUUID,
		Author:    channel.Actor{Type: actorType, ID: actorUUID},
		Content:   req.Content,
	}
	if len(validatedAttachments) > 0 {
		// Build the metadata blob with the attachment id array. Future
		// metadata writers should preserve existing keys; today only this
		// path writes metadata so we can construct from scratch.
		ids := make([]string, len(validatedAttachments))
		for i, u := range validatedAttachments {
			ids[i] = uuidToString(u)
		}
		metaJSON, err := json.Marshal(map[string]any{"attachments": ids})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to marshal metadata")
			return
		}
		params.Metadata = metaJSON
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
	// Hydrate attachments onto the create response so the client can
	// render the inline previews/cards immediately without a follow-up
	// list refetch.
	if atts := h.groupChannelAttachments(r, wsID, []db.ChannelMessage{msg}); atts != nil {
		if items, ok := atts[uuidToString(msg.ID)]; ok && len(items) > 0 {
			resp.Attachments = items
		}
	}
	h.publish(protocol.EventChannelMessage, uuidToString(wsID), actorType, actorID, map[string]any{
		"channel_id":   uuidToString(channelUUID),
		"channel_name": ch.Name,
		"message":      resp,
	})

	// Mention fan-out (inbox + agent triggers). Best-effort: a failure
	// here must not 500 the message-create. We log and move on.
	authorUUID2 := actorUUID // capture for goroutine
	go h.fanOutChannelMentions(context.Background(), wsID, ch, msg, actorType, actorID)
	go h.triggerMentionedAgentTasks(context.Background(), wsID, ch, msg, channel.Actor{Type: actorType, ID: authorUUID2})

	writeJSON(w, http.StatusCreated, resp)
}

// ChannelSearchResultMessage is one hit returned by /api/channels/search.
// Carries enough channel context that the UI can render a "result row"
// without a follow-up GetChannel call per hit.
type ChannelSearchResultMessage struct {
	ChannelMessageResponse
	ChannelName        string  `json:"channel_name"`
	ChannelDisplayName string  `json:"channel_display_name"`
	ChannelKind        string  `json:"channel_kind"`
	Rank               float32 `json:"rank"`
}

// SearchChannelMessages handles GET /api/channels/search?q=&channel_id=&limit=&offset=.
//
// Visibility is enforced inline by the SQL — a non-member never sees
// hits from a private channel they don't belong to. Soft-deleted
// messages are excluded (the existing `idx_channel_message_timeline`
// partial index already excludes them, and the search query inherits).
//
// Phase 5 v1: text-only. Attachments / reactions are NOT hydrated on
// hits to keep the search query cheap; clicking a result navigates to
// the channel where the full row materializes.
func (h *Handler) SearchChannelMessages(w http.ResponseWriter, r *http.Request) {
	wsID, _, ok := h.requireChannelsEnabled(w, r)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q parameter is required")
		return
	}

	limit := int32(20)
	offset := int32(0)
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 100 {
				n = 100
			}
			limit = int32(n)
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = int32(n)
		}
	}

	actorType, actorID := h.resolveActor(r, userID, uuidToString(wsID))
	actorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor id")
	if !ok {
		return
	}

	params := db.SearchChannelMessagesParams{
		Query:        q,
		WorkspaceID:  wsID,
		ActorType:    actorType,
		ActorID:      actorUUID,
		ResultLimit:  limit,
		ResultOffset: offset,
	}
	if v := r.URL.Query().Get("channel_id"); v != "" {
		uid, ok := parseUUIDOrBadRequest(w, v, "channel_id")
		if !ok {
			return
		}
		params.ChannelIDFilter = uid
	}

	rows, err := h.Queries.SearchChannelMessages(r.Context(), params)
	if err != nil {
		slog.Warn("search channel messages failed",
			"error", err,
			"workspace_id", uuidToString(wsID),
			"query", q,
		)
		writeError(w, http.StatusInternalServerError, "failed to search messages")
		return
	}

	out := make([]ChannelSearchResultMessage, len(rows))
	for i, row := range rows {
		// Reconstruct the embedded ChannelMessage shape from the row's
		// fields; sqlc emits a custom row type because of the JOIN +
		// ts_rank.
		msg := db.ChannelMessage{
			ID:              row.ID,
			ChannelID:       row.ChannelID,
			AuthorType:      row.AuthorType,
			AuthorID:        row.AuthorID,
			Content:         row.Content,
			ParentMessageID: row.ParentMessageID,
			EditedAt:        row.EditedAt,
			DeletedAt:       row.DeletedAt,
			DeletionReason:  row.DeletionReason,
			Metadata:        row.Metadata,
			CreatedAt:       row.CreatedAt,
		}
		out[i] = ChannelSearchResultMessage{
			ChannelMessageResponse: channelMessageToResponse(msg),
			ChannelName:            row.ChannelName,
			ChannelDisplayName:     row.ChannelDisplayName,
			ChannelKind:            row.ChannelKind,
			Rank:                   row.Rank,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// channelAgentDedupWindow returns the dedup window in seconds. Reads
// CHANNEL_AGENT_DEDUP_WINDOW_SECONDS at call time so operators can adjust
// without a deploy (config changes via env+restart of the API tier).
// Default 30s matches the spec.
func channelAgentDedupWindow() float64 {
	if v := os.Getenv("CHANNEL_AGENT_DEDUP_WINDOW_SECONDS"); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil && n >= 0 {
			return n
		}
	}
	return 30
}

// triggerMentionedAgentTasks runs the Phase 3 mention-triggers-agent flow.
// Mirrors the existing `enqueueMentionedAgentTasks` in comment.go but
// scoped to channels (no issue, no thread context — channels are
// conversational, not historical).
//
// Selection logic lives in the channel service so it remains testable
// against pure data; this handler glue only calls TaskService.Enqueue
// for each candidate the service hands back.
//
// Concurrency: runs in a separate goroutine so the user's POST returns
// 201 immediately. The cost is that the agent task may be enqueued a
// few hundred ms later than the message lands — fine for a chat surface.
func (h *Handler) triggerMentionedAgentTasks(ctx context.Context, workspaceID pgtype.UUID, ch db.Channel, msg db.ChannelMessage, author channel.Actor) {
	candidates, err := h.ChannelMessageService.SelectAgentsForMention(ctx, channel.SelectAgentsForMentionParams{
		ChannelID:          ch.ID,
		Content:            msg.Content,
		Author:             author,
		DedupWindowSeconds: channelAgentDedupWindow(),
		MentionParser: func(content string) []channel.ParsedMention {
			parsed := util.ParseMentions(content)
			out := make([]channel.ParsedMention, len(parsed))
			for i, m := range parsed {
				out[i] = channel.ParsedMention{Type: m.Type, ID: m.ID}
			}
			return out
		},
	})
	if err != nil {
		slog.Warn("channel mention: select candidates failed",
			"channel_id", uuidToString(ch.ID),
			"message_id", uuidToString(msg.ID),
			"error", err,
		)
		return
	}

	for _, cand := range candidates {
		_, err := h.TaskService.EnqueueTaskForChannelMention(ctx, service.EnqueueTaskForChannelMentionParams{
			WorkspaceID:    workspaceID,
			AgentID:        cand.AgentID,
			ChannelID:      ch.ID,
			ChannelName:    ch.Name,
			ChannelKind:    ch.Kind,
			MessageID:      msg.ID,
			MessageContent: msg.Content,
			AuthorType:     author.Type,
			AuthorID:       uuidToString(author.ID),
		})
		if err != nil {
			slog.Warn("channel mention: enqueue task failed",
				"agent_id", uuidToString(cand.AgentID),
				"channel_id", uuidToString(ch.ID),
				"error", err,
			)
		}
	}
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

// ListChannelMessageThread handles GET
// /api/channels/{channelId}/messages/{messageId}/thread.
//
// Returns the parent message + its non-deleted replies (chronological),
// each with reactions populated. Used by the side-panel UI when the user
// clicks "view thread (N)" on a parent.
func (h *Handler) ListChannelMessageThread(w http.ResponseWriter, r *http.Request) {
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
	messageUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "messageId"), "message id")
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

	// Verify the parent belongs to this channel — defense against
	// cross-channel id confusion.
	parent, err := h.ChannelMessageService.Get(r.Context(), messageUUID)
	if err != nil || parent.ChannelID.Bytes != channelUUID.Bytes {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}

	replies, err := h.ChannelMessageService.ListThread(r.Context(), messageUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list thread")
		return
	}

	// Hydrate the parent + each reply with reactions and attachments in
	// batched queries. allMsgs preserves order so the attachment-batch
	// can return its grouping keyed by message id.
	allMsgs := make([]db.ChannelMessage, 0, len(replies)+1)
	allMsgs = append(allMsgs, parent)
	allMsgs = append(allMsgs, replies...)
	allIDs := make([]pgtype.UUID, len(allMsgs))
	for i, m := range allMsgs {
		allIDs[i] = m.ID
	}
	reactions := h.groupChannelReactions(r, allIDs)
	attachments := h.groupChannelAttachments(r, wsID, allMsgs)

	parentResp := channelMessageToResponse(parent)
	if rs, ok := reactions[uuidToString(parent.ID)]; ok && len(rs) > 0 {
		parentResp.Reactions = rs
	}
	if atts, ok := attachments[uuidToString(parent.ID)]; ok && len(atts) > 0 {
		parentResp.Attachments = atts
	}
	parentResp.ThreadReplyCount = int32(len(replies))

	replyResps := make([]ChannelMessageResponse, len(replies))
	for i, m := range replies {
		rresp := channelMessageToResponse(m)
		mid := uuidToString(m.ID)
		if rs, ok := reactions[mid]; ok && len(rs) > 0 {
			rresp.Reactions = rs
		}
		if atts, ok := attachments[mid]; ok && len(atts) > 0 {
			rresp.Attachments = atts
		}
		replyResps[i] = rresp
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"parent":  parentResp,
		"replies": replyResps,
	})
}

// UpdateChannelMessageRequest is the body for PATCH
// /api/channels/{channelId}/messages/{messageId}.
type UpdateChannelMessageRequest struct {
	Content string `json:"content"`
}

// UpdateChannelMessage handles PATCH
// /api/channels/{channelId}/messages/{messageId}. Author-only;
// admins can delete others' messages but not edit them (the spec
// calls out moderation as deletion, not rewriting words in someone
// else's mouth). Sets edited_at = now() and broadcasts
// EventChannelMessageUpdated.
func (h *Handler) UpdateChannelMessage(w http.ResponseWriter, r *http.Request) {
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
	messageUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "messageId"), "message id")
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
	// Cross-channel guard.
	msg, err := h.ChannelMessageService.Get(r.Context(), messageUUID)
	if err != nil || msg.ChannelID.Bytes != channelUUID.Bytes {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}

	var req UpdateChannelMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	updated, err := h.ChannelMessageService.UpdateContent(r.Context(), messageUUID, channel.Actor{Type: actorType, ID: actorUUID}, req.Content)
	if err != nil {
		status, msgStr := channelErrorStatus(err)
		writeError(w, status, msgStr)
		return
	}

	resp := channelMessageToResponse(updated)
	// Hydrate the chip row so the WS subscriber doesn't need a refetch
	// just to keep reactions visible after an edit.
	if rs := h.groupChannelReactions(r, []pgtype.UUID{updated.ID}); rs != nil {
		if items, ok := rs[uuidToString(updated.ID)]; ok && len(items) > 0 {
			resp.Reactions = items
		}
	}

	h.publish(protocol.EventChannelMessageUpdated, uuidToString(wsID), actorType, actorID, map[string]any{
		"channel_id": uuidToString(channelUUID),
		"message":    resp,
	})
	writeJSON(w, http.StatusOK, resp)
}

// DeleteChannelMessage handles DELETE
// /api/channels/{channelId}/messages/{messageId}. Author or channel
// admin. Soft-deletes (sets deleted_at + deletion_reason). The row
// stays in the timeline so thread continuity isn't broken — the UI
// renders a "[message deleted]" placeholder.
func (h *Handler) DeleteChannelMessage(w http.ResponseWriter, r *http.Request) {
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
	messageUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "messageId"), "message id")
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
	// Cross-channel guard.
	msg, err := h.ChannelMessageService.Get(r.Context(), messageUUID)
	if err != nil || msg.ChannelID.Bytes != channelUUID.Bytes {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}

	deleted, err := h.ChannelMessageService.Delete(r.Context(), messageUUID, channel.Actor{Type: actorType, ID: actorUUID})
	if err != nil {
		status, msgStr := channelErrorStatus(err)
		writeError(w, status, msgStr)
		return
	}

	h.publish(protocol.EventChannelMessageDeleted, uuidToString(wsID), actorType, actorID, map[string]any{
		"channel_id": uuidToString(channelUUID),
		"message_id": uuidToString(messageUUID),
		"deleted_at": timestampToString(deleted.DeletedAt),
		"reason":     deleted.DeletionReason.String,
	})
	w.WriteHeader(http.StatusNoContent)
}

// AddChannelReactionRequest is the body for POST
// /api/channels/{channelId}/messages/{messageId}/reactions.
type AddChannelReactionRequest struct {
	Emoji string `json:"emoji"`
}

// AddChannelReaction handles POST
// /api/channels/{channelId}/messages/{messageId}/reactions. Idempotent —
// adding the same reaction twice is a no-op (the SQL ON CONFLICT keeps
// the original created_at).
func (h *Handler) AddChannelReaction(w http.ResponseWriter, r *http.Request) {
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
	messageUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "messageId"), "message id")
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

	// Cross-channel confusion guard: the message must belong to this
	// channel. Without this check, a member of channel A could react to
	// a message in private channel B by submitting B's message id with
	// A's channel id in the URL.
	msg, err := h.ChannelMessageService.Get(r.Context(), messageUUID)
	if err != nil || msg.ChannelID.Bytes != channelUUID.Bytes {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}

	var req AddChannelReactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Emoji == "" {
		writeError(w, http.StatusBadRequest, "emoji is required")
		return
	}

	row, err := h.Queries.AddChannelMessageReaction(r.Context(), db.AddChannelMessageReactionParams{
		ChannelMessageID: messageUUID,
		WorkspaceID:      wsID,
		ActorType:        actorType,
		ActorID:          actorUUID,
		Emoji:            req.Emoji,
	})
	if err != nil {
		slog.Warn("add channel reaction failed",
			"channel_id", uuidToString(channelUUID),
			"message_id", uuidToString(messageUUID),
			"error", err,
		)
		writeError(w, http.StatusInternalServerError, "failed to add reaction")
		return
	}

	resp := channelReactionToResponse(row)
	h.publish(protocol.EventChannelReactionAdded, uuidToString(wsID), actorType, actorID, map[string]any{
		"reaction":   resp,
		"channel_id": uuidToString(channelUUID),
		"message_id": uuidToString(messageUUID),
	})
	writeJSON(w, http.StatusCreated, resp)
}

// RemoveChannelReaction handles DELETE
// /api/channels/{channelId}/messages/{messageId}/reactions. Body carries
// the emoji being removed (cleaner than an emoji-in-URL design — emojis
// are multi-codepoint and URL-encode awkwardly).
func (h *Handler) RemoveChannelReaction(w http.ResponseWriter, r *http.Request) {
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
	messageUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "messageId"), "message id")
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
	msg, err := h.ChannelMessageService.Get(r.Context(), messageUUID)
	if err != nil || msg.ChannelID.Bytes != channelUUID.Bytes {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}

	var req AddChannelReactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Emoji == "" {
		writeError(w, http.StatusBadRequest, "emoji is required")
		return
	}

	if err := h.Queries.RemoveChannelMessageReaction(r.Context(), db.RemoveChannelMessageReactionParams{
		ChannelMessageID: messageUUID,
		ActorType:        actorType,
		ActorID:          actorUUID,
		Emoji:            req.Emoji,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove reaction")
		return
	}

	h.publish(protocol.EventChannelReactionRemoved, uuidToString(wsID), actorType, actorID, map[string]any{
		"channel_id": uuidToString(channelUUID),
		"message_id": uuidToString(messageUUID),
		"emoji":      req.Emoji,
		"actor_type": actorType,
		"actor_id":   actorID,
	})
	w.WriteHeader(http.StatusNoContent)
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
