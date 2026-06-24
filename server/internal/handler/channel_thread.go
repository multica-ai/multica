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

// ---------- response shapes ----------

type ChannelThreadResponse struct {
	ID            string  `json:"id"`
	ChannelID     string  `json:"channel_id"`
	WorkspaceID   string  `json:"workspace_id"`
	Title         string  `json:"title"`
	CreatedBy     *string `json:"created_by"`
	CreatorName   *string `json:"creator_name,omitempty"`
	CreatorAvatar *string `json:"creator_avatar_url,omitempty"`
	MessageCount  int32   `json:"message_count"`
	IssueCount    int64   `json:"issue_count"`
	LastMessageAt string  `json:"last_message_at"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

type ChannelMessageResponse struct {
	ID          string  `json:"id"`
	ThreadID    string  `json:"thread_id"`
	ChannelID   string  `json:"channel_id"`
	WorkspaceID string  `json:"workspace_id"`
	AuthorType  string  `json:"author_type"`
	AuthorID    *string `json:"author_id"`
	Content     string  `json:"content"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func channelThreadToResponse(t db.ChannelThread) ChannelThreadResponse {
	return ChannelThreadResponse{
		ID:            uuidToString(t.ID),
		ChannelID:     uuidToString(t.ChannelID),
		WorkspaceID:   uuidToString(t.WorkspaceID),
		Title:         t.Title,
		CreatedBy:     uuidToPtr(t.CreatedBy),
		MessageCount:  t.MessageCount,
		LastMessageAt: timestampToString(t.LastMessageAt),
		CreatedAt:     timestampToString(t.CreatedAt),
		UpdatedAt:     timestampToString(t.UpdatedAt),
	}
}

func channelThreadRowToResponse(t db.ListChannelThreadsRow) ChannelThreadResponse {
	return ChannelThreadResponse{
		ID:            uuidToString(t.ID),
		ChannelID:     uuidToString(t.ChannelID),
		WorkspaceID:   uuidToString(t.WorkspaceID),
		Title:         t.Title,
		CreatedBy:     uuidToPtr(t.CreatedBy),
		CreatorName:   textToPtr(t.CreatorName),
		CreatorAvatar: textToPtr(t.CreatorAvatarUrl),
		MessageCount:  t.MessageCount,
		IssueCount:    t.IssueCount,
		LastMessageAt: timestampToString(t.LastMessageAt),
		CreatedAt:     timestampToString(t.CreatedAt),
		UpdatedAt:     timestampToString(t.UpdatedAt),
	}
}

func channelMessageToResponse(m db.ChannelMessage) ChannelMessageResponse {
	return ChannelMessageResponse{
		ID:          uuidToString(m.ID),
		ThreadID:    uuidToString(m.ThreadID),
		ChannelID:   uuidToString(m.ChannelID),
		WorkspaceID: uuidToString(m.WorkspaceID),
		AuthorType:  m.AuthorType,
		AuthorID:    uuidToPtr(m.AuthorID),
		Content:     m.Content,
		CreatedAt:   timestampToString(m.CreatedAt),
		UpdatedAt:   timestampToString(m.UpdatedAt),
	}
}

// ---------- thread handlers ----------

func (h *Handler) ListChannelThreads(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	cctx, ok := h.loadChannelContext(w, r, wsUUID, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	threads, err := h.Queries.ListChannelThreads(r.Context(), cctx.channel.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list threads")
		return
	}
	resp := make([]ChannelThreadResponse, len(threads))
	for i, t := range threads {
		resp[i] = channelThreadRowToResponse(t)
	}
	writeJSON(w, http.StatusOK, map[string]any{"threads": resp, "total": len(resp)})
}

type CreateChannelThreadRequest struct {
	Title   string  `json:"title"`
	Content *string `json:"content"`
}

// CreateChannelThread opens a new thread and optionally posts its first message.
func (h *Handler) CreateChannelThread(w http.ResponseWriter, r *http.Request) {
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

	var req CreateChannelThreadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	title := strings.TrimSpace(req.Title)
	content := ""
	if req.Content != nil {
		content = strings.TrimSpace(*req.Content)
	}
	if title == "" && content == "" {
		writeError(w, http.StatusBadRequest, "title or content is required")
		return
	}

	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	var createdBy pgtype.UUID
	if actorType == "member" && actorID != "" {
		if uid, err := parseUUIDErr(actorID); err == nil {
			createdBy = uid
		}
	}

	thread, err := h.Queries.CreateChannelThread(r.Context(), db.CreateChannelThreadParams{
		ChannelID:   cctx.channel.ID,
		WorkspaceID: wsUUID,
		Title:       title,
		CreatedBy:   createdBy,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create thread")
		return
	}

	resp := channelThreadToResponse(thread)
	h.publish(protocol.EventChannelThreadCreated, workspaceID, actorType, actorID, map[string]any{"thread": resp})
	h.Queries.TouchChannel(r.Context(), cctx.channel.ID)

	// Post the opening message if content was provided.
	if content != "" {
		if msg, ok := h.insertChannelMessage(r, cctx.channel, thread, actorType, actorID, content); ok {
			writeJSON(w, http.StatusCreated, map[string]any{"thread": resp, "message": channelMessageToResponse(msg)})
			return
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"thread": resp})
}

func (h *Handler) DeleteChannelThread(w http.ResponseWriter, r *http.Request) {
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
	if !cctx.canManage() && uuidToString(thread.CreatedBy) != requestUserID(r) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	if err := h.Queries.DeleteChannelThread(r.Context(), thread.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete thread")
		return
	}
	h.publish(protocol.EventChannelThreadDeleted, workspaceID, "member", requestUserID(r), map[string]any{
		"thread_id":  uuidToString(thread.ID),
		"channel_id": uuidToString(cctx.channel.ID),
	})
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "thread_id": uuidToString(thread.ID)})
}

// ---------- message handlers ----------

func (h *Handler) ListThreadMessages(w http.ResponseWriter, r *http.Request) {
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
	messages, err := h.Queries.ListThreadMessages(r.Context(), thread.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list messages")
		return
	}
	resp := make([]ChannelMessageResponse, len(messages))
	for i, m := range messages {
		resp[i] = channelMessageToResponse(m)
	}
	issues, _ := h.Queries.ListThreadIssues(r.Context(), thread.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"thread":   channelThreadToResponse(thread),
		"messages": resp,
		"issues":   threadIssuesToResponse(issues, h.getIssuePrefix(r.Context(), wsUUID)),
	})
}

type CreateChannelMessageRequest struct {
	Content string `json:"content"`
}

func (h *Handler) CreateChannelMessage(w http.ResponseWriter, r *http.Request) {
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
	thread, ok := h.loadThreadInChannel(w, r, cctx.channel.ID)
	if !ok {
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
	msg, ok := h.insertChannelMessage(r, cctx.channel, thread, actorType, actorID, content)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to post message")
		return
	}
	writeJSON(w, http.StatusCreated, channelMessageToResponse(msg))
}

// insertChannelMessage persists a message, bumps the thread + channel activity,
// and broadcasts the realtime event. Returns ok=false on persistence failure.
func (h *Handler) insertChannelMessage(r *http.Request, channel db.Channel, thread db.ChannelThread, actorType, actorID, content string) (db.ChannelMessage, bool) {
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
	msg, err := h.Queries.CreateChannelMessage(r.Context(), db.CreateChannelMessageParams{
		ThreadID:    thread.ID,
		ChannelID:   channel.ID,
		WorkspaceID: channel.WorkspaceID,
		AuthorType:  authorType,
		AuthorID:    authorID,
		Content:     content,
	})
	if err != nil {
		return db.ChannelMessage{}, false
	}
	h.Queries.BumpChannelThread(r.Context(), thread.ID)
	h.Queries.TouchChannel(r.Context(), channel.ID)
	h.publish(protocol.EventChannelMessageCreated, uuidToString(channel.WorkspaceID), actorType, actorID, map[string]any{
		"message":    channelMessageToResponse(msg),
		"channel_id": uuidToString(channel.ID),
		"thread_id":  uuidToString(thread.ID),
	})
	h.processChannelMessageMentions(r.Context(), channel, msg, actorType, actorID)
	return msg, true
}

// ---------- helpers ----------

// loadThreadInChannel loads the thread referenced by the {threadId} URL param
// and verifies it belongs to the given channel.
func (h *Handler) loadThreadInChannel(w http.ResponseWriter, r *http.Request, channelID pgtype.UUID) (db.ChannelThread, bool) {
	tUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "threadId"), "thread id")
	if !ok {
		return db.ChannelThread{}, false
	}
	thread, err := h.Queries.GetChannelThread(r.Context(), tUUID)
	if err != nil || uuidToString(thread.ChannelID) != uuidToString(channelID) {
		writeError(w, http.StatusNotFound, "thread not found")
		return db.ChannelThread{}, false
	}
	return thread, true
}

type threadIssueResponse struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Number     int32  `json:"number"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	Priority   string `json:"priority"`
}

func threadIssuesToResponse(rows []db.ListThreadIssuesRow, issuePrefix string) []threadIssueResponse {
	out := make([]threadIssueResponse, len(rows))
	for i, row := range rows {
		out[i] = threadIssueResponse{
			ID:         uuidToString(row.ID),
			Identifier: issuePrefix + "-" + strconv.Itoa(int(row.Number)),
			Number:     row.Number,
			Title:      row.Title,
			Status:     row.Status,
			Priority:   row.Priority,
		}
	}
	return out
}
