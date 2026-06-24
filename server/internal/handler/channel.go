package handler

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var channelSlugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// ---------- response shapes ----------

type ChannelResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description string  `json:"description"`
	AccessMode  string  `json:"access_mode"`
	IsLocked    bool    `json:"is_locked"`
	IsArchived  bool    `json:"is_archived"`
	CreatedBy   *string `json:"created_by"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	// Membership context for the requesting user (only set on list).
	IsMember       bool    `json:"is_member"`
	MemberRole     *string `json:"member_role,omitempty"`
	HasUnread      bool    `json:"has_unread"`
	// First top-level message newer than the member's last_read_at. Drives the
	// "jump to last read" landing point on channel entry. Nil when not a member
	// or fully read.
	FirstUnreadMessageID *string `json:"first_unread_message_id,omitempty"`
	LastActivityAt       string  `json:"last_activity_at,omitempty"`
	// Group context (only set on list).
	GroupID       *string `json:"group_id,omitempty"`
	GroupName     *string `json:"group_name,omitempty"`
	GroupPosition float64 `json:"group_position"`
	Position      float64 `json:"position"`
}

type ChannelMemberResponse struct {
	ChannelID  string  `json:"channel_id"`
	UserID     string  `json:"user_id"`
	Role       string  `json:"role"`
	LastReadAt string  `json:"last_read_at"`
	JoinedAt   string  `json:"joined_at"`
	UserName   string  `json:"user_name"`
	UserEmail  string  `json:"user_email"`
	UserAvatar *string `json:"user_avatar_url"`
}

func channelToResponse(c db.Channel) ChannelResponse {
	return ChannelResponse{
		ID:          uuidToString(c.ID),
		WorkspaceID: uuidToString(c.WorkspaceID),
		Name:        c.Name,
		Slug:        c.Slug,
		Description: c.Description,
		AccessMode:  c.AccessMode,
		IsLocked:    c.IsLocked,
		IsArchived:  c.IsArchived,
		CreatedBy:   uuidToPtr(c.CreatedBy),
		CreatedAt:   timestampToString(c.CreatedAt),
		UpdatedAt:   timestampToString(c.UpdatedAt),
	}
}

func channelListRowToResponse(c db.ListChannelsRow) ChannelResponse {
	resp := ChannelResponse{
		ID:             uuidToString(c.ID),
		WorkspaceID:    uuidToString(c.WorkspaceID),
		Name:           c.Name,
		Slug:           c.Slug,
		Description:    c.Description,
		AccessMode:     c.AccessMode,
		IsLocked:       c.IsLocked,
		IsArchived:     c.IsArchived,
		CreatedBy:      uuidToPtr(c.CreatedBy),
		CreatedAt:      timestampToString(c.CreatedAt),
		UpdatedAt:      timestampToString(c.UpdatedAt),
		IsMember:            c.IsMember,
		MemberRole:          textToPtr(c.MemberRole),
		HasUnread:           c.HasUnread,
		FirstUnreadMessageID: uuidToPtr(c.FirstUnreadMessageID),
		LastActivityAt:      timestampToString(c.LastActivityAt),
		GroupID:        uuidToPtr(c.GroupID),
		GroupName:      textToPtr(c.GroupName),
		GroupPosition:  c.GroupPosition,
		Position:       c.Position,
	}
	return resp
}

func channelMemberRowToResponse(m db.ListChannelMembersRow) ChannelMemberResponse {
	return ChannelMemberResponse{
		ChannelID:  uuidToString(m.ChannelID),
		UserID:     uuidToString(m.UserID),
		Role:       m.Role,
		LastReadAt: timestampToString(m.LastReadAt),
		JoinedAt:   timestampToString(m.JoinedAt),
		UserName:   m.UserName,
		UserEmail:  m.UserEmail,
		UserAvatar: textToPtr(m.UserAvatarUrl),
	}
}

func channelSlugFromName(name string) string {
	slug := strings.ToLower(strings.TrimSpace(name))
	slug = channelSlugNonAlnum.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return "channel"
	}
	return slug
}

func channelSlugWithSuffix(base string, attempt int) string {
	if attempt <= 1 {
		return base
	}
	return base + "-" + strconv.Itoa(attempt)
}

// ---------- access control ----------

// channelContext bundles a channel with the requesting user's relationship to it.
type channelContext struct {
	channel     db.Channel
	member      *db.ChannelMember // channel-level membership, nil if not a member
	wsAdmin     bool              // workspace owner/admin
	channelOwn  bool              // channel-level owner
}

// loadChannelContext resolves the channel and the requesting user's access.
// It writes a 4xx and returns ok=false only when the channel is missing.
// Visibility is NOT gated here — invite-only channels are visible to all
// workspace members (channel + messages); posting/management is gated
// per-handler via canPost() / canManage().
func (h *Handler) loadChannelContext(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, channelID string) (channelContext, bool) {
	cUUID, ok := parseUUIDOrBadRequest(w, channelID, "channel id")
	if !ok {
		return channelContext{}, false
	}
	channel, err := h.Queries.GetChannel(r.Context(), db.GetChannelParams{ID: cUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "channel not found")
		return channelContext{}, false
	}

	ctx := channelContext{channel: channel}
	if m, ok := ctxMember(r.Context()); ok && roleAllowed(m.Role, "owner", "admin") {
		ctx.wsAdmin = true
	}
	if uid := requestUserID(r); uid != "" {
		if uUUID, err := parseUUIDErr(uid); err == nil {
			if cm, err := h.Queries.GetChannelMember(r.Context(), db.GetChannelMemberParams{ChannelID: cUUID, UserID: uUUID}); err == nil {
				ctx.member = &cm
				ctx.channelOwn = cm.Role == "owner"
			}
		}
	}

	return ctx, true
}

// canManage reports whether the user may edit channel settings / membership.
func (c channelContext) canManage() bool { return c.wsAdmin || c.channelOwn }

// canPost reports whether the user may post threads/messages. Locked channels
// restrict posting to managers. Open channels are workspace-wide — any
// workspace member may post (matching the frontend canPost gate and the
// open-channel notify semantics); invite-only channels require channel
// membership. Reaching this handler already proves workspace membership.
func (c channelContext) canPost() bool {
	if c.channel.IsLocked {
		return c.canManage()
	}
	if c.channel.AccessMode == "open" {
		return true
	}
	return c.member != nil || c.wsAdmin
}

func parseUUIDErr(s string) (pgtype.UUID, error) {
	return util.ParseUUID(s)
}

// ---------- handlers ----------

func (h *Handler) ListChannels(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	uUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	channels, err := h.Queries.ListChannels(r.Context(), db.ListChannelsParams{WorkspaceID: wsUUID, UserID: uUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list channels")
		return
	}
	resp := make([]ChannelResponse, len(channels))
	for i, c := range channels {
		resp[i] = channelListRowToResponse(c)
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": resp, "total": len(resp)})
}

func (h *Handler) GetChannel(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	cctx, ok := h.loadChannelContext(w, r, wsUUID, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	resp := channelToResponse(cctx.channel)
	resp.IsMember = cctx.member != nil
	if cctx.member != nil {
		resp.MemberRole = &cctx.member.Role
	}
	writeJSON(w, http.StatusOK, resp)
}

type CreateChannelRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	AccessMode  *string `json:"access_mode"`
}

func (h *Handler) CreateChannel(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	var req CreateChannelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	accessMode := "open"
	if req.AccessMode != nil {
		switch *req.AccessMode {
		case "open", "invite":
			accessMode = *req.AccessMode
		default:
			writeError(w, http.StatusBadRequest, "access_mode must be 'open' or 'invite'")
			return
		}
	}
	var description any
	if req.Description != nil {
		description = *req.Description
	}

	baseSlug := channelSlugFromName(name)
	var channel db.Channel
	var err error
	for attempt := 1; attempt <= 20; attempt++ {
		channel, err = h.Queries.CreateChannel(r.Context(), db.CreateChannelParams{
			WorkspaceID: wsUUID,
			Name:        name,
			Slug:        channelSlugWithSuffix(baseSlug, attempt),
			Description: description,
			AccessMode:  accessMode,
			CreatedBy:   member.UserID,
		})
		if err == nil {
			break
		}
		if !isUniqueViolation(err) {
			writeError(w, http.StatusInternalServerError, "failed to create channel")
			return
		}
	}
	if err != nil {
		writeError(w, http.StatusConflict, "channel slug already exists")
		return
	}

	// Creator becomes the channel owner.
	if _, err := h.Queries.UpsertChannelMember(r.Context(), db.UpsertChannelMemberParams{
		ChannelID: channel.ID,
		UserID:    member.UserID,
		Role:      "owner",
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add channel owner")
		return
	}

	resp := channelToResponse(channel)
	resp.IsMember = true
	ownerRole := "owner"
	resp.MemberRole = &ownerRole
	h.publish(protocol.EventChannelCreated, workspaceID, "member", requestUserID(r), map[string]any{"channel": resp})
	writeJSON(w, http.StatusCreated, resp)
}

type UpdateChannelRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	AccessMode  *string `json:"access_mode"`
	IsLocked    *bool   `json:"is_locked"`
	IsArchived  *bool   `json:"is_archived"`
}

func (h *Handler) UpdateChannel(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	cctx, ok := h.loadChannelContext(w, r, wsUUID, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	var req UpdateChannelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// is_locked is an admin-only control; other fields require channel management.
	if req.IsLocked != nil && !cctx.wsAdmin {
		writeError(w, http.StatusForbidden, "only workspace admins can lock a channel")
		return
	}
	if (req.Name != nil || req.Description != nil || req.AccessMode != nil || req.IsArchived != nil) && !cctx.canManage() {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	params := db.UpdateChannelParams{ID: cctx.channel.ID, WorkspaceID: wsUUID}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		params.Name = pgtype.Text{String: name, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.AccessMode != nil {
		if *req.AccessMode != "open" && *req.AccessMode != "invite" {
			writeError(w, http.StatusBadRequest, "access_mode must be 'open' or 'invite'")
			return
		}
		params.AccessMode = pgtype.Text{String: *req.AccessMode, Valid: true}
	}
	if req.IsLocked != nil {
		params.IsLocked = pgtype.Bool{Bool: *req.IsLocked, Valid: true}
	}
	if req.IsArchived != nil {
		params.IsArchived = pgtype.Bool{Bool: *req.IsArchived, Valid: true}
	}

	channel, err := h.Queries.UpdateChannel(r.Context(), params)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "channel not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update channel")
		return
	}
	resp := channelToResponse(channel)
	h.publish(protocol.EventChannelUpdated, workspaceID, "member", requestUserID(r), map[string]any{"channel": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteChannel(w http.ResponseWriter, r *http.Request) {
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
	if err := h.Queries.DeleteChannel(r.Context(), db.DeleteChannelParams{ID: cctx.channel.ID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete channel")
		return
	}
	h.publish(protocol.EventChannelDeleted, workspaceID, "member", requestUserID(r), map[string]any{"channel_id": uuidToString(cctx.channel.ID)})
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "channel_id": uuidToString(cctx.channel.ID)})
}

// ---------- membership ----------

func (h *Handler) ListChannelMembers(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	cctx, ok := h.loadChannelContext(w, r, wsUUID, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	members, err := h.Queries.ListChannelMembers(r.Context(), cctx.channel.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list channel members")
		return
	}
	resp := make([]ChannelMemberResponse, len(members))
	for i, m := range members {
		resp[i] = channelMemberRowToResponse(m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": resp, "total": len(resp)})
}

// JoinChannel lets a member self-join an open channel.
func (h *Handler) JoinChannel(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	cctx, ok := h.loadChannelContext(w, r, wsUUID, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if cctx.channel.AccessMode != "open" && !cctx.wsAdmin {
		writeError(w, http.StatusForbidden, "this channel is invite-only")
		return
	}
	cm, err := h.Queries.UpsertChannelMember(r.Context(), db.UpsertChannelMemberParams{
		ChannelID: cctx.channel.ID,
		UserID:    member.UserID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to join channel")
		return
	}
	h.publish(protocol.EventChannelMemberAdded, workspaceID, "member", requestUserID(r), map[string]any{
		"channel_id": uuidToString(cctx.channel.ID),
		"user_id":    uuidToString(cm.UserID),
	})
	writeJSON(w, http.StatusOK, map[string]any{"channel_id": uuidToString(cctx.channel.ID), "user_id": uuidToString(cm.UserID), "role": cm.Role})
}

type AddChannelMemberRequest struct {
	UserID string  `json:"user_id"`
	Role   *string `json:"role"`
}

// AddChannelMember lets a channel manager invite another workspace member.
func (h *Handler) AddChannelMember(w http.ResponseWriter, r *http.Request) {
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
	var req AddChannelMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	targetUUID, ok := parseUUIDOrBadRequest(w, req.UserID, "user_id")
	if !ok {
		return
	}
	// Ensure the target is a member of the workspace.
	if _, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{WorkspaceID: wsUUID, UserID: targetUUID}); err != nil {
		writeError(w, http.StatusBadRequest, "user is not a member of this workspace")
		return
	}
	role := any(nil)
	if req.Role != nil {
		if *req.Role != "owner" && *req.Role != "member" {
			writeError(w, http.StatusBadRequest, "role must be 'owner' or 'member'")
			return
		}
		role = *req.Role
	}
	cm, err := h.Queries.UpsertChannelMember(r.Context(), db.UpsertChannelMemberParams{
		ChannelID: cctx.channel.ID,
		UserID:    targetUUID,
		Role:      role,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add channel member")
		return
	}
	h.publish(protocol.EventChannelMemberAdded, workspaceID, "member", requestUserID(r), map[string]any{
		"channel_id": uuidToString(cctx.channel.ID),
		"user_id":    uuidToString(cm.UserID),
	})
	writeJSON(w, http.StatusCreated, map[string]any{"channel_id": uuidToString(cctx.channel.ID), "user_id": uuidToString(cm.UserID), "role": cm.Role})
}

// LeaveChannel removes the requesting user from the channel.
func (h *Handler) LeaveChannel(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id"); !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	cUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "channel id")
	if !ok {
		return
	}
	if err := h.Queries.RemoveChannelMember(r.Context(), db.RemoveChannelMemberParams{ChannelID: cUUID, UserID: member.UserID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to leave channel")
		return
	}
	h.publish(protocol.EventChannelMemberLeft, workspaceID, "member", requestUserID(r), map[string]any{
		"channel_id": uuidToString(cUUID),
		"user_id":    uuidToString(member.UserID),
	})
	writeJSON(w, http.StatusOK, map[string]any{"left": true, "channel_id": uuidToString(cUUID)})
}

// MarkChannelRead clears the unread indicator for the requesting user.
func (h *Handler) MarkChannelRead(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	cUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "channel id")
	if !ok {
		return
	}
	// Use wsUUID only to ensure the channel exists in this workspace.
	if _, err := h.Queries.GetChannel(r.Context(), db.GetChannelParams{ID: cUUID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	if err := h.Queries.MarkChannelRead(r.Context(), db.MarkChannelReadParams{ChannelID: cUUID, UserID: member.UserID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark channel read")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"read": true, "channel_id": uuidToString(cUUID)})
}
