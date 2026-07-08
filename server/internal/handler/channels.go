package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"multica/server/internal/db"
	"multica/server/internal/util"
)

type ChannelResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IsPrivate   bool   `json:"is_private"`
	CreatedBy   string `json:"created_by"`
	CreatedAt   string `json:"created_at"`
}

func channelToResponse(c db.Channel) ChannelResponse {
	var createdBy string
	if c.CreatedBy.Valid {
		createdBy = util.UUIDToString(c.CreatedBy)
	}
	return ChannelResponse{
		ID:          util.UUIDToString(c.ID),
		WorkspaceID: util.UUIDToString(c.WorkspaceID),
		Name:        c.Name,
		Description: c.Description.String,
		IsPrivate:   c.IsPrivate,
		CreatedBy:   createdBy,
		CreatedAt:   util.TimestampToString(c.CreatedAt),
	}
}

func (h *Handler) ListChannels(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}

	channels, err := h.Queries.ListChannels(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list channels")
		return
	}

	resp := make([]ChannelResponse, len(channels))
	for i, c := range channels {
		resp[i] = channelToResponse(c)
	}
	writeJSON(w, http.StatusOK, resp)
}

type CreateChannelRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	IsPrivate   bool    `json:"is_private"`
}

func (h *Handler) CreateChannel(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, util.UUIDToString(wsUUID))
	if !ok {
		return
	}

	var req CreateChannelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	c, err := h.Queries.CreateChannel(r.Context(), db.CreateChannelParams{
		WorkspaceID: wsUUID,
		Name:        req.Name,
		Description: ptrToText(req.Description),
		IsPrivate:   req.IsPrivate,
		CreatedBy:   member.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create channel")
		return
	}

	// Creator auto-joins
	_, _ = h.Queries.AddChannelMember(r.Context(), db.AddChannelMemberParams{
		ChannelID: c.ID,
		MemberID:  member.ID,
	})

	writeJSON(w, http.StatusCreated, channelToResponse(c))
}

func (h *Handler) GetChannel(w http.ResponseWriter, r *http.Request) {
	channelID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "channelId"), "channel id")
	if !ok {
		return
	}

	c, err := h.Queries.GetChannel(r.Context(), channelID)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "channel not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to get channel")
		}
		return
	}

	writeJSON(w, http.StatusOK, channelToResponse(c))
}

type UpdateChannelRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	IsPrivate   *bool   `json:"is_private"`
}

func (h *Handler) UpdateChannel(w http.ResponseWriter, r *http.Request) {
	channelID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "channelId"), "channel id")
	if !ok {
		return
	}
	// TODO check if user is a member of channel if private? For now allow any workspace member to update.

	var req UpdateChannelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	var isPrivate pgtype.Bool
	if req.IsPrivate != nil {
		isPrivate.Valid = true
		isPrivate.Bool = *req.IsPrivate
	}

	c, err := h.Queries.UpdateChannel(r.Context(), db.UpdateChannelParams{
		ID:          channelID,
		Name:        ptrToText(req.Name),
		Description: ptrToText(req.Description),
		IsPrivate:   isPrivate,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "channel not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to update channel")
		}
		return
	}

	writeJSON(w, http.StatusOK, channelToResponse(c))
}

func (h *Handler) DeleteChannel(w http.ResponseWriter, r *http.Request) {
	channelID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "channelId"), "channel id")
	if !ok {
		return
	}
	// Note: normally we'd check if user is creator or admin.

	if err := h.Queries.DeleteChannel(r.Context(), channelID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete channel")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// Channel Members

type ChannelMemberResponse struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Role      string `json:"role"`
	JoinedAt  string `json:"joined_at"`
}

func (h *Handler) ListChannelMembers(w http.ResponseWriter, r *http.Request) {
	channelID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "channelId"), "channel id")
	if !ok {
		return
	}

	members, err := h.Queries.ListChannelMembers(r.Context(), channelID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list members")
		return
	}

	resp := make([]ChannelMemberResponse, len(members))
	for i, m := range members {
		resp[i] = ChannelMemberResponse{
			ID:       util.UUIDToString(m.ID),
			UserID:   util.UUIDToString(m.UserID),
			Role:     m.Role,
			JoinedAt: util.TimestampToString(m.JoinedAt),
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) AddChannelMember(w http.ResponseWriter, r *http.Request) {
	channelID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "channelId"), "channel id")
	if !ok {
		return
	}
	memberID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "memberId"), "member id")
	if !ok {
		return
	}

	cm, err := h.Queries.AddChannelMember(r.Context(), db.AddChannelMemberParams{
		ChannelID: channelID,
		MemberID:  memberID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add member")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"channel_id": util.UUIDToString(cm.ChannelID),
		"member_id":  util.UUIDToString(cm.MemberID),
		"joined_at":  util.TimestampToString(cm.JoinedAt),
	})
}

func (h *Handler) RemoveChannelMember(w http.ResponseWriter, r *http.Request) {
	channelID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "channelId"), "channel id")
	if !ok {
		return
	}
	memberID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "memberId"), "member id")
	if !ok {
		return
	}

	if err := h.Queries.RemoveChannelMember(r.Context(), db.RemoveChannelMemberParams{
		ChannelID: channelID,
		MemberID:  memberID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove member")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// Channel Messages

type ChannelMessageResponse struct {
	ID        string  `json:"id"`
	ChannelID string  `json:"channel_id"`
	AuthorID  string  `json:"author_id"`
	Content   string  `json:"content"`
	ParentID  *string `json:"parent_id,omitempty"`
	EditedAt  *string `json:"edited_at,omitempty"`
	CreatedAt string  `json:"created_at"`
}

func channelMessageToResponse(m db.ChannelMessage) ChannelMessageResponse {
	var parentID *string
	if m.ParentID.Valid {
		s := util.UUIDToString(m.ParentID)
		parentID = &s
	}
	var editedAt *string
	if m.EditedAt.Valid {
		s := util.TimestampToString(m.EditedAt)
		editedAt = &s
	}
	return ChannelMessageResponse{
		ID:        util.UUIDToString(m.ID),
		ChannelID: util.UUIDToString(m.ChannelID),
		AuthorID:  util.UUIDToString(m.AuthorID),
		Content:   m.Content,
		ParentID:  parentID,
		EditedAt:  editedAt,
		CreatedAt: util.TimestampToString(m.CreatedAt),
	}
}

func (h *Handler) ListChannelMessages(w http.ResponseWriter, r *http.Request) {
	channelID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "channelId"), "channel id")
	if !ok {
		return
	}

	messages, err := h.Queries.ListChannelMessages(r.Context(), db.ListChannelMessagesParams{
		ChannelID: channelID,
		Limit:     50,
		Offset:    0,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list messages")
		return
	}

	resp := make([]ChannelMessageResponse, len(messages))
	for i, m := range messages {
		resp[i] = channelMessageToResponse(m)
	}
	writeJSON(w, http.StatusOK, resp)
}

type CreateChannelMessageRequest struct {
	Content  string  `json:"content"`
	ParentID *string `json:"parent_id"`
}

func (h *Handler) CreateChannelMessage(w http.ResponseWriter, r *http.Request) {
	channelID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "channelId"), "channel id")
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, h.resolveWorkspaceID(r))
	if !ok {
		return
	}

	var req CreateChannelMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	var parentID pgtype.UUID
	if req.ParentID != nil {
		parentID, ok = parseUUIDOrBadRequest(w, *req.ParentID, "parent id")
		if !ok {
			return
		}
	}

	m, err := h.Queries.CreateChannelMessage(r.Context(), db.CreateChannelMessageParams{
		ChannelID: channelID,
		AuthorID:  member.ID,
		Content:   req.Content,
		ParentID:  parentID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create message")
		return
	}

	resp := channelMessageToResponse(m)

	// Dispatch real-time event
	h.publish("channel_message_created", h.resolveWorkspaceID(r), "member", util.UUIDToString(member.UserID), resp)

	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) UpdateChannelMessage(w http.ResponseWriter, r *http.Request) {
	messageID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "messageId"), "message id")
	if !ok {
		return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	m, err := h.Queries.UpdateChannelMessage(r.Context(), db.UpdateChannelMessageParams{
		ID:      messageID,
		Content: ptrToText(&req.Content),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update message")
		return
	}

	resp := channelMessageToResponse(m)
	h.publish("channel_message_updated", h.resolveWorkspaceID(r), "member", util.UUIDToString(m.AuthorID), resp)

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteChannelMessage(w http.ResponseWriter, r *http.Request) {
	messageID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "messageId"), "message id")
	if !ok {
		return
	}

	if err := h.Queries.DeleteChannelMessage(r.Context(), messageID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete message")
		return
	}

	h.publish("channel_message_deleted", h.resolveWorkspaceID(r), "member", "", map[string]string{"id": util.UUIDToString(messageID)})

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
