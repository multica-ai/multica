package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---------- V2 response shapes ----------

type ChannelMessageV2Response struct {
	ID          string  `json:"id"`
	ChannelID   string  `json:"channel_id"`
	WorkspaceID string  `json:"workspace_id"`
	AuthorType  string  `json:"author_type"`
	AuthorID    *string `json:"author_id"`
	Content     string  `json:"content"`
	ReplyToID   *string `json:"reply_to_id,omitempty"`
	ReplyCount  int32   `json:"reply_count"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type ChannelContextResponse struct {
	Channel        ChannelResponse         `json:"channel"`
	Members        []ChannelMemberResponse `json:"members"`
	Messages       []ChannelContextMessage `json:"messages"`
	TriggerMessage *ChannelContextMessage  `json:"trigger_message,omitempty"`
	Replies        []ChannelContextMessage `json:"replies,omitempty"`
}

type ChannelContextMessage struct {
	ID         string  `json:"id"`
	AuthorType string  `json:"author_type"`
	AuthorID   *string `json:"author_id"`
	AuthorName *string `json:"author_name,omitempty"`
	Content    string  `json:"content"`
	CreatedAt  string  `json:"created_at"`
}

func channelMessageV2ToResponse(m db.ListChannelMessagesRow) ChannelMessageV2Response {
	return ChannelMessageV2Response{
		ID:          uuidToString(m.ID),
		ChannelID:   uuidToString(m.ChannelID),
		WorkspaceID: uuidToString(m.WorkspaceID),
		AuthorType:  m.AuthorType,
		AuthorID:    uuidToPtr(m.AuthorID),
		Content:     m.Content,
		ReplyToID:   uuidToPtr(m.ReplyToID),
		ReplyCount:  m.ReplyCount,
		CreatedAt:   timestampToString(m.CreatedAt),
		UpdatedAt:   timestampToString(m.UpdatedAt),
	}
}

func channelMessageToV2Response(m db.ChannelMessage, replyCount int32) ChannelMessageV2Response {
	return ChannelMessageV2Response{
		ID:          uuidToString(m.ID),
		ChannelID:   uuidToString(m.ChannelID),
		WorkspaceID: uuidToString(m.WorkspaceID),
		AuthorType:  m.AuthorType,
		AuthorID:    uuidToPtr(m.AuthorID),
		Content:     m.Content,
		ReplyToID:   uuidToPtr(m.ReplyToID),
		ReplyCount:  replyCount,
		CreatedAt:   timestampToString(m.CreatedAt),
		UpdatedAt:   timestampToString(m.UpdatedAt),
	}
}

func channelContextRowToMessage(m db.GetChannelContextRow) ChannelContextMessage {
	return ChannelContextMessage{
		ID:         uuidToString(m.ID),
		AuthorType: m.AuthorType,
		AuthorID:   uuidToPtr(m.AuthorID),
		AuthorName: textToPtr(m.AuthorName),
		Content:    m.Content,
		CreatedAt:  timestampToString(m.CreatedAt),
	}
}

func channelMessageForContextToMessage(m db.GetChannelMessageForContextRow) ChannelContextMessage {
	return ChannelContextMessage{
		ID:         uuidToString(m.ID),
		AuthorType: m.AuthorType,
		AuthorID:   uuidToPtr(m.AuthorID),
		AuthorName: textToPtr(m.AuthorName),
		Content:    m.Content,
		CreatedAt:  timestampToString(m.CreatedAt),
	}
}

func channelReplyForContextToMessage(m db.ListChannelMessageRepliesForContextRow) ChannelContextMessage {
	return ChannelContextMessage{
		ID:         uuidToString(m.ID),
		AuthorType: m.AuthorType,
		AuthorID:   uuidToPtr(m.AuthorID),
		AuthorName: textToPtr(m.AuthorName),
		Content:    m.Content,
		CreatedAt:  timestampToString(m.CreatedAt),
	}
}

// ---------- V2 handlers ----------

// ListChannelMessages lists top-level messages in a channel (flat timeline).
func (h *Handler) ListChannelMessages(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	cctx, ok := h.loadChannelContext(w, r, wsUUID, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	messages, err := h.Queries.ListChannelMessages(r.Context(), cctx.channel.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list messages")
		return
	}
	resp := make([]ChannelMessageV2Response, len(messages))
	for i, m := range messages {
		resp[i] = channelMessageV2ToResponse(m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": resp, "total": len(resp)})
}

// SendChannelMessage posts a top-level message to a channel (no thread required).
func (h *Handler) SendChannelMessage(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	cctx, ok := h.loadChannelContext(w, r, wsUUID, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if !cctx.canPost() {
		writeError(w, http.StatusForbidden, "you cannot post in this channel")
		return
	}
	var req CreateChannelMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	var authorID pgtype.UUID
	if actorID != "" {
		if id, err := parseUUIDErr(actorID); err == nil {
			authorID = id
		}
	}
	authorType := actorType
	if authorType != "agent" {
		authorType = "member"
	}
	msg, err := h.Queries.CreateChannelMessageTopLevel(r.Context(), db.CreateChannelMessageTopLevelParams{
		ChannelID:   cctx.channel.ID,
		WorkspaceID: wsUUID,
		AuthorType:  authorType,
		Content:     content,
		AuthorID:    authorID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to send message")
		return
	}
	h.Queries.TouchChannel(r.Context(), cctx.channel.ID)
	resp := channelMessageToV2Response(msg, 0)
	h.publish(protocol.EventChannelMessageCreated, workspaceID, actorType, actorID, map[string]any{
		"message":    resp,
		"channel_id": uuidToString(cctx.channel.ID),
	})
	h.processChannelMessageMentions(r.Context(), cctx.channel, msg, actorType, actorID)
	writeJSON(w, http.StatusCreated, resp)
}

// ReplyToMessage replies to a message, auto-creating a thread if needed.
func (h *Handler) ReplyToMessage(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	cctx, ok := h.loadChannelContext(w, r, wsUUID, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if !cctx.canPost() {
		writeError(w, http.StatusForbidden, "you cannot post in this channel")
		return
	}
	msgID := chi.URLParam(r, "msgId")
	msgUUID, ok := parseUUIDOrBadRequest(w, msgID, "message id")
	if !ok {
		return
	}

	// Verify target message exists and belongs to this channel.
	targetMsg, err := h.Queries.GetChannelMessage(r.Context(), msgUUID)
	if err != nil || uuidToString(targetMsg.ChannelID) != uuidToString(cctx.channel.ID) {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}

	var req CreateChannelMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	var authorID pgtype.UUID
	if actorID != "" {
		if id, err := parseUUIDErr(actorID); err == nil {
			authorID = id
		}
	}
	authorType := actorType
	if authorType != "agent" {
		authorType = "member"
	}

	// Find or create thread for this message.
	thread, err := h.Queries.GetThreadByRootMessage(r.Context(), msgUUID)
	if err != nil {
		// Thread doesn't exist yet — create one.
		title := targetMsg.Content
		if len(title) > 50 {
			title = title[:50]
		}
		thread, err = h.Queries.CreateChannelThread(r.Context(), db.CreateChannelThreadParams{
			ChannelID:     cctx.channel.ID,
			WorkspaceID:   wsUUID,
			Title:         title,
			CreatedBy:     authorID,
			RootMessageID: msgUUID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create thread")
			return
		}
		h.publish(protocol.EventChannelThreadCreated, workspaceID, actorType, actorID, map[string]any{
			"thread":     channelThreadToResponse(thread),
			"channel_id": uuidToString(cctx.channel.ID),
		})
	}

	// Insert the reply message.
	reply, err := h.Queries.CreateChannelMessageReply(r.Context(), db.CreateChannelMessageReplyParams{
		ThreadID:    thread.ID,
		ChannelID:   cctx.channel.ID,
		WorkspaceID: wsUUID,
		AuthorType:  authorType,
		Content:     content,
		ReplyToID:   msgUUID,
		AuthorID:    authorID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to post reply")
		return
	}
	h.Queries.BumpChannelThread(r.Context(), thread.ID)
	h.Queries.TouchChannel(r.Context(), cctx.channel.ID)

	resp := channelMessageToV2Response(reply, 0)
	h.publish(protocol.EventChannelMessageCreated, workspaceID, actorType, actorID, map[string]any{
		"message":    resp,
		"channel_id": uuidToString(cctx.channel.ID),
		"thread_id":  uuidToString(thread.ID),
		"reply_to":   msgID,
	})
	h.processChannelMessageMentions(r.Context(), cctx.channel, reply, actorType, actorID)
	writeJSON(w, http.StatusCreated, map[string]any{
		"message": resp,
		"thread":  channelThreadToResponse(thread),
	})
}

// GetMessageThread returns the thread (replies) for a specific message.
func (h *Handler) GetMessageThread(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	cctx, ok := h.loadChannelContext(w, r, wsUUID, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	msgID := chi.URLParam(r, "msgId")
	msgUUID, ok := parseUUIDOrBadRequest(w, msgID, "message id")
	if !ok {
		return
	}

	// Get the root message.
	rootMsg, err := h.Queries.GetChannelMessage(r.Context(), msgUUID)
	if err != nil || uuidToString(rootMsg.ChannelID) != uuidToString(cctx.channel.ID) {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}

	// Get the thread (may not exist yet).
	thread, err := h.Queries.GetThreadByRootMessage(r.Context(), msgUUID)
	if err != nil {
		// No thread yet — return just the root message with no replies.
		writeJSON(w, http.StatusOK, map[string]any{
			"root_message": channelMessageToV2Response(rootMsg, 0),
			"replies":      []ChannelMessageV2Response{},
			"thread":       nil,
		})
		return
	}

	// List replies in the thread.
	replies, err := h.Queries.ListMessageReplies(r.Context(), msgUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list replies")
		return
	}
	replyResp := make([]ChannelMessageResponse, len(replies))
	for i, m := range replies {
		replyResp[i] = channelMessageToResponse(m)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"root_message": channelMessageToV2Response(rootMsg, int32(len(replies))),
		"replies":      replyResp,
		"thread":       channelThreadToResponse(thread),
	})
}

// RemoveChannelMember removes a member from a channel (by manager).
func (h *Handler) RemoveChannelMember(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	cctx, ok := h.loadChannelContext(w, r, wsUUID, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if !cctx.canManage() {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	targetID := chi.URLParam(r, "userId")
	targetUUID, ok := parseUUIDOrBadRequest(w, targetID, "user id")
	if !ok {
		return
	}
	if err := h.Queries.RemoveChannelMember(r.Context(), db.RemoveChannelMemberParams{
		ChannelID: cctx.channel.ID,
		UserID:    targetUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove member")
		return
	}
	h.publish(protocol.EventChannelMemberLeft, workspaceID, "member", requestUserID(r), map[string]any{
		"channel_id": uuidToString(cctx.channel.ID),
		"user_id":    targetID,
	})
	writeJSON(w, http.StatusOK, map[string]any{"removed": true, "channel_id": uuidToString(cctx.channel.ID), "user_id": targetID})
}

// ConvertMessageToIssue converts a channel message into an issue.
// If the message does not already have a thread, one is created implicitly so
// that the spec invariant "Issue is produced from a thread" always holds.
func (h *Handler) ConvertMessageToIssue(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	cctx, ok := h.loadChannelContext(w, r, wsUUID, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	msgID := chi.URLParam(r, "msgId")
	msgUUID, ok := parseUUIDOrBadRequest(w, msgID, "message id")
	if !ok {
		return
	}

	// Get the message.
	msg, err := h.Queries.GetChannelMessage(r.Context(), msgUUID)
	if err != nil || uuidToString(msg.ChannelID) != uuidToString(cctx.channel.ID) {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}

	// Parse optional overrides from request body.
	var req struct {
		Title       *string `json:"title"`
		Description *string `json:"description"`
		ProjectID   *string `json:"project_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	// Auto-fill title from message content.
	title := msg.Content
	if len(title) > 80 {
		title = title[:80]
	}
	if req.Title != nil && *req.Title != "" {
		title = *req.Title
	}

	// Description: full message content + source annotation.
	description := msg.Content + "\n\n---\n_来源：频道消息_"
	if req.Description != nil && *req.Description != "" {
		description = *req.Description
	}

	// Determine the creator user ID.
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}

	// Use transaction to safely create the issue with a number.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	issueNumber, err := qtx.IncrementIssueCounter(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to allocate issue number")
		return
	}

	var projectID pgtype.UUID
	if req.ProjectID != nil && *req.ProjectID != "" {
		if pid, perr := parseUUIDErr(*req.ProjectID); perr == nil {
			projectID = pid
		}
	}

	issue, err := qtx.CreateIssue(r.Context(), db.CreateIssueParams{
		WorkspaceID: wsUUID,
		Title:       title,
		Description: pgtype.Text{String: description, Valid: true},
		Status:      "todo",
		Priority:    "none",
		CreatorType: "member",
		CreatorID:   userUUID,
		Number:      issueNumber,
		ProjectID:   projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create issue")
		return
	}

	// Ensure thread exists: if no thread yet, create one implicitly.
	thread, terr := qtx.GetThreadByRootMessage(r.Context(), msgUUID)
	if terr != nil {
		// No thread yet — create one anchored to this message.
		threadTitle := msg.Content
		if len(threadTitle) > 50 {
			threadTitle = threadTitle[:50]
		}
		thread, err = qtx.CreateChannelThread(r.Context(), db.CreateChannelThreadParams{
			ChannelID:     cctx.channel.ID,
			WorkspaceID:   wsUUID,
			Title:         threadTitle,
			CreatedBy:     userUUID,
			RootMessageID: msgUUID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create thread for issue")
			return
		}
	}

	// Link the issue to the source channel and thread.
	qtx.LinkIssueSource(r.Context(), db.LinkIssueSourceParams{
		ID:              issue.ID,
		SourceChannelID: cctx.channel.ID,
		SourceThreadID:  thread.ID,
	})

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}

	// Post "created from thread" system activity (best-effort, outside tx).
	issue.SourceChannelID = cctx.channel.ID
	issue.SourceThreadID = thread.ID
	h.linkIssueToThreadActivity(r.Context(), &issue, thread)

	writeJSON(w, http.StatusCreated, map[string]any{
		"issue_id":     uuidToString(issue.ID),
		"issue_number": issue.Number,
		"title":        issue.Title,
		"thread_id":    uuidToString(thread.ID),
	})
}

// GetChannelContext returns channel info, members, and recent messages for agent injection.
func (h *Handler) GetChannelContext(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	cctx, ok := h.loadChannelContext(w, r, wsUUID, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	recent := int32(20)
	if v := r.URL.Query().Get("recent"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			recent = int32(n)
		}
	}
	messageID := strings.TrimSpace(r.URL.Query().Get("message"))
	includeReplies := r.URL.Query().Get("include_replies") == "true" || r.URL.Query().Get("include-replies") == "true"

	// Get members.
	members, err := h.Queries.ListChannelMembers(r.Context(), cctx.channel.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list members")
		return
	}
	memberResp := make([]ChannelMemberResponse, len(members))
	for i, m := range members {
		memberResp[i] = channelMemberRowToResponse(m)
	}

	// Get recent messages.
	msgs, err := h.Queries.GetChannelContext(r.Context(), db.GetChannelContextParams{
		ChannelID: cctx.channel.ID,
		Limit:     recent,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get context messages")
		return
	}
	// Reverse to chronological order (query returns DESC).
	msgResp := make([]ChannelContextMessage, len(msgs))
	for i, m := range msgs {
		msgResp[len(msgs)-1-i] = channelContextRowToMessage(m)
	}

	resp := ChannelContextResponse{
		Channel:  channelToResponse(cctx.channel),
		Members:  memberResp,
		Messages: msgResp,
	}
	if messageID != "" {
		msgUUID, ok := parseUUIDOrBadRequest(w, messageID, "message id")
		if !ok {
			return
		}
		trigger, err := h.Queries.GetChannelMessageForContext(r.Context(), db.GetChannelMessageForContextParams{
			ChannelID: cctx.channel.ID,
			ID:        msgUUID,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "message not found")
			return
		}
		triggerResp := channelMessageForContextToMessage(trigger)
		resp.TriggerMessage = &triggerResp
		if includeReplies {
			replies, err := h.Queries.ListChannelMessageRepliesForContext(r.Context(), db.ListChannelMessageRepliesForContextParams{
				ChannelID: cctx.channel.ID,
				ID:        msgUUID,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to get message replies")
				return
			}
			resp.Replies = make([]ChannelContextMessage, len(replies))
			for i, reply := range replies {
				resp.Replies[i] = channelReplyForContextToMessage(reply)
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---------- Message Update / Delete ----------

// UpdateChannelMessage updates a message's content. Only the original author
// or a channel manager may update a message.
func (h *Handler) UpdateChannelMessage(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	cctx, ok := h.loadChannelContext(w, r, wsUUID, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	msgID := chi.URLParam(r, "msgId")
	msgUUID, ok := parseUUIDOrBadRequest(w, msgID, "message id")
	if !ok {
		return
	}
	msg, err := h.Queries.GetChannelMessage(r.Context(), msgUUID)
	if err != nil || uuidToString(msg.ChannelID) != uuidToString(cctx.channel.ID) {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}

	// Permission: only the author or a channel manager can update.
	userID := requestUserID(r)
	isAuthor := msg.AuthorID.Valid && uuidToString(msg.AuthorID) == userID
	if !isAuthor && !cctx.canManage() {
		writeError(w, http.StatusForbidden, "you can only edit your own messages")
		return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	updated, err := h.Queries.UpdateChannelMessage(r.Context(), db.UpdateChannelMessageParams{
		ID:      msgUUID,
		Content: content,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update message")
		return
	}
	resp := channelMessageToV2Response(updated, 0)
	h.publish(protocol.EventChannelMessageUpdated, workspaceID, "member", userID, map[string]any{
		"message":    resp,
		"channel_id": uuidToString(cctx.channel.ID),
	})
	writeJSON(w, http.StatusOK, resp)
}

// DeleteChannelMessage deletes a message. Only the original author or a channel
// manager may delete a message.
func (h *Handler) DeleteChannelMessage(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	cctx, ok := h.loadChannelContext(w, r, wsUUID, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	msgID := chi.URLParam(r, "msgId")
	msgUUID, ok := parseUUIDOrBadRequest(w, msgID, "message id")
	if !ok {
		return
	}
	msg, err := h.Queries.GetChannelMessage(r.Context(), msgUUID)
	if err != nil || uuidToString(msg.ChannelID) != uuidToString(cctx.channel.ID) {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}

	userID := requestUserID(r)
	isAuthor := msg.AuthorID.Valid && uuidToString(msg.AuthorID) == userID
	if !isAuthor && !cctx.canManage() {
		writeError(w, http.StatusForbidden, "you can only delete your own messages")
		return
	}

	if err := h.Queries.DeleteChannelMessage(r.Context(), msgUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete message")
		return
	}
	h.publish(protocol.EventChannelMessageDeleted, workspaceID, "member", userID, map[string]any{
		"message_id": msgID,
		"channel_id": uuidToString(cctx.channel.ID),
	})
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "message_id": msgID})
}

// ---------- Thread Get / Update ----------

// GetChannelThread returns a single thread by ID.
func (h *Handler) GetChannelThreadByID(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	cctx, ok := h.loadChannelContext(w, r, wsUUID, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	thread, ok := h.loadThreadInChannel(w, r, cctx.channel.ID)
	if !ok {
		return
	}
	issues, _ := h.Queries.ListThreadIssues(r.Context(), thread.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"thread": channelThreadToResponse(thread),
		"issues": threadIssuesToResponse(issues),
	})
}

// UpdateChannelThread updates a thread's title.
func (h *Handler) UpdateChannelThread(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	cctx, ok := h.loadChannelContext(w, r, wsUUID, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	thread, ok := h.loadThreadInChannel(w, r, cctx.channel.ID)
	if !ok {
		return
	}

	// Permission: thread creator or channel manager.
	userID := requestUserID(r)
	isCreator := thread.CreatedBy.Valid && uuidToString(thread.CreatedBy) == userID
	if !isCreator && !cctx.canManage() {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var req struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	updated, err := h.Queries.UpdateChannelThreadTitle(r.Context(), db.UpdateChannelThreadTitleParams{
		ID:    thread.ID,
		Title: title,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update thread")
		return
	}
	resp := channelThreadToResponse(updated)
	h.publish(protocol.EventChannelThreadUpdated, workspaceID, "member", userID, map[string]any{
		"thread":     resp,
		"channel_id": uuidToString(cctx.channel.ID),
	})
	writeJSON(w, http.StatusOK, resp)
}
