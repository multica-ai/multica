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
	Channel  ChannelResponse         `json:"channel"`
	Members  []ChannelMemberResponse `json:"members"`
	Messages []ChannelContextMessage `json:"messages"`
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

	// Link to source channel and thread if one exists.
	threadID := pgtype.UUID{}
	if thread, terr := qtx.GetThreadByRootMessage(r.Context(), msgUUID); terr == nil {
		threadID = thread.ID
	}
	qtx.LinkIssueSource(r.Context(), db.LinkIssueSourceParams{
		ID:              issue.ID,
		SourceChannelID: cctx.channel.ID,
		SourceThreadID:  threadID,
	})

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"issue_id":     uuidToString(issue.ID),
		"issue_number": issue.Number,
		"title":        issue.Title,
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
		msgResp[len(msgs)-1-i] = ChannelContextMessage{
			ID:         uuidToString(m.ID),
			AuthorType: m.AuthorType,
			AuthorID:   uuidToPtr(m.AuthorID),
			AuthorName: textToPtr(m.AuthorName),
			Content:    m.Content,
			CreatedAt:  timestampToString(m.CreatedAt),
		}
	}

	resp := ChannelContextResponse{
		Channel:  channelToResponse(cctx.channel),
		Members:  memberResp,
		Messages: msgResp,
	}
	writeJSON(w, http.StatusOK, resp)
}
